//go:build !js || !wasm

package tue

import "fmt"

func mount(target string, component *Comp) error {
	if target == "" {
		return fmt.Errorf("mount target is required")
	}
	if component == nil {
		return fmt.Errorf("component is required")
	}
	return fmt.Errorf("mount is only supported under js/wasm")
}
