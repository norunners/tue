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
	Writable   bool
	Method     bool
	Parameters int
	Results    int
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
		scope.add(symbol{Name: method.Name, Type: funcType, Method: true, Parameters: len(method.Parameters), Results: len(method.Results)})
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

func iterableElementType(typ string) (string, bool) {
	typ = normalizeType(typ)
	if typ == unknownType || typ == "" {
		return unknownType, true
	}
	if strings.HasPrefix(typ, "[]") {
		return strings.TrimSpace(strings.TrimPrefix(typ, "[]")), true
	}
	if strings.HasPrefix(typ, "[") {
		close := strings.IndexByte(typ, ']')
		if close != -1 && close+1 < len(typ) {
			return strings.TrimSpace(typ[close+1:]), true
		}
	}
	if strings.HasPrefix(typ, "map[") {
		close := strings.IndexByte(typ, ']')
		if close != -1 && close+1 < len(typ) {
			return strings.TrimSpace(typ[close+1:]), true
		}
	}
	if typ == "string" {
		return "rune", true
	}
	return "", false
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
