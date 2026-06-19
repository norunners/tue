package tue

import (
	stdcontext "context"
	"sync"
)

// Resource is the async-state interface exposed to component code.
type Resource[T any] interface {
	Value() (T, bool)
	Error() error
	Loading() bool
	Reload()
}

// ResourceValue is the concrete runtime storage for async resource state.
type ResourceValue[T any] struct {
	load func(stdcontext.Context) (T, error)

	mu       sync.Mutex
	value    T
	hasValue bool
	err      error
	loading  bool
	stopped  bool
	runID    int
	cancel   stdcontext.CancelFunc
	dep      dependency
}

// ResourceOfFunc returns a resource backed by a load function and starts the
// initial load immediately.
func ResourceOfFunc[T any](ctx Context, load func() (T, error)) *ResourceValue[T] {
	if load == nil {
		return ResourceOfContextFunc[T](ctx, nil)
	}
	return ResourceOfContextFunc(ctx, func(stdcontext.Context) (T, error) {
		return load()
	})
}

// ResourceOfContextFunc returns a resource backed by a context-aware load
// function and starts the initial load immediately. Cancellation is cooperative:
// Reload and component cleanup cancel the load context, and stale results are
// ignored if a loader keeps running after cancellation.
func ResourceOfContextFunc[T any](ctx Context, load func(stdcontext.Context) (T, error)) *ResourceValue[T] {
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

	load, loadCtx, runID, ok := r.beginLoad()
	if !ok {
		return
	}

	go func() {
		value, err := load(loadCtx)
		r.finishLoad(runID, value, err)
	}()
}

func (r *ResourceValue[T]) beginLoad() (func(stdcontext.Context) (T, error), stdcontext.Context, int, bool) {
	r.mu.Lock()
	if r.stopped || r.load == nil {
		r.mu.Unlock()
		return nil, nil, 0, false
	}

	var zero T
	oldCancel := r.cancel
	loadCtx, cancel := stdcontext.WithCancel(stdcontext.Background())
	r.runID++
	runID := r.runID
	load := r.load
	r.cancel = cancel
	r.value = zero
	r.hasValue = false
	r.err = nil
	r.loading = true
	r.mu.Unlock()

	if oldCancel != nil {
		oldCancel()
	}
	r.dep.notify()
	return load, loadCtx, runID, true
}

func (r *ResourceValue[T]) finishLoad(runID int, value T, err error) {
	r.mu.Lock()
	if r.stopped || r.runID != runID {
		r.mu.Unlock()
		return
	}

	var zero T
	cancel := r.cancel
	r.cancel = nil
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

	if cancel != nil {
		cancel()
	}
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
	cancel := r.cancel
	r.cancel = nil
	r.loading = false
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	r.dep.notify()
}
