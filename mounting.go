package tue

import "fmt"

// Mounted is a live component mounted into a runtime target.
type Mounted struct {
	component    *Comp
	target       mountTarget
	tree         *mountedVNode
	renderEffect *componentRenderEffect

	mounted   bool
	unmounted bool
	renderErr error
}

type mountTarget interface {
	domBoundary

	root() domNode
	clear() error
}

func validateMount(target string, component *Comp) error {
	if target == "" {
		return fmt.Errorf("mount target is required")
	}
	if component == nil {
		return fmt.Errorf("component is required")
	}
	return nil
}

func mountComponent(component *Comp, target mountTarget) (*Mounted, error) {
	if component == nil {
		return nil, fmt.Errorf("component is required")
	}
	if target == nil {
		return nil, fmt.Errorf("mount target is required")
	}

	if err := target.clear(); err != nil {
		return nil, fmt.Errorf("clear mount target: %w", err)
	}

	mounted := &Mounted{component: component, target: target}
	mounted.renderEffect = newComponentRenderEffect(mounted)
	mounted.renderEffect.run()
	if mounted.renderErr != nil {
		mounted.renderEffect.stop()
		return nil, mounted.renderErr
	}
	mounted.mounted = true
	component.mounted()
	return mounted, nil
}

// Update renders the component again and then calls optional OnUpdated.
func (m *Mounted) Update() error {
	if m == nil {
		return fmt.Errorf("mounted component is required")
	}
	if m.unmounted {
		return fmt.Errorf("mounted component is unmounted")
	}
	if m.renderEffect == nil {
		return fmt.Errorf("mounted render effect is required")
	}
	m.renderEffect.run()
	return m.renderErr
}

// Unmount calls cleanup functions, clears the target, and calls optional OnUnmounted.
func (m *Mounted) Unmount() error {
	if m == nil {
		return fmt.Errorf("mounted component is required")
	}
	if m.unmounted {
		return nil
	}
	m.unmounted = true

	if m.renderEffect != nil {
		m.renderEffect.stop()
	}
	m.component.runCleanups()
	err := m.target.clear()
	m.component.unmounted()
	if err != nil {
		return fmt.Errorf("clear mount target: %w", err)
	}
	return nil
}

type componentRenderEffect struct {
	mounted *Mounted
	deps    map[*dependency]struct{}
	stopped bool
	queued  bool
}

func newComponentRenderEffect(mounted *Mounted) *componentRenderEffect {
	return &componentRenderEffect{
		mounted: mounted,
		deps:    make(map[*dependency]struct{}),
	}
}

func (e *componentRenderEffect) run() {
	if e == nil || e.stopped || e.mounted == nil || e.mounted.unmounted {
		return
	}

	e.dispose()
	var vnode VNode
	pushSubscriber(e)
	func() {
		defer popSubscriber()
		vnode = e.mounted.component.renderVNode()
	}()

	e.mounted.renderErr = e.mounted.patchRenderedVNode(vnode)
	if e.mounted.renderErr != nil {
		return
	}
	if e.mounted.mounted {
		e.mounted.component.updated()
	}
}

func (e *componentRenderEffect) addDependency(dep *dependency) {
	if e == nil || dep == nil {
		return
	}
	if e.deps == nil {
		e.deps = make(map[*dependency]struct{})
	}
	if _, ok := e.deps[dep]; ok {
		return
	}
	e.deps[dep] = struct{}{}
	dep.addSubscriber(e)
}

func (e *componentRenderEffect) invalidate() {
	if e == nil || e.stopped {
		return
	}
	queueSubscriber(e)
}

func (e *componentRenderEffect) stop() {
	if e == nil || e.stopped {
		return
	}
	e.stopped = true
	e.dispose()
	removeQueuedSubscriber(e)
}

func (e *componentRenderEffect) dispose() {
	if e == nil {
		return
	}
	for dep := range e.deps {
		dep.removeSubscriber(e)
		delete(e.deps, dep)
	}
}

func (e *componentRenderEffect) isQueued() bool {
	return e != nil && e.queued
}

func (e *componentRenderEffect) setQueued(queued bool) {
	if e == nil {
		return
	}
	e.queued = queued
}

func (e *componentRenderEffect) isStopped() bool {
	return e == nil || e.stopped
}

func (m *Mounted) patchRenderedVNode(vnode VNode) error {
	if m == nil {
		return fmt.Errorf("mounted component is required")
	}
	operation := "patch"
	if m.tree == nil {
		operation = "render"
	}
	tree, err := patchVNode(m.target, m.target.root(), m.tree, vnode)
	if err != nil {
		return fmt.Errorf("%s mount target: %w", operation, err)
	}
	m.tree = tree
	return nil
}
