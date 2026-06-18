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
	return normalizeRoutePath(location.Get("hash").String())
}

func (hashRouteLocation) SetPath(path string) {
	location := js.Global().Get("location")
	if location.IsUndefined() || location.IsNull() {
		return
	}
	location.Set("hash", normalizeRoutePath(path))
}

func (hashRouteLocation) Href(path string) string {
	return "#" + normalizeRoutePath(path)
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
