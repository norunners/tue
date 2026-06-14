package vue

import (
	"html"
	"strings"
)

// VNodeType identifies the kind of virtual node.
type VNodeType int

const (
	VNodeInvalid VNodeType = iota
	VNodeElement
	VNodeText
	VNodeFragment
	VNodeComponent
)

// VNode is the runtime's virtual DOM node.
type VNode struct {
	Type      VNodeType
	Tag       string
	Key       any
	Props     []Attribute
	Events    []EventBinding
	Children  []VNode
	Text      string
	Component *Comp
}

// Attribute is a static DOM attribute on a VNode element.
type Attribute struct {
	Name     string
	Value    string
	HasValue bool
}

// EventBinding reserves the VNode event shape for the event task.
type EventBinding struct {
	Name    string
	Handler func()
}

// Element creates an element VNode.
func Element(tag string, attrs []Attribute, children ...VNode) VNode {
	return ElementWithEvents(tag, attrs, nil, children...)
}

// ElementWithEvents creates an element VNode with native DOM event bindings.
func ElementWithEvents(tag string, attrs []Attribute, events []EventBinding, children ...VNode) VNode {
	return VNode{
		Type:     VNodeElement,
		Tag:      tag,
		Props:    append([]Attribute(nil), attrs...),
		Events:   append([]EventBinding(nil), events...),
		Children: append([]VNode(nil), children...),
	}
}

// Text creates a text VNode.
func Text(text string) VNode {
	return VNode{Type: VNodeText, Text: text}
}

// Fragment creates a fragment VNode.
func Fragment(children ...VNode) VNode {
	return VNode{Type: VNodeFragment, Children: append([]VNode(nil), children...)}
}

// Attr creates a string-valued static attribute.
func Attr(name, value string) Attribute {
	return Attribute{Name: name, Value: value, HasValue: true}
}

// BoolAttr creates a boolean static attribute.
func BoolAttr(name string) Attribute {
	return Attribute{Name: name}
}

// On creates a native DOM event binding.
func On(name string, handler func()) EventBinding {
	return EventBinding{Name: name, Handler: handler}
}

// RenderHTML renders a VNode tree into static HTML.
func RenderHTML(node VNode) string {
	var b strings.Builder
	renderVNodeHTML(&b, node)
	return b.String()
}

func renderVNodeHTML(b *strings.Builder, node VNode) {
	switch node.Type {
	case VNodeElement:
		renderElementHTML(b, node)
	case VNodeText:
		b.WriteString(html.EscapeString(node.Text))
	case VNodeFragment, VNodeInvalid:
		for _, child := range node.Children {
			renderVNodeHTML(b, child)
		}
	case VNodeComponent:
		if node.Component != nil {
			b.WriteString(node.Component.RenderHTML())
		}
	}
}

func renderElementHTML(b *strings.Builder, node VNode) {
	if node.Tag == "" {
		for _, child := range node.Children {
			renderVNodeHTML(b, child)
		}
		return
	}

	b.WriteByte('<')
	b.WriteString(node.Tag)
	for _, attr := range node.Props {
		if attr.Name == "" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(attr.Name)
		if attr.HasValue {
			b.WriteString(`="`)
			b.WriteString(html.EscapeString(attr.Value))
			b.WriteByte('"')
		}
	}
	b.WriteByte('>')

	if isVoidElement(node.Tag) {
		return
	}
	for _, child := range node.Children {
		renderVNodeHTML(b, child)
	}
	b.WriteString("</")
	b.WriteString(node.Tag)
	b.WriteByte('>')
}

func isVoidElement(name string) bool {
	switch strings.ToLower(name) {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}
