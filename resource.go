package tue

import "sync"

// Resource is the async-state interface exposed to component code.
type Resource[T any] interface {
	Value() (T, bool)
	Error() error
	Loading() bool
	Reload()
}

// ResourceValue is the concrete runtime storage for async resource state.
type ResourceValue[T any] struct {
	load func() (T, error)

	mu       sync.Mutex
	value    T
	hasValue bool
	err      error
	loading  bool
	stopped  bool
	runID    int
	dep      dependency
}

// ResourceOfFunc returns a resource backed by a load function and starts the
// initial load immediately.
func ResourceOfFunc[T any](ctx Context, load func() (T, error)) *ResourceValue[T] {
	resource := &ResourceValue[T]{load: load}
	if ctx != nil {
		ctx.OnCleanup(resource.stop)
	}
	resource.Reload()
	return resource
}

// Value returns the loaded value and whether a value is currently available.
func (r *ResourceValue[T]) Value() (T, bool) {
	if r == nil {
		var zero T
		return zero, false
	}
	r.dep.track()

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value, r.hasValue
}

// Error returns the most recent load error.
func (r *ResourceValue[T]) Error() error {
	if r == nil {
		return nil
	}
	r.dep.track()

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// Loading reports whether a load is currently in flight.
func (r *ResourceValue[T]) Loading() bool {
	if r == nil {
		return false
	}
	r.dep.track()

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loading
}

// Reload starts a new load and invalidates any older in-flight result.
func (r *ResourceValue[T]) Reload() {
	if r == nil {
		return
	}

	load, runID, ok := r.beginLoad()
	if !ok {
		return
	}

	go func() {
		value, err := load()
		r.finishLoad(runID, value, err)
	}()
}

func (r *ResourceValue[T]) beginLoad() (func() (T, error), int, bool) {
	r.mu.Lock()
	if r.stopped || r.load == nil {
		r.mu.Unlock()
		return nil, 0, false
	}

	var zero T
	r.runID++
	runID := r.runID
	load := r.load
	r.value = zero
	r.hasValue = false
	r.err = nil
	r.loading = true
	r.mu.Unlock()

	r.dep.notify()
	return load, runID, true
}

func (r *ResourceValue[T]) finishLoad(runID int, value T, err error) {
	r.mu.Lock()
	if r.stopped || r.runID != runID {
		r.mu.Unlock()
		return
	}

	var zero T
	r.loading = false
	r.err = err
	if err != nil {
		r.value = zero
		r.hasValue = false
	} else {
		r.value = value
		r.hasValue = true
	}
	r.mu.Unlock()

	r.dep.notify()
}

func (r *ResourceValue[T]) stop() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	r.runID++
	r.loading = false
	r.mu.Unlock()

	r.dep.notify()
}
