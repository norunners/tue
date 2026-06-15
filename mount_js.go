//go:build js && wasm

package tue

import (
	"fmt"
	"syscall/js"
)

func mount(target string, component *Comp) (*Mounted, error) {
	if err := validateMount(target, component); err != nil {
		return nil, err
	}

	document := js.Global().Get("document")
	element := document.Call("querySelector", target)
	if element.IsNull() || element.IsUndefined() {
		return nil, fmt.Errorf("mount target %q not found", target)
	}
	return mountComponent(component, jsMountTarget{element: element})
}

type jsMountTarget struct {
	element js.Value
}

func (t jsMountTarget) root() domNode {
	return t.element
}

func (t jsMountTarget) createElement(tag string) (domNode, error) {
	return js.Global().Get("document").Call("createElement", tag), nil
}

func (t jsMountTarget) createText(text string) (domNode, error) {
	return js.Global().Get("document").Call("createTextNode", text), nil
}

func (t jsMountTarget) createMarker(text string) (domNode, error) {
	return js.Global().Get("document").Call("createComment", text), nil
}

func (t jsMountTarget) appendChild(parent domNode, child domNode) error {
	parentValue, childValue, err := jsParentChild(parent, child)
	if err != nil {
		return err
	}
	parentValue.Call("appendChild", childValue)
	return nil
}

func (t jsMountTarget) insertBefore(parent domNode, child domNode, before domNode) error {
	parentValue, childValue, err := jsParentChild(parent, child)
	if err != nil {
		return err
	}
	beforeValue, ok := before.(js.Value)
	if !ok {
		return fmt.Errorf("expected js.Value before node, got %T", before)
	}
	parentValue.Call("insertBefore", childValue, beforeValue)
	return nil
}

func (t jsMountTarget) removeChild(parent domNode, child domNode) error {
	parentValue, childValue, err := jsParentChild(parent, child)
	if err != nil {
		return err
	}
	parentValue.Call("removeChild", childValue)
	return nil
}

func (t jsMountTarget) setText(node domNode, text string) error {
	nodeValue, ok := node.(js.Value)
	if !ok {
		return fmt.Errorf("expected js.Value text node, got %T", node)
	}
	nodeValue.Set("nodeValue", text)
	return nil
}

func (t jsMountTarget) setAttr(node domNode, attr Attribute) error {
	nodeValue, ok := node.(js.Value)
	if !ok {
		return fmt.Errorf("expected js.Value element node, got %T", node)
	}
	if attr.HasBoolValue {
		nodeValue.Set(attr.Name, attr.BoolValue)
		if attr.BoolValue {
			nodeValue.Call("setAttribute", attr.Name, "")
		} else {
			nodeValue.Call("removeAttribute", attr.Name)
		}
		return nil
	}
	value := ""
	if attr.HasValue {
		value = attr.Value
	}
	if attr.Name == "value" {
		nodeValue.Set("value", value)
	}
	nodeValue.Call("setAttribute", attr.Name, value)
	return nil
}

func (t jsMountTarget) removeAttr(node domNode, name string) error {
	nodeValue, ok := node.(js.Value)
	if !ok {
		return fmt.Errorf("expected js.Value element node, got %T", node)
	}
	nodeValue.Call("removeAttribute", name)
	return nil
}

func (t jsMountTarget) addEventListener(node domNode, name string, handler func(Event)) (func(), error) {
	nodeValue, ok := node.(js.Value)
	if !ok {
		return nil, fmt.Errorf("expected js.Value element node, got %T", node)
	}
	listener := js.FuncOf(func(this js.Value, args []js.Value) any {
		if handler != nil {
			handler(jsEvent(args))
		}
		return nil
	})
	nodeValue.Call("addEventListener", name, listener)
	return func() {
		nodeValue.Call("removeEventListener", name, listener)
		listener.Release()
	}, nil
}

type jsEvent []js.Value

func (e jsEvent) Value() string {
	target := e.target()
	if target.IsUndefined() || target.IsNull() {
		return ""
	}
	return target.Get("value").String()
}

func (e jsEvent) Checked() bool {
	target := e.target()
	if target.IsUndefined() || target.IsNull() {
		return false
	}
	return target.Get("checked").Bool()
}

func (e jsEvent) target() js.Value {
	if len(e) == 0 {
		return js.Undefined()
	}
	event := e[0]
	if event.IsUndefined() || event.IsNull() {
		return js.Undefined()
	}
	return event.Get("target")
}

func (t jsMountTarget) clear() error {
	t.element.Set("textContent", "")
	return nil
}

func jsParentChild(parent domNode, child domNode) (js.Value, js.Value, error) {
	parentValue, ok := parent.(js.Value)
	if !ok {
		return js.Value{}, js.Value{}, fmt.Errorf("expected js.Value parent node, got %T", parent)
	}
	childValue, ok := child.(js.Value)
	if !ok {
		return js.Value{}, js.Value{}, fmt.Errorf("expected js.Value child node, got %T", child)
	}
	return parentValue, childValue, nil
}
