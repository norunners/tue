package vue

import "fmt"

// Context is passed to component initialization so components can register
// lifecycle hooks without depending on the DOM implementation.
type Context interface {
	OnMounted(func())
	OnUnmounted(func())
	OnUpdated(func())
}

// Comp is the runtime descriptor generated constructors return.
//
// The user-authored component value remains the concrete struct declared in a
// .tue file; Comp carries the render function and lifecycle state the runtime
// needs to mount it.
type Comp struct {
	instance any
	render   func() VNode
	ctx      *componentContext

	target       mountTarget
	mountedTree  *mountedVNode
	renderEffect *reactiveEffect
	renderErr    error
	mounted      bool
}

type initializer interface {
	Init(Context)
}

// CompOf creates a runtime component descriptor from a user-authored component
// value and generated render function.
//
// Generated code is expected to allocate the component value and populate prop
// fields before calling CompOf. If the value implements Init(Context), Init is
// called immediately, before any DOM is attached.
func CompOf(instance any, render func() VNode) *Comp {
	ctx := &componentContext{}
	comp := &Comp{
		instance: instance,
		render:   render,
		ctx:      ctx,
	}
	if init, ok := instance.(initializer); ok {
		withReactiveScope(ctx, func() {
			init.Init(ctx)
		})
	}
	return comp
}

// Instance returns the user-authored component value behind this descriptor.
func (comp *Comp) Instance() any {
	if comp == nil {
		return nil
	}
	return comp.instance
}

// Render returns the current virtual node tree for the component.
func (comp *Comp) Render() VNode {
	if comp == nil || comp.render == nil {
		return Fragment()
	}
	return comp.render()
}

// RenderHTML renders the component's current virtual node tree to static HTML.
func (comp *Comp) RenderHTML() string {
	return RenderHTML(comp.Render())
}

func (comp *Comp) mount(target mountTarget) error {
	if comp == nil {
		return fmt.Errorf("mount component: nil component")
	}
	_, root, err := mountTargetParts(target)
	if err != nil {
		return fmt.Errorf("mount component: %w", err)
	}
	wasMounted := comp.mounted
	if wasMounted {
		comp.stopRenderEffect()
		comp.unmountDOM()
		comp.mounted = false
	}

	root.setTextContent("")
	comp.target = target
	comp.mounted = true
	comp.startRenderEffect()
	if err := comp.runRenderEffect(); err != nil {
		comp.stopRenderEffect()
		comp.unmountDOM()
		comp.mounted = false
		return fmt.Errorf("mount component: %w", err)
	}
	if wasMounted {
		comp.ctx.runUpdated()
		return nil
	}
	comp.ctx.runMounted()
	return nil
}

// Update re-renders the component into its current mount target.
func (comp *Comp) Update() error {
	if comp == nil {
		return fmt.Errorf("update component: nil component")
	}
	if !comp.mounted {
		return fmt.Errorf("update component: component is not mounted")
	}
	if err := comp.runRenderEffect(); err != nil {
		return fmt.Errorf("update component: %w", err)
	}
	return nil
}

func (comp *Comp) startRenderEffect() {
	comp.renderEffect = newReactiveEffect(func() {
		if comp == nil || !comp.mounted {
			return
		}
		withReactiveScope(comp.ctx, func() {
			comp.renderErr = comp.renderMountedTree()
		})
	}, nil)
}

func (comp *Comp) runRenderEffect() error {
	if comp == nil {
		return fmt.Errorf("nil component")
	}
	if comp.renderEffect == nil {
		return comp.renderMountedTree()
	}
	comp.renderErr = nil
	comp.renderEffect.run()
	return comp.renderErr
}

func (comp *Comp) renderMountedTree() error {
	document, root, err := mountTargetParts(comp.target)
	if err != nil {
		return err
	}
	next := comp.Render()
	if comp.mountedTree == nil {
		var mounted *mountedVNode
		withoutReactiveTracking(func() {
			mounted, err = mountVNode(document, root, next)
		})
		if err != nil {
			return err
		}
		comp.mountedTree = mounted
		return nil
	}
	var mounted *mountedVNode
	withoutReactiveTracking(func() {
		mounted, err = patchMountedVNode(document, root, comp.mountedTree, next)
	})
	if err != nil {
		return err
	}
	comp.mountedTree = mounted
	withoutReactiveTracking(comp.ctx.runUpdated)
	return nil
}

// Unmount removes the component's static HTML and runs unmount hooks.
func (comp *Comp) Unmount() error {
	if comp == nil {
		return fmt.Errorf("unmount component: nil component")
	}
	if !comp.mounted {
		return nil
	}
	comp.stopRenderEffect()
	comp.unmountDOM()
	comp.mounted = false
	comp.ctx.cleanupReactiveEffects()
	comp.ctx.runUnmounted()
	return nil
}

func (comp *Comp) stopRenderEffect() {
	if comp == nil || comp.renderEffect == nil {
		return
	}
	comp.renderEffect.stop()
	comp.renderEffect = nil
	comp.renderErr = nil
}

func (comp *Comp) unmountDOM() {
	if comp == nil || comp.target == nil || comp.mountedTree == nil {
		return
	}
	root := comp.target.root()
	if root != nil {
		unmountVNode(root, comp.mountedTree)
	}
	comp.target = nil
	comp.mountedTree = nil
}

func mountTargetParts(target mountTarget) (domDocumentAccess, domNodeAccess, error) {
	if target == nil {
		return nil, nil, fmt.Errorf("nil target")
	}
	document := target.document()
	if document == nil {
		return nil, nil, fmt.Errorf("nil DOM document")
	}
	root := target.root()
	if root == nil {
		return nil, nil, fmt.Errorf("nil DOM root")
	}
	return document, root, nil
}

type componentContext struct {
	mounted          []func()
	unmounted        []func()
	updated          []func()
	reactiveCleanups []func()
}

func (ctx *componentContext) OnMounted(hook func()) {
	if hook != nil {
		ctx.mounted = append(ctx.mounted, hook)
	}
}

func (ctx *componentContext) OnUnmounted(hook func()) {
	if hook != nil {
		ctx.unmounted = append(ctx.unmounted, hook)
	}
}

func (ctx *componentContext) OnUpdated(hook func()) {
	if hook != nil {
		ctx.updated = append(ctx.updated, hook)
	}
}

func (ctx *componentContext) runMounted() {
	for _, hook := range ctx.mounted {
		hook()
	}
}

func (ctx *componentContext) runUnmounted() {
	for _, hook := range ctx.unmounted {
		hook()
	}
}

func (ctx *componentContext) runUpdated() {
	for _, hook := range ctx.updated {
		hook()
	}
}

func (ctx *componentContext) addReactiveCleanup(cleanup func()) {
	if cleanup != nil {
		ctx.reactiveCleanups = append(ctx.reactiveCleanups, cleanup)
	}
}

func (ctx *componentContext) cleanupReactiveEffects() {
	for i := len(ctx.reactiveCleanups) - 1; i >= 0; i-- {
		ctx.reactiveCleanups[i]()
	}
	ctx.reactiveCleanups = nil
}
