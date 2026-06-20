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

// On is an optional component event callback.
//
// F must be a function type. Go does not provide a constraint that accepts
// every function signature, so the compiler validates this requirement for
// component fields.
type On[F any] struct {
	fn F
	ok bool
}

// OnOf returns a component event callback backed by fn.
func OnOf[F any](fn F) On[F] {
	return On[F]{fn: fn, ok: true}
}

// Func returns the wrapped component event callback.
func (on On[F]) Func() F {
	return on.fn
}

// Ok reports whether the component event callback was initialized by OnOf.
func (on On[F]) Ok() bool {
	return on.ok
}

// FuncOk returns the wrapped callback and whether it was initialized by OnOf.
func (on On[F]) FuncOk() (F, bool) {
	return on.Func(), on.Ok()
}

// EventOf returns a native DOM event binding.
func EventOf(name string, handler func()) EventBinding {
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
