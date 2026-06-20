package tue

// DOMEvent exposes the DOM event target values used by generated bindings.
type DOMEvent interface {
	Value() string
	Checked() bool
}

// EventBinding is a native DOM event handler generated from @event attributes.
type EventBinding struct {
	Name    string
	Handler func(DOMEvent)
}

// EventOf returns a native DOM event binding.
func EventOf(name string, handler func()) EventBinding {
	return EventBinding{Name: name, Handler: func(DOMEvent) {
		if handler != nil {
			handler()
		}
	}}
}

// OnValue returns a native DOM event binding that receives target.value.
func OnValue(name string, handler func(string)) EventBinding {
	return EventBinding{Name: name, Handler: func(event DOMEvent) {
		if handler != nil {
			if event == nil {
				handler("")
				return
			}
			handler(event.Value())
		}
	}}
}

// OnChecked returns a native DOM event binding that receives target.checked.
func OnChecked(name string, handler func(bool)) EventBinding {
	return EventBinding{Name: name, Handler: func(event DOMEvent) {
		if handler != nil {
			if event == nil {
				handler(false)
				return
			}
			handler(event.Checked())
		}
	}}
}
