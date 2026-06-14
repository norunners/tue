package tue

// EventBinding is a native DOM event handler generated from @event attributes.
type EventBinding struct {
	Name    string
	Handler func()
}

// On returns a native DOM event binding.
func On(name string, handler func()) EventBinding {
	return EventBinding{Name: name, Handler: handler}
}
