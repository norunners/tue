package checker

import (
	"strings"

	"github.com/norunners/tue/internal/compiler/script"
)

const (
	unknownType = "unknown"
	funcType    = "func"
)

type scope struct {
	parent  *scope
	symbols map[string]symbol
}

type symbol struct {
	Name       string
	Type       string
	ResultType string
	Writable   bool
	Method     bool
	Parameters int
	Results    int
}

type iterableTypes struct {
	Item string
	Key  string
}

func componentScope(component *script.Component) *scope {
	scope := newScope(nil)
	for _, prop := range component.Props {
		fieldType := propType(prop)
		scope.add(symbol{Name: prop.Field.Name, Type: fieldType})
	}
	for _, field := range component.State {
		scope.add(symbol{Name: field.Name, Type: fieldType(field), Writable: true})
	}
	for _, field := range component.Refs {
		scope.add(symbol{Name: field.Name, Type: fieldType(field), Writable: true})
	}
	for _, field := range component.Computed {
		scope.add(symbol{Name: field.Name, Type: fieldType(field)})
	}
	for _, field := range component.Resources {
		scope.add(symbol{Name: field.Name, Type: fieldType(field)})
	}
	for _, method := range component.Methods {
		scope.add(symbol{
			Name:       method.Name,
			Type:       funcType,
			ResultType: methodResultType(method),
			Method:     true,
			Parameters: len(method.Parameters),
			Results:    len(method.Results),
		})
	}
	return scope
}

func newScope(parent *scope) *scope {
	return &scope{
		parent:  parent,
		symbols: make(map[string]symbol),
	}
}

func (s *scope) add(symbol symbol) {
	s.symbols[symbol.Name] = symbol
}

func (s *scope) lookup(name string) (symbol, bool) {
	for current := s; current != nil; current = current.parent {
		if symbol, ok := current.symbols[name]; ok {
			return symbol, true
		}
	}
	return symbol{}, false
}

func propType(prop script.Prop) string {
	return fieldType(prop.Field)
}

func fieldType(field script.Field) string {
	if field.ValueType != "" {
		return field.ValueType
	}
	if field.Type != "" {
		return field.Type
	}
	return unknownType
}

func methodResultType(method script.Method) string {
	if len(method.Results) != 1 {
		return unknownType
	}
	return method.Results[0].Type
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

func iterableTypesFor(typ string) (iterableTypes, bool) {
	typ = normalizeType(typ)
	if typ == unknownType || typ == "" {
		return iterableTypes{Item: unknownType, Key: unknownType}, true
	}
	if strings.HasPrefix(typ, "[]") {
		return iterableTypes{Item: strings.TrimSpace(strings.TrimPrefix(typ, "[]")), Key: "int"}, true
	}
	if strings.HasPrefix(typ, "[") {
		close := closingTypeBracket(typ, 0)
		if close != -1 && close+1 < len(typ) {
			return iterableTypes{Item: strings.TrimSpace(typ[close+1:]), Key: "int"}, true
		}
	}
	if strings.HasPrefix(typ, "map[") {
		close := closingTypeBracket(typ, len("map"))
		if close != -1 && close+1 < len(typ) {
			return iterableTypes{
				Item: strings.TrimSpace(typ[close+1:]),
				Key:  strings.TrimSpace(typ[len("map["):close]),
			}, true
		}
	}
	if typ == "string" {
		return iterableTypes{Item: "rune", Key: "int"}, true
	}
	return iterableTypes{}, false
}

func closingTypeBracket(typ string, open int) int {
	if open < 0 || open >= len(typ) || typ[open] != '[' {
		return -1
	}
	depth := 0
	for i := open; i < len(typ); i++ {
		switch typ[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func assignable(expected string, actual string) bool {
	expected = normalizeType(expected)
	actual = normalizeType(actual)
	if expected == "" || actual == "" || expected == unknownType || actual == unknownType {
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
		return unknownType
	}
	return typ
}

func isNoArgFunc(typ string) bool {
	return strings.TrimSpace(typ) == "func()"
}

func isNumeric(typ string) bool {
	switch normalizeType(typ) {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "rune", "byte":
		return true
	default:
		return false
	}
}

func isScalar(typ string) bool {
	switch normalizeType(typ) {
	case "string", "bool":
		return true
	default:
		return isNumeric(typ)
	}
}
