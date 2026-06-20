package script

import "github.com/norunners/tue/internal/compiler/sfc"

// File is the extracted contract view of one Go script block.
type File struct {
	Path        string
	PackageName string
	PackageSpan sfc.Span
	Imports     []Import
	Types       []TypeInfo
	Structs     []Struct
	Component   *Component
}

// Import is a Go import declaration used by the script block.
type Import struct {
	Name     string
	Path     string
	Span     sfc.Span
	NameSpan sfc.Span
	PathSpan sfc.Span
}

// TypeInfo records Go type information used to validate template expressions.
type TypeInfo struct {
	Expression string
	Comparable bool
}

// Component is the contract extracted from the expected component struct.
type Component struct {
	Name       string
	Span       sfc.Span
	NameSpan   sfc.Span
	Props      []Prop
	Events     []Field
	State      []Field
	Refs       []Field
	Computed   []Field
	Resources  []Field
	Methods    []Method
	Init       *Method
	Allocation Allocation
}

// Allocation describes the generated-code construction shape for a component.
type Allocation struct {
	ComponentName string
	PropFields    []string
	CallsInit     bool
}

// Prop is a component prop field plus prop-specific metadata.
type Prop struct {
	Field    Field
	Name     string
	Required bool
}

// Struct is a top-level Go struct declaration available to template expressions.
type Struct struct {
	Name   string
	Fields []Field
}

// Field is a named field on the component struct.
type Field struct {
	Kind      FieldKind
	Name      string
	Exported  bool
	Type      string
	ValueType string
	Tag       string
	Span      sfc.Span
	NameSpan  sfc.Span
	TypeSpan  sfc.Span
	TagSpan   sfc.Span
}

// Method is a method declared on the component type.
type Method struct {
	Name            string
	ReceiverName    string
	PointerReceiver bool
	Parameters      []Parameter
	Results         []Parameter
	Span            sfc.Span
	NameSpan        sfc.Span
	ReceiverSpan    sfc.Span
}

// ParameterTypes returns the method parameter types in declaration order.
func (m Method) ParameterTypes() []string {
	return parameterTypes(m.Parameters)
}

// ResultTypes returns the method result types in declaration order.
func (m Method) ResultTypes() []string {
	return parameterTypes(m.Results)
}

func parameterTypes(parameters []Parameter) []string {
	types := make([]string, len(parameters))
	for index, parameter := range parameters {
		types[index] = parameter.Type
	}
	return types
}

// Parameter is a method parameter or result.
type Parameter struct {
	Name     string
	Type     string
	Span     sfc.Span
	NameSpan sfc.Span
	TypeSpan sfc.Span
}

// Diagnostic is a source-mapped script extraction diagnostic.
type Diagnostic struct {
	Message string
	Span    sfc.Span
}

// EventName returns the template event name represented by an event callback field.
func EventName(field Field) (string, bool) {
	if field.Kind != FieldKindEvent {
		return "", false
	}
	return eventNameFromFieldName(field.Name)
}
