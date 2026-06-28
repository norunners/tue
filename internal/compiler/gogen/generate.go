package gogen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/dave/jennifer/jen"
	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
	"github.com/norunners/tue/internal/compiler/typecap"
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
	events    map[string]script.Event
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
		events := make(map[string]script.Event, len(component.Events))
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
		g.add(path, "missing component declaration", file.Template.Span)
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
	componentPath := ""
	renderPath := stem + "_render_tue.go"
	scopeAttr := ""
	if file.Style != nil && file.Style.Scoped {
		scopeAttr = scopeAttrFor(path)
	}

	scriptSource, ok := g.generatedScriptSource(file)
	if !ok {
		return
	}
	componentSource, ok := g.generatedComponentSource(file)
	if !ok {
		return
	}
	renderSource, nodes, ok := g.generatedRenderSource(file, scopeAttr)
	if !ok {
		return
	}

	g.files = append(g.files, GeneratedFile{Path: scriptPath, Source: scriptSource})
	if componentSource != nil {
		componentPath = stem + "_component_tue.go"
		g.files = append(g.files, GeneratedFile{Path: componentPath, Source: componentSource})
	}
	g.files = append(g.files, GeneratedFile{Path: renderPath, Source: renderSource})
	g.manifest.Files = append(g.manifest.Files, ManifestFile{
		Source:        path,
		Component:     file.Script.Component.Name,
		ScriptFile:    scriptPath,
		ComponentFile: componentPath,
		RenderFile:    renderPath,
		ScopeAttr:     scopeAttr,
		Nodes:         nodes,
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

	fset := token.NewFileSet()
	parsed, err := goparser.ParseFile(fset, filePath(file), source, goparser.ParseComments|goparser.AllErrors)
	if err != nil {
		g.add(filePath(file), fmt.Sprintf("parse generated script: %v", err), file.Script.PackageSpan)
		return nil, false
	}
	if file.Script.Component.GeneratedType != "" {
		if !injectGeneratedField(parsed, file.Script.Component) {
			g.add(filePath(file), fmt.Sprintf("component %q struct not found while generating input storage", file.Script.Component.Name), file.Script.Component.NameSpan)
			return nil, false
		}
	}

	var generated bytes.Buffer
	generated.WriteString("// Code generated by tue; DO NOT EDIT.\n\n")
	if err := format.Node(&generated, fset, parsed); err != nil {
		g.add(filePath(file), fmt.Sprintf("format generated script: %v", err), file.Script.PackageSpan)
		return nil, false
	}
	if bytes := generated.Bytes(); len(bytes) == 0 || bytes[len(bytes)-1] != '\n' {
		generated.WriteByte('\n')
	}
	return generated.Bytes(), true
}

func injectGeneratedField(file *ast.File, component *script.Component) bool {
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != component.Name {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				return false
			}
			field := &ast.Field{
				Names: []*ast.Ident{ast.NewIdent(script.GeneratedFieldName())},
				Type:  ast.NewIdent(component.GeneratedType),
			}
			structure.Fields.List = append([]*ast.Field{field}, structure.Fields.List...)
			return true
		}
	}
	return false
}

func (g *projectGenerator) generatedComponentSource(file File) ([]byte, bool) {
	component := file.Script.Component
	if component.GeneratedType == "" {
		return nil, true
	}

	generated := jen.NewFile(file.Script.PackageName)
	generated.HeaderComment("Code generated by tue; DO NOT EDIT.")
	imports := generatedImports(file.Script.Imports)
	for alias, path := range imports {
		generated.ImportName(path, alias)
	}
	fields := make([]jen.Code, 0, len(component.Props)+len(component.Events)+len(component.States)+len(component.Computed)+len(component.Resources)+1)
	for _, prop := range component.Props {
		valueType, ok := renderGeneratedTypeExpression(prop.Type, imports)
		if !ok {
			g.add(filePath(file), fmt.Sprintf("component prop %q has unsupported type %q", prop.Name, prop.Type), prop.TypeSpan)
			return nil, false
		}
		fields = append(fields, jen.Id(script.PropFieldName(prop.GoName)).Func().Params().Params(valueType, jen.Bool()))
	}
	if len(component.Props) != 0 {
		fields = append(fields, jen.Id(script.InputVersionFieldName()).Qual(tueImportPath, "State").Types(jen.Int()))
	}
	for _, event := range component.Events {
		callbackType, ok := renderGeneratedTypeExpression(event.FunctionType(), imports)
		if !ok {
			g.add(filePath(file), fmt.Sprintf("component event %q has unsupported function type %q", event.Name, event.FunctionType()), event.Span)
			return nil, false
		}
		fields = append(fields, jen.Id(script.EventFieldName(event.GoName)).Add(callbackType))
	}
	for _, state := range component.States {
		valueType, ok := renderGeneratedTypeExpression(state.Type, imports)
		if !ok {
			g.add(filePath(file), fmt.Sprintf("component state %q has unsupported type %q", state.Name, state.Type), state.TypeSpan)
			return nil, false
		}
		fields = append(fields, jen.Id(script.StateFieldName(state.GoName)).Qual(tueImportPath, "State").Types(valueType))
	}
	for _, computed := range component.Computed {
		valueType, ok := renderGeneratedTypeExpression(computed.Type, imports)
		if !ok {
			g.add(filePath(file), fmt.Sprintf("component computed %q has unsupported type %q", computed.Name, computed.Type), computed.TypeSpan)
			return nil, false
		}
		fields = append(fields, jen.Id(script.ComputedFieldName(computed.GoName)).Qual(tueImportPath, "Computed").Types(valueType))
	}
	for _, resource := range component.Resources {
		valueType, ok := renderGeneratedTypeExpression(resource.Type, imports)
		if !ok {
			g.add(filePath(file), fmt.Sprintf("component resource %q has unsupported type %q", resource.Name, resource.Type), resource.TypeSpan)
			return nil, false
		}
		fields = append(fields, jen.Id(script.ResourceFieldName(resource.GoName)).Qual(tueImportPath, "Resource").Types(valueType))
	}
	generated.Type().Id(component.GeneratedType).Struct(fields...)
	initialValues := make([]jen.Code, 0, len(component.States)+1)
	if len(component.Props) != 0 {
		initialValues = append(initialValues,
			jen.Id(script.InputVersionFieldName()).Op(":").Qual(tueImportPath, "StateOf").Call(jen.Lit(0)),
		)
	}
	for _, state := range component.States {
		valueType, _ := renderGeneratedTypeExpression(state.Type, imports)
		initialValues = append(initialValues,
			jen.Id(script.StateFieldName(state.GoName)).Op(":").Qual(tueImportPath, "StateOf").Call(jen.Op("*").New(valueType)),
		)
	}
	generated.Func().Id(script.GeneratedConstructorName(component.Name)).Params().Id(component.GeneratedType).Block(
		jen.Return(jen.Id(component.GeneratedType).Values(initialValues...)),
	)

	for _, prop := range component.Props {
		valueType, _ := renderGeneratedTypeExpression(prop.Type, imports)
		fieldName := script.PropFieldName(prop.GoName)
		presenceName := script.PropOKName(prop.GoName)
		presenceStatements := []jen.Code{
			jen.If(jen.Id("component").Op("==").Nil()).Block(
				jen.Var().Id("zero").Add(valueType),
				jen.Return(jen.Id("zero"), jen.False()),
			),
			jen.If(jen.Id("component").Dot(script.GeneratedFieldName()).Dot(script.InputVersionFieldName()).Op("!=").Nil()).Block(
				jen.Id("_").Op("=").Id("component").Dot(script.GeneratedFieldName()).Dot(script.InputVersionFieldName()).Dot("Get").Call(),
			),
			jen.If(jen.Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Op("==").Nil()).Block(
				jen.Var().Id("zero").Add(valueType),
				jen.Return(jen.Id("zero"), jen.False()),
			),
			jen.List(jen.Id("value"), jen.Id("ok")).Op(":=").Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Call(),
			jen.If(jen.Id("ok")).Block(jen.Return(jen.Id("value"), jen.True())),
			jen.Return(jen.Id("value"), jen.False()),
		}
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(presenceName).Params().Params(valueType, jen.Bool()).Block(presenceStatements...)
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(script.PropGetterName(prop.GoName)).Params().Add(valueType).Block(
			jen.List(jen.Id("value"), jen.Id("_")).Op(":=").Id("component").Dot(presenceName).Call(),
			jen.Return(jen.Id("value")),
		)
	}
	for _, event := range component.Events {
		parameters, arguments, ok := renderEventParameters(event, imports)
		if !ok {
			g.add(filePath(file), fmt.Sprintf("component event %q has unsupported function type %q", event.Name, event.FunctionType()), event.Span)
			return nil, false
		}
		fieldName := script.EventFieldName(event.GoName)
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(script.EventMethodName(event.GoName)).Params(parameters...).Bool().Block(
			jen.If(
				jen.Id("component").Op("==").Nil().Op("||").Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Op("==").Nil(),
			).Block(jen.Return(jen.False())),
			jen.Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Call(arguments...),
			jen.Return(jen.True()),
		)
	}
	for _, state := range component.States {
		valueType, _ := renderGeneratedTypeExpression(state.Type, imports)
		fieldName := script.StateFieldName(state.GoName)
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(script.StateGetterName(state.GoName)).Params().Add(valueType).Block(
			jen.If(
				jen.Id("component").Op("==").Nil().Op("||").Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Op("==").Nil(),
			).Block(
				jen.Var().Id("zero").Add(valueType),
				jen.Return(jen.Id("zero")),
			),
			jen.Return(jen.Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Dot("Get").Call()),
		)
		setterStatements := []jen.Code{
			jen.If(
				jen.Id("component").Op("==").Nil().Op("||").Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Op("==").Nil(),
			).Block(jen.Return()),
			jen.Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Dot("Set").Call(jen.Id("value")),
		}
		setterStatements = append(setterStatements, generatedResourceReloadStatements(component, "component")...)
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(script.StateSetterName(state.GoName)).Params(jen.Id("value").Add(valueType)).Block(setterStatements...)
	}
	for _, computed := range component.Computed {
		valueType, _ := renderGeneratedTypeExpression(computed.Type, imports)
		fieldName := script.ComputedFieldName(computed.GoName)
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(script.ComputedGetterName(computed.GoName)).Params().Add(valueType).Block(
			jen.If(
				jen.Id("component").Op("==").Nil().Op("||").Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Op("==").Nil(),
			).Block(
				jen.Var().Id("zero").Add(valueType),
				jen.Return(jen.Id("zero")),
			),
			jen.Return(jen.Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Dot("Get").Call()),
		)
	}
	for _, resource := range component.Resources {
		valueType, _ := renderGeneratedTypeExpression(resource.Type, imports)
		fieldName := script.ResourceFieldName(resource.GoName)
		okName := script.ResourceOKName(resource.GoName)
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(okName).Params().Params(valueType, jen.Bool()).Block(
			jen.If(
				jen.Id("component").Op("==").Nil().Op("||").Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Op("==").Nil(),
			).Block(
				jen.Var().Id("zero").Add(valueType),
				jen.Return(jen.Id("zero"), jen.False()),
			),
			jen.Return(jen.Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Dot("Value").Call()),
		)
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(script.ResourceGetterName(resource.GoName)).Params().Add(valueType).Block(
			jen.List(jen.Id("value"), jen.Id("_")).Op(":=").Id("component").Dot(okName).Call(),
			jen.Return(jen.Id("value")),
		)
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(script.ResourceLoadingName(resource.GoName)).Params().Bool().Block(
			jen.If(
				jen.Id("component").Op("==").Nil().Op("||").Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Op("==").Nil(),
			).Block(jen.Return(jen.False())),
			jen.Return(jen.Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Dot("Loading").Call()),
		)
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(script.ResourceErrorName(resource.GoName)).Params().Error().Block(
			jen.If(
				jen.Id("component").Op("==").Nil().Op("||").Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Op("==").Nil(),
			).Block(jen.Return(jen.Nil())),
			jen.Return(jen.Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Dot("Error").Call()),
		)
		generated.Func().Params(jen.Id("component").Op("*").Id(component.Name)).Id(script.ResourceReloadName(resource.GoName)).Params().Block(
			jen.If(
				jen.Id("component").Op("==").Nil().Op("||").Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Op("==").Nil(),
			).Block(jen.Return()),
			jen.Id("component").Dot(script.GeneratedFieldName()).Dot(fieldName).Dot("Reload").Call(),
		)
	}

	source, err := renderJenniferFile(generated)
	if err != nil {
		g.add(filePath(file), fmt.Sprintf("render generated component declaration: %v", err), component.Span)
		return nil, false
	}
	return source, true
}

func generatedImports(imports []script.Import) map[string]string {
	byName := make(map[string]string)
	for _, imported := range imports {
		if imported.Name == "_" || imported.Name == "." {
			continue
		}
		name := imported.Name
		if name == "" {
			name = path.Base(imported.Path)
		}
		byName[name] = imported.Path
	}
	return byName
}

func renderEventParameters(event script.Event, imports map[string]string) ([]jen.Code, []jen.Code, bool) {
	parameters := make([]jen.Code, 0, len(event.Parameters))
	arguments := make([]jen.Code, 0, len(event.Parameters))
	for index, parameter := range event.Parameters {
		parameterType := parameter.Type
		variadic := strings.HasPrefix(parameterType, "...")
		if variadic {
			parameterType = strings.TrimPrefix(parameterType, "...")
		}
		typeCode, ok := renderGeneratedTypeExpression(parameterType, imports)
		if !ok {
			return nil, nil, false
		}
		if variadic {
			typeCode = jen.Op("...").Add(typeCode)
		}
		parameters = append(parameters, jen.Id(parameter.Name).Add(typeCode))
		argument := jen.Id(parameter.Name)
		if variadic && index == len(event.Parameters)-1 {
			argument = argument.Op("...")
		}
		arguments = append(arguments, argument)
	}
	return parameters, arguments, true
}

func (g *projectGenerator) generatedRenderSource(file File, scopeAttr string) ([]byte, []ManifestNode, bool) {
	renderer := fileGenerator{
		path:       filePath(file),
		file:       jen.NewFile(file.Script.PackageName),
		component:  file.Script.Component,
		components: g.components,
		fields:     componentFields(file.Script.Component),
		methods:    componentMethods(file.Script.Component),
		typeFields: structFieldMaps(file.Script.Structs),
		comparable: comparableTypeMap(file.Script.Types),
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
	typeFields  map[string]map[string]script.Field
	comparable  map[string]bool
	scopeAttr   string
	assets      *assetPipeline
	locals      map[string]string
	localTypes  map[string]string
	nodes       []ManifestNode
	diagnostics []Diagnostic
}

func (g *fileGenerator) generate(tree *gotemplate.Tree) {
	componentName := g.component.Name
	componentValues := []jen.Code(nil)
	if g.component.GeneratedType != "" {
		componentValues = append(componentValues,
			jen.Id(script.GeneratedFieldName()).Op(":").Id(script.GeneratedConstructorName(g.component.Name)).Call(),
		)
	}
	compArgs := []jen.Code{jen.Id("component"), jen.Id("render" + componentName)}
	if initializer := generatedReactiveInitializer(g.component, "component"); initializer != nil {
		compArgs = append(compArgs, initializer)
	}
	g.file.Func().Id("New"+componentName).Params().Op("*").Qual(tueImportPath, "CompInstance").Block(
		jen.Id("component").Op(":=").Op("&").Id(componentName).Values(componentValues...),
		jen.Return(jen.Qual(tueImportPath, "CompOf").Call(compArgs...)),
	)
	g.file.Line()
	g.file.Func().Id("render"+componentName).Params(jen.Id("component").Op("*").Id(componentName)).Qual(tueImportPath, "VNode").Block(
		jen.Return(g.renderNodes(tree.Nodes)),
	)
}

func (g *fileGenerator) renderNodes(nodes []*gotemplate.Node) jen.Code {
	rendered := g.renderNodeList(nodes)
	if len(rendered) == 1 {
		return rendered[0]
	}
	return jen.Qual(tueImportPath, "Fragment").Call(g.vnodeSlice(rendered))
}

func (g *fileGenerator) renderNodeList(nodes []*gotemplate.Node) []jen.Code {
	rendered := make([]jen.Code, 0, len(nodes))
	for index := 0; index < len(nodes); index++ {
		node := nodes[index]
		if g.ignorableControlSibling(node) {
			continue
		}
		if attr, ok := nodeDirectiveAttr(node, gotemplate.DirectiveCase); ok {
			g.add("v-case must be a direct child of v-switch", attr.DirectiveSpan)
			continue
		}
		if attr, ok := nodeDirectiveAttr(node, gotemplate.DirectiveDefault); ok {
			g.add("v-default must be a direct child of v-switch", attr.DirectiveSpan)
			continue
		}
		if attr, ok := nodeDirectiveAttr(node, gotemplate.DirectiveElseIf); ok {
			g.add("v-else-if must follow v-if or v-else-if", attr.DirectiveSpan)
			continue
		}
		if attr, ok := nodeDirectiveAttr(node, gotemplate.DirectiveElse); ok {
			g.add("v-else must follow v-if or v-else-if", attr.DirectiveSpan)
			continue
		}
		if attr, ok := nodeDirectiveAttr(node, gotemplate.DirectiveIf); ok {
			branches, end := g.conditionalChain(nodes, index, *attr)
			if len(branches) > 1 {
				if code, ok := g.renderConditionalChain(branches); ok {
					rendered = append(rendered, code)
				}
				index = end
				continue
			}
		}
		if code, ok := g.renderNode(node); ok {
			rendered = append(rendered, code)
		}
	}
	return rendered
}

type conditionalBranch struct {
	node *gotemplate.Node
	attr gotemplate.Attr
}

func (g *fileGenerator) conditionalChain(nodes []*gotemplate.Node, start int, ifAttr gotemplate.Attr) ([]conditionalBranch, int) {
	branches := []conditionalBranch{{node: nodes[start], attr: ifAttr}}
	end := start
	for index := start + 1; index < len(nodes); index++ {
		node := nodes[index]
		if g.ignorableControlSibling(node) {
			continue
		}
		if attr, ok := nodeDirectiveAttr(node, gotemplate.DirectiveElseIf); ok {
			branches = append(branches, conditionalBranch{node: node, attr: *attr})
			end = index
			continue
		}
		if attr, ok := nodeDirectiveAttr(node, gotemplate.DirectiveElse); ok {
			branches = append(branches, conditionalBranch{node: node, attr: *attr})
			end = index
		}
		break
	}
	return branches, end
}

func (g *fileGenerator) ignorableControlSibling(node *gotemplate.Node) bool {
	if node == nil {
		return true
	}
	switch node.Kind {
	case gotemplate.NodeComment:
		return true
	case gotemplate.NodeText:
		return strings.TrimSpace(node.Text) == ""
	default:
		return false
	}
}

func (g *fileGenerator) renderNode(node *gotemplate.Node) (jen.Code, bool) {
	if node == nil {
		return nil, false
	}
	attr, hasFor := nodeDirectiveAttr(node, gotemplate.DirectiveFor)
	if hasFor {
		return g.renderFor(node, *attr)
	}

	return g.renderNodeAfterFor(node)
}

func (g *fileGenerator) renderNodeAfterFor(node *gotemplate.Node) (jen.Code, bool) {
	if attr, ok := nodeDirectiveAttr(node, gotemplate.DirectiveSwitch); ok {
		return g.renderSwitch(node, *attr)
	}
	attr, hasIf := nodeDirectiveAttr(node, gotemplate.DirectiveIf)
	if hasIf {
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

	keyAttr, hasKey := nodeBindAttr(node, "key")
	if !hasKey {
		g.add("v-for requires a :key attribute", attr.DirectiveSpan)
		return nil, false
	}

	sourceSpan := spanWithin(attr.ExpressionSpan, attr.Expression, clause.SourceStart, clause.SourceEnd)
	source, sourceOK := g.renderExpressionFor("v-for source", clause.Source, sourceSpan)
	if !sourceOK {
		return nil, false
	}

	names := g.freshForNames(*clause)
	sourceType := g.expressionType(clause.Source)
	types, iterable := typecap.IterableFor(sourceType)
	if !iterable {
		g.add(fmt.Sprintf("v-for source must be iterable, got %s", displayType(sourceType)), sourceSpan)
		return nil, false
	}

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
	condition, conditionOK := g.renderConditionalExpression(attr)
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

func (g *fileGenerator) renderConditionalChain(branches []conditionalBranch) (jen.Code, bool) {
	ok := true
	statements := make([]jen.Code, 0, len(branches)+1)
	for index, branch := range branches {
		if switchAttr, ok := nodeDirectiveAttr(branch.node, gotemplate.DirectiveSwitch); ok {
			g.add("v-switch cannot be combined with v-if, v-else-if, or v-else", switchAttr.DirectiveSpan)
			ok = false
		}
		if caseAttr, ok := nodeDirectiveAttr(branch.node, gotemplate.DirectiveCase); ok {
			g.add("v-case must be a direct child of v-switch", caseAttr.DirectiveSpan)
			ok = false
		}
		if defaultAttr, ok := nodeDirectiveAttr(branch.node, gotemplate.DirectiveDefault); ok {
			g.add("v-default must be a direct child of v-switch", defaultAttr.DirectiveSpan)
			ok = false
		}
		if _, ok := nodeDirectiveAttr(branch.node, gotemplate.DirectiveFor); ok {
			switch {
			case index == 0 && len(branches) > 1:
				next := branches[1].attr
				g.add(fmt.Sprintf("%s cannot follow a conditional branch that also has v-for; use a <template v-for> wrapper", next.RawName), next.DirectiveSpan)
			case branch.attr.Directive == gotemplate.DirectiveElseIf:
				g.add("v-else-if cannot be combined with v-for; use a <template v-for> wrapper", branch.attr.DirectiveSpan)
			case branch.attr.Directive == gotemplate.DirectiveElse:
				g.add("v-else cannot be combined with v-for; use a <template v-for> wrapper", branch.attr.DirectiveSpan)
			}
			ok = false
		}
	}
	if !ok {
		return nil, false
	}

	for _, branch := range branches {
		rendered, renderedOK := g.renderNodeBody(branch.node)
		if !renderedOK {
			ok = false
			continue
		}
		if branch.attr.Directive == gotemplate.DirectiveElse {
			statements = append(statements, jen.Return(rendered))
			continue
		}
		condition, conditionOK := g.renderConditionalExpression(branch.attr)
		if !conditionOK {
			ok = false
			continue
		}
		statements = append(statements, jen.If(condition).Block(jen.Return(rendered)))
	}
	if !ok {
		return nil, false
	}
	if branches[len(branches)-1].attr.Directive != gotemplate.DirectiveElse {
		statements = append(statements, jen.Return(g.emptyFragment()))
	}
	return jen.Func().Params().Qual(tueImportPath, "VNode").Block(statements...).Call(), true
}

func (g *fileGenerator) renderConditionalExpression(attr gotemplate.Attr) (jen.Code, bool) {
	condition, ok := g.renderExpressionFor(attr.RawName, attr.Expression, attr.ExpressionSpan)
	if !ok {
		return nil, false
	}
	conditionType := g.expressionType(attr.Expression)
	if !typecap.Assignable("bool", conditionType) {
		g.add(fmt.Sprintf("%s expects bool, got %s", attr.RawName, displayType(conditionType)), attr.ExpressionSpan)
		return nil, false
	}
	return condition, true
}

func (g *fileGenerator) renderSwitch(node *gotemplate.Node, attr gotemplate.Attr) (jen.Code, bool) {
	ok := true
	hostValid := node.Tag == "template" && !node.IsComponent
	if !hostValid {
		g.add("v-switch is only supported on <template>", attr.DirectiveSpan)
		ok = false
	}
	if conditionalAttr, ok := nodeConditionalAttr(node); ok {
		g.add("v-switch cannot be combined with v-if, v-else-if, or v-else", conditionalAttr.DirectiveSpan)
		ok = false
	}
	if caseAttr, ok := nodeDirectiveAttr(node, gotemplate.DirectiveCase); ok {
		g.add("v-switch cannot be combined with v-case on the same element", caseAttr.DirectiveSpan)
		ok = false
	}
	if defaultAttr, ok := nodeDirectiveAttr(node, gotemplate.DirectiveDefault); ok {
		g.add("v-switch cannot be combined with v-default on the same element", defaultAttr.DirectiveSpan)
		ok = false
	}

	switchValue, valueOK := g.renderExpressionFor("v-switch", attr.Expression, attr.ExpressionSpan)
	switchType := g.expressionType(attr.Expression)
	if !typecap.Comparable(switchType, g.comparable) {
		g.add(fmt.Sprintf("v-switch expression type %s is not comparable", displayType(switchType)), attr.ExpressionSpan)
		ok = false
	}
	if !valueOK {
		ok = false
	}
	if !hostValid {
		return nil, false
	}

	branchCount := 0
	defaultSeen := false
	switchName := freshIdentifier(g.usedGeneratedNames(), "__tueSwitch")
	caseStatements := make([]jen.Code, 0, len(node.Children))
	var defaultRendered jen.Code
	for _, branch := range node.Children {
		if g.ignorableControlSibling(branch) {
			continue
		}
		branchCount++

		caseAttr, hasCase := nodeDirectiveAttr(branch, gotemplate.DirectiveCase)
		defaultAttr, hasDefault := nodeDirectiveAttr(branch, gotemplate.DirectiveDefault)
		branchRenderable := true
		if conditionalAttr, ok := nodeConditionalAttr(branch); ok {
			g.add("v-switch branches cannot combine v-case or v-default with v-if, v-else-if, or v-else", conditionalAttr.DirectiveSpan)
			branchRenderable = false
			ok = false
		}
		if nestedSwitch, ok := nodeDirectiveAttr(branch, gotemplate.DirectiveSwitch); ok {
			g.add("a v-switch branch must nest another v-switch inside its content", nestedSwitch.DirectiveSpan)
			branchRenderable = false
			ok = false
		}
		switch {
		case hasCase && hasDefault:
			g.add("v-switch branch cannot use both v-case and v-default", defaultAttr.DirectiveSpan)
			ok = false
		case hasCase:
			if defaultSeen {
				g.add("v-case must appear before v-default", caseAttr.DirectiveSpan)
				ok = false
			}
			caseValue, caseOK := g.renderExpressionFor("v-case", caseAttr.Expression, caseAttr.ExpressionSpan)
			caseType := g.expressionType(caseAttr.Expression)
			if !typecap.SwitchCompatible(switchType, caseType) {
				g.add(fmt.Sprintf("v-case expects %s, got %s", displayType(switchType), displayType(caseType)), caseAttr.ExpressionSpan)
				caseOK = false
			}
			var rendered jen.Code
			renderedOK := false
			if branchRenderable {
				rendered, renderedOK = g.renderNode(branch)
			}
			if caseOK && renderedOK {
				caseStatements = append(caseStatements, jen.If(jen.Id(switchName).Op("==").Add(caseValue)).Block(jen.Return(rendered)))
			} else {
				ok = false
			}
		case hasDefault:
			if defaultSeen {
				g.add("v-switch may only have one v-default", defaultAttr.DirectiveSpan)
				ok = false
			}
			defaultSeen = true
			var rendered jen.Code
			renderedOK := false
			if branchRenderable {
				rendered, renderedOK = g.renderNode(branch)
			}
			if renderedOK {
				defaultRendered = rendered
			} else {
				ok = false
			}
		default:
			g.add("v-switch children must use v-case or v-default", branch.Span)
			ok = false
		}
	}
	if branchCount == 0 {
		g.add("v-switch requires at least one v-case or v-default child", attr.DirectiveSpan)
		ok = false
	}
	if !ok {
		return nil, false
	}

	statements := append([]jen.Code{jen.Id(switchName).Op(":=").Add(switchValue)}, caseStatements...)
	if defaultSeen {
		statements = append(statements, jen.Return(defaultRendered))
	} else {
		statements = append(statements, jen.Return(g.emptyFragment()))
	}
	return jen.Func().Params().Qual(tueImportPath, "VNode").Block(statements...).Call(), true
}

func (g *fileGenerator) renderElement(node *gotemplate.Node) (jen.Code, bool) {
	if node.Tag == "template" {
		return g.renderTemplate(node)
	}
	if node.Tag == "slot" {
		return g.renderSlot(node)
	}
	if node.IsComponent {
		return g.renderComponent(node)
	}

	htmlAttr, hasHTML := nodeDirectiveAttr(node, gotemplate.DirectiveHTML)
	attrs, events, ok := g.renderAttrsAndEvents(node)
	if !ok {
		return nil, false
	}
	attrs = g.withScopeAttr(attrs)

	if hasHTML {
		innerHTML, htmlOK := g.renderHTMLBinding(*htmlAttr)
		if !htmlOK {
			return nil, false
		}

		g.recordNode(node)
		return jen.Qual(tueImportPath, "ElementWithTrustedHTML").Call(
			jen.Lit(node.Tag),
			g.attributeSlice(attrs),
			g.eventSlice(events),
			innerHTML,
		), true
	}

	children := g.renderNodeList(node.Children)

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

func (g *fileGenerator) renderTemplate(node *gotemplate.Node) (jen.Code, bool) {
	ok := true
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrDirective && isControlDirective(attr.Directive) {
			continue
		}
		if attr.Kind == gotemplate.AttrBind && attr.Argument == "key" {
			continue
		}
		g.add(fmt.Sprintf("template attribute %q generation is not supported", attr.RawName), attr.Span)
		ok = false
	}
	if !ok {
		return nil, false
	}
	return g.renderNodes(node.Children), true
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
	childValues := []jen.Code(nil)
	if child.component.GeneratedType != "" {
		childValues = append(childValues,
			jen.Id(script.GeneratedFieldName()).Op(":").Id(script.GeneratedConstructorName(child.component.Name)).Call(),
		)
	}
	statements := []jen.Code{
		jen.Id("child").Op(":=").Op("&").Id(child.component.Name).Values(childValues...),
	}
	for _, field := range fields {
		statements = append(statements, jen.Id("child").Dot(script.GeneratedFieldName()).Dot(field.Name).Op("=").Add(field.Value))
	}
	compArgs := []jen.Code{jen.Id("child"), jen.Id("render" + child.component.Name)}
	if initializer := generatedReactiveInitializer(child.component, "child"); initializer != nil {
		compArgs = append(compArgs, initializer)
	}
	statements = append(statements,
		jen.Id("childComp").Op(":=").Qual(tueImportPath, "CompOf").Call(compArgs...),
	)
	if defaultSlot != nil {
		statements = append(statements, jen.Id("childComp").Dot("DefaultSlot").Op("=").Add(defaultSlot))
	}
	statements = append(statements, jen.Return(jen.Id("childComp")))

	component := jen.Qual(tueImportPath, "ComponentWithUpdate").Call(
		jen.Lit(node.Tag),
		jen.Func().Params().Op("*").Qual(tueImportPath, "CompInstance").Block(statements...),
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
			statements = append(statements, jen.Id("child").Dot(script.GeneratedFieldName()).Dot(field.Name).Op("=").Add(field.Value))
		}
		if len(child.component.Props) != 0 {
			inputVersion := func() *jen.Statement {
				return jen.Id("child").Dot(script.GeneratedFieldName()).Dot(script.InputVersionFieldName())
			}
			statements = append(statements,
				jen.If(inputVersion().Op("!=").Nil()).Block(
					inputVersion().Dot("Set").Call(inputVersion().Dot("Get").Call().Op("+").Lit(1)),
				),
			)
			statements = append(statements, generatedResourceReloadStatements(child.component, "child")...)
		}
	}
	if defaultSlot == nil {
		statements = append(statements, jen.Id("childComp").Dot("DefaultSlot").Op("=").Nil())
	} else {
		statements = append(statements, jen.Id("childComp").Dot("DefaultSlot").Op("=").Add(defaultSlot))
	}
	return jen.Func().Params(jen.Id("childComp").Op("*").Qual(tueImportPath, "CompInstance")).Block(statements...)
}

func (g *fileGenerator) renderSlot(node *gotemplate.Node) (jen.Code, bool) {
	ok := true
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrDirective && isControlDirective(attr.Directive) {
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
			if isControlDirective(attr.Directive) {
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
			value = jen.Nil()
		}
		fields = append(fields, componentField{Name: script.PropFieldName(prop.GoName), Value: value})
	}
	for _, event := range child.component.Events {
		fieldName := script.EventFieldName(event.GoName)
		value, found := events[fieldName]
		if !found {
			value = jen.Nil()
		}
		fields = append(fields, componentField{Name: fieldName, Value: value})
	}
	return fields, ok
}

type componentEventField struct {
	Name  string
	Value jen.Code
}

func (g *fileGenerator) renderComponentEventField(node *gotemplate.Node, child componentBinding, attr gotemplate.Attr) (*componentEventField, bool) {
	event, found := child.events[attr.Argument]
	if !found {
		g.add(fmt.Sprintf("component %q has no event %q", node.Tag, attr.Argument), attr.ArgumentSpan)
		return nil, false
	}
	eventType := event.FunctionType()
	signature, ok := typecap.ParseFunction(eventType)
	if !ok {
		g.add(fmt.Sprintf("component %q event %q has invalid function type %q", node.Tag, attr.Argument, eventType), attr.ArgumentSpan)
		return nil, false
	}
	if len(signature.Results) != 0 {
		g.add(fmt.Sprintf("component %q event %q must not return values", node.Tag, attr.Argument), attr.ArgumentSpan)
		return nil, false
	}

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
	if !signature.Matches(method.ParameterTypes(), method.ResultTypes()) {
		g.add(fmt.Sprintf("event handler %q must have signature %s", methodName, signature.String()), attr.ExpressionSpan)
		return nil, false
	}

	return &componentEventField{
		Name:  script.EventFieldName(event.GoName),
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
	if !typecap.Assignable(expected, actual) {
		g.add(
			fmt.Sprintf("component %q prop %q expects %s, got %s", componentName, prop.Name, displayType(expected), displayType(actual)),
			span,
		)
		return nil, false
	}
	if !attr.HasValue {
		return renderPropGetter(prop, jen.True())
	}
	return renderPropGetter(prop, jen.Lit(attr.Value))
}

func (g *fileGenerator) renderBoundComponentProp(prop script.Prop, attr gotemplate.Attr) (jen.Code, bool) {
	expression, ok := g.renderExpression(attr.Expression, attr.ExpressionSpan)
	if !ok {
		return nil, false
	}
	return renderPropGetter(prop, expression)
}

func renderPropGetter(prop script.Prop, value jen.Code) (jen.Code, bool) {
	valueType, ok := renderTypeExpression(propValueType(prop))
	if !ok {
		return nil, false
	}
	return jen.Func().Params().Params(valueType, jen.Bool()).Block(jen.Return(value, jen.True())), true
}

func (g *fileGenerator) renderAttrsAndEvents(node *gotemplate.Node) ([]jen.Code, []jen.Code, bool) {
	attrs := make([]jen.Code, 0, len(node.Attrs))
	events := make([]jen.Code, 0, len(node.Attrs))
	_, hasClassBinding := nodeBindAttr(node, "class")
	_, hasStyleBinding := nodeBindAttr(node, "style")
	modelAttr, hasModel := nodeDirectiveAttr(node, gotemplate.DirectiveModel)
	modelControlledAttr := ""
	if hasModel {
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
			attrValue, attrOK := g.renderNativeAttrBinding(attr)
			if !attrOK {
				ok = false
				continue
			}
			attrs = append(attrs, attrValue)
		case gotemplate.AttrEvent:
			event, eventOK := g.renderEvent(attr)
			if !eventOK {
				ok = false
				continue
			}
			events = append(events, event)
		case gotemplate.AttrDirective:
			if isControlDirective(attr.Directive) || attr.Directive == gotemplate.DirectiveModel || attr.Directive == gotemplate.DirectiveHTML {
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
	if hasModel {
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

func (g *fileGenerator) renderHTMLBinding(attr gotemplate.Attr) (jen.Code, bool) {
	expression, ok := g.renderExpressionFor("v-html", attr.Expression, attr.ExpressionSpan)
	if !ok {
		return nil, false
	}

	valueType := g.expressionType(attr.Expression)
	if !typecap.TrustedHTML(valueType) {
		g.add(fmt.Sprintf("v-html expects tue.TrustedHTML, got %s", displayType(valueType)), attr.ExpressionSpan)
		return nil, false
	}
	return expression, true
}

func (g *fileGenerator) renderClassBinding(attr gotemplate.Attr) (jen.Code, bool) {
	expression, ok := g.renderExpressionFor("class binding", attr.Expression, attr.ExpressionSpan)
	if !ok {
		return nil, false
	}

	valueType := g.expressionType(attr.Expression)
	if !typecap.Assignable("string", valueType) {
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
	if !typecap.Assignable("string", valueType) {
		g.add(fmt.Sprintf("style binding expects string, got %s", displayType(valueType)), attr.ExpressionSpan)
		return nil, false
	}
	return expression, true
}

func (g *fileGenerator) renderNativeAttrBinding(attr gotemplate.Attr) (jen.Code, bool) {
	subject := fmt.Sprintf("bound attribute %q", attr.RawName)
	expression, ok := g.renderExpressionFor(subject, attr.Expression, attr.ExpressionSpan)
	if !ok {
		return nil, false
	}

	valueType := g.expressionType(attr.Expression)
	if !typecap.Assignable("string", valueType) {
		g.add(fmt.Sprintf("%s expects string, got %s", subject, displayType(valueType)), attr.ExpressionSpan)
		return nil, false
	}
	return jen.Qual(tueImportPath, "Attr").Call(jen.Lit(attr.Argument), expression), true
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

func (g *fileGenerator) renderModelBinding(node *gotemplate.Node, attr gotemplate.Attr) ([]jen.Code, jen.Code, bool) {
	binding, ok := nativeModelBinding(node)
	if !ok {
		g.add(modelUnsupportedMessage(node), attr.DirectiveSpan)
		return nil, nil, false
	}

	actualType := g.expressionType(attr.Expression)
	if !typecap.Assignable(binding.ValueType, actualType) {
		g.add(fmt.Sprintf("v-model expects %s, got %s", displayType(binding.ValueType), displayType(actualType)), attr.ExpressionSpan)
		return nil, nil, false
	}

	setter, setterOK := g.renderModelSetter(attr.Expression, attr.ExpressionSpan, binding.ValueName)
	if !setterOK {
		return nil, nil, false
	}
	expression, expressionOK := g.renderExpressionFor("v-model", attr.Expression, attr.ExpressionSpan)
	if !expressionOK {
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
	if method, ok := g.methods[ident.Name]; ok && method.StateGetter {
		return jen.Id("component").Dot(script.StateSetterName(method.Name)).Call(jen.Id(valueName)), true
	}

	field, ok := g.fields[ident.Name]
	if !ok {
		g.add(fmt.Sprintf("v-model target %q is not writable", target), span)
		return nil, false
	}

	access := jen.Id("component").Dot(field.Name)
	switch field.Kind {
	case script.FieldKindLocal:
		return jen.Add(access).Op("=").Id(valueName), true
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

	return jen.Qual(tueImportPath, "EventOf").Call(
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
		fields:     g.fields,
		methods:    g.methods,
		locals:     g.locals,
		localTypes: g.localTypes,
		typeFields: g.typeFields,
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

func nodeDirectiveAttr(node *gotemplate.Node, kind gotemplate.DirectiveKind) (*gotemplate.Attr, bool) {
	if node == nil || node.Kind != gotemplate.NodeElement {
		return nil, false
	}
	for i := range node.Attrs {
		attr := &node.Attrs[i]
		if attr.Kind == gotemplate.AttrDirective && attr.Directive == kind {
			return attr, true
		}
	}
	return nil, false
}

func nodeConditionalAttr(node *gotemplate.Node) (*gotemplate.Attr, bool) {
	for _, kind := range []gotemplate.DirectiveKind{
		gotemplate.DirectiveIf,
		gotemplate.DirectiveElseIf,
		gotemplate.DirectiveElse,
	} {
		if attr, ok := nodeDirectiveAttr(node, kind); ok {
			return attr, true
		}
	}
	return nil, false
}

func nodeBindAttr(node *gotemplate.Node, argument string) (*gotemplate.Attr, bool) {
	if node == nil || node.Kind != gotemplate.NodeElement {
		return nil, false
	}
	for i := range node.Attrs {
		attr := &node.Attrs[i]
		if attr.Kind == gotemplate.AttrBind && attr.Argument == argument {
			return attr, true
		}
	}
	return nil, false
}

func isControlDirective(kind gotemplate.DirectiveKind) bool {
	switch kind {
	case gotemplate.DirectiveIf,
		gotemplate.DirectiveElseIf,
		gotemplate.DirectiveElse,
		gotemplate.DirectiveFor,
		gotemplate.DirectiveSwitch,
		gotemplate.DirectiveCase,
		gotemplate.DirectiveDefault:
		return true
	default:
		return false
	}
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

func nativeModelBinding(node *gotemplate.Node) (*nativeModel, bool) {
	if node == nil || node.IsComponent {
		return nil, false
	}

	switch node.Tag {
	case "input":
		inputType, _ := nodeStaticAttrValue(node, "type")
		if isTextInputType(inputType) {
			return &nativeModel{ValueType: "string", ValueName: "value", EventName: "input"}, true
		}
		if inputType == "checkbox" {
			return &nativeModel{ValueType: "bool", ValueName: "checked", EventName: "change", Checked: true}, true
		}
		return nil, false
	case "select":
		return &nativeModel{ValueType: "string", ValueName: "value", EventName: "change"}, true
	case "textarea":
		return &nativeModel{ValueType: "string", ValueName: "value", EventName: "input"}, true
	default:
		return nil, false
	}
}

func isTextInputType(inputType string) bool {
	switch inputType {
	case "", "text", "email", "password", "search", "tel", "url":
		return true
	default:
		return false
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
	return "v-model is only supported on text inputs, textareas, checkboxes, and selects"
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
	for _, field := range component.LocalFields {
		fields[field.Name] = field
	}
	return fields
}

func generatedReactiveInitializer(component *script.Component, variable string) jen.Code {
	if component == nil || (len(component.Computed) == 0 && len(component.Resources) == 0) {
		return nil
	}
	statements := make([]jen.Code, 0, len(component.Computed)+len(component.Resources))
	for _, computed := range component.Computed {
		statements = append(statements,
			jen.Id(variable).Dot(script.GeneratedFieldName()).Dot(script.ComputedFieldName(computed.GoName)).Op("=").Qual(tueImportPath, "ComputedOfFunc").Call(
				jen.Id(variable).Dot(computed.MethodName),
			),
		)
	}
	for _, resource := range component.Resources {
		statements = append(statements,
			jen.Id(variable).Dot(script.GeneratedFieldName()).Dot(script.ResourceFieldName(resource.GoName)).Op("=").Qual(tueImportPath, "ResourceOfContextFunc").Call(
				jen.Id("ctx"),
				jen.Id(variable).Dot(resource.MethodName),
			),
		)
	}
	return jen.Func().Params(jen.Id("ctx").Qual(tueImportPath, "Context")).Block(statements...)
}

func generatedResourceReloadStatements(component *script.Component, variable string) []jen.Code {
	if component == nil || len(component.Resources) == 0 {
		return nil
	}
	statements := make([]jen.Code, 0, len(component.Resources))
	for _, resource := range component.Resources {
		statements = append(statements, jen.Id(variable).Dot(script.ResourceReloadName(resource.GoName)).Call())
	}
	return statements
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

func structFieldMaps(structs []script.Struct) map[string]map[string]script.Field {
	byType := make(map[string]map[string]script.Field, len(structs))
	for _, structure := range structs {
		fields := make(map[string]script.Field, len(structure.Fields))
		for _, field := range structure.Fields {
			fields[field.Name] = field
		}
		byType[structure.Name] = fields
	}
	return byType
}

func comparableTypeMap(types []script.TypeInfo) map[string]bool {
	comparable := make(map[string]bool, len(types))
	for _, info := range types {
		comparable[info.Expression] = info.Comparable
	}
	return comparable
}

func propValueType(prop script.Prop) string {
	return prop.Type
}

func renderTypeExpression(typ string) (jen.Code, bool) {
	expr, err := goparser.ParseExpr(typ)
	if err != nil {
		return nil, false
	}
	return renderType(expr)
}

func renderGeneratedTypeExpression(typ string, imports map[string]string) (jen.Code, bool) {
	expr, err := goparser.ParseExpr(typ)
	if err != nil {
		return nil, false
	}
	return renderGeneratedType(expr, imports)
}

func renderGeneratedType(expr ast.Expr, imports map[string]string) (jen.Code, bool) {
	switch typed := expr.(type) {
	case *ast.Ident:
		return jen.Id(typed.Name), true
	case *ast.SelectorExpr:
		if identifier, ok := typed.X.(*ast.Ident); ok {
			if importPath, found := imports[identifier.Name]; found {
				return jen.Qual(importPath, typed.Sel.Name), true
			}
		}
		base, ok := renderGeneratedType(typed.X, imports)
		if !ok {
			return nil, false
		}
		statement, ok := base.(*jen.Statement)
		if !ok {
			return nil, false
		}
		return statement.Dot(typed.Sel.Name), true
	case *ast.StarExpr:
		base, ok := renderGeneratedType(typed.X, imports)
		if !ok {
			return nil, false
		}
		return jen.Op("*").Add(base), true
	case *ast.ArrayType:
		element, ok := renderGeneratedType(typed.Elt, imports)
		if !ok || typed.Len != nil {
			return nil, false
		}
		return jen.Index().Add(element), true
	case *ast.MapType:
		key, ok := renderGeneratedType(typed.Key, imports)
		if !ok {
			return nil, false
		}
		value, ok := renderGeneratedType(typed.Value, imports)
		if !ok {
			return nil, false
		}
		return jen.Map(key).Add(value), true
	case *ast.Ellipsis:
		element, ok := renderGeneratedType(typed.Elt, imports)
		if !ok {
			return nil, false
		}
		return jen.Op("...").Add(element), true
	case *ast.FuncType:
		parameters, ok := renderGeneratedFunctionFields(typed.Params, imports)
		if !ok {
			return nil, false
		}
		results, ok := renderGeneratedFunctionFields(typed.Results, imports)
		if !ok {
			return nil, false
		}
		function := jen.Func().Params(parameters...)
		switch len(results) {
		case 0:
			return function, true
		case 1:
			return function.Add(results[0]), true
		default:
			return function.Params(results...), true
		}
	default:
		return nil, false
	}
}

func renderGeneratedFunctionFields(fields *ast.FieldList, imports map[string]string) ([]jen.Code, bool) {
	if fields == nil {
		return nil, true
	}
	var rendered []jen.Code
	for _, field := range fields.List {
		fieldType, ok := renderGeneratedType(field.Type, imports)
		if !ok {
			return nil, false
		}
		if len(field.Names) == 0 {
			rendered = append(rendered, fieldType)
			continue
		}
		for _, name := range field.Names {
			rendered = append(rendered, jen.Id(name.Name).Add(fieldType))
		}
	}
	return rendered, true
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
	case *ast.Ellipsis:
		element, ok := renderType(typed.Elt)
		if !ok {
			return nil, false
		}
		return jen.Op("...").Add(element), true
	case *ast.FuncType:
		parameters, ok := renderFunctionFields(typed.Params)
		if !ok {
			return nil, false
		}
		results, ok := renderFunctionFields(typed.Results)
		if !ok {
			return nil, false
		}
		function := jen.Func().Params(parameters...)
		switch len(results) {
		case 0:
			return function, true
		case 1:
			return function.Add(results[0]), true
		default:
			return function.Params(results...), true
		}
	default:
		return nil, false
	}
}

func renderFunctionFields(fields *ast.FieldList) ([]jen.Code, bool) {
	if fields == nil {
		return nil, true
	}

	var rendered []jen.Code
	for _, field := range fields.List {
		fieldType, ok := renderType(field.Type)
		if !ok {
			return nil, false
		}
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			rendered = append(rendered, fieldType)
		}
	}
	return rendered, true
}

func displayType(typ string) string {
	if typ == "" {
		return "unknown"
	}
	return typ
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
