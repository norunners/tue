//go:build js && wasm

package tue

import (
	"fmt"
	"syscall/js"
)

func mount(target string, component *Comp) error {
	if target == "" {
		return fmt.Errorf("mount target is required")
	}
	if component == nil {
		return fmt.Errorf("component is required")
	}

	document := js.Global().Get("document")
	element := document.Call("querySelector", target)
	if element.IsNull() || element.IsUndefined() {
		return fmt.Errorf("mount target %q not found", target)
	}
	element.Set("innerHTML", RenderHTML(component.Render()))
	return nil
}
