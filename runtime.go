package tue

import (
	"fmt"
	"html"
	"strings"
)

// Context is passed to optional component Init methods.
type Context interface {
	OnCleanup(func())
}

type contextValue struct {
	component *Comp
}

// OnCleanup registers a function to run before OnUnmounted.
func (c *contextValue) OnCleanup(cleanup func()) {
	if c == nil || c.component == nil {
		return
	}
	c.component.addCleanup(cleanup)
}

// VNode is the generated render tree consumed by the runtime.
type VNode struct {
	Type     VNodeType
	Key      string
	Tag      string
	Attrs    []Attribute
	Events   []EventBinding
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

// ElementWithEvents returns an element VNode with native event bindings.
func ElementWithEvents(tag string, attrs []Attribute, events []EventBinding, children []VNode) VNode {
	return VNode{Type: VNodeTypeElement, Tag: tag, Attrs: attrs, Events: events, Children: children}
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

	effectCleanups []func()
	cleanups       []func()
}

type initComponent interface {
	Init(Context)
}

type mountedComponent interface {
	OnMounted()
}

type updatedComponent interface {
	OnUpdated()
}

type unmountedComponent interface {
	OnUnmounted()
}

// CompOf returns a component wrapper for generated code.
func CompOf[C any](component *C, render func(*C) VNode) *Comp {
	comp := &Comp{Component: component}
	if render != nil {
		comp.Render = func() VNode {
			return render(component)
		}
	}
	if initializable, ok := any(component).(initComponent); ok {
		withComponentScope(comp, func() {
			initializable.Init(&contextValue{component: comp})
		})
	}
	return comp
}

func (c *Comp) renderVNode() VNode {
	if c == nil || c.Render == nil {
		return Fragment(nil)
	}
	return c.Render()
}

func (c *Comp) addCleanup(cleanup func()) {
	if c == nil || cleanup == nil {
		return
	}
	c.cleanups = append(c.cleanups, cleanup)
}

func (c *Comp) addEffectCleanup(cleanup func()) {
	if c == nil || cleanup == nil {
		return
	}
	c.effectCleanups = append(c.effectCleanups, cleanup)
}

func (c *Comp) runCleanups() {
	if c == nil {
		return
	}
	effectCleanups := c.effectCleanups
	c.effectCleanups = nil
	for _, cleanup := range effectCleanups {
		cleanup()
	}

	cleanups := c.cleanups
	c.cleanups = nil
	for _, cleanup := range cleanups {
		cleanup()
	}
}

func (c *Comp) mounted() {
	if c == nil {
		return
	}
	if component, ok := c.Component.(mountedComponent); ok {
		component.OnMounted()
	}
}

func (c *Comp) updated() {
	if c == nil {
		return
	}
	if component, ok := c.Component.(updatedComponent); ok {
		component.OnUpdated()
	}
}

func (c *Comp) unmounted() {
	if c == nil {
		return
	}
	if component, ok := c.Component.(unmountedComponent); ok {
		component.OnUnmounted()
	}
}

// Mount renders a component into a browser target under js/wasm.
func Mount(target string, component *Comp) (*Mounted, error) {
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
