package checker

import (
	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/typecap"
)

const (
	unknownType = typecap.Unknown
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

func (s *scope) lookup(name string) (*symbol, bool) {
	for current := s; current != nil; current = current.parent {
		if symbol, ok := current.symbols[name]; ok {
			return &symbol, true
		}
	}
	return nil, false
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

func comparableTypeMap(types []script.TypeInfo) map[string]bool {
	comparable := make(map[string]bool, len(types))
	for _, info := range types {
		comparable[info.Expression] = info.Comparable
	}
	return comparable
}

func displayType(typ string) string {
	if typ == "" {
		return unknownType
	}
	return typ
}
