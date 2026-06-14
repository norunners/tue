package vue

// Prop is a parent-provided component input.
type Prop[T any] interface {
	Get() T
	Watch(func(T)) func()
}

// Ref is mutable component-local reactive state.
type Ref[T any] interface {
	Get() T
	Set(T)
}

// Computed is a derived value.
type Computed[T any] interface {
	Get() T
}

type PropValue[T any] struct {
	value T
	get   func() T
	dep   reactiveDep
}

// PropOf creates a concrete prop with a fixed value for generated code.
func PropOf[T any](value T) *PropValue[T] {
	return &PropValue[T]{value: value}
}

// PropOfFunc creates a concrete prop that reads from a generated getter.
func PropOfFunc[T any](get func() T) *PropValue[T] {
	return &PropValue[T]{get: get}
}

func (prop *PropValue[T]) Get() T {
	if prop == nil {
		var zero T
		return zero
	}
	prop.dep.track()
	if prop.get != nil {
		return prop.get()
	}
	return prop.value
}

func (prop *PropValue[T]) Watch(watcher func(T)) func() {
	if prop == nil || watcher == nil {
		return func() {}
	}
	first := true
	return Watch(func() {
		value := prop.Get()
		if first {
			first = false
			return
		}
		watcher(value)
	})
}

type RefValue[T any] struct {
	value T
	dep   reactiveDep
}

// RefOf creates mutable component-local state.
func RefOf[T any](initial T) *RefValue[T] {
	return &RefValue[T]{value: initial}
}

func (ref *RefValue[T]) Get() T {
	if ref == nil {
		var zero T
		return zero
	}
	ref.dep.track()
	return ref.value
}

func (ref *RefValue[T]) Set(value T) {
	if ref != nil {
		ref.value = value
		ref.dep.trigger()
	}
}

type ComputedValue[T any] struct {
	compute func() T
	value   T
	dirty   bool
	dep     reactiveDep
	effect  *reactiveEffect
}

// ComputedOfFunc creates a derived value.
func ComputedOfFunc[T any](compute func() T) *ComputedValue[T] {
	computed := &ComputedValue[T]{
		compute: compute,
		dirty:   true,
	}
	if compute == nil {
		return computed
	}
	computed.effect = newReactiveEffect(func() {
		computed.value = compute()
		computed.dirty = false
	}, func() {
		if computed.dirty {
			return
		}
		computed.dirty = true
		computed.dep.trigger()
	})
	registerReactiveCleanup(computed.effect.stop)
	return computed
}

func (computed *ComputedValue[T]) Get() T {
	if computed == nil || computed.compute == nil {
		var zero T
		return zero
	}
	computed.dep.track()
	if computed.dirty {
		computed.effect.run()
	}
	return computed.value
}

// Batch coalesces reactive invalidations until fn returns.
//
// Outside Batch, effects flush before a reactive Set call returns. Inside nested
// Batch calls, effects are deduplicated and flushed after the outermost Batch.
func Batch(fn func()) {
	if fn == nil {
		return
	}
	defaultScheduler.batch(fn)
}

// Watch runs an effect immediately, tracks any reactive values it reads, and
// reruns it after those values change. The returned function stops the watcher.
func Watch(effect func()) func() {
	if effect == nil {
		return func() {}
	}
	watcher := newReactiveEffect(effect, nil)
	stop := watcher.stop
	registerReactiveCleanup(stop)
	watcher.run()
	return stop
}

type reactiveDep struct {
	effects map[*reactiveEffect]struct{}
}

func (dep *reactiveDep) track() {
	if activeEffect == nil || activeEffect.stopped {
		return
	}
	if dep.effects == nil {
		dep.effects = map[*reactiveEffect]struct{}{}
	}
	if _, ok := dep.effects[activeEffect]; ok {
		return
	}
	dep.effects[activeEffect] = struct{}{}
	activeEffect.deps = append(activeEffect.deps, dep)
}

func (dep *reactiveDep) trigger() {
	if len(dep.effects) == 0 {
		return
	}
	effects := make([]*reactiveEffect, 0, len(dep.effects))
	for effect := range dep.effects {
		if effect != activeEffect {
			effects = append(effects, effect)
		}
	}
	for _, effect := range effects {
		effect.schedule()
	}
}

type reactiveEffect struct {
	runEffect func()
	scheduler func()
	deps      []*reactiveDep
	stopped   bool
}

func newReactiveEffect(runEffect func(), scheduler func()) *reactiveEffect {
	return &reactiveEffect{
		runEffect: runEffect,
		scheduler: scheduler,
	}
}

func (effect *reactiveEffect) run() {
	if effect == nil || effect.stopped || effect.runEffect == nil {
		return
	}
	effect.cleanupDeps()

	previous := activeEffect
	activeEffect = effect
	defer func() {
		activeEffect = previous
	}()

	effect.runEffect()
}

func (effect *reactiveEffect) schedule() {
	if effect == nil || effect.stopped {
		return
	}
	if effect.scheduler != nil {
		effect.scheduler()
		return
	}
	defaultScheduler.enqueue(effect)
}

func (effect *reactiveEffect) stop() {
	if effect == nil || effect.stopped {
		return
	}
	effect.stopped = true
	effect.cleanupDeps()
}

func (effect *reactiveEffect) cleanupDeps() {
	for _, dep := range effect.deps {
		delete(dep.effects, effect)
	}
	effect.deps = nil
}

var activeEffect *reactiveEffect

type reactiveScheduler struct {
	batchDepth int
	flushing   bool
	queue      []*reactiveEffect
	queued     map[*reactiveEffect]struct{}
}

var defaultScheduler reactiveScheduler

func (scheduler *reactiveScheduler) batch(fn func()) {
	scheduler.batchDepth++
	defer func() {
		scheduler.batchDepth--
		if scheduler.batchDepth == 0 {
			scheduler.flush()
		}
	}()
	fn()
}

func (scheduler *reactiveScheduler) enqueue(effect *reactiveEffect) {
	if effect == nil || effect.stopped {
		return
	}
	if scheduler.queued == nil {
		scheduler.queued = map[*reactiveEffect]struct{}{}
	}
	if _, ok := scheduler.queued[effect]; ok {
		return
	}
	scheduler.queued[effect] = struct{}{}
	scheduler.queue = append(scheduler.queue, effect)
	if scheduler.batchDepth == 0 && !scheduler.flushing {
		scheduler.flush()
	}
}

func (scheduler *reactiveScheduler) flush() {
	if scheduler.flushing {
		return
	}
	scheduler.flushing = true
	defer func() {
		scheduler.flushing = false
	}()

	for len(scheduler.queue) > 0 {
		queue := scheduler.queue
		scheduler.queue = nil
		scheduler.queued = nil
		for _, effect := range queue {
			if !effect.stopped {
				effect.run()
			}
		}
	}
}

type reactiveScope interface {
	addReactiveCleanup(func())
}

var activeReactiveScope reactiveScope

func withReactiveScope(scope reactiveScope, fn func()) {
	previous := activeReactiveScope
	activeReactiveScope = scope
	defer func() {
		activeReactiveScope = previous
	}()
	fn()
}

func registerReactiveCleanup(cleanup func()) {
	if activeReactiveScope == nil || cleanup == nil {
		return
	}
	activeReactiveScope.addReactiveCleanup(cleanup)
}

func withoutReactiveTracking(fn func()) {
	if fn == nil {
		return
	}
	previous := activeEffect
	activeEffect = nil
	defer func() {
		activeEffect = previous
	}()
	fn()
}
