package gogen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/dave/jennifer/jen"
	"github.com/norunners/vue/internal/compiler/script"
	"github.com/norunners/vue/internal/compiler/sfc"
	templateparser "github.com/norunners/vue/internal/compiler/template"
)

const (
	cacheDirName = ".tue-cache"
	manifestName = "manifest.json"
)

type File struct {
	Path     string
	Template *templateparser.Tree
	Contract *script.Contract
	Script   string
}

type Project struct {
	Files    []GeneratedFile
	Manifest Manifest
}

type GeneratedFile struct {
	Path   string
	Source []byte
}

type Manifest struct {
	Version    int                 `json:"version"`
	Components []ManifestComponent `json:"components"`
}

type ManifestComponent struct {
	Source       string         `json:"source"`
	Package      string         `json:"package"`
	Component    string         `json:"component"`
	Constructor  string         `json:"constructor"`
	Generated    []string       `json:"generated"`
	TemplateSpan ManifestSpan   `json:"templateSpan"`
	Nodes        []ManifestNode `json:"nodes"`
}

type ManifestNode struct {
	ID   string       `json:"id"`
	Kind string       `json:"kind"`
	Name string       `json:"name,omitempty"`
	Span ManifestSpan `json:"span"`
}

type ManifestSpan struct {
	Start ManifestPosition `json:"start"`
	End   ManifestPosition `json:"end"`
}

type ManifestPosition struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

type Error struct {
	Path    string
	Message string
	Span    sfc.Span
}

func (e *Error) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return sfc.Diagnostic{
		Path:    e.Path,
		Message: e.Message,
		Span:    e.Span,
	}.String()
}

func GenerateProject(files []File) (*Project, error) {
	project := &Project{
		Manifest: Manifest{Version: 1},
	}

	for _, file := range files {
		generator := newGenerator(file)
		generated, component, err := generator.generate()
		if err != nil {
			return nil, err
		}
		project.Files = append(project.Files, generated...)
		project.Manifest.Components = append(project.Manifest.Components, component)
	}

	sort.Slice(project.Files, func(i, j int) bool {
		return project.Files[i].Path < project.Files[j].Path
	})
	sort.Slice(project.Manifest.Components, func(i, j int) bool {
		return project.Manifest.Components[i].Source < project.Manifest.Components[j].Source
	})

	manifest, err := renderManifest(project.Manifest)
	if err != nil {
		return nil, err
	}
	project.Files = append(project.Files, GeneratedFile{
		Path:   manifestName,
		Source: manifest,
	})

	return project, nil
}

func WriteProject(root string, project *Project) error {
	if project == nil {
		return fmt.Errorf("write generated project: nil project")
	}

	cacheRoot := filepath.Join(root, cacheDirName)
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	for _, file := range project.Files {
		path := filepath.Join(cacheRoot, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create generated dir %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, file.Source, 0o644); err != nil {
			return fmt.Errorf("write generated file %s: %w", path, err)
		}
	}
	return nil
}

type generator struct {
	file           File
	fieldKinds     map[string]script.FieldKind
	callables      map[string]callable
	constructor    string
	renderFunction string
	nodes          []ManifestNode
	nodeID         int
}

type callable struct {
	params  []string
	results []string
}

func newGenerator(file File) *generator {
	fieldKinds := make(map[string]script.FieldKind)
	callables := make(map[string]callable)
	if file.Contract != nil {
		for _, field := range file.Contract.Fields {
			fieldKinds[field.Name] = field.Kind
		}
		for _, callback := range file.Contract.Callbacks {
			callables[callback.FieldName] = callable{
				params:  callback.Params,
				results: callback.Results,
			}
		}
		for _, method := range file.Contract.Methods {
			callables[method.Name] = callable{
				params:  method.Params,
				results: method.Results,
			}
		}
	}

	componentName := ""
	if file.Contract != nil {
		componentName = file.Contract.ComponentName
	}

	return &generator{
		file:           file,
		fieldKinds:     fieldKinds,
		callables:      callables,
		constructor:    "New" + componentName,
		renderFunction: lowerFirst(componentName) + "Render",
	}
}

func (g *generator) generate() ([]GeneratedFile, ManifestComponent, error) {
	if g.file.Template == nil {
		return nil, ManifestComponent{}, fmt.Errorf("generate %s: nil template", g.file.Path)
	}
	if g.file.Contract == nil {
		return nil, ManifestComponent{}, fmt.Errorf("generate %s: nil contract", g.file.Path)
	}
	if g.file.Contract.PackageName == "" {
		return nil, ManifestComponent{}, fmt.Errorf("generate %s: missing package name", g.file.Path)
	}
	if g.file.Contract.ComponentName == "" {
		return nil, ManifestComponent{}, fmt.Errorf("generate %s: missing component name", g.file.Path)
	}

	componentRel := generatedRelPath(g.file.Path, "_tue.go")
	renderRel := generatedRelPath(g.file.Path, "_render_tue.go")

	componentFile, err := g.generateScriptFile()
	if err != nil {
		return nil, ManifestComponent{}, err
	}
	renderFile, err := g.generateRenderFile()
	if err != nil {
		return nil, ManifestComponent{}, err
	}

	generated := []GeneratedFile{
		{Path: componentRel, Source: componentFile},
		{Path: renderRel, Source: renderFile},
	}
	manifest := ManifestComponent{
		Source:       g.file.Path,
		Package:      g.file.Contract.PackageName,
		Component:    g.file.Contract.ComponentName,
		Constructor:  g.constructor,
		Generated:    []string{componentRel, renderRel},
		TemplateSpan: manifestSpan(g.file.Template.Span),
		Nodes:        g.nodes,
	}
	return generated, manifest, nil
}

func (g *generator) generateScriptFile() ([]byte, error) {
	if strings.TrimSpace(g.file.Script) == "" {
		return nil, fmt.Errorf("generate %s: missing script source", g.file.Path)
	}

	formatted, err := format.Source([]byte(g.file.Script))
	if err != nil {
		return nil, fmt.Errorf("format script for %s: %w", g.file.Path, err)
	}
	out := append([]byte("// Code generated by tue; DO NOT EDIT.\n\n"), formatted...)
	return out, nil
}

func (g *generator) generateRenderFile() ([]byte, error) {
	f := jen.NewFile(g.file.Contract.PackageName)
	f.HeaderComment("Code generated by tue; DO NOT EDIT.")

	root, err := g.renderNodes(g.file.Template.Nodes)
	if err != nil {
		return nil, err
	}

	f.Func().
		Id(g.constructor).
		Params().
		Op("*").
		Qual("github.com/norunners/vue", "Comp").
		Block(
			jen.Id("c").Op(":=").Op("&").Id(g.file.Contract.ComponentName).Values(),
			jen.Return(jen.Qual("github.com/norunners/vue", "CompOf").Call(
				jen.Id("c"),
				jen.Func().Params().Qual("github.com/norunners/vue", "VNode").Block(
					jen.Return(jen.Id(g.renderFunction).Call(jen.Id("c"))),
				),
			)),
		)

	f.Func().
		Id(g.renderFunction).
		Params(jen.Id("c").Op("*").Id(g.file.Contract.ComponentName)).
		Qual("github.com/norunners/vue", "VNode").
		Block(jen.Return(root))

	return renderFile(f)
}

func (g *generator) renderNodes(nodes []templateparser.Node) (jen.Code, error) {
	rendered := make([]jen.Code, 0, len(nodes))
	for _, node := range nodes {
		code, err := g.renderNode(node)
		if err != nil {
			return nil, err
		}
		if code != nil {
			rendered = append(rendered, code)
		}
	}
	switch len(rendered) {
	case 0:
		return jen.Qual("github.com/norunners/vue", "Fragment").Call(), nil
	default:
		return jen.Qual("github.com/norunners/vue", "Fragment").Call(rendered...), nil
	}
}

func (g *generator) renderNode(node templateparser.Node) (jen.Code, error) {
	switch node := node.(type) {
	case *templateparser.Element:
		return g.renderElement(node)
	case *templateparser.Text:
		return g.renderText(node.Content)
	case *templateparser.Interpolation:
		return g.renderInterpolation(node)
	case *templateparser.Comment:
		return nil, nil
	default:
		return nil, g.unsupported(node.NodeSpan(), "unsupported template node")
	}
}

func (g *generator) renderElement(element *templateparser.Element) (jen.Code, error) {
	if element.Component {
		return nil, g.unsupported(element.NameSpan, "component rendering is not supported in the static render slice")
	}

	id := g.recordNode("element", element.Name, element.Span)
	attrs := make([]jen.Code, 0, len(element.Attrs))
	events := make([]jen.Code, 0, len(element.Attrs))
	for _, attr := range element.Attrs {
		switch attr.Kind {
		case templateparser.AttrStatic:
			if attr.HasValue {
				attrs = append(attrs, jen.Qual("github.com/norunners/vue", "Attr").Call(
					jen.Lit(attr.Name),
					jen.Lit(attr.Value),
				))
				continue
			}
			attrs = append(attrs, jen.Qual("github.com/norunners/vue", "BoolAttr").Call(jen.Lit(attr.Name)))
		case templateparser.AttrEvent:
			event, err := g.renderEvent(attr)
			if err != nil {
				return nil, err
			}
			events = append(events, event)
		default:
			return nil, g.unsupported(attr.Span, fmt.Sprintf("%s attributes are not supported in this render slice", attr.Kind))
		}
	}

	children := make([]jen.Code, 0, len(element.Children))
	for _, child := range element.Children {
		rendered, err := g.renderNode(child)
		if err != nil {
			return nil, err
		}
		if rendered != nil {
			children = append(children, rendered)
		}
	}

	attrArg := jen.Nil()
	if len(attrs) > 0 {
		attrArg = jen.Index().Qual("github.com/norunners/vue", "Attribute").Values(attrs...)
	}
	args := []jen.Code{jen.Lit(element.Name), attrArg}
	if len(events) > 0 {
		eventArg := jen.Index().Qual("github.com/norunners/vue", "EventBinding").Values(events...)
		args = append(args, eventArg)
	}
	args = append(args, children...)

	elementCall := jen.Qual("github.com/norunners/vue", "Element").Call(args...)
	if len(events) > 0 {
		elementCall = jen.Qual("github.com/norunners/vue", "ElementWithEvents").Call(args...)
	}

	return jen.Commentf("tue source %s %s", g.file.Path, formatSpan(element.Span)).
		Line().
		Comment("tue node " + id).
		Line().
		Add(elementCall), nil
}

func (g *generator) renderEvent(attr templateparser.Attr) (jen.Code, error) {
	handler, err := g.eventHandler(attr.Expression, attr.ExpressionSpan)
	if err != nil {
		return nil, err
	}
	return jen.Qual("github.com/norunners/vue", "On").Call(
		jen.Lit(attr.Argument),
		handler,
	), nil
}

func (g *generator) eventHandler(source string, span sfc.Span) (jen.Code, error) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil, &Error{
			Path:    g.file.Path,
			Message: fmt.Sprintf("invalid generated event handler %q: %s", source, trimGoParseError(err.Error())),
			Span:    span,
		}
	}
	return g.eventHandlerCode(expr, span)
}

func (g *generator) eventHandlerCode(expr ast.Expr, span sfc.Span) (jen.Code, error) {
	switch expr := expr.(type) {
	case *ast.Ident:
		return g.callableCode(expr.Name, span)
	case *ast.CallExpr:
		if len(expr.Args) > 0 {
			return nil, g.unsupported(span, "event handler calls with arguments are not supported in this render slice")
		}
		target, err := g.eventHandlerCode(expr.Fun, span)
		if err != nil {
			return nil, err
		}
		return jen.Func().Params().Block(jen.Add(target).Call()), nil
	case *ast.ParenExpr:
		return g.eventHandlerCode(expr.X, span)
	default:
		return nil, g.unsupported(span, "event handler must be a method or function field")
	}
}

func (g *generator) callableCode(name string, span sfc.Span) (jen.Code, error) {
	callable, ok := g.callables[name]
	if !ok {
		if kind, exists := g.fieldKinds[name]; exists && kind != script.FieldCallback {
			return nil, g.unsupported(span, fmt.Sprintf("event handler %q is not callable", name))
		}
		return nil, g.unsupported(span, fmt.Sprintf("event handler %q does not exist", name))
	}
	if len(callable.params) > 0 || len(callable.results) > 0 {
		return nil, g.unsupported(span, fmt.Sprintf("event handler %q must have signature func()", name))
	}
	return jen.Id("c").Dot(name), nil
}

func (g *generator) renderText(content string) (jen.Code, error) {
	if content == "" {
		return nil, nil
	}
	return jen.Qual("github.com/norunners/vue", "Text").Call(jen.Lit(content)), nil
}

func (g *generator) renderInterpolation(interpolation *templateparser.Interpolation) (jen.Code, error) {
	expr, err := g.expression(interpolation.Expression, interpolation.ExpressionSpan)
	if err != nil {
		return nil, err
	}
	id := g.recordNode("interpolation", "", interpolation.Span)
	return jen.Commentf("tue source %s %s", g.file.Path, formatSpan(interpolation.ExpressionSpan)).
		Line().
		Comment("tue node "+id).
		Line().
		Qual("github.com/norunners/vue", "Text").Call(jen.Qual("fmt", "Sprint").Call(expr)), nil
}

func (g *generator) expression(source string, span sfc.Span) (jen.Code, error) {
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil, &Error{
			Path:    g.file.Path,
			Message: fmt.Sprintf("invalid generated expression %q: %s", source, trimGoParseError(err.Error())),
			Span:    span,
		}
	}
	return g.expressionCode(expr, span)
}

func (g *generator) expressionCode(expr ast.Expr, span sfc.Span) (jen.Code, error) {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		return literalCode(expr)
	case *ast.Ident:
		return g.identCode(expr.Name, span)
	case *ast.SelectorExpr:
		return g.selectorCode(expr, span)
	case *ast.ParenExpr:
		code, err := g.expressionCode(expr.X, span)
		if err != nil {
			return nil, err
		}
		return jen.Parens(code), nil
	case *ast.UnaryExpr:
		code, err := g.expressionCode(expr.X, span)
		if err != nil {
			return nil, err
		}
		return jen.Op(expr.Op.String()).Add(code), nil
	case *ast.BinaryExpr:
		left, err := g.expressionCode(expr.X, span)
		if err != nil {
			return nil, err
		}
		right, err := g.expressionCode(expr.Y, span)
		if err != nil {
			return nil, err
		}
		return jen.Add(left).Op(expr.Op.String()).Add(right), nil
	default:
		return nil, g.unsupported(span, fmt.Sprintf("unsupported interpolation expression %T", expr))
	}
}

func (g *generator) identCode(name string, span sfc.Span) (jen.Code, error) {
	switch name {
	case "true":
		return jen.True(), nil
	case "false":
		return jen.False(), nil
	case "nil":
		return jen.Nil(), nil
	}
	kind, ok := g.fieldKinds[name]
	if !ok {
		return nil, g.unsupported(span, fmt.Sprintf("unknown generated expression identifier %q", name))
	}
	return fieldAccess(kind, jen.Id("c").Dot(name)), nil
}

func (g *generator) selectorCode(expr *ast.SelectorExpr, span sfc.Span) (jen.Code, error) {
	base, err := g.expressionCode(expr.X, span)
	if err != nil {
		return nil, err
	}
	return jen.Add(base).Dot(expr.Sel.Name), nil
}

func fieldAccess(kind script.FieldKind, field jen.Code) jen.Code {
	switch kind {
	case script.FieldProp, script.FieldRef, script.FieldComputed:
		return jen.Add(field).Dot("Get").Call()
	default:
		return field
	}
}

func literalCode(lit *ast.BasicLit) (jen.Code, error) {
	switch lit.Kind {
	case token.STRING:
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return nil, err
		}
		return jen.Lit(value), nil
	case token.INT:
		value, err := strconv.ParseInt(lit.Value, 0, 64)
		if err == nil {
			return jen.Lit(value), nil
		}
		uintValue, uintErr := strconv.ParseUint(lit.Value, 0, 64)
		if uintErr != nil {
			return nil, err
		}
		return jen.Lit(uintValue), nil
	case token.FLOAT:
		value, err := strconv.ParseFloat(lit.Value, 64)
		if err != nil {
			return nil, err
		}
		return jen.Lit(value), nil
	case token.CHAR:
		value, _, _, err := strconv.UnquoteChar(strings.Trim(lit.Value, "'"), '\'')
		if err != nil {
			return nil, err
		}
		return jen.LitRune(value), nil
	default:
		return jen.Id(lit.Value), nil
	}
}

func renderFile(file *jen.File) ([]byte, error) {
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		return nil, fmt.Errorf("render generated Go: %w", err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated Go: %w", err)
	}
	return formatted, nil
}

func renderManifest(manifest Manifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render generated manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func (g *generator) unsupported(span sfc.Span, message string) error {
	return &Error{
		Path:    g.file.Path,
		Message: message,
		Span:    span,
	}
}

func (g *generator) recordNode(kind, name string, span sfc.Span) string {
	g.nodeID++
	id := fmt.Sprintf("n%d", g.nodeID)
	g.nodes = append(g.nodes, ManifestNode{
		ID:   id,
		Kind: kind,
		Name: name,
		Span: manifestSpan(span),
	})
	return id
}

func generatedRelPath(sourcePath, suffix string) string {
	dir := filepath.Dir(filepath.FromSlash(sourcePath))
	if dir == "." {
		dir = ""
	}
	stem := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	filename := sanitizeFilename(stem) + suffix
	return filepath.ToSlash(filepath.Join(dir, filename))
}

func sanitizeFilename(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "component"
	}
	return out
}

func lowerFirst(name string) string {
	if name == "" {
		return "component"
	}
	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func manifestSpan(span sfc.Span) ManifestSpan {
	return ManifestSpan{
		Start: manifestPosition(span.Start),
		End:   manifestPosition(span.End),
	}
}

func manifestPosition(position sfc.Position) ManifestPosition {
	return ManifestPosition{
		Offset: position.Offset,
		Line:   position.Line,
		Column: position.Column,
	}
}

func formatSpan(span sfc.Span) string {
	return fmt.Sprintf("%d:%d-%d:%d", span.Start.Line, span.Start.Column, span.End.Line, span.End.Column)
}

func trimGoParseError(message string) string {
	parts := strings.SplitN(message, ": ", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return message
}
