//go:build !js || !wasm

package tue

import "fmt"

func mount(target string, component *Comp) (*Mounted, error) {
	if err := validateMount(target, component); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("mount is only supported under js/wasm")
}
