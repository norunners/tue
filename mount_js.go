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
	value := ""
	if attr.HasValue {
		value = attr.Value
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
