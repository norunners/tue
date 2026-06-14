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

func (t jsMountTarget) render(node VNode) error {
	t.element.Set("innerHTML", RenderHTML(node))
	return nil
}

func (t jsMountTarget) clear() error {
	t.element.Set("innerHTML", "")
	return nil
}
