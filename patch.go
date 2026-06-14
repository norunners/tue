package tue

import "fmt"

type domNode any

type domBoundary interface {
	createElement(tag string) (domNode, error)
	createText(text string) (domNode, error)
	createMarker(text string) (domNode, error)
	appendChild(parent domNode, child domNode) error
	insertBefore(parent domNode, child domNode, before domNode) error
	removeChild(parent domNode, child domNode) error
	setText(node domNode, text string) error
	setAttr(node domNode, attr Attribute) error
	removeAttr(node domNode, name string) error
	addEventListener(node domNode, name string, handler func()) (func(), error)
}

type mountedVNode struct {
	vnode     VNode
	nodes     []domNode
	children  []*mountedVNode
	events    map[string]*mountedEvent
	component *Mounted
}

type mountedEvent struct {
	handler func()
	cleanup func()
}

func (e *mountedEvent) handle() {
	if e == nil || e.handler == nil {
		return
	}
	e.handler()
}

func mountVNode(dom domBoundary, parent domNode, before domNode, vnode VNode) (*mountedVNode, error) {
	switch vnode.Type {
	case VNodeTypeElement:
		return mountElement(dom, parent, before, vnode)
	case VNodeTypeText:
		return mountText(dom, parent, before, vnode)
	case VNodeTypeFragment:
		return mountFragment(dom, parent, before, vnode)
	case VNodeTypeComponent:
		return mountComponentVNode(dom, parent, before, vnode)
	default:
		return mountText(dom, parent, before, VNode{Type: VNodeTypeText, Text: fmt.Sprint(vnode.Text)})
	}
}

func mountElement(dom domBoundary, parent domNode, before domNode, vnode VNode) (*mountedVNode, error) {
	node, err := dom.createElement(vnode.Tag)
	if err != nil {
		return nil, fmt.Errorf("create element %q: %w", vnode.Tag, err)
	}
	for _, attr := range vnode.Attrs {
		if err := dom.setAttr(node, attr); err != nil {
			return nil, fmt.Errorf("set attribute %q: %w", attr.Name, err)
		}
	}
	events, err := mountEvents(dom, node, vnode.Events)
	if err != nil {
		return nil, err
	}

	children := make([]*mountedVNode, 0, len(vnode.Children))
	for _, child := range vnode.Children {
		mountedChild, err := mountVNode(dom, node, nil, child)
		if err != nil {
			return nil, err
		}
		children = append(children, mountedChild)
	}

	if err := insertNode(dom, parent, node, before); err != nil {
		return nil, fmt.Errorf("insert element %q: %w", vnode.Tag, err)
	}
	return &mountedVNode{vnode: vnode, nodes: []domNode{node}, children: children, events: events}, nil
}

func mountText(dom domBoundary, parent domNode, before domNode, vnode VNode) (*mountedVNode, error) {
	node, err := dom.createText(vnode.Text)
	if err != nil {
		return nil, fmt.Errorf("create text: %w", err)
	}
	if err := insertNode(dom, parent, node, before); err != nil {
		return nil, fmt.Errorf("insert text: %w", err)
	}
	return &mountedVNode{vnode: vnode, nodes: []domNode{node}}, nil
}

func mountFragment(dom domBoundary, parent domNode, before domNode, vnode VNode) (*mountedVNode, error) {
	start, err := dom.createMarker("tue-fragment-start")
	if err != nil {
		return nil, fmt.Errorf("create fragment start: %w", err)
	}
	end, err := dom.createMarker("tue-fragment-end")
	if err != nil {
		return nil, fmt.Errorf("create fragment end: %w", err)
	}
	if err := insertNode(dom, parent, start, before); err != nil {
		return nil, fmt.Errorf("insert fragment start: %w", err)
	}
	if err := insertNode(dom, parent, end, before); err != nil {
		return nil, fmt.Errorf("insert fragment end: %w", err)
	}

	children := make([]*mountedVNode, 0, len(vnode.Children))
	for _, child := range vnode.Children {
		mountedChild, err := mountVNode(dom, parent, end, child)
		if err != nil {
			return nil, err
		}
		children = append(children, mountedChild)
	}

	return &mountedVNode{
		vnode:    vnode,
		nodes:    fragmentNodes(start, children, end),
		children: children,
	}, nil
}

func patchVNode(dom domBoundary, parent domNode, old *mountedVNode, next VNode) (*mountedVNode, error) {
	return patchVNodeAt(dom, parent, nil, old, next)
}

func patchVNodeAt(dom domBoundary, parent domNode, before domNode, old *mountedVNode, next VNode) (*mountedVNode, error) {
	if old == nil {
		return mountVNode(dom, parent, before, next)
	}
	if !sameVNode(old.vnode, next) {
		insertBefore := firstDOMNode(old)
		mounted, err := mountVNode(dom, parent, insertBefore, next)
		if err != nil {
			return nil, err
		}
		if err := removeMountedVNode(dom, parent, old); err != nil {
			return nil, err
		}
		return mounted, nil
	}

	switch next.Type {
	case VNodeTypeElement:
		return patchElement(dom, old, next)
	case VNodeTypeText:
		return patchText(dom, old, next)
	case VNodeTypeFragment:
		return patchFragment(dom, parent, old, next)
	case VNodeTypeComponent:
		return patchComponentVNode(old, next)
	default:
		return patchText(dom, old, VNode{Type: VNodeTypeText, Text: fmt.Sprint(next.Text)})
	}
}

func patchElement(dom domBoundary, old *mountedVNode, next VNode) (*mountedVNode, error) {
	node := firstDOMNode(old)
	if err := patchAttrs(dom, node, old.vnode.Attrs, next.Attrs); err != nil {
		return nil, err
	}
	events, err := patchEvents(dom, node, old.events, next.Events)
	if err != nil {
		return nil, err
	}
	children, err := patchChildren(dom, node, nil, old.children, next.Children)
	if err != nil {
		return nil, err
	}
	old.vnode = next
	old.children = children
	old.events = events
	return old, nil
}

func patchText(dom domBoundary, old *mountedVNode, next VNode) (*mountedVNode, error) {
	if old.vnode.Text != next.Text {
		if err := dom.setText(firstDOMNode(old), next.Text); err != nil {
			return nil, fmt.Errorf("set text: %w", err)
		}
	}
	old.vnode = next
	return old, nil
}

func patchFragment(dom domBoundary, parent domNode, old *mountedVNode, next VNode) (*mountedVNode, error) {
	end := lastDOMNode(old)
	children, err := patchChildren(dom, parent, end, old.children, next.Children)
	if err != nil {
		return nil, err
	}
	old.vnode = next
	old.children = children
	old.nodes = fragmentNodes(firstDOMNode(old), children, end)
	return old, nil
}

func mountComponentVNode(dom domBoundary, parent domNode, before domNode, vnode VNode) (*mountedVNode, error) {
	if vnode.ComponentFactory == nil {
		return nil, fmt.Errorf("component %q factory is required", vnode.Tag)
	}
	component := vnode.ComponentFactory()
	if component == nil {
		return nil, fmt.Errorf("component %q factory returned nil", vnode.Tag)
	}

	mounted := &mountedVNode{vnode: vnode}
	child := &Mounted{
		component:    component,
		target:       childMountTarget{domBoundary: dom, parent: parent},
		owner:        mounted,
		insertBefore: before,
	}
	child.renderEffect = newComponentRenderEffect(child)
	child.renderEffect.run()
	if child.renderErr != nil {
		child.renderEffect.stop()
		child.component.runCleanups()
		return nil, child.renderErr
	}
	child.mounted = true
	component.mounted()

	mounted.component = child
	mounted.nodes = mountedNodes(child.tree)
	return mounted, nil
}

func patchComponentVNode(old *mountedVNode, next VNode) (*mountedVNode, error) {
	if old.component == nil {
		return nil, fmt.Errorf("mounted component %q is required", old.vnode.Tag)
	}
	old.vnode = next
	if err := old.component.Update(); err != nil {
		return nil, err
	}
	old.nodes = mountedNodes(old.component.tree)
	return old, nil
}

func patchChildren(dom domBoundary, parent domNode, before domNode, old []*mountedVNode, next []VNode) ([]*mountedVNode, error) {
	shared := len(old)
	if len(next) < shared {
		shared = len(next)
	}

	children := make([]*mountedVNode, 0, len(next))
	for i := 0; i < shared; i++ {
		child, err := patchVNode(dom, parent, old[i], next[i])
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	for i := shared; i < len(next); i++ {
		child, err := mountVNode(dom, parent, before, next[i])
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	for i := shared; i < len(old); i++ {
		if err := removeMountedVNode(dom, parent, old[i]); err != nil {
			return nil, err
		}
	}
	return children, nil
}

func patchAttrs(dom domBoundary, node domNode, old []Attribute, next []Attribute) error {
	oldAttrs := attrsByName(old)
	nextAttrs := attrsByName(next)

	for name, nextAttr := range nextAttrs {
		if oldAttr, ok := oldAttrs[name]; !ok || oldAttr != nextAttr {
			if err := dom.setAttr(node, nextAttr); err != nil {
				return fmt.Errorf("set attribute %q: %w", name, err)
			}
		}
	}
	for name := range oldAttrs {
		if _, ok := nextAttrs[name]; !ok {
			if err := dom.removeAttr(node, name); err != nil {
				return fmt.Errorf("remove attribute %q: %w", name, err)
			}
		}
	}
	return nil
}

func attrsByName(attrs []Attribute) map[string]Attribute {
	byName := make(map[string]Attribute, len(attrs))
	for _, attr := range attrs {
		byName[attr.Name] = attr
	}
	return byName
}

func mountEvents(dom domBoundary, node domNode, events []EventBinding) (map[string]*mountedEvent, error) {
	nextEvents := eventsByName(events)
	if len(nextEvents) == 0 {
		return nil, nil
	}

	mounted := make(map[string]*mountedEvent, len(nextEvents))
	for name, event := range nextEvents {
		mountedEvent := &mountedEvent{handler: event.Handler}
		cleanup, err := dom.addEventListener(node, name, mountedEvent.handle)
		if err != nil {
			cleanupEvents(mounted)
			return nil, fmt.Errorf("add event listener %q: %w", name, err)
		}
		mountedEvent.cleanup = cleanup
		mounted[name] = mountedEvent
	}
	return mounted, nil
}

func patchEvents(dom domBoundary, node domNode, old map[string]*mountedEvent, next []EventBinding) (map[string]*mountedEvent, error) {
	nextEvents := eventsByName(next)
	for name, event := range nextEvents {
		if mounted, ok := old[name]; ok {
			mounted.handler = event.Handler
			continue
		}
		mounted := &mountedEvent{handler: event.Handler}
		cleanup, err := dom.addEventListener(node, name, mounted.handle)
		if err != nil {
			return old, fmt.Errorf("add event listener %q: %w", name, err)
		}
		mounted.cleanup = cleanup
		if old == nil {
			old = make(map[string]*mountedEvent, len(nextEvents))
		}
		old[name] = mounted
	}
	for name, mounted := range old {
		if _, ok := nextEvents[name]; ok {
			continue
		}
		cleanupEvent(mounted)
		delete(old, name)
	}
	if len(old) == 0 {
		return nil, nil
	}
	return old, nil
}

func eventsByName(events []EventBinding) map[string]EventBinding {
	byName := make(map[string]EventBinding, len(events))
	for _, event := range events {
		if event.Name == "" {
			continue
		}
		byName[event.Name] = event
	}
	return byName
}

func sameVNode(old VNode, next VNode) bool {
	if old.Type != next.Type || old.Key != next.Key {
		return false
	}
	if old.Type == VNodeTypeElement || old.Type == VNodeTypeComponent {
		return old.Tag == next.Tag
	}
	return true
}

func insertNode(dom domBoundary, parent domNode, child domNode, before domNode) error {
	if before == nil {
		return dom.appendChild(parent, child)
	}
	return dom.insertBefore(parent, child, before)
}

func removeMountedVNode(dom domBoundary, parent domNode, mounted *mountedVNode) error {
	if mounted == nil {
		return nil
	}
	cleanupMountedVNode(mounted)
	for _, node := range mounted.nodes {
		if err := dom.removeChild(parent, node); err != nil {
			return fmt.Errorf("remove node: %w", err)
		}
	}
	return nil
}

func cleanupMountedVNode(mounted *mountedVNode) {
	if mounted == nil {
		return
	}
	if mounted.component != nil {
		_ = mounted.component.unmount(false)
		mounted.component = nil
		return
	}
	cleanupEvents(mounted.events)
	mounted.events = nil
	for _, child := range mounted.children {
		cleanupMountedVNode(child)
	}
}

func cleanupEvents(events map[string]*mountedEvent) {
	for _, event := range events {
		cleanupEvent(event)
	}
}

func cleanupEvent(event *mountedEvent) {
	if event == nil || event.cleanup == nil {
		return
	}
	event.cleanup()
	event.cleanup = nil
	event.handler = nil
}

func firstDOMNode(mounted *mountedVNode) domNode {
	if mounted == nil || len(mounted.nodes) == 0 {
		return nil
	}
	return mounted.nodes[0]
}

func lastDOMNode(mounted *mountedVNode) domNode {
	if mounted == nil || len(mounted.nodes) == 0 {
		return nil
	}
	return mounted.nodes[len(mounted.nodes)-1]
}

func fragmentNodes(start domNode, children []*mountedVNode, end domNode) []domNode {
	nodes := []domNode{start}
	for _, child := range children {
		nodes = append(nodes, child.nodes...)
	}
	return append(nodes, end)
}

func mountedNodes(mounted *mountedVNode) []domNode {
	if mounted == nil {
		return nil
	}
	return mounted.nodes
}

type childMountTarget struct {
	domBoundary
	parent domNode
}

func (t childMountTarget) root() domNode {
	return t.parent
}

func (t childMountTarget) clear() error {
	return nil
}
