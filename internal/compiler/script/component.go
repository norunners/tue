package script

import (
	"strings"

	"github.com/norunners/tue/internal/compiler/sfc"
)

// File is the extracted component view of one Go script block.
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

// Component is component metadata extracted from the expected component struct.
type Component struct {
	Name          string
	GeneratedType string
	Span          sfc.Span
	NameSpan      sfc.Span
	Props         []Prop
	Events        []Event
	States        []State
	Computed      []Computed
	Resources     []Resource
	LocalFields   []Field
	Methods       []Method
	Init          *Method
	Allocation    Allocation
}

// Allocation describes the generated-code construction shape for a component.
type Allocation struct {
	ComponentName string
	CallsInit     bool
}

// Prop is a parent-provided value declared by a Comp marker.
type Prop struct {
	Name     string
	GoName   string
	Type     string
	Required bool
	Span     sfc.Span
	NameSpan sfc.Span
	TypeSpan sfc.Span
}

// Event is a parent callback declared by a Comp marker.
type Event struct {
	Name       string
	GoName     string
	Parameters []Parameter
	Span       sfc.Span
	NameSpan   sfc.Span
}

// State is generated reactive state declared by a Comp marker.
type State struct {
	Name     string
	GoName   string
	Type     string
	Span     sfc.Span
	NameSpan sfc.Span
	TypeSpan sfc.Span
}

// Computed is a generated read-only reactive value declared by a Comp marker.
type Computed struct {
	Name       string
	GoName     string
	MethodName string
	Type       string
	Span       sfc.Span
	NameSpan   sfc.Span
	TypeSpan   sfc.Span
}

// Resource is a generated async value declared by a Comp marker.
type Resource struct {
	Name       string
	GoName     string
	MethodName string
	Type       string
	Span       sfc.Span
	NameSpan   sfc.Span
	TypeSpan   sfc.Span
}

// FunctionType returns the Go callback type represented by the event.
func (e Event) FunctionType() string {
	var builder strings.Builder
	builder.WriteString("func(")
	for index, parameter := range e.Parameters {
		if index != 0 {
			builder.WriteString(", ")
		}
		if parameter.Name != "" {
			builder.WriteString(parameter.Name)
			builder.WriteByte(' ')
		}
		builder.WriteString(parameter.Type)
	}
	builder.WriteByte(')')
	return builder.String()
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
	ImplicitGetter  bool
	StateGetter     bool
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

// EventName returns the template event name represented by an event declaration.
func EventName(event Event) (string, bool) {
	return event.Name, event.Name != ""
}
