package tue

import (
	"fmt"
	"html"
	"strings"
)

// Context is passed to optional component Init methods.
type Context interface{}

type contextValue struct{}

// Prop is the read interface exposed to component code.
type Prop[T any] interface {
	Get() T
	Watch(func(T)) func()
}

// PropValue is the concrete runtime storage for a prop.
type PropValue[T any] struct {
	get func() T
}

// PropOf returns a prop with a fixed value.
func PropOf[T any](value T) *PropValue[T] {
	return PropOfFunc(func() T {
		return value
	})
}

// PropOfFunc returns a prop backed by a getter function.
func PropOfFunc[T any](get func() T) *PropValue[T] {
	return &PropValue[T]{get: get}
}

// Get returns the current prop value.
func (p *PropValue[T]) Get() T {
	if p == nil || p.get == nil {
		var zero T
		return zero
	}
	return p.get()
}

// Watch observes prop changes. Dependency tracking is added in a later slice.
func (p *PropValue[T]) Watch(func(T)) func() {
	return func() {}
}

// Ref is the read/write interface exposed to component code.
type Ref[T any] interface {
	Get() T
	Set(T)
	Watch(func(T)) func()
}

// RefValue is the concrete runtime storage for a ref.
type RefValue[T any] struct {
	value T
}

// RefOf returns a ref with an initial value.
func RefOf[T any](value T) *RefValue[T] {
	return &RefValue[T]{value: value}
}

// Get returns the current ref value.
func (r *RefValue[T]) Get() T {
	if r == nil {
		var zero T
		return zero
	}
	return r.value
}

// Set updates the ref value.
func (r *RefValue[T]) Set(value T) {
	if r == nil {
		return
	}
	r.value = value
}

// Watch observes ref changes. Dependency tracking is added in a later slice.
func (r *RefValue[T]) Watch(func(T)) func() {
	return func() {}
}

// Computed is the read interface exposed to component code.
type Computed[T any] interface {
	Get() T
	Watch(func(T)) func()
}

// ComputedValue is the concrete runtime storage for a computed value.
type ComputedValue[T any] struct {
	compute func() T
}

// ComputedOfFunc returns a computed value backed by a function.
func ComputedOfFunc[T any](compute func() T) *ComputedValue[T] {
	return &ComputedValue[T]{compute: compute}
}

// Get returns the current computed value.
func (c *ComputedValue[T]) Get() T {
	if c == nil || c.compute == nil {
		var zero T
		return zero
	}
	return c.compute()
}

// Watch observes computed changes. Dependency tracking is added in a later slice.
func (c *ComputedValue[T]) Watch(func(T)) func() {
	return func() {}
}

// VNode is the generated render tree consumed by the runtime.
type VNode struct {
	Type     VNodeType
	Tag      string
	Attrs    []Attribute
	Children []VNode
	Text     string
}

// Attribute is a static DOM attribute.
type Attribute struct {
	Name     string
	Value    string
	HasValue bool
}

// Attr returns a static name/value attribute.
func Attr(name string, value string) Attribute {
	return Attribute{Name: name, Value: value, HasValue: true}
}

// BoolAttr returns a static boolean attribute.
func BoolAttr(name string) Attribute {
	return Attribute{Name: name}
}

// Element returns an element VNode.
func Element(tag string, attrs []Attribute, children []VNode) VNode {
	return VNode{Type: VNodeTypeElement, Tag: tag, Attrs: attrs, Children: children}
}

// Text returns a text VNode.
func Text(value string) VNode {
	return VNode{Type: VNodeTypeText, Text: value}
}

// Fragment returns a fragment VNode.
func Fragment(children []VNode) VNode {
	return VNode{Type: VNodeTypeFragment, Children: children}
}

// Comp is a generated component instance.
type Comp struct {
	Component any
	Render    func() VNode
}

type initComponent interface {
	Init(Context)
}

// CompOf returns a component wrapper for generated code.
func CompOf[C any](component *C, render func(*C) VNode) *Comp {
	if initializable, ok := any(component).(initComponent); ok {
		initializable.Init(contextValue{})
	}
	return &Comp{
		Component: component,
		Render: func() VNode {
			return render(component)
		},
	}
}

// Mount renders a component into a browser target under js/wasm.
func Mount(target string, component *Comp) error {
	return mount(target, component)
}

// RenderHTML renders a VNode to static HTML.
func RenderHTML(node VNode) string {
	var builder strings.Builder
	renderHTML(&builder, node)
	return builder.String()
}

func renderHTML(builder *strings.Builder, node VNode) {
	switch node.Type {
	case VNodeTypeElement:
		builder.WriteByte('<')
		builder.WriteString(node.Tag)
		for _, attr := range node.Attrs {
			builder.WriteByte(' ')
			builder.WriteString(attr.Name)
			if attr.HasValue {
				builder.WriteString(`="`)
				builder.WriteString(html.EscapeString(attr.Value))
				builder.WriteByte('"')
			}
		}
		builder.WriteByte('>')
		for _, child := range node.Children {
			renderHTML(builder, child)
		}
		builder.WriteString("</")
		builder.WriteString(node.Tag)
		builder.WriteByte('>')
	case VNodeTypeText:
		builder.WriteString(html.EscapeString(node.Text))
	case VNodeTypeFragment:
		for _, child := range node.Children {
			renderHTML(builder, child)
		}
	default:
		builder.WriteString(html.EscapeString(fmt.Sprint(node.Text)))
	}
}
