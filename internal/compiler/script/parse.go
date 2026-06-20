package script

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	goparser "go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/norunners/tue/internal/compiler/sfc"
)

const tueImportPath = "github.com/norunners/tue"

// Parse parses Go script source with positions relative to the start of source.
func Parse(source []byte, componentName string) (*File, []Diagnostic) {
	return parseSource("", string(source), componentName, sfc.Position{Line: 1, Column: 1})
}

// ParseBlock parses a script block returned by the SFC parser.
func ParseBlock(block *sfc.Block, componentName string) (*File, []Diagnostic) {
	if block == nil {
		return &File{}, []Diagnostic{{
			Message: "missing script block",
		}}
	}
	return parseSource("", block.Content, componentName, block.ContentSpan.Start)
}

// ParseSFC parses the script block from an SFC file and derives the component
// name from the file basename.
func ParseSFC(file *sfc.File) (*File, []Diagnostic) {
	if file == nil {
		return &File{}, []Diagnostic{{
			Message: "missing SFC file",
		}}
	}

	if file.Script == nil {
		return &File{Path: file.Path}, []Diagnostic{{Message: "missing script block"}}
	}
	componentName := ComponentNameFromPath(file.Path)
	script, diagnostics := parseSource(file.Path, file.Script.Content, componentName, file.Script.ContentSpan.Start)
	script.Path = file.Path
	return script, diagnostics
}

// ComponentNameFromPath returns the PascalCase component name for a .tue path.
func ComponentNameFromPath(path string) string {
	base := path
	if index := strings.LastIndexAny(base, `/\`); index != -1 {
		base = base[index+1:]
	}
	if index := strings.LastIndexByte(base, '.'); index != -1 {
		base = base[:index]
	}

	var builder strings.Builder
	upperNext := true
	for _, r := range base {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upperNext = true
			continue
		}
		if upperNext {
			r = unicode.ToUpper(r)
			upperNext = false
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

type extractor struct {
	path           string
	source         string
	lineStarts     []int
	base           sfc.Position
	fset           *token.FileSet
	tokenFile      *token.File
	tueNames       map[string]bool
	tueDot         bool
	component      string
	contract       bool
	props          []Prop
	events         []Event
	contractStates []ContractState
	diagnostics    []Diagnostic
}

func parseSource(path string, source string, componentName string, base sfc.Position) (*File, []Diagnostic) {
	extractor := newExtractor(path, source, base)
	return extractor.parse(componentName)
}

func newExtractor(path string, source string, base sfc.Position) *extractor {
	if base.Line == 0 {
		base.Line = 1
	}
	if base.Column == 0 {
		base.Column = 1
	}

	lineStarts := []int{0}
	for offset, r := range source {
		if r == '\n' {
			lineStarts = append(lineStarts, offset+1)
		}
	}

	return &extractor{
		path:       path,
		source:     source,
		lineStarts: lineStarts,
		base:       base,
		fset:       token.NewFileSet(),
		tueNames:   make(map[string]bool),
	}
}

func (e *extractor) parse(componentName string) (*File, []Diagnostic) {
	file := &File{Path: e.path}
	e.component = componentName
	if componentName == "" {
		e.addDiagnostic("component name is required", e.span(len(e.source), len(e.source)))
	}

	astFile, ok := e.parseGo()
	if !ok {
		return file, e.diagnostics
	}

	file.PackageName = astFile.Name.Name
	file.PackageSpan = e.posSpan(astFile.Pos(), astFile.Name.End())
	file.Imports = e.extractImports(astFile)
	if componentName != "" {
		e.extractComponentContract(astFile, componentName)
	}
	typesPackage, typesInfo := e.typeCheck(astFile, componentName)
	file.Types = e.extractTypes(astFile, typesPackage, typesInfo)
	file.Structs = e.extractStructs(astFile)

	if componentName != "" {
		e.extractComponent(file, astFile, componentName)
	}
	return file, e.diagnostics
}

func (e *extractor) parseGo() (*ast.File, bool) {
	file, err := goparser.ParseFile(e.fset, e.path, e.source, goparser.ParseComments|goparser.AllErrors)
	if file != nil {
		e.tokenFile = e.fset.File(file.Pos())
	}
	if err != nil {
		e.addParseDiagnostics(err)
	}
	return file, file != nil
}

func (e *extractor) addParseDiagnostics(err error) {
	if list, ok := err.(scanner.ErrorList); ok {
		for _, parseErr := range list {
			offset := parseErr.Pos.Offset
			if offset < 0 || offset > len(e.source) {
				offset = len(e.source)
			}
			e.addDiagnostic(parseErr.Msg, e.span(offset, offset))
		}
		return
	}
	e.addDiagnostic(err.Error(), e.span(len(e.source), len(e.source)))
}

func (e *extractor) extractImports(file *ast.File) []Import {
	imports := make([]Import, 0, len(file.Imports))
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			e.addDiagnostic("invalid import path", e.nodeSpan(spec.Path))
			path = spec.Path.Value
		}

		importName := defaultImportName(path)
		var nameSpan sfc.Span
		if spec.Name != nil {
			importName = spec.Name.Name
			nameSpan = e.nodeSpan(spec.Name)
		}

		if path == tueImportPath {
			switch importName {
			case ".":
				e.tueDot = true
			case "_":
			default:
				e.tueNames[importName] = true
			}
		}

		imports = append(imports, Import{
			Name:     importName,
			Path:     path,
			Span:     e.nodeSpan(spec),
			NameSpan: nameSpan,
			PathSpan: e.nodeSpan(spec.Path),
		})
	}
	return imports
}

func (e *extractor) typeCheck(file *ast.File, componentName string) (*types.Package, *types.Info) {
	var diagnostics []Diagnostic
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}
	config := types.Config{
		GoVersion:                "go1.26",
		IgnoreFuncBodies:         true,
		DisableUnusedImportCheck: true,
		Importer:                 newTueImporter(),
		Error: func(err error) {
			diagnostic := Diagnostic{
				Message: err.Error(),
				Span:    e.span(len(e.source), len(e.source)),
			}
			if typeErr, ok := err.(types.Error); ok && e.scriptPosition(typeErr.Pos) {
				diagnostic.Message = typeErr.Msg
				diagnostic.Span = e.posSpan(typeErr.Pos, typeErr.Pos)
			} else if len(e.props) != 0 {
				diagnostic.Span = e.props[0].Span
			} else if len(e.events) != 0 {
				diagnostic.Span = e.events[0].Span
			} else if len(e.contractStates) != 0 {
				diagnostic.Span = e.contractStates[0].Span
			}
			diagnostics = append(diagnostics, diagnostic)
		},
	}
	originalDeclarations := len(file.Decls)
	file.Decls = append(file.Decls, e.generatedContractDeclarations(file, componentName)...)
	pkg, _ := config.Check(e.path, e.fset, []*ast.File{file}, info)
	file.Decls = file.Decls[:originalDeclarations]
	e.diagnostics = append(e.diagnostics, diagnostics...)
	return pkg, info
}

func (e *extractor) scriptPosition(position token.Pos) bool {
	if e.tokenFile == nil || !position.IsValid() {
		return false
	}
	offset := int(position) - e.tokenFile.Base()
	return offset >= 0 && offset <= e.tokenFile.Size()
}

func (e *extractor) generatedContractDeclarations(file *ast.File, componentName string) []ast.Decl {
	if !e.contract {
		return nil
	}

	declared := componentMemberNames(file, componentName)
	var source strings.Builder
	fmt.Fprintf(&source, "package %s\n", file.Name.Name)
	for _, prop := range e.props {
		getter := PropGetterName(prop.GoName)
		if !declared[getter] {
			fmt.Fprintf(&source, "func (*%s) %s() %s { panic(\"generated\") }\n", componentName, getter, prop.Type)
			declared[getter] = true
		}
		presence := PropOKName(prop.GoName)
		if !declared[presence] {
			fmt.Fprintf(&source, "func (*%s) %s() (%s, bool) { panic(\"generated\") }\n", componentName, presence, prop.Type)
			declared[presence] = true
		}
	}
	for _, event := range e.events {
		method := EventMethodName(event.GoName)
		if !declared[method] {
			fmt.Fprintf(&source, "func (*%s) %s%s bool { panic(\"generated\") }\n", componentName, method, strings.TrimPrefix(event.FunctionType(), "func"))
			declared[method] = true
		}
	}
	for _, state := range e.contractStates {
		getter := StateGetterName(state.GoName)
		if !declared[getter] {
			fmt.Fprintf(&source, "func (*%s) %s() %s { panic(\"generated\") }\n", componentName, getter, state.Type)
			declared[getter] = true
		}
		setter := StateSetterName(state.GoName)
		if !declared[setter] {
			fmt.Fprintf(&source, "func (*%s) %s(value %s) { panic(\"generated\") }\n", componentName, setter, state.Type)
			declared[setter] = true
		}
	}

	generated, err := goparser.ParseFile(e.fset, e.path+"#contract", source.String(), goparser.AllErrors)
	if err != nil {
		e.addDiagnostic(fmt.Sprintf("build generated component contract: %v", err), e.span(len(e.source), len(e.source)))
		return nil
	}
	return generated.Decls
}

func componentMemberNames(file *ast.File, componentName string) map[string]bool {
	names := make(map[string]bool)
	if spec, ok := findTypeSpec(file, componentName); ok {
		if structure, ok := spec.Type.(*ast.StructType); ok {
			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					names[name.Name] = true
				}
			}
		}
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || len(function.Recv.List) != 1 {
			continue
		}
		receiver := function.Recv.List[0].Type
		if pointer, ok := receiver.(*ast.StarExpr); ok {
			receiver = pointer.X
		}
		identifier, ok := receiver.(*ast.Ident)
		if ok && identifier.Name == componentName {
			names[function.Name.Name] = true
		}
	}
	return names
}

func (e *extractor) extractTypes(file *ast.File, pkg *types.Package, info *types.Info) []TypeInfo {
	if pkg == nil || info == nil {
		return nil
	}

	comparability := make(map[string]bool)
	record := func(expression string, typ types.Type) {
		comparable := types.Comparable(typ)
		if previous, ok := comparability[expression]; ok {
			comparable = previous && comparable
		}
		comparability[expression] = comparable
	}

	scope := pkg.Scope()
	for _, name := range scope.Names() {
		typeName, ok := scope.Lookup(name).(*types.TypeName)
		if ok {
			record(name, typeName.Type())
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		expr, ok := node.(ast.Expr)
		if !ok {
			return true
		}
		typeAndValue, ok := info.Types[expr]
		if ok && typeAndValue.IsType() && typeAndValue.Type != nil {
			record(e.nodeString(expr), typeAndValue.Type)
		}
		return true
	})

	expressions := make([]string, 0, len(comparability))
	for expression := range comparability {
		expressions = append(expressions, expression)
	}
	sort.Strings(expressions)

	extracted := make([]TypeInfo, len(expressions))
	for i, expression := range expressions {
		extracted[i] = TypeInfo{
			Expression: expression,
			Comparable: comparability[expression],
		}
	}
	return extracted
}

func (e *extractor) extractComponent(file *File, astFile *ast.File, componentName string) {
	spec, ok := findTypeSpec(astFile, componentName)
	if !ok {
		e.addDiagnostic(fmt.Sprintf("component %q struct not found", componentName), e.span(len(e.source), len(e.source)))
		return
	}

	structType, ok := spec.Type.(*ast.StructType)
	if !ok {
		e.addDiagnostic(fmt.Sprintf("component %q must be a struct", componentName), e.nodeSpan(spec.Type))
		return
	}

	component := &Component{
		Name:           componentName,
		Span:           e.nodeSpan(spec),
		NameSpan:       e.nodeSpan(spec.Name),
		Props:          append([]Prop(nil), e.props...),
		Events:         append([]Event(nil), e.events...),
		ContractStates: append([]ContractState(nil), e.contractStates...),
	}
	if e.contract {
		component.ContractType = ContractTypeName(componentName)
	}
	e.extractFields(component, structType)
	e.extractMethods(component, astFile)
	e.validateGeneratedContract(component)
	component.Allocation = allocationFor(component)
	file.Component = component
}

func findTypeSpec(file *ast.File, name string) (*ast.TypeSpec, bool) {
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && typeSpec.Name.Name == name {
				return typeSpec, true
			}
		}
	}
	return nil, false
}

func (e *extractor) extractStructs(file *ast.File) []Struct {
	var structs []Struct
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			structs = append(structs, Struct{
				Name:   typeSpec.Name.Name,
				Fields: e.structFields(structType),
			})
		}
	}
	return structs
}

func (e *extractor) structFields(structType *ast.StructType) []Field {
	var fields []Field
	for _, astField := range structType.Fields.List {
		if len(astField.Names) == 0 {
			continue
		}
		for _, name := range astField.Names {
			tag, tagSpan := e.fieldTag(astField)
			fields = append(fields, Field{
				Kind:     FieldKindState,
				Name:     name.Name,
				Exported: name.IsExported(),
				Type:     e.nodeString(astField.Type),
				Tag:      tag,
				Span:     e.posSpan(name.Pos(), astField.End()),
				NameSpan: e.nodeSpan(name),
				TypeSpan: e.nodeSpan(astField.Type),
				TagSpan:  tagSpan,
			})
		}
	}
	return fields
}

func (e *extractor) extractFields(component *Component, structType *ast.StructType) {
	for _, astField := range structType.Fields.List {
		if len(astField.Names) == 0 {
			continue
		}

		for _, name := range astField.Names {
			field := e.fieldFromAST(name, astField)
			switch field.Kind {
			case FieldKindRef:
				component.Refs = append(component.Refs, field)
			case FieldKindComputed:
				component.Computed = append(component.Computed, field)
			case FieldKindResource:
				component.Resources = append(component.Resources, field)
			default:
				component.State = append(component.State, field)
			}
		}
	}
}

func (e *extractor) fieldFromAST(name *ast.Ident, astField *ast.Field) Field {
	kind, valueType := e.classifyField(name.Name, astField.Type)
	tag, tagSpan := e.fieldTag(astField)
	return Field{
		Kind:      kind,
		Name:      name.Name,
		Exported:  name.IsExported(),
		Type:      e.nodeString(astField.Type),
		ValueType: valueType,
		Tag:       tag,
		Span:      e.posSpan(name.Pos(), astField.End()),
		NameSpan:  e.nodeSpan(name),
		TypeSpan:  e.nodeSpan(astField.Type),
		TagSpan:   tagSpan,
	}
}

func (e *extractor) classifyField(fieldName string, expr ast.Expr) (FieldKind, string) {
	if _, ok := expr.(*ast.FuncType); ok {
		return FieldKindState, ""
	}

	typeName, args, ok := e.tueGenericType(expr)
	if !ok {
		return FieldKindState, ""
	}
	if typeName == "Comp" {
		e.addDiagnostic("tue.Comp contract marker must be embedded anonymously", e.nodeSpan(expr))
		return FieldKindState, ""
	}
	kind, ok := fieldKindForTueType(typeName)
	if !ok {
		return FieldKindState, ""
	}
	if len(args) != 1 {
		e.addDiagnostic(
			fmt.Sprintf("field %q must use tue.%s[T] with exactly one type argument", fieldName, typeName),
			e.nodeSpan(expr),
		)
		return kind, ""
	}
	return kind, e.nodeString(args[0])
}

func (e *extractor) tueGenericType(expr ast.Expr) (string, []ast.Expr, bool) {
	switch typed := expr.(type) {
	case *ast.IndexExpr:
		name, ok := e.tueTypeName(typed.X)
		if !ok {
			return "", nil, false
		}
		return name, []ast.Expr{typed.Index}, true
	case *ast.IndexListExpr:
		name, ok := e.tueTypeName(typed.X)
		if !ok {
			return "", nil, false
		}
		return name, typed.Indices, true
	default:
		name, ok := e.tueTypeName(expr)
		return name, nil, ok
	}
}

func (e *extractor) tueTypeName(expr ast.Expr) (string, bool) {
	switch typed := expr.(type) {
	case *ast.SelectorExpr:
		ident, ok := typed.X.(*ast.Ident)
		if !ok || !e.tueNames[ident.Name] {
			return "", false
		}
		return typed.Sel.Name, true
	case *ast.Ident:
		if e.tueDot {
			return typed.Name, true
		}
		return "", false
	default:
		return "", false
	}
}

func fieldKindForTueType(name string) (FieldKind, bool) {
	switch name {
	case "Ref":
		return FieldKindRef, true
	case "Computed":
		return FieldKindComputed, true
	case "Resource":
		return FieldKindResource, true
	default:
		return FieldKindState, false
	}
}

func (e *extractor) fieldTag(field *ast.Field) (string, sfc.Span) {
	if field.Tag == nil {
		return "", sfc.Span{}
	}
	tag, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		e.addDiagnostic("invalid struct tag", e.nodeSpan(field.Tag))
		return "", e.nodeSpan(field.Tag)
	}
	return tag, e.nodeSpan(field.Tag)
}

func (e *extractor) extractMethods(component *Component, file *ast.File) {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil {
			continue
		}

		receiverName, pointerReceiver, receiverSpan, ok := e.receiver(function)
		if !ok || receiverName != component.Name {
			continue
		}

		method := e.methodFromAST(function, receiverName, pointerReceiver, *receiverSpan)
		if method.Name == "Init" {
			if !e.validInit(function, component.Name, pointerReceiver) {
				e.addDiagnostic(
					fmt.Sprintf("Init must have signature func (c *%s) Init(tue.Context)", component.Name),
					method.NameSpan,
				)
				continue
			}
			component.Init = &method
			continue
		}
		component.Methods = append(component.Methods, method)
	}
}

func (e *extractor) validateGeneratedContract(component *Component) {
	if !e.contract {
		component.ContractType = ""
		return
	}

	declared := make(map[string]sfc.Span)
	if component.Init != nil {
		declared[component.Init.Name] = component.Init.NameSpan
	}
	for _, method := range component.Methods {
		declared[method.Name] = method.NameSpan
	}
	for _, fields := range [][]Field{component.State, component.Refs, component.Computed, component.Resources} {
		for _, field := range fields {
			declared[field.Name] = field.NameSpan
		}
	}
	if span, exists := declared[ContractFieldName()]; exists {
		e.addDiagnostic(fmt.Sprintf("component member %s is reserved for generated contract storage", ContractFieldName()), span)
	}

	for _, prop := range component.Props {
		name := PropGetterName(prop.GoName)
		if _, exists := declared[name]; exists {
			e.addDiagnostic(fmt.Sprintf("generated prop getter %s conflicts with a component member", name), prop.NameSpan)
		} else {
			declared[name] = prop.NameSpan
			component.Methods = append(component.Methods, Method{
				Name:            name,
				ReceiverName:    component.Name,
				PointerReceiver: true,
				ImplicitGetter:  true,
				Results:         []Parameter{{Type: prop.Type, Span: prop.TypeSpan, TypeSpan: prop.TypeSpan}},
				Span:            prop.Span,
				NameSpan:        prop.NameSpan,
			})
		}

		presence := PropOKName(prop.GoName)
		if _, exists := declared[presence]; exists {
			e.addDiagnostic(fmt.Sprintf("generated prop presence getter %s conflicts with a component member", presence), prop.NameSpan)
			continue
		}
		declared[presence] = prop.NameSpan
		component.Methods = append(component.Methods, Method{
			Name:            presence,
			ReceiverName:    component.Name,
			PointerReceiver: true,
			Results: []Parameter{
				{Type: prop.Type, Span: prop.TypeSpan, TypeSpan: prop.TypeSpan},
				{Type: "bool", Span: prop.Span, TypeSpan: prop.Span},
			},
			Span:     prop.Span,
			NameSpan: prop.NameSpan,
		})
	}
	for _, event := range component.Events {
		name := EventMethodName(event.GoName)
		if _, exists := declared[name]; exists {
			e.addDiagnostic(fmt.Sprintf("generated event method %s conflicts with a component member", name), event.NameSpan)
			continue
		}
		declared[name] = event.NameSpan
		component.Methods = append(component.Methods, Method{
			Name:            name,
			ReceiverName:    component.Name,
			PointerReceiver: true,
			Parameters:      append([]Parameter(nil), event.Parameters...),
			Results:         []Parameter{{Type: "bool", Span: event.Span, TypeSpan: event.Span}},
			Span:            event.Span,
			NameSpan:        event.NameSpan,
		})
	}
	for _, state := range component.ContractStates {
		getter := StateGetterName(state.GoName)
		if _, exists := declared[getter]; exists {
			e.addDiagnostic(fmt.Sprintf("generated state getter %s conflicts with a component member", getter), state.NameSpan)
		} else {
			declared[getter] = state.NameSpan
			component.Methods = append(component.Methods, Method{
				Name:            getter,
				ReceiverName:    component.Name,
				PointerReceiver: true,
				ImplicitGetter:  true,
				Results:         []Parameter{{Type: state.Type, Span: state.TypeSpan, TypeSpan: state.TypeSpan}},
				Span:            state.Span,
				NameSpan:        state.NameSpan,
			})
		}

		setter := StateSetterName(state.GoName)
		if _, exists := declared[setter]; exists {
			e.addDiagnostic(fmt.Sprintf("generated state setter %s conflicts with a component member", setter), state.NameSpan)
			continue
		}
		declared[setter] = state.NameSpan
		component.Methods = append(component.Methods, Method{
			Name:            setter,
			ReceiverName:    component.Name,
			PointerReceiver: true,
			Parameters:      []Parameter{{Name: "value", Type: state.Type, Span: state.TypeSpan, TypeSpan: state.TypeSpan}},
			Span:            state.Span,
			NameSpan:        state.NameSpan,
		})
	}
}

func (e *extractor) receiver(function *ast.FuncDecl) (string, bool, *sfc.Span, bool) {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return "", false, nil, false
	}

	receiverType := function.Recv.List[0].Type
	pointerReceiver := false
	if star, ok := receiverType.(*ast.StarExpr); ok {
		pointerReceiver = true
		receiverType = star.X
	}

	ident, ok := receiverType.(*ast.Ident)
	if !ok {
		return "", false, nil, false
	}
	span := e.nodeSpan(function.Recv.List[0].Type)
	return ident.Name, pointerReceiver, &span, true
}

func (e *extractor) methodFromAST(function *ast.FuncDecl, receiverName string, pointerReceiver bool, receiverSpan sfc.Span) Method {
	return Method{
		Name:            function.Name.Name,
		ReceiverName:    receiverName,
		PointerReceiver: pointerReceiver,
		Parameters:      e.parameters(function.Type.Params),
		Results:         e.parameters(function.Type.Results),
		Span:            e.nodeSpan(function),
		NameSpan:        e.nodeSpan(function.Name),
		ReceiverSpan:    receiverSpan,
	}
}

func (e *extractor) parameters(fields *ast.FieldList) []Parameter {
	if fields == nil {
		return nil
	}

	var parameters []Parameter
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			parameters = append(parameters, Parameter{
				Type:     e.nodeString(field.Type),
				Span:     e.nodeSpan(field),
				TypeSpan: e.nodeSpan(field.Type),
			})
			continue
		}
		for _, name := range field.Names {
			parameters = append(parameters, Parameter{
				Name:     name.Name,
				Type:     e.nodeString(field.Type),
				Span:     e.posSpan(name.Pos(), field.End()),
				NameSpan: e.nodeSpan(name),
				TypeSpan: e.nodeSpan(field.Type),
			})
		}
	}
	return parameters
}

func (e *extractor) validInit(function *ast.FuncDecl, componentName string, pointerReceiver bool) bool {
	if !pointerReceiver {
		return false
	}
	if function.Type.Results != nil && len(function.Type.Results.List) != 0 {
		return false
	}
	if function.Type.Params == nil || len(function.Type.Params.List) != 1 {
		return false
	}

	parameter := function.Type.Params.List[0]
	if len(parameter.Names) > 1 {
		return false
	}
	return e.isTueContext(parameter.Type)
}

func (e *extractor) isTueContext(expr ast.Expr) bool {
	name, ok := e.tueTypeName(expr)
	return ok && name == "Context"
}

func allocationFor(component *Component) Allocation {
	return Allocation{
		ComponentName: component.Name,
		CallsInit:     component.Init != nil,
	}
}

func defaultImportName(path string) string {
	if index := strings.LastIndexByte(path, '/'); index != -1 {
		return path[index+1:]
	}
	return path
}

func (e *extractor) addDiagnostic(message string, span sfc.Span) {
	e.diagnostics = append(e.diagnostics, Diagnostic{
		Message: message,
		Span:    span,
	})
}

func (e *extractor) nodeString(node any) string {
	var buffer bytes.Buffer
	if err := format.Node(&buffer, e.fset, node); err != nil {
		return ""
	}
	return buffer.String()
}

func (e *extractor) nodeSpan(node interface {
	Pos() token.Pos
	End() token.Pos
}) sfc.Span {
	return e.posSpan(node.Pos(), node.End())
}

func (e *extractor) posSpan(start token.Pos, end token.Pos) sfc.Span {
	return e.span(e.offset(start), e.offset(end))
}

func (e *extractor) offset(position token.Pos) int {
	if e.tokenFile == nil || !position.IsValid() {
		return len(e.source)
	}
	offset := e.tokenFile.Offset(position)
	if offset < 0 {
		return 0
	}
	if offset > len(e.source) {
		return len(e.source)
	}
	return offset
}

func (e *extractor) span(start int, end int) sfc.Span {
	return sfc.Span{
		Start: e.position(start),
		End:   e.position(end),
	}
}

func (e *extractor) position(offset int) sfc.Position {
	lineIndex := sort.Search(len(e.lineStarts), func(i int) bool {
		return e.lineStarts[i] > offset
	}) - 1
	if lineIndex < 0 {
		lineIndex = 0
	}

	position := sfc.Position{
		Offset: e.base.Offset + offset,
		Line:   e.base.Line + lineIndex,
		Column: offset - e.lineStarts[lineIndex] + 1,
	}
	if lineIndex == 0 {
		position.Column = e.base.Column + offset
	}
	return position
}
