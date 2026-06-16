package gogen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/dave/jennifer/jen"
	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
)

const tueImportPath = "github.com/norunners/tue"

// GenerateProject generates cache files for a parsed project.
func GenerateProject(project Project) (*Result, []Diagnostic) {
	generator := projectGenerator{
		manifest: Manifest{GeneratedBy: "tue"},
		assets:   newAssetPipeline(project.Root),
	}
	generator.generate(project)
	if len(generator.diagnostics) != 0 {
		return nil, generator.diagnostics
	}
	return &Result{
		Files:    generator.files,
		Assets:   generator.assetFiles,
		Manifest: generator.manifest,
	}, nil
}

type projectGenerator struct {
	packageName string
	components  map[string]componentBinding
	files       []GeneratedFile
	assetFiles  []GeneratedAsset
	manifest    Manifest
	assets      *assetPipeline
	diagnostics []Diagnostic
}

func (g *projectGenerator) generate(project Project) {
	g.indexComponents(project.Files)
	if len(g.diagnostics) != 0 {
		return
	}
	for _, file := range project.Files {
		g.generateFile(file)
	}
	if len(g.diagnostics) != 0 {
		return
	}
	g.generateStyles(project.Files)
	if len(g.diagnostics) != 0 {
		return
	}
	g.generateAssets()
}

type componentBinding struct {
	path      string
	component *script.Component
	props     map[string]script.Prop
	events    map[string]script.Field
}

type componentField struct {
	Name  string
	Value jen.Code
}

func (g *projectGenerator) indexComponents(files []File) {
	g.components = make(map[string]componentBinding, len(files))
	for _, file := range files {
		path := filePath(file)
		if file.Script == nil || file.Script.Component == nil {
			continue
		}

		component := file.Script.Component
		if previous, ok := g.components[component.Name]; ok {
			g.add(path, fmt.Sprintf("duplicate component %q; first declared in %s", component.Name, previous.path), component.NameSpan)
			continue
		}

		props := make(map[string]script.Prop, len(component.Props))
		for _, prop := range component.Props {
			props[prop.Name] = prop
		}
		events := make(map[string]script.Field, len(component.Events))
		for _, event := range component.Events {
			name, ok := script.EventName(event)
			if ok {
				events[name] = event
			}
		}
		g.components[component.Name] = componentBinding{
			path:      path,
			component: component,
			props:     props,
			events:    events,
		}
	}
}

func (g *projectGenerator) generateFile(file File) {
	path := filePath(file)
	if file.Template == nil {
		g.add(path, "missing parsed template", sfc.Span{})
		return
	}
	if file.Script == nil || file.Script.Component == nil {
		g.add(path, "missing component contract", file.Template.Span)
		return
	}
	if file.Script.PackageName == "" {
		g.add(path, "missing script package name", file.Template.Span)
		return
	}
	if g.packageName == "" {
		g.packageName = file.Script.PackageName
	} else if file.Script.PackageName != g.packageName {
		g.add(path, fmt.Sprintf("generated cache supports one package, got %q after %q", file.Script.PackageName, g.packageName), file.Script.PackageSpan)
		return
	}

	stem := generatedStem(path)
	scriptPath := stem + "_tue.go"
	renderPath := stem + "_render_tue.go"
	scopeAttr := ""
	if file.Style != nil && file.Style.Scoped {
		scopeAttr = scopeAttrFor(path)
	}

	scriptSource, ok := g.generatedScriptSource(file)
	if !ok {
		return
	}
	renderSource, nodes, ok := g.generatedRenderSource(file, scopeAttr)
	if !ok {
		return
	}

	g.files = append(g.files,
		GeneratedFile{Path: scriptPath, Source: scriptSource},
		GeneratedFile{Path: renderPath, Source: renderSource},
	)
	g.manifest.Files = append(g.manifest.Files, ManifestFile{
		Source:     path,
		Component:  file.Script.Component.Name,
		ScriptFile: scriptPath,
		RenderFile: renderPath,
		ScopeAttr:  scopeAttr,
		Nodes:      nodes,
	})
}

func (g *projectGenerator) generateStyles(files []File) {
	files = cloneStyleFiles(files)
	for _, file := range files {
		if g.assets != nil && !g.assets.rewriteStyleURLs(file) {
			g.diagnostics = append(g.diagnostics, g.assets.diagnostics...)
			g.assets.diagnostics = nil
			return
		}
	}

	source, ok := generatedStyleSource(files)
	if !ok {
		return
	}

	g.files = append(g.files, GeneratedFile{Path: styleFilePath, Source: source})
	g.manifest.StyleFile = styleFilePath
}

func cloneStyleFiles(files []File) []File {
	cloned := make([]File, len(files))
	copy(cloned, files)
	for i := range cloned {
		if cloned[i].Style == nil {
			continue
		}
		style := *cloned[i].Style
		cloned[i].Style = &style
	}
	return cloned
}

func (g *projectGenerator) generateAssets() {
	if g.assets == nil {
		return
	}

	g.assets.collectPublicAssets()
	if len(g.assets.diagnostics) != 0 {
		g.diagnostics = append(g.diagnostics, g.assets.diagnostics...)
		g.assets.diagnostics = nil
		return
	}

	assets := g.assets.sortedAssets()
	if len(assets) == 0 {
		return
	}
	g.assetFiles = append(g.assetFiles, assets...)
	g.manifest.Assets = make([]ManifestAsset, len(assets))
	for i, asset := range assets {
		g.manifest.Assets[i] = ManifestAsset{
			Source: asset.SourcePath,
			Output: asset.OutputPath,
			Public: asset.Public,
		}
	}
}

func (g *projectGenerator) generatedScriptSource(file File) ([]byte, bool) {
	source := strings.TrimSpace(file.ScriptSource)
	if source == "" {
		g.add(filePath(file), "missing script source", file.Template.Span)
		return nil, false
	}

	generated := []byte("// Code generated by tue; DO NOT EDIT.\n\n" + source + "\n")
	formatted, err := format.Source(generated)
	if err != nil {
		g.add(filePath(file), fmt.Sprintf("format generated script: %v", err), file.Script.PackageSpan)
		return nil, false
	}
	return formatted, true
}

func (g *projectGenerator) generatedRenderSource(file File, scopeAttr string) ([]byte, []ManifestNode, bool) {
	renderer := fileGenerator{
		path:       filePath(file),
		file:       jen.NewFile(file.Script.PackageName),
		component:  file.Script.Component,
		components: g.components,
		fields:     componentFields(file.Script.Component),
		methods:    componentMethods(file.Script.Component),
		scopeAttr:  scopeAttr,
		assets:     g.assets,
	}
	renderer.file.HeaderComment("Code generated by tue; DO NOT EDIT.")
	renderer.file.ImportName(tueImportPath, "tue")
	renderer.file.ImportName("fmt", "fmt")
	renderer.generate(file.Template)
	if len(renderer.diagnostics) != 0 {
		g.diagnostics = append(g.diagnostics, renderer.diagnostics...)
		return nil, nil, false
	}

	source, err := renderJenniferFile(renderer.file)
	if err != nil {
		g.add(filePath(file), fmt.Sprintf("render generated Go: %v", err), file.Script.Component.NameSpan)
		return nil, nil, false
	}
	return source, renderer.nodes, true
}

type fileGenerator struct {
	path        string
	file        *jen.File
	component   *script.Component
	components  map[string]componentBinding
	fields      map[string]script.Field
	methods     map[string]script.Method
	scopeAttr   string
	assets      *assetPipeline
	locals      map[string]string
	localTypes  map[string]string
	nodes       []ManifestNode
	diagnostics []Diagnostic
}

func (g *fileGenerator) generate(tree *gotemplate.Tree) {
	componentName := g.component.Name
	g.file.Func().Id("New"+componentName).Params().Op("*").Qual(tueImportPath, "Comp").Block(
		jen.Id("component").Op(":=").Op("&").Id(componentName).Values(),
		jen.Return(jen.Qual(tueImportPath, "CompOf").Call(jen.Id("component"), jen.Id("render"+componentName))),
	)
	g.file.Line()
	g.file.Func().Id("render"+componentName).Params(jen.Id("component").Op("*").Id(componentName)).Qual(tueImportPath, "VNode").Block(
		jen.Return(g.renderNodes(tree.Nodes)),
	)
}

func (g *fileGenerator) renderNodes(nodes []*gotemplate.Node) jen.Code {
	rendered := make([]jen.Code, 0, len(nodes))
	for _, node := range nodes {
		if code, ok := g.renderNode(node); ok {
			rendered = append(rendered, code)
		}
	}
	if len(rendered) == 1 {
		return rendered[0]
	}
	return jen.Qual(tueImportPath, "Fragment").Call(g.vnodeSlice(rendered))
}

func (g *fileGenerator) renderNode(node *gotemplate.Node) (jen.Code, bool) {
	if node == nil {
		return nil, false
	}
	attr := nodeDirectiveAttr(node, gotemplate.DirectiveFor)
	if attr != nil {
		return g.renderFor(node, *attr)
	}

	return g.renderNodeAfterFor(node)
}

func (g *fileGenerator) renderNodeAfterFor(node *gotemplate.Node) (jen.Code, bool) {
	attr := nodeDirectiveAttr(node, gotemplate.DirectiveIf)
	if attr != nil {
		return g.renderIf(node, *attr)
	}

	return g.renderNodeBody(node)
}

func (g *fileGenerator) renderFor(node *gotemplate.Node, attr gotemplate.Attr) (jen.Code, bool) {
	clause, ok := gotemplate.ParseForClause(attr.Expression)
	if !ok {
		g.add("v-for must use '<item> in <items>'", attr.ExpressionSpan)
		return nil, false
	}

	keyAttr := nodeBindAttr(node, "key")
	if keyAttr == nil {
		g.add("v-for requires a :key attribute", attr.DirectiveSpan)
		return nil, false
	}

	sourceSpan := spanWithin(attr.ExpressionSpan, attr.Expression, clause.SourceStart, clause.SourceEnd)
	source, sourceOK := g.renderExpressionFor("v-for source", clause.Source, sourceSpan)
	if !sourceOK {
		return nil, false
	}

	names := g.freshForNames(clause)
	sourceType := g.expressionType(clause.Source)
	types, _ := iterableTypesFor(sourceType)

	locals := map[string]string{clause.Item: names.item}
	localTypes := map[string]string{clause.Item: types.Item}
	if clause.Index != "" {
		locals[clause.Index] = names.index
		localTypes[clause.Index] = types.Key
	}
	popLocals := g.pushLocals(locals, localTypes)
	rendered, renderedOK := g.renderNodeWithKey(node, *keyAttr)
	popLocals()
	if !renderedOK {
		return nil, false
	}

	return jen.Func().Params().Qual(tueImportPath, "VNode").Block(
		jen.Id(names.source).Op(":=").Add(source),
		jen.Id(names.nodes).Op(":=").Make(jen.Index().Qual(tueImportPath, "VNode"), jen.Lit(0), jen.Len(jen.Id(names.source))),
		jen.For(jen.List(jen.Id(names.index), jen.Id(names.item)).Op(":=").Range().Id(names.source)).Block(
			jen.Id(names.nodes).Op("=").Append(jen.Id(names.nodes), rendered),
		),
		jen.Return(jen.Qual(tueImportPath, "Fragment").Call(jen.Id(names.nodes))),
	).Call(), true
}

func (g *fileGenerator) renderNodeWithKey(node *gotemplate.Node, attr gotemplate.Attr) (jen.Code, bool) {
	rendered, nodeOK := g.renderNodeAfterFor(node)
	key, keyOK := g.renderExpressionFor("v-for key", attr.Expression, attr.ExpressionSpan)
	if !nodeOK || !keyOK {
		return nil, false
	}

	name := freshIdentifier(g.usedGeneratedNames(), "__tueVNode")
	return jen.Func().Params().Qual(tueImportPath, "VNode").Block(
		jen.Id(name).Op(":=").Add(rendered),
		jen.Id(name).Dot("Key").Op("=").Qual("fmt", "Sprint").Call(key),
		jen.Return(jen.Id(name)),
	).Call(), true
}

func (g *fileGenerator) renderNodeBody(node *gotemplate.Node) (jen.Code, bool) {
	switch node.Kind {
	case gotemplate.NodeElement:
		return g.renderElement(node)
	case gotemplate.NodeText:
		if strings.TrimSpace(node.Text) == "" {
			return nil, false
		}
		g.recordNode(node)
		return jen.Qual(tueImportPath, "Text").Call(jen.Lit(node.Text)), true
	case gotemplate.NodeInterpolation:
		return g.renderInterpolation(node)
	case gotemplate.NodeComment:
		return nil, false
	default:
		g.add(fmt.Sprintf("unsupported template node kind %q", node.Kind), node.Span)
		return nil, false
	}
}

func (g *fileGenerator) renderIf(node *gotemplate.Node, attr gotemplate.Attr) (jen.Code, bool) {
	condition, conditionOK := g.renderExpressionFor("v-if", attr.Expression, attr.ExpressionSpan)
	rendered, nodeOK := g.renderNodeBody(node)
	if !conditionOK || !nodeOK {
		return nil, false
	}

	return jen.Func().Params().Qual(tueImportPath, "VNode").Block(
		jen.If(condition).Block(
			jen.Return(rendered),
		),
		jen.Return(g.emptyFragment()),
	).Call(), true
}

func (g *fileGenerator) renderElement(node *gotemplate.Node) (jen.Code, bool) {
	if node.Tag == "slot" {
		return g.renderSlot(node)
	}
	if node.IsComponent {
		return g.renderComponent(node)
	}

	attrs, events, ok := g.renderAttrsAndEvents(node)
	if !ok {
		return nil, false
	}
	attrs = g.withScopeAttr(attrs)

	children := make([]jen.Code, 0, len(node.Children))
	for _, child := range node.Children {
		childCode, ok := g.renderNode(child)
		if ok {
			children = append(children, childCode)
		}
	}

	g.recordNode(node)
	if len(events) != 0 {
		return jen.Qual(tueImportPath, "ElementWithEvents").Call(
			jen.Lit(node.Tag),
			g.attributeSlice(attrs),
			g.eventSlice(events),
			g.vnodeSlice(children),
		), true
	}
	return jen.Qual(tueImportPath, "Element").Call(
		jen.Lit(node.Tag),
		g.attributeSlice(attrs),
		g.vnodeSlice(children),
	), true
}

func (g *fileGenerator) withScopeAttr(attrs []jen.Code) []jen.Code {
	if g.scopeAttr == "" {
		return attrs
	}
	return append(attrs, jen.Qual(tueImportPath, "BoolAttr").Call(jen.Lit(g.scopeAttr)))
}

func (g *fileGenerator) renderComponent(node *gotemplate.Node) (jen.Code, bool) {
	child, ok := g.components[node.Tag]
	if !ok {
		g.add(fmt.Sprintf("component %q is not registered", node.Tag), node.TagSpan)
		return nil, false
	}

	fields, ok := g.renderComponentFields(node, child)
	if !ok {
		return nil, false
	}
	defaultSlot := g.renderComponentDefaultSlot(node)

	g.recordNode(node)
	statements := []jen.Code{
		jen.Id("child").Op(":=").Op("&").Id(child.component.Name).Values(componentFieldValues(fields)...),
		jen.Id("childComp").Op(":=").Qual(tueImportPath, "CompOf").Call(jen.Id("child"), jen.Id("render"+child.component.Name)),
	}
	if defaultSlot != nil {
		statements = append(statements, jen.Id("childComp").Dot("DefaultSlot").Op("=").Add(defaultSlot))
	}
	statements = append(statements, jen.Return(jen.Id("childComp")))

	component := jen.Qual(tueImportPath, "ComponentWithUpdate").Call(
		jen.Lit(node.Tag),
		jen.Func().Params().Op("*").Qual(tueImportPath, "Comp").Block(statements...),
		g.renderComponentUpdater(child, fields, defaultSlot),
	)
	if g.scopeAttr == "" {
		return component, true
	}
	return jen.Qual(tueImportPath, "WithScopeAttrs").Call(component, jen.Lit(g.scopeAttr)), true
}

func (g *fileGenerator) renderComponentDefaultSlot(node *gotemplate.Node) jen.Code {
	if !hasRenderableComponentChildren(node.Children) {
		return nil
	}
	return jen.Func().Params().Qual(tueImportPath, "VNode").Block(
		jen.Return(g.renderNodes(node.Children)),
	)
}

func (g *fileGenerator) renderComponentUpdater(child componentBinding, fields []componentField, defaultSlot jen.Code) jen.Code {
	statements := make([]jen.Code, 0, len(fields)+2)
	if len(fields) != 0 {
		statements = append(
			statements,
			jen.Id("child").Op(":=").Id("childComp").Dot("Component").Assert(jen.Op("*").Id(child.component.Name)),
		)
		for _, field := range fields {
			statements = append(statements, jen.Id("child").Dot(field.Name).Op("=").Add(field.Value))
		}
	}
	if defaultSlot == nil {
		statements = append(statements, jen.Id("childComp").Dot("DefaultSlot").Op("=").Nil())
	} else {
		statements = append(statements, jen.Id("childComp").Dot("DefaultSlot").Op("=").Add(defaultSlot))
	}
	return jen.Func().Params(jen.Id("childComp").Op("*").Qual(tueImportPath, "Comp")).Block(statements...)
}

func (g *fileGenerator) renderSlot(node *gotemplate.Node) (jen.Code, bool) {
	ok := true
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrDirective && (attr.Directive == gotemplate.DirectiveIf || attr.Directive == gotemplate.DirectiveFor) {
			continue
		}
		if isNamedSlotAttr(attr) {
			g.add("named slots are not supported in the default slot slice", attr.Span)
		} else {
			g.add(fmt.Sprintf("slot attribute %q generation is not supported in the default slot slice", attr.RawName), attr.Span)
		}
		ok = false
	}
	if !ok {
		return nil, false
	}

	g.recordNode(node)
	return jen.Qual(tueImportPath, "Slot").Call(g.renderNodes(node.Children)), true
}

func (g *fileGenerator) renderComponentFields(node *gotemplate.Node, child componentBinding) ([]componentField, bool) {
	props := make(map[string]jen.Code, len(node.Attrs))
	events := make(map[string]jen.Code, len(node.Attrs))
	ok := true
	for _, attr := range node.Attrs {
		switch attr.Kind {
		case gotemplate.AttrStatic:
			prop, found := child.props[attr.Name]
			if !found {
				g.add(fmt.Sprintf("component %q has no prop %q", node.Tag, attr.Name), attr.NameSpan)
				ok = false
				continue
			}
			value, valueOK := g.renderStaticComponentProp(node.Tag, prop, attr)
			if !valueOK {
				ok = false
				continue
			}
			props[prop.Name] = value
		case gotemplate.AttrBind:
			if attr.Argument == "key" {
				continue
			}
			prop, found := child.props[attr.Argument]
			if !found {
				g.add(fmt.Sprintf("component %q has no prop %q", node.Tag, attr.Argument), attr.ArgumentSpan)
				ok = false
				continue
			}
			value, valueOK := g.renderBoundComponentProp(prop, attr)
			if !valueOK {
				ok = false
				continue
			}
			props[prop.Name] = value
		case gotemplate.AttrEvent:
			field, eventOK := g.renderComponentEventField(node, child, attr)
			if !eventOK {
				ok = false
				continue
			}
			events[field.Name] = field.Value
		case gotemplate.AttrDirective:
			if attr.Directive == gotemplate.DirectiveIf || attr.Directive == gotemplate.DirectiveFor {
				continue
			}
			if attr.Directive == gotemplate.DirectiveModel {
				g.add(modelUnsupportedMessage(node), attr.DirectiveSpan)
				ok = false
				continue
			}
			g.add(fmt.Sprintf("directive %q generation is not supported on components in the component render slice", attr.RawName), attr.Span)
			ok = false
		default:
			g.add(fmt.Sprintf("unsupported component attribute kind %q", attr.Kind), attr.Span)
			ok = false
		}
	}

	fields := make([]componentField, 0, len(child.component.Props)+len(child.component.Events))
	for _, prop := range child.component.Props {
		value, found := props[prop.Name]
		if !found {
			if prop.Required {
				g.add(fmt.Sprintf("component %q requires prop %q", node.Tag, prop.Name), node.TagSpan)
				ok = false
				continue
			}
			zero, zeroOK := zeroPropValue(prop)
			if !zeroOK {
				g.add(fmt.Sprintf("component %q prop %q has unsupported type %q", node.Tag, prop.Name, propValueType(prop)), prop.Field.TypeSpan)
				ok = false
				continue
			}
			value = zero
		}
		fields = append(fields, componentField{Name: prop.Field.Name, Value: value})
	}
	for _, event := range child.component.Events {
		value, found := events[event.Name]
		if !found {
			value = jen.Nil()
		}
		fields = append(fields, componentField{Name: event.Name, Value: value})
	}
	return fields, ok
}

type componentEventField struct {
	Name  string
	Value jen.Code
}

func (g *fileGenerator) renderComponentEventField(node *gotemplate.Node, child componentBinding, attr gotemplate.Attr) (componentEventField, bool) {
	event, found := child.events[attr.Argument]
	if !found {
		g.add(fmt.Sprintf("component %q has no event %q", node.Tag, attr.Argument), attr.ArgumentSpan)
		return componentEventField{}, false
	}
	if !isNoArgFunc(event.Type) {
		g.add(fmt.Sprintf("component %q event %q must have signature func()", node.Tag, attr.Argument), attr.ArgumentSpan)
		return componentEventField{}, false
	}

	methodName, callWithArgs, ok := g.eventHandlerMethod(attr)
	if !ok {
		return componentEventField{}, false
	}
	if callWithArgs {
		g.add(fmt.Sprintf("event handler %q does not accept arguments", methodName), attr.ExpressionSpan)
		return componentEventField{}, false
	}

	method, ok := g.methods[methodName]
	if !ok {
		g.add(fmt.Sprintf("event handler %q is not a method on %s", methodName, g.component.Name), attr.ExpressionSpan)
		return componentEventField{}, false
	}
	if len(method.Parameters) != 0 || len(method.Results) != 0 {
		g.add(fmt.Sprintf("event handler %q must have signature func()", methodName), attr.ExpressionSpan)
		return componentEventField{}, false
	}

	return componentEventField{
		Name:  event.Name,
		Value: jen.Id("component").Dot(methodName),
	}, true
}

func (g *fileGenerator) renderStaticComponentProp(componentName string, prop script.Prop, attr gotemplate.Attr) (jen.Code, bool) {
	expected := propValueType(prop)
	actual := "string"
	span := attr.ValueSpan
	if !attr.HasValue {
		actual = "bool"
		span = attr.NameSpan
	}
	if !assignableType(expected, actual) {
		g.add(
			fmt.Sprintf("component %q prop %q expects %s, got %s", componentName, prop.Name, displayType(expected), displayType(actual)),
			span,
		)
		return nil, false
	}
	if !attr.HasValue {
		return jen.Qual(tueImportPath, "PropOf").Call(jen.True()), true
	}
	return jen.Qual(tueImportPath, "PropOf").Call(jen.Lit(attr.Value)), true
}

func (g *fileGenerator) renderBoundComponentProp(prop script.Prop, attr gotemplate.Attr) (jen.Code, bool) {
	expression, ok := g.renderExpression(attr.Expression, attr.ExpressionSpan)
	if !ok {
		return nil, false
	}
	valueType, ok := renderTypeExpression(propValueType(prop))
	if !ok {
		g.add(fmt.Sprintf("component prop %q has unsupported type %q", prop.Name, propValueType(prop)), prop.Field.TypeSpan)
		return nil, false
	}
	return jen.Qual(tueImportPath, "PropOfFunc").Call(
		jen.Func().Params().Add(valueType).Block(jen.Return(expression)),
	), true
}

func (g *fileGenerator) renderAttrsAndEvents(node *gotemplate.Node) ([]jen.Code, []jen.Code, bool) {
	attrs := make([]jen.Code, 0, len(node.Attrs))
	events := make([]jen.Code, 0, len(node.Attrs))
	hasClassBinding := nodeBindAttr(node, "class") != nil
	hasStyleBinding := nodeBindAttr(node, "style") != nil
	modelAttr := nodeDirectiveAttr(node, gotemplate.DirectiveModel)
	modelControlledAttr := ""
	if modelAttr != nil {
		if binding, ok := nativeModelBinding(node); ok {
			modelControlledAttr = "value"
			if binding.Checked {
				modelControlledAttr = "checked"
			}
		}
	}
	classInsert := -1
	classOrder := -1
	var classStatic []string
	var classDynamic []jen.Code
	styleInsert := -1
	styleOrder := -1
	var styleStatic []string
	var styleDynamic []jen.Code
	ok := true
	for attrOrder, attr := range node.Attrs {
		switch attr.Kind {
		case gotemplate.AttrStatic:
			if attr.Name == modelControlledAttr {
				continue
			}
			if hasClassBinding && attr.Name == "class" {
				if classInsert == -1 {
					classInsert = len(attrs)
					classOrder = attrOrder
				}
				if attr.HasValue {
					classStatic = append(classStatic, attr.Value)
				}
				continue
			}
			if hasStyleBinding && attr.Name == "style" {
				if styleInsert == -1 {
					styleInsert = len(attrs)
					styleOrder = attrOrder
				}
				if attr.HasValue {
					styleStatic = append(styleStatic, attr.Value)
				}
				continue
			}
			if attr.HasValue {
				value := attr.Value
				if isStaticAssetAttr(node, attr) {
					rewritten, assetOK := g.assets.rewriteURL(g.path, attr.Value, attr.ValueSpan)
					if !assetOK {
						ok = false
						continue
					}
					value = rewritten
				}
				attrs = append(attrs, jen.Qual(tueImportPath, "Attr").Call(jen.Lit(attr.Name), jen.Lit(value)))
			} else {
				attrs = append(attrs, jen.Qual(tueImportPath, "BoolAttr").Call(jen.Lit(attr.Name)))
			}
		case gotemplate.AttrBind:
			if attr.Argument == "key" {
				continue
			}
			if attr.Argument == "class" {
				if classInsert == -1 {
					classInsert = len(attrs)
					classOrder = attrOrder
				}
				classValue, classOK := g.renderClassBinding(attr)
				if !classOK {
					ok = false
					continue
				}
				classDynamic = append(classDynamic, classValue)
				continue
			}
			if attr.Argument == "style" {
				if styleInsert == -1 {
					styleInsert = len(attrs)
					styleOrder = attrOrder
				}
				styleValue, styleOK := g.renderStyleBinding(attr)
				if !styleOK {
					ok = false
					continue
				}
				styleDynamic = append(styleDynamic, styleValue)
				continue
			}
			g.add(fmt.Sprintf("bound attribute %q generation is not supported in the static render slice", attr.RawName), attr.Span)
			ok = false
		case gotemplate.AttrEvent:
			event, eventOK := g.renderEvent(attr)
			if !eventOK {
				ok = false
				continue
			}
			events = append(events, event)
		case gotemplate.AttrDirective:
			if attr.Directive == gotemplate.DirectiveIf || attr.Directive == gotemplate.DirectiveFor || attr.Directive == gotemplate.DirectiveModel {
				continue
			}
			g.add(fmt.Sprintf("directive %q generation is not supported in the static render slice", attr.RawName), attr.Span)
			ok = false
		default:
			g.add(fmt.Sprintf("unsupported attribute kind %q", attr.Kind), attr.Span)
			ok = false
		}
	}
	var mergedAttrs []mergedAttr
	if len(classDynamic) != 0 {
		mergedAttrs = append(mergedAttrs, mergedAttr{
			Index: classInsert,
			Order: classOrder,
			Attr:  renderClassAttr(classStatic, classDynamic),
		})
	}
	if len(styleDynamic) != 0 {
		mergedAttrs = append(mergedAttrs, mergedAttr{
			Index: styleInsert,
			Order: styleOrder,
			Attr:  renderStyleAttr(styleStatic, styleDynamic),
		})
	}
	attrs = insertMergedAttrs(attrs, mergedAttrs)
	if modelAttr != nil {
		modelAttrs, modelEvent, modelOK := g.renderModelBinding(node, *modelAttr)
		if !modelOK {
			ok = false
		} else {
			attrs = append(attrs, modelAttrs...)
			events = append([]jen.Code{modelEvent}, events...)
		}
	}
	return attrs, events, ok
}

func (g *fileGenerator) renderClassBinding(attr gotemplate.Attr) (jen.Code, bool) {
	expression, ok := g.renderExpressionFor("class binding", attr.Expression, attr.ExpressionSpan)
	if !ok {
		return nil, false
	}

	valueType := g.expressionType(attr.Expression)
	if !assignableType("string", valueType) {
		g.add(fmt.Sprintf("class binding expects string, got %s", displayType(valueType)), attr.ExpressionSpan)
		return nil, false
	}
	return expression, true
}

func (g *fileGenerator) renderStyleBinding(attr gotemplate.Attr) (jen.Code, bool) {
	expression, ok := g.renderExpressionFor("style binding", attr.Expression, attr.ExpressionSpan)
	if !ok {
		return nil, false
	}

	valueType := g.expressionType(attr.Expression)
	if !assignableType("string", valueType) {
		g.add(fmt.Sprintf("style binding expects string, got %s", displayType(valueType)), attr.ExpressionSpan)
		return nil, false
	}
	return expression, true
}

func renderClassAttr(static []string, dynamic []jen.Code) jen.Code {
	args := make([]jen.Code, 0, len(dynamic)+1)
	args = append(args, jen.Lit(strings.Join(static, " ")))
	args = append(args, dynamic...)
	return jen.Qual(tueImportPath, "ClassAttr").Call(args...)
}

func renderStyleAttr(static []string, dynamic []jen.Code) jen.Code {
	args := make([]jen.Code, 0, len(dynamic)+1)
	args = append(args, jen.Lit(strings.Join(static, "; ")))
	args = append(args, dynamic...)
	return jen.Qual(tueImportPath, "StyleAttr").Call(args...)
}

func componentFieldValues(fields []componentField) []jen.Code {
	values := make([]jen.Code, len(fields))
	for i, field := range fields {
		values[i] = jen.Id(field.Name).Op(":").Add(field.Value)
	}
	return values
}

func (g *fileGenerator) renderModelBinding(node *gotemplate.Node, attr gotemplate.Attr) ([]jen.Code, jen.Code, bool) {
	binding, ok := nativeModelBinding(node)
	if !ok {
		g.add(modelUnsupportedMessage(node), attr.DirectiveSpan)
		return nil, nil, false
	}

	expression, expressionOK := g.renderExpressionFor("v-model", attr.Expression, attr.ExpressionSpan)
	if !expressionOK {
		return nil, nil, false
	}
	actualType := g.expressionType(attr.Expression)
	if !assignableType(binding.ValueType, actualType) {
		g.add(fmt.Sprintf("v-model expects %s, got %s", displayType(binding.ValueType), displayType(actualType)), attr.ExpressionSpan)
		return nil, nil, false
	}

	setter, setterOK := g.renderModelSetter(attr.Expression, attr.ExpressionSpan, binding.ValueName)
	if !setterOK {
		return nil, nil, false
	}

	if binding.Checked {
		return []jen.Code{
				jen.Qual(tueImportPath, "BoolStateAttr").Call(jen.Lit("checked"), expression),
			},
			jen.Qual(tueImportPath, "OnChecked").Call(
				jen.Lit(binding.EventName),
				jen.Func().Params(jen.Id(binding.ValueName).Bool()).Block(setter),
			),
			true
	}
	return []jen.Code{
			jen.Qual(tueImportPath, "Attr").Call(jen.Lit("value"), expression),
		},
		jen.Qual(tueImportPath, "OnValue").Call(
			jen.Lit(binding.EventName),
			jen.Func().Params(jen.Id(binding.ValueName).String()).Block(setter),
		),
		true
}

func (g *fileGenerator) renderModelSetter(target string, span sfc.Span, valueName string) (jen.Code, bool) {
	expr, err := goparser.ParseExpr(target)
	if err != nil {
		g.add(fmt.Sprintf("invalid v-model expression: %s", err), span)
		return nil, false
	}

	ident, ok := expr.(*ast.Ident)
	if !ok {
		g.add(fmt.Sprintf("v-model target %q is not writable", target), span)
		return nil, false
	}

	field, ok := g.fields[ident.Name]
	if !ok {
		g.add(fmt.Sprintf("v-model target %q is not writable", target), span)
		return nil, false
	}

	access := jen.Id("component").Dot(field.Name)
	switch field.Kind {
	case script.FieldKindState:
		return jen.Add(access).Op("=").Id(valueName), true
	case script.FieldKindRef:
		return jen.Add(access).Dot("Set").Call(jen.Id(valueName)), true
	default:
		g.add(fmt.Sprintf("v-model target %q is not writable", target), span)
		return nil, false
	}
}

type mergedAttr struct {
	Index int
	Order int
	Attr  jen.Code
}

func insertMergedAttrs(attrs []jen.Code, mergedAttrs []mergedAttr) []jen.Code {
	sort.SliceStable(mergedAttrs, func(i int, j int) bool {
		if mergedAttrs[i].Index != mergedAttrs[j].Index {
			return mergedAttrs[i].Index > mergedAttrs[j].Index
		}
		return mergedAttrs[i].Order > mergedAttrs[j].Order
	})
	for _, merged := range mergedAttrs {
		attrs = insertAttr(attrs, merged.Index, merged.Attr)
	}
	return attrs
}

func insertAttr(attrs []jen.Code, index int, attr jen.Code) []jen.Code {
	if index < 0 || index >= len(attrs) {
		return append(attrs, attr)
	}
	attrs = append(attrs, nil)
	copy(attrs[index+1:], attrs[index:])
	attrs[index] = attr
	return attrs
}

func (g *fileGenerator) renderEvent(attr gotemplate.Attr) (jen.Code, bool) {
	methodName, callWithArgs, ok := g.eventHandlerMethod(attr)
	if !ok {
		return nil, false
	}
	if callWithArgs {
		g.add(fmt.Sprintf("event handler %q does not accept arguments", methodName), attr.ExpressionSpan)
		return nil, false
	}

	method, ok := g.methods[methodName]
	if !ok {
		g.add(fmt.Sprintf("event handler %q is not a method on %s", methodName, g.component.Name), attr.ExpressionSpan)
		return nil, false
	}
	if len(method.Parameters) != 0 || len(method.Results) != 0 {
		g.add(fmt.Sprintf("event handler %q must have signature func()", methodName), attr.ExpressionSpan)
		return nil, false
	}

	return jen.Qual(tueImportPath, "On").Call(
		jen.Lit(attr.Argument),
		jen.Id("component").Dot(methodName),
	), true
}

func (g *fileGenerator) eventHandlerMethod(attr gotemplate.Attr) (string, bool, bool) {
	expr, err := goparser.ParseExprFrom(token.NewFileSet(), "", attr.Expression, 0)
	if err != nil {
		g.add(fmt.Sprintf("invalid event handler expression: %s", err), attr.ExpressionSpan)
		return "", false, false
	}

	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name, false, true
	case *ast.CallExpr:
		ident, ok := typed.Fun.(*ast.Ident)
		if !ok {
			g.add("event handler generation supports component method calls only", attr.ExpressionSpan)
			return "", false, false
		}
		return ident.Name, len(typed.Args) != 0, true
	default:
		g.add("event handler generation supports component methods only", attr.ExpressionSpan)
		return "", false, false
	}
}

func (g *fileGenerator) renderInterpolation(node *gotemplate.Node) (jen.Code, bool) {
	expression, ok := g.renderExpression(node.Expression, node.ExpressionSpan)
	if !ok {
		return nil, false
	}
	g.recordNode(node)
	return jen.Qual(tueImportPath, "Text").Call(jen.Qual("fmt", "Sprint").Call(expression)), true
}

func (g *fileGenerator) renderExpression(expression string, span sfc.Span) (jen.Code, bool) {
	return g.renderExpressionFor("interpolation", expression, span)
}

func (g *fileGenerator) renderExpressionFor(subject string, expression string, span sfc.Span) (jen.Code, bool) {
	expr, err := goparser.ParseExprFrom(token.NewFileSet(), "", expression, 0)
	if err != nil {
		g.add(fmt.Sprintf("invalid %s expression: %s", subject, err), span)
		return nil, false
	}

	generated := expressionGenerator{
		fields: g.fields,
		locals: g.locals,
	}
	code, ok := generated.render(expr)
	if !ok {
		g.add(fmt.Sprintf("%s expression is not supported in the static render slice", subject), span)
		return nil, false
	}
	return code, true
}

func (g *fileGenerator) attributeSlice(attrs []jen.Code) jen.Code {
	if len(attrs) == 0 {
		return jen.Nil()
	}
	return jen.Index().Qual(tueImportPath, "Attribute").Values(attrs...)
}

func (g *fileGenerator) eventSlice(events []jen.Code) jen.Code {
	if len(events) == 0 {
		return jen.Nil()
	}
	return jen.Index().Qual(tueImportPath, "EventBinding").Values(events...)
}

func (g *fileGenerator) vnodeSlice(nodes []jen.Code) jen.Code {
	if len(nodes) == 0 {
		return jen.Nil()
	}
	return jen.Index().Qual(tueImportPath, "VNode").Values(nodes...)
}

func (g *fileGenerator) emptyFragment() jen.Code {
	return jen.Qual(tueImportPath, "Fragment").Call(jen.Nil())
}

func (g *fileGenerator) recordNode(node *gotemplate.Node) {
	manifestNode := ManifestNode{
		Kind:       string(node.Kind),
		Tag:        node.Tag,
		SourceSpan: node.Span,
	}
	g.nodes = append(g.nodes, manifestNode)
}

func (g *fileGenerator) add(message string, span sfc.Span) {
	g.diagnostics = append(g.diagnostics, Diagnostic{
		Path:    g.path,
		Message: message,
		Span:    span,
	})
}

func (g *projectGenerator) add(path string, message string, span sfc.Span) {
	g.diagnostics = append(g.diagnostics, Diagnostic{
		Path:    path,
		Message: message,
		Span:    span,
	})
}

func nodeDirectiveAttr(node *gotemplate.Node, kind gotemplate.DirectiveKind) *gotemplate.Attr {
	if node == nil || node.Kind != gotemplate.NodeElement {
		return nil
	}
	for i := range node.Attrs {
		attr := &node.Attrs[i]
		if attr.Kind == gotemplate.AttrDirective && attr.Directive == kind {
			return attr
		}
	}
	return nil
}

func nodeBindAttr(node *gotemplate.Node, argument string) *gotemplate.Attr {
	if node == nil || node.Kind != gotemplate.NodeElement {
		return nil
	}
	for i := range node.Attrs {
		attr := &node.Attrs[i]
		if attr.Kind == gotemplate.AttrBind && attr.Argument == argument {
			return attr
		}
	}
	return nil
}

func isNamedSlotAttr(attr gotemplate.Attr) bool {
	return (attr.Kind == gotemplate.AttrStatic && attr.Name == "name") ||
		(attr.Kind == gotemplate.AttrBind && attr.Argument == "name")
}

type nativeModel struct {
	ValueType string
	ValueName string
	EventName string
	Checked   bool
}

func nativeModelBinding(node *gotemplate.Node) (nativeModel, bool) {
	if node == nil || node.IsComponent {
		return nativeModel{}, false
	}

	switch node.Tag {
	case "input":
		inputType, _ := nodeStaticAttrValue(node, "type")
		switch inputType {
		case "", "text":
			return nativeModel{ValueType: "string", ValueName: "value", EventName: "input"}, true
		case "checkbox":
			return nativeModel{ValueType: "bool", ValueName: "checked", EventName: "change", Checked: true}, true
		default:
			return nativeModel{}, false
		}
	case "select":
		return nativeModel{ValueType: "string", ValueName: "value", EventName: "change"}, true
	default:
		return nativeModel{}, false
	}
}

func modelUnsupportedMessage(node *gotemplate.Node) string {
	if node != nil && node.IsComponent {
		return "component v-model is not supported"
	}
	if node != nil && node.Tag == "input" {
		if inputType, ok := nodeStaticAttrValue(node, "type"); ok {
			return fmt.Sprintf("v-model is not supported for input type %q", inputType)
		}
	}
	return "v-model is only supported on text inputs, checkboxes, and selects"
}

func nodeStaticAttrValue(node *gotemplate.Node, name string) (string, bool) {
	if node == nil {
		return "", false
	}
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrStatic && attr.Name == name && attr.HasValue {
			return attr.Value, true
		}
	}
	return "", false
}

func isStaticAssetAttr(node *gotemplate.Node, attr gotemplate.Attr) bool {
	if node == nil || node.IsComponent || attr.Kind != gotemplate.AttrStatic || !attr.HasValue {
		return false
	}

	switch node.Tag {
	case "audio", "embed", "iframe", "img", "script", "source", "track":
		return attr.Name == "src"
	case "input":
		inputType, _ := nodeStaticAttrValue(node, "type")
		return inputType == "image" && attr.Name == "src"
	case "link":
		return attr.Name == "href"
	case "object":
		return attr.Name == "data"
	case "use":
		return attr.Name == "href" || attr.Name == "xlink:href"
	case "video":
		return attr.Name == "src" || attr.Name == "poster"
	default:
		return false
	}
}

func (g *fileGenerator) pushLocals(locals map[string]string, localTypes map[string]string) func() {
	previous := g.locals
	previousTypes := g.localTypes
	next := make(map[string]string, len(previous)+len(locals))
	for name, value := range previous {
		next[name] = value
	}
	for name, value := range locals {
		next[name] = value
	}
	g.locals = next

	nextTypes := make(map[string]string, len(previousTypes)+len(localTypes))
	for name, value := range previousTypes {
		nextTypes[name] = value
	}
	for name, value := range localTypes {
		nextTypes[name] = value
	}
	g.localTypes = nextTypes
	return func() {
		g.locals = previous
		g.localTypes = previousTypes
	}
}

type forNames struct {
	source string
	nodes  string
	item   string
	index  string
}

func (g *fileGenerator) freshForNames(clause gotemplate.ForClause) forNames {
	used := g.usedGeneratedNames()
	source := freshIdentifier(used, "__tueItems")
	used[source] = true
	nodes := freshIdentifier(used, "__tueNodes")
	used[nodes] = true
	item := freshIdentifier(used, "__tueItem")
	used[item] = true
	index := "_"
	if clause.Index != "" {
		index = freshIdentifier(used, "__tueIndex")
	}
	return forNames{source: source, nodes: nodes, item: item, index: index}
}

func freshIdentifier(used map[string]bool, base string) string {
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s%d", base, i)
		if !used[name] {
			return name
		}
	}
}

func (g *fileGenerator) usedGeneratedNames() map[string]bool {
	names := map[string]bool{
		"component": true,
		"child":     true,
	}
	for _, name := range g.locals {
		names[name] = true
	}
	return names
}

func spanWithin(base sfc.Span, source string, start int, end int) sfc.Span {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(source) {
		start = len(source)
	}
	if end > len(source) {
		end = len(source)
	}

	return sfc.Span{
		Start: positionWithin(base.Start, source, start),
		End:   positionWithin(base.Start, source, end),
	}
}

func positionWithin(base sfc.Position, source string, offset int) sfc.Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}

	position := sfc.Position{
		Offset: base.Offset + offset,
		Line:   base.Line,
		Column: base.Column,
	}
	for index := 0; index < offset; index++ {
		if source[index] == '\n' {
			position.Line++
			position.Column = 1
			continue
		}
		position.Column++
	}
	return position
}

func renderJenniferFile(file *jen.File) ([]byte, error) {
	var buffer bytes.Buffer
	if err := file.Render(&buffer); err != nil {
		return nil, err
	}
	return format.Source(buffer.Bytes())
}

func componentFields(component *script.Component) map[string]script.Field {
	fields := make(map[string]script.Field)
	for _, prop := range component.Props {
		fields[prop.Field.Name] = prop.Field
	}
	for _, field := range component.State {
		fields[field.Name] = field
	}
	for _, field := range component.Refs {
		fields[field.Name] = field
	}
	for _, field := range component.Computed {
		fields[field.Name] = field
	}
	for _, field := range component.Resources {
		fields[field.Name] = field
	}
	return fields
}

func componentMethods(component *script.Component) map[string]script.Method {
	methods := make(map[string]script.Method)
	if component == nil {
		return methods
	}
	for _, method := range component.Methods {
		methods[method.Name] = method
	}
	return methods
}

func propValueType(prop script.Prop) string {
	if prop.Field.ValueType != "" {
		return prop.Field.ValueType
	}
	return prop.Field.Type
}

func zeroPropValue(prop script.Prop) (jen.Code, bool) {
	valueType := propValueType(prop)
	switch normalizeType(valueType) {
	case "string":
		return jen.Qual(tueImportPath, "PropOf").Call(jen.Lit("")), true
	case "bool":
		return jen.Qual(tueImportPath, "PropOf").Call(jen.False()), true
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "rune", "byte":
		return jen.Qual(tueImportPath, "PropOf").Call(jen.Lit(0)), true
	}

	typeCode, ok := renderTypeExpression(valueType)
	if !ok {
		return nil, false
	}
	return jen.Qual(tueImportPath, "PropOf").Call(jen.Op("*").Call(jen.New(typeCode))), true
}

func renderTypeExpression(typ string) (jen.Code, bool) {
	expr, err := goparser.ParseExpr(typ)
	if err != nil {
		return nil, false
	}
	return renderType(expr)
}

func renderType(expr ast.Expr) (jen.Code, bool) {
	switch typed := expr.(type) {
	case *ast.Ident:
		return jen.Id(typed.Name), true
	case *ast.SelectorExpr:
		base, ok := renderType(typed.X)
		if !ok {
			return nil, false
		}
		statement, ok := base.(*jen.Statement)
		if !ok {
			return nil, false
		}
		return statement.Dot(typed.Sel.Name), true
	case *ast.StarExpr:
		base, ok := renderType(typed.X)
		if !ok {
			return nil, false
		}
		return jen.Op("*").Add(base), true
	case *ast.ArrayType:
		element, ok := renderType(typed.Elt)
		if !ok || typed.Len != nil {
			return nil, false
		}
		return jen.Index().Add(element), true
	case *ast.MapType:
		key, ok := renderType(typed.Key)
		if !ok {
			return nil, false
		}
		value, ok := renderType(typed.Value)
		if !ok {
			return nil, false
		}
		return jen.Map(key).Add(value), true
	default:
		return nil, false
	}
}

func assignableType(expected string, actual string) bool {
	expected = normalizeType(expected)
	actual = normalizeType(actual)
	if expected == "" || actual == "" || expected == "unknown" || actual == "unknown" {
		return true
	}
	return expected == actual
}

func normalizeType(typ string) string {
	typ = strings.TrimSpace(typ)
	for strings.HasPrefix(typ, "*") {
		typ = strings.TrimSpace(strings.TrimPrefix(typ, "*"))
	}
	return typ
}

func displayType(typ string) string {
	if typ == "" {
		return "unknown"
	}
	return typ
}

func isNoArgFunc(typ string) bool {
	return strings.TrimSpace(typ) == "func()"
}

func hasRenderableComponentChildren(children []*gotemplate.Node) bool {
	for _, child := range children {
		if child == nil || child.Kind == gotemplate.NodeComment {
			continue
		}
		if child.Kind == gotemplate.NodeText && strings.TrimSpace(child.Text) == "" {
			continue
		}
		return true
	}
	return false
}

func generatedStem(path string) string {
	base := strings.TrimSuffix(filepath.ToSlash(path), filepath.Ext(path))
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}

func filePath(file File) string {
	if file.Path != "" {
		return file.Path
	}
	if file.Script != nil && file.Script.Path != "" {
		return file.Script.Path
	}
	return ""
}
