//go:build js && wasm

package tue

import "syscall/js"

func newRouteLocation() routeLocation {
	return hashRouteLocation{}
}

type hashRouteLocation struct{}

func (hashRouteLocation) Path() string {
	location := js.Global().Get("location")
	if location.IsUndefined() || location.IsNull() {
		return "/"
	}
	return normalizeRouteTarget(location.Get("hash").String()).String()
}

func (hashRouteLocation) SetPath(path string) {
	location := js.Global().Get("location")
	if location.IsUndefined() || location.IsNull() {
		return
	}
	location.Set("hash", normalizeRouteTarget(path).String())
}

func (hashRouteLocation) Href(path string) string {
	return "#" + normalizeRouteTarget(path).String()
}

func (l hashRouteLocation) Watch(handler func(string)) func() {
	window := js.Global()
	if window.IsUndefined() || window.IsNull() {
		return func() {}
	}
	listener := js.FuncOf(func(js.Value, []js.Value) any {
		if handler != nil {
			handler(l.Path())
		}
		return nil
	})
	window.Call("addEventListener", "hashchange", listener)
	return func() {
		window.Call("removeEventListener", "hashchange", listener)
		listener.Release()
	}
}
