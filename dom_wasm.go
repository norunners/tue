//go:build js && wasm

package vue

import "syscall/js"

type domDocument struct {
	value js.Value
}

type domNode struct {
	value js.Value
}

type domEvent struct {
	value js.Value
}

func wrapDocument(value js.Value) *domDocument {
	return &domDocument{value: value}
}

func wrapNode(value js.Value) *domNode {
	if value.IsUndefined() || value.IsNull() {
		return nil
	}
	return &domNode{value: value}
}

func (document *domDocument) QuerySelector(selector string) *domNode {
	return wrapNode(document.value.Call("querySelector", selector))
}

func (document *domDocument) CreateElement(tag string) *domNode {
	return wrapNode(document.value.Call("createElement", tag))
}

func (document *domDocument) CreateTextNode(text string) *domNode {
	return wrapNode(document.value.Call("createTextNode", text))
}

func (document *domDocument) createElement(tag string) domNodeAccess {
	node := document.CreateElement(tag)
	if node == nil {
		return nil
	}
	return node
}

func (document *domDocument) createTextNode(text string) domNodeAccess {
	node := document.CreateTextNode(text)
	if node == nil {
		return nil
	}
	return node
}

func (node *domNode) Attributes() map[string]string {
	if node == nil {
		return nil
	}

	attrs := node.value.Get("attributes")
	if attrs.IsUndefined() || attrs.IsNull() {
		return nil
	}

	n := attrs.Get("length").Int()
	out := make(map[string]string, n)
	for i := 0; i < n; i++ {
		attr := attrs.Index(i)
		out[attr.Get("name").String()] = attr.Get("value").String()
	}
	return out
}

func (node *domNode) Underlying() js.Value {
	if node == nil {
		return js.Undefined()
	}
	return node.value
}

func (node *domNode) SetAttribute(key, value string) {
	node.value.Call("setAttribute", key, value)
}

func (node *domNode) setAttribute(key, value string) {
	node.SetAttribute(key, value)
}

func (node *domNode) RemoveAttribute(key string) {
	node.value.Call("removeAttribute", key)
}

func (node *domNode) removeAttribute(key string) {
	node.RemoveAttribute(key)
}

func (node *domNode) SetTextContent(content string) {
	node.value.Set("textContent", content)
}

func (node *domNode) setTextContent(content string) {
	node.SetTextContent(content)
}

func (node *domNode) addEventListener(name string, handler func()) func() {
	if node == nil || name == "" || handler == nil {
		return nil
	}
	fn := node.AddEventListener(name, func(domEvent) {
		handler()
	}, false)
	return func() {
		node.RemoveEventListener(name, fn, false)
	}
}

func (node *domNode) AppendChild(child *domNode) {
	if child != nil {
		node.value.Call("appendChild", child.value)
	}
}

func (node *domNode) appendChild(child domNodeAccess) {
	node.AppendChild(asDOMNode(child))
}

func (node *domNode) insertBefore(child, before domNodeAccess) {
	node.InsertBefore(asDOMNode(child), asDOMNode(before))
}

func (node *domNode) InsertBefore(newChild, before *domNode) {
	if newChild == nil {
		return
	}
	if before == nil {
		node.AppendChild(newChild)
		return
	}
	node.value.Call("insertBefore", newChild.value, before.value)
}

func (node *domNode) ReplaceChild(newChild, oldChild *domNode) {
	if newChild != nil && oldChild != nil {
		node.value.Call("replaceChild", newChild.value, oldChild.value)
	}
}

func (node *domNode) RemoveChild(child *domNode) {
	if child != nil {
		node.value.Call("removeChild", child.value)
	}
}

func (node *domNode) removeChild(child domNodeAccess) {
	node.RemoveChild(asDOMNode(child))
}

func asDOMNode(node domNodeAccess) *domNode {
	if node == nil {
		return nil
	}
	dom, _ := node.(*domNode)
	return dom
}

func (node *domNode) AddEventListener(typ string, cb func(domEvent), useCapture bool) js.Func {
	fn := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			cb(domEvent{value: args[0]})
		}
		return nil
	})
	node.value.Call("addEventListener", typ, fn, useCapture)
	return fn
}

func (node *domNode) RemoveEventListener(typ string, fn js.Func, useCapture bool) {
	node.value.Call("removeEventListener", typ, fn, useCapture)
	fn.Release()
}

func (node *domNode) ParentElement() *domNode {
	return wrapNode(node.value.Get("parentElement"))
}

func (event domEvent) StopImmediatePropagation() {
	event.value.Call("stopImmediatePropagation")
}

func (event domEvent) Target() *domNode {
	return wrapNode(event.value.Get("target"))
}

func (event domEvent) Type() string {
	return event.value.Get("type").String()
}

func (event domEvent) Underlying() js.Value {
	return event.value
}
