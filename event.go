package tue

// Event exposes the DOM event target values used by generated bindings.
type Event interface {
	Value() string
	Checked() bool
}

// EventBinding is a native DOM event handler generated from @event attributes.
type EventBinding struct {
	Name    string
	Handler func(Event)
}

// On returns a native DOM event binding.
func On(name string, handler func()) EventBinding {
	return EventBinding{Name: name, Handler: func(Event) {
		if handler != nil {
			handler()
		}
	}}
}

// OnValue returns a native DOM event binding that receives target.value.
func OnValue(name string, handler func(string)) EventBinding {
	return EventBinding{Name: name, Handler: func(event Event) {
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
	return EventBinding{Name: name, Handler: func(event Event) {
		if handler != nil {
			if event == nil {
				handler(false)
				return
			}
			handler(event.Checked())
		}
	}}
}
