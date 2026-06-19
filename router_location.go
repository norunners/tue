//go:build !js || !wasm

package tue

func newRouteLocation() routeLocation {
	return &memoryRouteLocation{path: "/"}
}

type memoryRouteLocation struct {
	path string
}

func (l *memoryRouteLocation) Path() string {
	if l == nil {
		return "/"
	}
	return normalizeRouteTarget(l.path).String()
}

func (l *memoryRouteLocation) SetPath(path string) {
	if l == nil {
		return
	}
	l.path = normalizeRouteTarget(path).String()
}

func (l *memoryRouteLocation) Href(path string) string {
	return "#" + normalizeRouteTarget(path).String()
}

func (l *memoryRouteLocation) Watch(func(string)) func() {
	return func() {}
}
