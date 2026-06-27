package tue

// State is the read/write interface used by generated component state.
type State[T any] interface {
	Get() T
	Set(T)
	Watch(func(T)) func()
}

// StateValue is the concrete runtime storage for reactive state.
type StateValue[T any] struct {
	value T
	dep   dependency
}

// StateOf returns reactive state with an initial value.
func StateOf[T any](value T) *StateValue[T] {
	return &StateValue[T]{value: value}
}

// Get returns the current state value.
func (r *StateValue[T]) Get() T {
	if r == nil {
		var zero T
		return zero
	}
	r.dep.track()
	return r.value
}

// Set updates the state value and invalidates dependents.
func (r *StateValue[T]) Set(value T) {
	if r == nil {
		return
	}
	r.value = value
	r.dep.notify()
}

// Watch observes state changes and returns a stop function.
func (r *StateValue[T]) Watch(effect func(T)) func() {
	if effect == nil {
		return func() {}
	}
	return Watch(func() {
		effect(r.Get())
	})
}

// Computed is the read interface used by generated computed accessors and
// handwritten reactive code.
type Computed[T any] interface {
	Get() T
	Watch(func(T)) func()
}

// ComputedValue is the concrete runtime storage for a computed value.
type ComputedValue[T any] struct {
	compute func() T
	value   T
	dirty   bool
	dep     dependency
	deps    map[*dependency]struct{}
}

// ComputedOfFunc returns a lazy, cached computed value backed by a function.
func ComputedOfFunc[T any](compute func() T) *ComputedValue[T] {
	computed := &ComputedValue[T]{
		compute: compute,
		dirty:   true,
		deps:    make(map[*dependency]struct{}),
	}
	registerEffectCleanup(computed.dispose)
	return computed
}

// Get returns the current computed value, recomputing only when dirty.
func (c *ComputedValue[T]) Get() T {
	if c == nil || c.compute == nil {
		var zero T
		return zero
	}
	c.dep.track()
	if c.dirty {
		c.dispose()
		pushSubscriber(c)
		defer popSubscriber()

		c.value = c.compute()
		c.dirty = false
	}
	return c.value
}

// Watch observes computed changes and returns a stop function.
func (c *ComputedValue[T]) Watch(effect func(T)) func() {
	if effect == nil {
		return func() {}
	}
	return Watch(func() {
		effect(c.Get())
	})
}

func (c *ComputedValue[T]) addDependency(dep *dependency) {
	if c == nil || dep == nil {
		return
	}
	if c.deps == nil {
		c.deps = make(map[*dependency]struct{})
	}
	if _, ok := c.deps[dep]; ok {
		return
	}
	c.deps[dep] = struct{}{}
	dep.addSubscriber(c)
}

func (c *ComputedValue[T]) invalidate() {
	if c == nil || c.dirty {
		return
	}
	c.dirty = true
	c.dep.notify()
}

func (c *ComputedValue[T]) dispose() {
	if c == nil {
		return
	}
	for dep := range c.deps {
		dep.removeSubscriber(c)
		delete(c.deps, dep)
	}
}

// Batch deduplicates reactive effect reruns until the outermost batch returns.
func Batch(fn func()) {
	if fn == nil {
		return
	}
	scheduler.batchDepth++
	defer func() {
		scheduler.batchDepth--
		if scheduler.batchDepth == 0 {
			flushSubscribers()
		}
	}()
	fn()
}

// Watch runs effect immediately, tracks reactive reads, and returns a stop function.
func Watch(effect func()) func() {
	if effect == nil {
		return func() {}
	}

	watcher := &watcher{
		effect: effect,
		deps:   make(map[*dependency]struct{}),
	}
	registerEffectCleanup(watcher.stop)
	watcher.run()
	return watcher.stop
}

type reactiveSubscriber interface {
	addDependency(*dependency)
	invalidate()
}

type dependency struct {
	subscribers map[reactiveSubscriber]struct{}
}

func (d *dependency) track() {
	subscriber := currentSubscriber()
	if d == nil || subscriber == nil {
		return
	}
	subscriber.addDependency(d)
}

func (d *dependency) addSubscriber(subscriber reactiveSubscriber) {
	if d == nil || subscriber == nil {
		return
	}
	if d.subscribers == nil {
		d.subscribers = make(map[reactiveSubscriber]struct{})
	}
	d.subscribers[subscriber] = struct{}{}
}

func (d *dependency) removeSubscriber(subscriber reactiveSubscriber) {
	if d == nil || d.subscribers == nil || subscriber == nil {
		return
	}
	delete(d.subscribers, subscriber)
}

func (d *dependency) notify() {
	if d == nil || len(d.subscribers) == 0 {
		return
	}
	subscribers := make([]reactiveSubscriber, 0, len(d.subscribers))
	for subscriber := range d.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	for _, subscriber := range subscribers {
		subscriber.invalidate()
	}
}

type watcher struct {
	effect  func()
	deps    map[*dependency]struct{}
	stopped bool
	queued  bool
}

func (w *watcher) run() {
	if w == nil || w.stopped {
		return
	}
	w.queued = false
	w.dispose()
	pushSubscriber(w)
	defer popSubscriber()
	w.effect()
}

func (w *watcher) addDependency(dep *dependency) {
	if w == nil || dep == nil {
		return
	}
	if w.deps == nil {
		w.deps = make(map[*dependency]struct{})
	}
	if _, ok := w.deps[dep]; ok {
		return
	}
	w.deps[dep] = struct{}{}
	dep.addSubscriber(w)
}

func (w *watcher) stop() {
	if w == nil || w.stopped {
		return
	}
	w.stopped = true
	w.dispose()
	removeQueuedSubscriber(w)
}

func (w *watcher) dispose() {
	if w == nil {
		return
	}
	for dep := range w.deps {
		dep.removeSubscriber(w)
		delete(w.deps, dep)
	}
}

func (w *watcher) invalidate() {
	if w == nil || w.stopped {
		return
	}
	queueSubscriber(w)
}

func (w *watcher) isQueued() bool {
	return w != nil && w.queued
}

func (w *watcher) setQueued(queued bool) {
	if w == nil {
		return
	}
	w.queued = queued
}

func (w *watcher) isStopped() bool {
	return w == nil || w.stopped
}

var subscriberStack []reactiveSubscriber

func pushSubscriber(subscriber reactiveSubscriber) {
	subscriberStack = append(subscriberStack, subscriber)
}

func popSubscriber() {
	if len(subscriberStack) == 0 {
		return
	}
	subscriberStack = subscriberStack[:len(subscriberStack)-1]
}

func currentSubscriber() reactiveSubscriber {
	if len(subscriberStack) == 0 {
		return nil
	}
	return subscriberStack[len(subscriberStack)-1]
}

type schedulerState struct {
	batchDepth int
	flushing   bool
	pending    []scheduledSubscriber
}

var scheduler schedulerState

type scheduledSubscriber interface {
	reactiveSubscriber
	run()
	isQueued() bool
	setQueued(bool)
	isStopped() bool
}

func queueSubscriber(subscriber scheduledSubscriber) {
	if subscriber == nil || subscriber.isStopped() || subscriber.isQueued() {
		return
	}
	subscriber.setQueued(true)
	scheduler.pending = append(scheduler.pending, subscriber)
	if scheduler.batchDepth == 0 {
		flushSubscribers()
	}
}

func removeQueuedSubscriber(subscriber scheduledSubscriber) {
	if subscriber == nil || !subscriber.isQueued() {
		return
	}
	for i, pending := range scheduler.pending {
		if pending == subscriber {
			scheduler.pending = append(scheduler.pending[:i], scheduler.pending[i+1:]...)
			break
		}
	}
	subscriber.setQueued(false)
}

func flushSubscribers() {
	if scheduler.flushing {
		return
	}
	scheduler.flushing = true
	defer func() {
		scheduler.flushing = false
	}()

	for len(scheduler.pending) > 0 {
		pending := scheduler.pending
		scheduler.pending = nil
		for _, subscriber := range pending {
			if !subscriber.isQueued() {
				continue
			}
			subscriber.setQueued(false)
			subscriber.run()
		}
	}
}

var componentScopeStack []*CompInstance

func withComponentScope(component *CompInstance, fn func()) {
	componentScopeStack = append(componentScopeStack, component)
	defer func() {
		componentScopeStack = componentScopeStack[:len(componentScopeStack)-1]
	}()
	fn()
}

func currentComponentScope() (*CompInstance, bool) {
	if len(componentScopeStack) == 0 {
		return nil, false
	}
	return componentScopeStack[len(componentScopeStack)-1], true
}

func registerEffectCleanup(cleanup func()) {
	if component, ok := currentComponentScope(); ok {
		component.addEffectCleanup(cleanup)
	}
}
