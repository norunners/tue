//go:build !js || !wasm

package vue

import "fmt"

// Mount is only available in browser WebAssembly builds.
func Mount(selector string, component *Comp) error {
	if component == nil {
		return fmt.Errorf("mount %q: nil component", selector)
	}
	return fmt.Errorf("mount %q: js/wasm runtime is unavailable", selector)
}
