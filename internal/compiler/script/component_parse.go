package script

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/norunners/tue/internal/compiler/sfc"
)

const generatedFieldName = "__tue"

type componentFieldTag struct {
	name     string
	required bool
}

func (e *extractor) extractCompDeclaration(file *ast.File, componentName string) {
	spec, ok := findTypeSpec(file, componentName)
	if !ok {
		return
	}
	structure, ok := spec.Type.(*ast.StructType)
	if !ok {
		return
	}

	found := false
	names := make(map[string]bool)
	for _, field := range structure.Fields.List {
		arguments, marker := e.compMarkerArguments(field.Type)
		if !marker {
			continue
		}
		if len(field.Names) != 0 {
			e.addDiagnostic("tue.Comp marker must be embedded anonymously", e.nodeSpan(field))
			continue
		}
		if found {
			e.addDiagnostic("component may embed at most one tue.Comp marker", e.nodeSpan(field))
			continue
		}
		found = true
		e.hasComp = true
		if len(arguments) != 1 {
			e.addDiagnostic("tue.Comp marker requires exactly one anonymous struct type argument", e.nodeSpan(field.Type))
			continue
		}
		declared, ok := arguments[0].(*ast.StructType)
		if !ok {
			e.addDiagnostic("tue.Comp type argument must be an anonymous struct", e.nodeSpan(arguments[0]))
			continue
		}
		for _, componentField := range declared.Fields.List {
			e.extractComponentField(componentField, names)
		}
	}
}

func (e *extractor) compMarkerArguments(expression ast.Expr) ([]ast.Expr, bool) {
	name, arguments, ok := e.tueGenericType(expression)
	return arguments, ok && name == "Comp"
}

func (e *extractor) extractComponentField(field *ast.Field, names map[string]bool) {
	if len(field.Names) != 1 || field.Names[0].Name == "_" {
		e.addDiagnostic("component declaration fields must have one exported name", e.nodeSpan(field))
		return
	}
	fieldName := field.Names[0]
	if !fieldName.IsExported() {
		e.addDiagnostic(fmt.Sprintf("component declaration field %q must be exported", fieldName.Name), e.nodeSpan(fieldName))
		return
	}

	category, value, err := componentFieldCategory(field)
	if err != nil {
		e.addDiagnostic(fmt.Sprintf("component declaration field %q: %v", fieldName.Name, err), componentTagSpan(e, field))
		return
	}

	switch category {
	case "prop":
		e.extractComponentProp(field, fieldName, value, names)
	case "event":
		e.extractComponentEvent(field, fieldName, value, names)
	case "state":
		e.extractComponentState(field, fieldName, value, names)
	case "computed":
		e.extractComponentComputed(field, fieldName, value, names)
	case "resource":
		e.extractComponentResource(field, fieldName, value, names)
	}
}

func (e *extractor) extractComponentProp(field *ast.Field, fieldName *ast.Ident, value string, names map[string]bool) {
	if _, ok := field.Type.(*ast.FuncType); ok {
		e.addDiagnostic(fmt.Sprintf("component prop %q must not have a function type", lowerIdentifier(fieldName.Name)), e.nodeSpan(field.Type))
		return
	}
	tag, err := parseComponentTagValue(value, lowerIdentifier(fieldName.Name), true)
	if err != nil {
		e.addDiagnostic(fmt.Sprintf("component prop %q: %v", fieldName.Name, err), componentTagSpan(e, field))
		return
	}
	if names[tag.name] {
		e.addDiagnostic(fmt.Sprintf("duplicate component declaration name %q", tag.name), e.nodeSpan(fieldName))
		return
	}
	typeName, ok := formatNode(e.fset, field.Type)
	if !ok {
		e.addDiagnostic(fmt.Sprintf("component prop %q has an unsupported Go type", tag.name), e.nodeSpan(field.Type))
		return
	}

	names[tag.name] = true
	e.props = append(e.props, Prop{
		Name:     tag.name,
		GoName:   fieldName.Name,
		Type:     typeName,
		Required: tag.required,
		Span:     e.nodeSpan(field),
		NameSpan: e.nodeSpan(fieldName),
		TypeSpan: e.nodeSpan(field.Type),
	})
}

func (e *extractor) extractComponentEvent(field *ast.Field, fieldName *ast.Ident, value string, names map[string]bool) {
	tag, err := parseComponentTagValue(value, lowerIdentifier(fieldName.Name), false)
	if err != nil {
		e.addDiagnostic(fmt.Sprintf("component event %q: %v", fieldName.Name, err), componentTagSpan(e, field))
		return
	}
	if names[tag.name] {
		e.addDiagnostic(fmt.Sprintf("duplicate component declaration name %q", tag.name), e.nodeSpan(fieldName))
		return
	}

	function, ok := field.Type.(*ast.FuncType)
	if !ok {
		e.addDiagnostic(fmt.Sprintf("component event %q must have a function type", tag.name), e.nodeSpan(field.Type))
		return
	}
	if function.Results != nil && len(function.Results.List) != 0 {
		e.addDiagnostic(fmt.Sprintf("component event %q must not return values", tag.name), e.nodeSpan(function.Results))
		return
	}
	parameters := e.parameters(function.Params)
	for index := range parameters {
		if parameters[index].Name != "" {
			continue
		}
		parameters[index].Name = "value"
		if len(parameters) > 1 {
			parameters[index].Name = fmt.Sprintf("value%d", index+1)
		}
	}

	names[tag.name] = true
	e.events = append(e.events, Event{
		Name:       tag.name,
		GoName:     fieldName.Name,
		Parameters: parameters,
		Span:       e.nodeSpan(field),
		NameSpan:   e.nodeSpan(fieldName),
	})
}

func (e *extractor) extractComponentState(field *ast.Field, fieldName *ast.Ident, value string, names map[string]bool) {
	tag, err := parseComponentTagValue(value, lowerIdentifier(fieldName.Name), false)
	if err != nil {
		e.addDiagnostic(fmt.Sprintf("component state %q: %v", fieldName.Name, err), componentTagSpan(e, field))
		return
	}
	if names[tag.name] {
		e.addDiagnostic(fmt.Sprintf("duplicate component declaration name %q", tag.name), e.nodeSpan(fieldName))
		return
	}
	if _, ok := field.Type.(*ast.FuncType); ok {
		e.addDiagnostic(fmt.Sprintf("component state %q must not have a function type", tag.name), e.nodeSpan(field.Type))
		return
	}
	typeName, ok := formatNode(e.fset, field.Type)
	if !ok {
		e.addDiagnostic(fmt.Sprintf("component state %q has an unsupported Go type", tag.name), e.nodeSpan(field.Type))
		return
	}
	names[tag.name] = true
	e.states = append(e.states, State{
		Name:     tag.name,
		GoName:   fieldName.Name,
		Type:     typeName,
		Span:     e.nodeSpan(field),
		NameSpan: e.nodeSpan(fieldName),
		TypeSpan: e.nodeSpan(field.Type),
	})
}

func (e *extractor) extractComponentComputed(field *ast.Field, fieldName *ast.Ident, value string, names map[string]bool) {
	name := lowerIdentifier(fieldName.Name)
	if names[name] {
		e.addDiagnostic(fmt.Sprintf("duplicate component declaration name %q", name), e.nodeSpan(fieldName))
		return
	}
	if value == "" {
		e.addDiagnostic(fmt.Sprintf("component computed %q must name its source method", name), componentTagSpan(e, field))
		return
	}
	if !token.IsIdentifier(value) {
		e.addDiagnostic(fmt.Sprintf("component computed %q has invalid source method %q", name, value), componentTagSpan(e, field))
		return
	}
	if _, ok := field.Type.(*ast.FuncType); ok {
		e.addDiagnostic(fmt.Sprintf("component computed %q must not have a function type", name), e.nodeSpan(field.Type))
		return
	}
	typeName, ok := formatNode(e.fset, field.Type)
	if !ok {
		e.addDiagnostic(fmt.Sprintf("component computed %q has an unsupported Go type", name), e.nodeSpan(field.Type))
		return
	}

	names[name] = true
	e.computeds = append(e.computeds, Computed{
		Name:       name,
		GoName:     fieldName.Name,
		MethodName: value,
		Type:       typeName,
		Span:       e.nodeSpan(field),
		NameSpan:   e.nodeSpan(fieldName),
		TypeSpan:   e.nodeSpan(field.Type),
	})
}

func (e *extractor) extractComponentResource(field *ast.Field, fieldName *ast.Ident, value string, names map[string]bool) {
	name := lowerIdentifier(fieldName.Name)
	if names[name] {
		e.addDiagnostic(fmt.Sprintf("duplicate component declaration name %q", name), e.nodeSpan(fieldName))
		return
	}
	if value == "" {
		e.addDiagnostic(fmt.Sprintf("component resource %q must name its loader method", name), componentTagSpan(e, field))
		return
	}
	if !token.IsIdentifier(value) {
		e.addDiagnostic(fmt.Sprintf("component resource %q has invalid loader method %q", name, value), componentTagSpan(e, field))
		return
	}
	if _, ok := field.Type.(*ast.FuncType); ok {
		e.addDiagnostic(fmt.Sprintf("component resource %q must not have a function type", name), e.nodeSpan(field.Type))
		return
	}
	typeName, ok := formatNode(e.fset, field.Type)
	if !ok {
		e.addDiagnostic(fmt.Sprintf("component resource %q has an unsupported Go type", name), e.nodeSpan(field.Type))
		return
	}

	names[name] = true
	e.resources = append(e.resources, Resource{
		Name:       name,
		GoName:     fieldName.Name,
		MethodName: value,
		Type:       typeName,
		Span:       e.nodeSpan(field),
		NameSpan:   e.nodeSpan(fieldName),
		TypeSpan:   e.nodeSpan(field.Type),
	})
}

func componentFieldCategory(field *ast.Field) (string, string, error) {
	if field.Tag == nil {
		return "", "", fmt.Errorf("must declare exactly one prop, event, state, computed, or resource tag")
	}
	raw, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return "", "", fmt.Errorf("invalid struct tag")
	}
	tags, err := parseStructTags(raw)
	if err != nil {
		return "", "", err
	}
	if len(tags) != 1 {
		return "", "", fmt.Errorf("must declare exactly one prop, event, state, computed, or resource tag")
	}
	for _, key := range []string{"prop", "event", "state", "computed", "resource"} {
		if value, ok := tags[key]; ok {
			return key, value, nil
		}
	}
	for key := range tags {
		return "", "", fmt.Errorf("unknown component tag %q", key)
	}
	return "", "", fmt.Errorf("must declare exactly one prop, event, state, computed, or resource tag")
}

func parseStructTags(raw string) (map[string]string, error) {
	tags := make(map[string]string)
	for raw != "" {
		raw = strings.TrimLeft(raw, " ")
		if raw == "" {
			break
		}
		colon := strings.IndexByte(raw, ':')
		if colon <= 0 || colon+1 >= len(raw) || raw[colon+1] != '"' {
			return nil, fmt.Errorf("invalid struct tag")
		}
		key := raw[:colon]
		for _, character := range key {
			if character <= ' ' || character == ':' || character == '"' || character == '\\' {
				return nil, fmt.Errorf("invalid struct tag")
			}
		}
		quoted := raw[colon+1:]
		end := 1
		for end < len(quoted) {
			if quoted[end] == '\\' {
				end += 2
				continue
			}
			if quoted[end] == '"' {
				break
			}
			end++
		}
		if end >= len(quoted) || quoted[end] != '"' {
			return nil, fmt.Errorf("invalid struct tag")
		}
		value, err := strconv.Unquote(quoted[:end+1])
		if err != nil {
			return nil, fmt.Errorf("invalid struct tag")
		}
		if _, exists := tags[key]; exists {
			return nil, fmt.Errorf("duplicate component tag %q", key)
		}
		tags[key] = value
		raw = quoted[end+1:]
	}
	return tags, nil
}

func parseComponentTagValue(value string, defaultName string, allowRequired bool) (componentFieldTag, error) {
	result := componentFieldTag{name: defaultName}
	parts := strings.Split(value, ",")
	if parts[0] != "" {
		result.name = parts[0]
	}
	if !isTemplateName(result.name) {
		return componentFieldTag{}, fmt.Errorf("invalid template name %q", result.name)
	}

	for index := 1; index < len(parts); index++ {
		option := parts[index]
		switch {
		case option == "":
			continue
		case !allowRequired:
			return componentFieldTag{}, fmt.Errorf("tag does not accept option %q", option)
		case option == "required":
			if result.required {
				return componentFieldTag{}, fmt.Errorf("duplicate required option")
			}
			result.required = true
		default:
			return componentFieldTag{}, fmt.Errorf("unknown option %q", option)
		}
	}
	return result, nil
}

func formatNode(fset *token.FileSet, node any) (string, bool) {
	var buffer bytes.Buffer
	if err := format.Node(&buffer, fset, node); err != nil {
		return "", false
	}
	return buffer.String(), true
}

func componentTagSpan(e *extractor, field *ast.Field) sfc.Span {
	if field.Tag == nil {
		return e.nodeSpan(field)
	}
	return e.nodeSpan(field.Tag)
}

func isTemplateName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if index == 0 {
			if unicode.IsLetter(character) {
				continue
			}
			return false
		}
		if character == '_' || character == '-' || unicode.IsLetter(character) || unicode.IsDigit(character) {
			continue
		}
		return false
	}
	return true
}

// GeneratedTypeName returns the hidden generated storage type for componentName.
func GeneratedTypeName(componentName string) string {
	return "tue" + componentName + "Data"
}

// GeneratedConstructorName returns the generated backing initializer.
func GeneratedConstructorName(componentName string) string {
	return "new" + exportedIdentifier(GeneratedTypeName(componentName))
}

// GeneratedFieldName returns the hidden generated component field.
func GeneratedFieldName() string {
	return generatedFieldName
}

// PropGetterName returns the generated effective-value prop accessor.
func PropGetterName(name string) string {
	return exportedIdentifier(name)
}

// PropOKName returns the generated prop accessor that reports parent supply.
func PropOKName(name string) string {
	return exportedIdentifier(name) + "Ok"
}

// EventMethodName returns the generated component event method.
func EventMethodName(name string) string {
	return exportedIdentifier(name)
}

// PropFieldName returns the hidden generated prop getter field.
func PropFieldName(name string) string {
	return "__prop" + exportedIdentifier(name)
}

// InputVersionFieldName returns the hidden generated prop binding version field.
func InputVersionFieldName() string {
	return "__inputVersion"
}

// EventFieldName returns the hidden generated event callback field.
func EventFieldName(name string) string {
	return "__event" + exportedIdentifier(name)
}

// StateGetterName returns the generated state getter.
func StateGetterName(name string) string {
	return exportedIdentifier(name)
}

// StateSetterName returns the generated state setter.
func StateSetterName(name string) string {
	return exportedIdentifier(name) + "Set"
}

// StateFieldName returns the hidden generated reactive state field.
func StateFieldName(name string) string {
	return "__state" + exportedIdentifier(name)
}

// ComputedGetterName returns the generated computed getter.
func ComputedGetterName(name string) string {
	return exportedIdentifier(name)
}

// ComputedFieldName returns the hidden generated computed field.
func ComputedFieldName(name string) string {
	return "__computed" + exportedIdentifier(name)
}

// ResourceGetterName returns the generated resource value getter.
func ResourceGetterName(name string) string {
	return exportedIdentifier(name)
}

// ResourceOKName returns the generated resource value/presence getter.
func ResourceOKName(name string) string {
	return exportedIdentifier(name) + "Ok"
}

// ResourceLoadingName returns the generated resource loading getter.
func ResourceLoadingName(name string) string {
	return exportedIdentifier(name) + "Loading"
}

// ResourceErrorName returns the generated resource error getter.
func ResourceErrorName(name string) string {
	return exportedIdentifier(name) + "Error"
}

// ResourceReloadName returns the generated resource reload method.
func ResourceReloadName(name string) string {
	return exportedIdentifier(name) + "Reload"
}

// ResourceFieldName returns the hidden generated resource field.
func ResourceFieldName(name string) string {
	return "__resource" + exportedIdentifier(name)
}

func lowerIdentifier(name string) string {
	if name == "" {
		return ""
	}
	runes := []rune(name)
	prefix := 1
	for prefix < len(runes) && unicode.IsUpper(runes[prefix]) {
		prefix++
	}
	if prefix > 1 && prefix < len(runes) && unicode.IsLower(runes[prefix]) {
		prefix--
	}
	for index := 0; index < prefix; index++ {
		runes[index] = unicode.ToLower(runes[index])
	}
	return string(runes)
}

func exportedIdentifier(name string) string {
	first, size := utf8.DecodeRuneInString(name)
	if first == utf8.RuneError && size == 0 {
		return ""
	}
	return string(unicode.ToUpper(first)) + name[size:]
}
