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
	component *CompInstance
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
	Type             VNodeType
	Key              string
	Tag              string
	Attrs            []Attribute
	Events           []EventBinding
	Children         []VNode
	Text             string
	InnerHTML        TrustedHTML
	HasInnerHTML     bool
	ComponentFactory func() *CompInstance
	ComponentUpdater func(*CompInstance)

	scopeAttrs []string
}

// TrustedHTML is an explicitly trusted raw HTML payload for v-html.
type TrustedHTML string

// Attribute is a static DOM attribute.
type Attribute struct {
	Name         string
	Value        string
	HasValue     bool
	BoolValue    bool
	HasBoolValue bool
}

// Attr returns a static name/value attribute.
func Attr(name string, value string) Attribute {
	return Attribute{Name: name, Value: value, HasValue: true}
}

// BoolAttr returns a static boolean attribute.
func BoolAttr(name string) Attribute {
	return Attribute{Name: name}
}

// BoolStateAttr returns a boolean DOM state attribute.
func BoolStateAttr(name string, value bool) Attribute {
	return Attribute{Name: name, BoolValue: value, HasBoolValue: true}
}

// ClassAttr returns a normalized class attribute from static and bound classes.
func ClassAttr(static string, values ...string) Attribute {
	classes := make([]string, 0, len(values)+1)
	classes = append(classes, strings.Fields(static)...)
	for _, value := range values {
		classes = append(classes, strings.Fields(value)...)
	}
	return Attr("class", strings.Join(classes, " "))
}

// StyleAttr returns a normalized style attribute from static and bound styles.
// It concatenates raw CSS strings and leaves conflicting declarations to the
// browser cascade.
func StyleAttr(static string, values ...string) Attribute {
	styles := make([]string, 0, len(values)+1)
	appendStyle := func(value string) {
		value = strings.TrimSpace(value)
		value = strings.TrimSuffix(value, ";")
		if value != "" {
			styles = append(styles, value)
		}
	}

	appendStyle(static)
	for _, value := range values {
		appendStyle(value)
	}
	return Attr("style", strings.Join(styles, "; "))
}

// Element returns an element VNode.
func Element(tag string, attrs []Attribute, children []VNode) VNode {
	return VNode{Type: VNodeTypeElement, Tag: tag, Attrs: attrs, Children: children}
}

// ElementWithEvents returns an element VNode with native event bindings.
func ElementWithEvents(tag string, attrs []Attribute, events []EventBinding, children []VNode) VNode {
	return VNode{Type: VNodeTypeElement, Tag: tag, Attrs: attrs, Events: events, Children: children}
}

// ElementWithTrustedHTML returns an element VNode with trusted raw inner HTML.
func ElementWithTrustedHTML(tag string, attrs []Attribute, events []EventBinding, innerHTML TrustedHTML) VNode {
	return VNode{Type: VNodeTypeElement, Tag: tag, Attrs: attrs, Events: events, InnerHTML: innerHTML, HasInnerHTML: true}
}

// Text returns a text VNode.
func Text(value string) VNode {
	return VNode{Type: VNodeTypeText, Text: value}
}

// Fragment returns a fragment VNode.
func Fragment(children []VNode) VNode {
	return VNode{Type: VNodeTypeFragment, Children: children}
}

// Component returns a component VNode backed by a lazy generated factory.
func Component(name string, factory func() *CompInstance) VNode {
	return VNode{Type: VNodeTypeComponent, Tag: name, ComponentFactory: factory}
}

// ComponentWithUpdate returns a component VNode that can refresh generated
// input bindings on an existing component instance during patching.
func ComponentWithUpdate(name string, factory func() *CompInstance, update func(*CompInstance)) VNode {
	return VNode{
		Type:             VNodeTypeComponent,
		Tag:              name,
		ComponentFactory: factory,
		ComponentUpdater: update,
	}
}

// WithScopeAttrs returns a VNode that applies inherited scoped-CSS attributes
// to component root elements.
func WithScopeAttrs(node VNode, attrs ...string) VNode {
	node.scopeAttrs = mergeScopeAttrNames(node.scopeAttrs, attrs)
	return node
}

// CompInstance is a generated component instance.
type CompInstance struct {
	Component   any
	Render      func() VNode
	DefaultSlot func() VNode

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

// CompOf returns a component wrapper for generated code. Generated initializers
// run in component scope before the optional user Init method.
func CompOf[C any](component *C, render func(*C) VNode, initializers ...func()) *CompInstance {
	comp := &CompInstance{Component: component}
	if render != nil {
		comp.Render = func() VNode {
			return render(component)
		}
	}
	initializable, hasInit := any(component).(initComponent)
	if len(initializers) != 0 || hasInit {
		withComponentScope(comp, func() {
			for _, initialize := range initializers {
				if initialize != nil {
					initialize()
				}
			}
			if hasInit {
				initializable.Init(&contextValue{component: comp})
			}
		})
	}
	return comp
}

func (c *CompInstance) renderVNode() VNode {
	if c == nil || c.Render == nil {
		return Fragment(nil)
	}
	var vnode VNode
	withComponentScope(c, func() {
		vnode = c.Render()
	})
	return vnode
}

// Slot renders the current component's default slot or the supplied fallback.
func Slot(fallback VNode) VNode {
	component, ok := currentComponentScope()
	if !ok || component.DefaultSlot == nil {
		return fallback
	}
	return component.DefaultSlot()
}

func (c *CompInstance) addCleanup(cleanup func()) {
	if c == nil || cleanup == nil {
		return
	}
	c.cleanups = append(c.cleanups, cleanup)
}

func (c *CompInstance) addEffectCleanup(cleanup func()) {
	if c == nil || cleanup == nil {
		return
	}
	c.effectCleanups = append(c.effectCleanups, cleanup)
}

func (c *CompInstance) runCleanups() {
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

func (c *CompInstance) mounted() {
	if c == nil {
		return
	}
	if component, ok := c.Component.(mountedComponent); ok {
		component.OnMounted()
	}
}

func (c *CompInstance) updated() {
	if c == nil {
		return
	}
	if component, ok := c.Component.(updatedComponent); ok {
		component.OnUpdated()
	}
}

func (c *CompInstance) unmounted() {
	if c == nil {
		return
	}
	if component, ok := c.Component.(unmountedComponent); ok {
		component.OnUnmounted()
	}
}

// Mount renders a component into a browser target under js/wasm.
func Mount(target string, component *CompInstance) (*Mounted, error) {
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
			renderHTMLAttr(builder, attr)
		}
		builder.WriteByte('>')
		if node.HasInnerHTML {
			builder.WriteString(string(node.InnerHTML))
		} else {
			for _, child := range node.Children {
				renderHTML(builder, child)
			}
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
	case VNodeTypeComponent:
		if node.ComponentFactory == nil {
			return
		}
		component := node.ComponentFactory()
		if component == nil {
			return
		}
		renderHTML(builder, withInheritedScopeAttrs(component.renderVNode(), node.scopeAttrs))
	default:
		builder.WriteString(html.EscapeString(fmt.Sprint(node.Text)))
	}
}

func withInheritedScopeAttrs(node VNode, scopeAttrs []string) VNode {
	if len(scopeAttrs) == 0 {
		return node
	}

	switch node.Type {
	case VNodeTypeElement:
		node.Attrs = attrsWithScopeAttrs(node.Attrs, scopeAttrs)
	case VNodeTypeFragment:
		children := append([]VNode(nil), node.Children...)
		for i := range children {
			children[i] = withInheritedScopeAttrs(children[i], scopeAttrs)
		}
		node.Children = children
	case VNodeTypeComponent:
		node.scopeAttrs = mergeScopeAttrNames(node.scopeAttrs, scopeAttrs)
	}
	return node
}

func attrsWithScopeAttrs(attrs []Attribute, scopeAttrs []string) []Attribute {
	next := append([]Attribute(nil), attrs...)
	for _, attr := range scopeAttrs {
		if attr == "" || hasAttr(next, attr) {
			continue
		}
		next = append(next, BoolAttr(attr))
	}
	return next
}

func mergeScopeAttrNames(existing []string, next []string) []string {
	merged := append([]string(nil), existing...)
	for _, attr := range next {
		if attr == "" || stringInSlice(merged, attr) {
			continue
		}
		merged = append(merged, attr)
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func hasAttr(attrs []Attribute, name string) bool {
	for _, attr := range attrs {
		if attr.Name == name {
			return true
		}
	}
	return false
}

func stringInSlice(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func renderHTMLAttr(builder *strings.Builder, attr Attribute) {
	if attr.HasBoolValue && !attr.BoolValue {
		return
	}

	builder.WriteByte(' ')
	builder.WriteString(attr.Name)
	if attr.HasBoolValue || !attr.HasValue {
		return
	}
	builder.WriteString(`="`)
	builder.WriteString(html.EscapeString(attr.Value))
	builder.WriteByte('"')
}
