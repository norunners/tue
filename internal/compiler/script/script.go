package script

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/norunners/vue/internal/compiler/sfc"
)

const tueImportPath = "github.com/norunners/vue"

type Contract struct {
	Path          string
	PackageName   string
	ComponentName string
	Imports       []Import
	Fields        []Field
	Props         []Prop
	State         []Field
	Refs          []Field
	Computed      []Field
	Resources     []Field
	Callbacks     []Callback
	Methods       []Method
	Init          *InitMethod
	Allocation    Allocation
	Span          sfc.Span
}

type Import struct {
	Name string
	Path string
	Span sfc.Span
}

type FieldKind string

const (
	FieldState    FieldKind = "state"
	FieldProp     FieldKind = "prop"
	FieldRef      FieldKind = "ref"
	FieldComputed FieldKind = "computed"
	FieldResource FieldKind = "resource"
	FieldCallback FieldKind = "callback"
)

type Field struct {
	Name        string
	Exported    bool
	Kind        FieldKind
	Type        string
	ElementType string
	Tag         string
	Span        sfc.Span
	NameSpan    sfc.Span
	TypeSpan    sfc.Span
}

type Prop struct {
	FieldName string
	Name      string
	Type      string
	Required  bool
	Span      sfc.Span
}

type Callback struct {
	FieldName string
	Params    []string
	Results   []string
	Span      sfc.Span
}

type Method struct {
	Name    string
	Params  []string
	Results []string
	Span    sfc.Span
}

type InitMethod struct {
	ReceiverName string
	Span         sfc.Span
}

type Allocation struct {
	ComponentName string
	PropFields    []Prop
	HasInit       bool
}

type Diagnostic = sfc.Diagnostic

type Error struct {
	Diagnostics []Diagnostic
}

func (e *Error) Error() string {
	if len(e.Diagnostics) == 0 {
		return "parse script"
	}
	if len(e.Diagnostics) == 1 {
		return e.Diagnostics[0].String()
	}
	return fmt.Sprintf("%s (and %d more diagnostics)", e.Diagnostics[0].String(), len(e.Diagnostics)-1)
}

func ParseBlock(path string, block *sfc.Block) (*Contract, error) {
	if block == nil {
		return nil, fmt.Errorf("parse script block: nil block")
	}
	return Parse(path, []byte(block.Content), block.ContentSpan.Start)
}

func Parse(path string, src []byte, base sfc.Position) (*Contract, error) {
	p := newParser(path, src, base)
	contract := p.parse()
	if len(p.diagnostics) > 0 {
		return contract, &Error{Diagnostics: p.diagnostics}
	}
	return contract, nil
}

type scriptParser struct {
	path        string
	src         []byte
	base        sfc.Position
	fset        *token.FileSet
	file        *ast.File
	imports     map[string]string
	diagnostics []Diagnostic
}

func newParser(path string, src []byte, base sfc.Position) *scriptParser {
	return &scriptParser{
		path:    path,
		src:     src,
		base:    normalizeBase(base),
		fset:    token.NewFileSet(),
		imports: make(map[string]string),
	}
}

func (p *scriptParser) parse() *Contract {
	contract := &Contract{
		Path:          p.path,
		ComponentName: componentNameFromPath(p.path),
		Allocation: Allocation{
			ComponentName: componentNameFromPath(p.path),
		},
	}
	if contract.ComponentName == "" {
		p.addDiagnosticOffset(0, 0, "could not derive component name from file path")
		return contract
	}

	file, err := parser.ParseFile(p.fset, p.path, p.src, parser.AllErrors|parser.ParseComments)
	if err != nil {
		p.addParseDiagnostics(err)
	}
	if file == nil {
		return contract
	}

	p.file = file
	contract.PackageName = file.Name.Name
	contract.Span = p.span(file.Pos(), file.End())
	contract.Imports = p.extractImports(file)

	component := p.findComponent(file, contract.ComponentName)
	if component == nil {
		p.addDiagnosticOffset(len(p.src), len(p.src), fmt.Sprintf("script must declare component type %s", contract.ComponentName))
		return contract
	}
	contract.Span = p.span(component.Pos(), component.End())
	structType, ok := component.Type.(*ast.StructType)
	if !ok {
		p.addDiagnostic(component.Name.Pos(), component.Name.End(), fmt.Sprintf("component %s must be a struct type", contract.ComponentName))
		return contract
	}

	p.extractFields(contract, structType)
	p.extractMethods(contract, file)
	contract.Allocation.PropFields = contract.Props
	contract.Allocation.HasInit = contract.Init != nil

	return contract
}

func (p *scriptParser) extractImports(file *ast.File) []Import {
	imports := make([]Import, 0, len(file.Imports))
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			p.addDiagnostic(spec.Path.Pos(), spec.Path.End(), "import path must be a quoted string")
			continue
		}

		name := importName(spec, importPath)
		imports = append(imports, Import{
			Name: name,
			Path: importPath,
			Span: p.span(spec.Pos(), spec.End()),
		})
		if name != "." && name != "_" {
			p.imports[name] = importPath
		}
	}
	return imports
}

func (p *scriptParser) findComponent(file *ast.File, componentName string) *ast.TypeSpec {
	var component *ast.TypeSpec
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != componentName {
				continue
			}
			if component != nil {
				p.addDiagnostic(typeSpec.Name.Pos(), typeSpec.Name.End(), fmt.Sprintf("duplicate component type %s", componentName))
				continue
			}
			component = typeSpec
		}
	}
	return component
}

func (p *scriptParser) extractFields(contract *Contract, structType *ast.StructType) {
	propNames := make(map[string]sfc.Span)
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			if classification, ok := p.classifyType(field.Type); ok && classification.kind == FieldProp {
				p.addDiagnostic(field.Pos(), field.End(), "prop field must be named")
			}
			continue
		}

		classification, _ := p.classifyType(field.Type)
		tag := fieldTag(field)
		for _, name := range field.Names {
			symbol := Field{
				Name:        name.Name,
				Exported:    name.IsExported(),
				Kind:        classification.kind,
				Type:        exprString(field.Type),
				ElementType: classification.elementType,
				Tag:         tag,
				Span:        p.span(field.Pos(), field.End()),
				NameSpan:    p.span(name.Pos(), name.End()),
				TypeSpan:    p.span(field.Type.Pos(), field.Type.End()),
			}
			if symbol.Kind == "" {
				symbol.Kind = FieldState
			}
			contract.Fields = append(contract.Fields, symbol)

			switch symbol.Kind {
			case FieldProp:
				prop, ok := p.extractProp(symbol)
				if !ok {
					continue
				}
				if previous, exists := propNames[prop.Name]; exists {
					p.addDiagnosticSpan(prop.Span, fmt.Sprintf("duplicate prop name %q", prop.Name))
					p.addDiagnosticSpan(previous, fmt.Sprintf("first prop name %q declared here", prop.Name))
					continue
				}
				propNames[prop.Name] = prop.Span
				contract.Props = append(contract.Props, prop)
			case FieldRef:
				contract.Refs = append(contract.Refs, symbol)
			case FieldComputed:
				contract.Computed = append(contract.Computed, symbol)
			case FieldResource:
				contract.Resources = append(contract.Resources, symbol)
			case FieldCallback:
				callback := p.extractCallback(symbol, field.Type)
				contract.Callbacks = append(contract.Callbacks, callback)
			default:
				contract.State = append(contract.State, symbol)
			}
		}
	}
}

func (p *scriptParser) extractProp(field Field) (Prop, bool) {
	prop := Prop{
		FieldName: field.Name,
		Name:      field.Name,
		Type:      field.ElementType,
		Span:      field.NameSpan,
	}

	if field.ElementType == "" {
		return prop, false
	}

	if field.Tag == "" {
		return prop, true
	}

	tagValue := reflect.StructTag(field.Tag).Get("prop")
	if tagValue == "" {
		return prop, true
	}
	if tagValue == "-" {
		p.addDiagnosticSpan(field.Span, fmt.Sprintf("prop field %s cannot use prop:\"-\"", field.Name))
		return prop, false
	}

	parts := strings.Split(tagValue, ",")
	if parts[0] != "" {
		prop.Name = parts[0]
	}
	for _, option := range parts[1:] {
		switch option {
		case "":
		case "required":
			prop.Required = true
		default:
			p.addDiagnosticSpan(field.Span, fmt.Sprintf("unsupported prop tag option %q", option))
			return prop, false
		}
	}
	return prop, true
}

func (p *scriptParser) extractCallback(field Field, expr ast.Expr) Callback {
	callback := Callback{
		FieldName: field.Name,
		Span:      field.NameSpan,
	}
	funcType, ok := expr.(*ast.FuncType)
	if !ok {
		return callback
	}
	callback.Params = fieldListTypes(funcType.Params)
	callback.Results = fieldListTypes(funcType.Results)
	return callback
}

func (p *scriptParser) extractMethods(contract *Contract, file *ast.File) {
	seenInit := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || !receiverMatches(fn.Recv, contract.ComponentName) {
			continue
		}

		method := Method{
			Name:    fn.Name.Name,
			Params:  fieldListTypes(fn.Type.Params),
			Results: fieldListTypes(fn.Type.Results),
			Span:    p.span(fn.Name.Pos(), fn.Name.End()),
		}
		contract.Methods = append(contract.Methods, method)

		if fn.Name.Name != "Init" {
			continue
		}
		if seenInit {
			p.addDiagnostic(fn.Name.Pos(), fn.Name.End(), "duplicate Init method")
			continue
		}
		seenInit = true

		if !p.validInitSignature(fn, contract.ComponentName) {
			p.addDiagnostic(fn.Name.Pos(), fn.Name.End(), fmt.Sprintf("Init must have signature func (c *%s) Init(ctx tue.Context)", contract.ComponentName))
			continue
		}
		contract.Init = &InitMethod{
			ReceiverName: receiverName(fn.Recv),
			Span:         p.span(fn.Name.Pos(), fn.Name.End()),
		}
	}
}

func (p *scriptParser) validInitSignature(fn *ast.FuncDecl, componentName string) bool {
	if !receiverPointerMatches(fn.Recv, componentName) {
		return false
	}
	if fieldCount(fn.Type.Params) != 1 || fieldCount(fn.Type.Results) != 0 {
		return false
	}
	param := fn.Type.Params.List[0]
	return p.isTueSelector(param.Type, "Context")
}

type typeClassification struct {
	kind        FieldKind
	elementType string
}

func (p *scriptParser) classifyType(expr ast.Expr) (typeClassification, bool) {
	if funcType, ok := expr.(*ast.FuncType); ok {
		_ = funcType
		return typeClassification{kind: FieldCallback}, true
	}

	target, args, ok := p.genericTueType(expr)
	if !ok {
		if selector, ok := expr.(*ast.SelectorExpr); ok && p.isTueSelectorExpr(selector) && tueFieldKind(selector.Sel.Name) != "" {
			p.addDiagnostic(expr.Pos(), expr.End(), fmt.Sprintf("%s must use exactly one type argument", exprString(selector)))
			return typeClassification{kind: tueFieldKind(selector.Sel.Name)}, true
		}
		return typeClassification{}, false
	}

	kind := tueFieldKind(target.Sel.Name)
	if kind == "" {
		return typeClassification{}, false
	}
	if len(args) != 1 {
		p.addDiagnostic(expr.Pos(), expr.End(), fmt.Sprintf("%s must use exactly one type argument", exprString(target)))
		return typeClassification{kind: kind}, true
	}
	return typeClassification{
		kind:        kind,
		elementType: exprString(args[0]),
	}, true
}

func (p *scriptParser) genericTueType(expr ast.Expr) (*ast.SelectorExpr, []ast.Expr, bool) {
	switch expr := expr.(type) {
	case *ast.IndexExpr:
		target, ok := expr.X.(*ast.SelectorExpr)
		if !ok || !p.isTueSelectorExpr(target) {
			return nil, nil, false
		}
		return target, []ast.Expr{expr.Index}, true
	case *ast.IndexListExpr:
		target, ok := expr.X.(*ast.SelectorExpr)
		if !ok || !p.isTueSelectorExpr(target) {
			return nil, nil, false
		}
		return target, expr.Indices, true
	default:
		return nil, nil, false
	}
}

func (p *scriptParser) isTueSelector(expr ast.Expr, name string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == name && p.isTueSelectorExpr(selector)
}

func (p *scriptParser) isTueSelectorExpr(selector *ast.SelectorExpr) bool {
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	importPath, ok := p.imports[ident.Name]
	if ok && importPath == tueImportPath {
		return true
	}
	return ident.Name == "tue"
}

func (p *scriptParser) addParseDiagnostics(err error) {
	var list scanner.ErrorList
	if errors.As(err, &list) {
		for _, item := range list {
			p.addDiagnosticPosition(item.Pos, item.Msg)
		}
		return
	}

	p.addDiagnosticOffset(0, len(p.src), err.Error())
}

func (p *scriptParser) addDiagnostic(start, end token.Pos, message string) {
	p.diagnostics = append(p.diagnostics, Diagnostic{
		Path:    p.path,
		Message: message,
		Span:    p.span(start, end),
	})
}

func (p *scriptParser) addDiagnosticOffset(start, end int, message string) {
	p.diagnostics = append(p.diagnostics, Diagnostic{
		Path:    p.path,
		Message: message,
		Span: sfc.Span{
			Start: p.positionFromOffset(start),
			End:   p.positionFromOffset(end),
		},
	})
}

func (p *scriptParser) addDiagnosticPosition(pos token.Position, message string) {
	p.diagnostics = append(p.diagnostics, Diagnostic{
		Path:    p.path,
		Message: message,
		Span: sfc.Span{
			Start: p.positionFromTokenPosition(pos),
			End:   p.positionFromTokenPosition(pos),
		},
	})
}

func (p *scriptParser) addDiagnosticSpan(span sfc.Span, message string) {
	p.diagnostics = append(p.diagnostics, Diagnostic{
		Path:    p.path,
		Message: message,
		Span:    span,
	})
}

func (p *scriptParser) span(start, end token.Pos) sfc.Span {
	return sfc.Span{
		Start: p.position(start),
		End:   p.position(end),
	}
}

func (p *scriptParser) position(pos token.Pos) sfc.Position {
	if !pos.IsValid() {
		return p.positionFromOffset(len(p.src))
	}
	return p.positionFromTokenPosition(p.fset.Position(pos))
}

func (p *scriptParser) positionFromTokenPosition(pos token.Position) sfc.Position {
	line := pos.Line
	column := pos.Column
	if line <= 0 {
		return p.positionFromOffset(pos.Offset)
	}
	if line == 1 {
		column = p.base.Column + column - 1
	}
	return sfc.Position{
		Offset: p.base.Offset + pos.Offset,
		Line:   p.base.Line + line - 1,
		Column: column,
	}
}

func (p *scriptParser) positionFromOffset(offset int) sfc.Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(p.src) {
		offset = len(p.src)
	}
	line := 1
	lineStart := 0
	for i, b := range p.src[:offset] {
		if b == '\n' {
			line++
			lineStart = i + 1
		}
	}
	column := offset - lineStart + 1
	if line == 1 {
		column = p.base.Column + column - 1
	}
	return sfc.Position{
		Offset: p.base.Offset + offset,
		Line:   p.base.Line + line - 1,
		Column: column,
	}
}

func importName(spec *ast.ImportSpec, importPath string) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	return path.Base(importPath)
}

func fieldTag(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	tag, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return ""
	}
	return tag
}

func fieldListTypes(fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	var out []string
	for _, field := range fields.List {
		typeText := exprString(field.Type)
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for i := 0; i < count; i++ {
			out = append(out, typeText)
		}
	}
	return out
}

func fieldCount(fields *ast.FieldList) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			count++
			continue
		}
		count += len(field.Names)
	}
	return count
}

func receiverMatches(recv *ast.FieldList, componentName string) bool {
	return receiverBaseName(recv) == componentName
}

func receiverPointerMatches(recv *ast.FieldList, componentName string) bool {
	if recv == nil || len(recv.List) != 1 {
		return false
	}
	_, ok := recv.List[0].Type.(*ast.StarExpr)
	return ok && receiverBaseName(recv) == componentName
}

func receiverBaseName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) != 1 {
		return ""
	}
	expr := recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

func receiverName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) != 1 || len(recv.List[0].Names) == 0 {
		return ""
	}
	return recv.List[0].Names[0].Name
}

func tueFieldKind(name string) FieldKind {
	switch name {
	case "Prop":
		return FieldProp
	case "Ref":
		return FieldRef
	case "Computed":
		return FieldComputed
	case "Resource":
		return FieldResource
	default:
		return ""
	}
}

func exprString(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	return types.ExprString(expr)
}

func componentNameFromPath(filePath string) string {
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	base = strings.TrimSuffix(base, ext)
	return pascalIdentifier(base)
}

func pascalIdentifier(name string) string {
	var b strings.Builder
	capNext := true
	for _, r := range name {
		if r == '_' || r == '-' || r == ' ' || r == '.' {
			capNext = true
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			capNext = true
			continue
		}
		if capNext {
			r = unicode.ToUpper(r)
			capNext = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

func normalizeBase(base sfc.Position) sfc.Position {
	if base.Line == 0 {
		base.Line = 1
	}
	if base.Column == 0 {
		base.Column = 1
	}
	return base
}
