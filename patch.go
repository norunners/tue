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
	setInnerHTML(node domNode, html string) error
	setAttr(node domNode, attr Attribute) error
	removeAttr(node domNode, name string) error
	addEventListener(node domNode, name string, handler func(Event)) (func(), error)
}

type mountedVNode struct {
	vnode     VNode
	nodes     []domNode
	children  []*mountedVNode
	events    map[string]*mountedEvent
	component *Mounted
}

type mountedEvent struct {
	handler func(Event)
	cleanup func()
}

func (e *mountedEvent) handle(event Event) {
	if e == nil || e.handler == nil {
		return
	}
	e.handler(event)
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
	if err := setMountAttrs(dom, node, vnode.Tag, vnode.Attrs, false); err != nil {
		return nil, err
	}
	events, err := mountEvents(dom, node, vnode.Events)
	if err != nil {
		return nil, err
	}

	var children []*mountedVNode
	if vnode.HasInnerHTML {
		if err := dom.setInnerHTML(node, string(vnode.InnerHTML)); err != nil {
			return nil, fmt.Errorf("set inner HTML: %w", err)
		}
	} else {
		children = make([]*mountedVNode, 0, len(vnode.Children))
		for _, child := range vnode.Children {
			mountedChild, err := mountVNode(dom, node, nil, child)
			if err != nil {
				return nil, err
			}
			children = append(children, mountedChild)
		}
	}
	if err := setMountAttrs(dom, node, vnode.Tag, vnode.Attrs, true); err != nil {
		return nil, err
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
	if err := patchAttrs(dom, node, next.Tag, old.vnode.Attrs, next.Attrs, false); err != nil {
		return nil, err
	}
	events, err := patchEvents(dom, node, old.events, next.Events)
	if err != nil {
		return nil, err
	}
	children, err := patchElementContent(dom, node, old, next)
	if err != nil {
		return nil, err
	}
	if err := patchAttrs(dom, node, next.Tag, old.vnode.Attrs, next.Attrs, true); err != nil {
		return nil, err
	}
	old.vnode = next
	old.children = children
	old.events = events
	return old, nil
}

func patchElementContent(dom domBoundary, node domNode, old *mountedVNode, next VNode) ([]*mountedVNode, error) {
	if next.HasInnerHTML {
		cleanupMountedChildren(old.children)
		if !old.vnode.HasInnerHTML || old.vnode.InnerHTML != next.InnerHTML {
			if err := dom.setInnerHTML(node, string(next.InnerHTML)); err != nil {
				return nil, fmt.Errorf("set inner HTML: %w", err)
			}
		}
		return nil, nil
	}

	if old.vnode.HasInnerHTML {
		if err := dom.setInnerHTML(node, ""); err != nil {
			return nil, fmt.Errorf("clear inner HTML: %w", err)
		}
		return patchChildren(dom, node, nil, nil, next.Children)
	}
	return patchChildren(dom, node, nil, old.children, next.Children)
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
		scopeAttrs:   vnode.scopeAttrs,
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
	if next.ComponentUpdater != nil {
		next.ComponentUpdater(old.component.component)
	}
	old.component.scopeAttrs = next.scopeAttrs
	old.vnode = next
	if err := old.component.Update(); err != nil {
		return nil, err
	}
	old.nodes = mountedNodes(old.component.tree)
	return old, nil
}

func patchChildren(dom domBoundary, parent domNode, before domNode, old []*mountedVNode, next []VNode) ([]*mountedVNode, error) {
	if hasKeyedChildren(old, next) {
		return patchKeyedChildren(dom, parent, before, old, next)
	}
	return patchUnkeyedChildren(dom, parent, before, old, next)
}

func patchUnkeyedChildren(dom domBoundary, parent domNode, before domNode, old []*mountedVNode, next []VNode) ([]*mountedVNode, error) {
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

func patchKeyedChildren(dom domBoundary, parent domNode, before domNode, old []*mountedVNode, next []VNode) ([]*mountedVNode, error) {
	oldByKey := groupOldChildrenByKey(old)
	used := make(map[*mountedVNode]struct{}, len(old))

	children := make([]*mountedVNode, 0, len(next))
	for index, nextVNode := range next {
		oldChild, found := nextOldChild(index, nextVNode, old, oldByKey, used)
		child, err := patchOrMountChild(dom, parent, before, oldChild, nextVNode)
		if err != nil {
			return nil, err
		}
		if found {
			used[oldChild] = struct{}{}
		}
		children = append(children, child)
	}

	for _, child := range old {
		if _, ok := used[child]; ok {
			continue
		}
		if err := removeMountedVNode(dom, parent, child); err != nil {
			return nil, err
		}
	}
	if sameMountedChildren(old, children) {
		return children, nil
	}
	if err := reorderMountedChildren(dom, parent, before, children); err != nil {
		return nil, err
	}
	return children, nil
}

func hasKeyedChildren(old []*mountedVNode, next []VNode) bool {
	for _, child := range old {
		if child != nil && child.vnode.Key != "" {
			return true
		}
	}
	for _, child := range next {
		if child.Key != "" {
			return true
		}
	}
	return false
}

func groupOldChildrenByKey(old []*mountedVNode) map[string]*mountedVNode {
	byKey := make(map[string]*mountedVNode)
	for _, child := range old {
		if child != nil && child.vnode.Key != "" {
			if _, ok := byKey[child.vnode.Key]; !ok {
				byKey[child.vnode.Key] = child
			}
		}
	}
	return byKey
}

func nextOldChild(index int, next VNode, old []*mountedVNode, oldByKey map[string]*mountedVNode, used map[*mountedVNode]struct{}) (*mountedVNode, bool) {
	if next.Key != "" {
		child := oldByKey[next.Key]
		if child == nil {
			return nil, false
		}
		if _, ok := used[child]; ok {
			return nil, false
		}
		return child, true
	}

	if index >= len(old) {
		return nil, false
	}
	child := old[index]
	if child == nil || child.vnode.Key != "" {
		return nil, false
	}
	if _, ok := used[child]; ok {
		return nil, false
	}
	return child, true
}

func patchOrMountChild(dom domBoundary, parent domNode, before domNode, old *mountedVNode, next VNode) (*mountedVNode, error) {
	if old != nil {
		return patchVNode(dom, parent, old, next)
	}
	return mountVNode(dom, parent, before, next)
}

func sameMountedChildren(old []*mountedVNode, next []*mountedVNode) bool {
	if len(old) != len(next) {
		return false
	}
	for i := range old {
		if old[i] != next[i] {
			return false
		}
	}
	return true
}

func reorderMountedChildren(dom domBoundary, parent domNode, before domNode, children []*mountedVNode) error {
	anchor := before
	for i := len(children) - 1; i >= 0; i-- {
		child := children[i]
		updateComponentInsertBefore(child, anchor)
		if err := moveMountedVNodeBefore(dom, parent, child, anchor); err != nil {
			return err
		}
		anchor = firstDOMNode(child)
	}
	return nil
}

func moveMountedVNodeBefore(dom domBoundary, parent domNode, mounted *mountedVNode, before domNode) error {
	if mounted == nil {
		return nil
	}
	for _, node := range mounted.nodes {
		if err := insertNode(dom, parent, node, before); err != nil {
			return fmt.Errorf("move node: %w", err)
		}
	}
	return nil
}

func updateComponentInsertBefore(mounted *mountedVNode, before domNode) {
	if mounted == nil || mounted.component == nil {
		return
	}
	mounted.component.insertBefore = before
}

func setMountAttrs(dom domBoundary, node domNode, tag string, attrs []Attribute, postChildren bool) error {
	for _, attr := range attrs {
		if postChildrenAttr(tag, attr) != postChildren {
			continue
		}
		if err := dom.setAttr(node, attr); err != nil {
			return fmt.Errorf("set attribute %q: %w", attr.Name, err)
		}
	}
	return nil
}

func patchAttrs(dom domBoundary, node domNode, tag string, old []Attribute, next []Attribute, postChildren bool) error {
	oldAttrs := attrsByName(tag, old, postChildren)
	nextAttrs := attrsByName(tag, next, postChildren)

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

func attrsByName(tag string, attrs []Attribute, postChildren bool) map[string]Attribute {
	byName := make(map[string]Attribute, len(attrs))
	for _, attr := range attrs {
		if postChildrenAttr(tag, attr) != postChildren {
			continue
		}
		byName[attr.Name] = attr
	}
	return byName
}

func postChildrenAttr(tag string, attr Attribute) bool {
	return tag == "select" && attr.Name == "value"
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
		if previous, ok := byName[event.Name]; ok {
			event.Handler = combinedEventHandler(previous.Handler, event.Handler)
		}
		byName[event.Name] = event
	}
	return byName
}

func combinedEventHandler(first func(Event), second func(Event)) func(Event) {
	return func(event Event) {
		if first != nil {
			first(event)
		}
		if second != nil {
			second(event)
		}
	}
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

func cleanupMountedChildren(children []*mountedVNode) {
	for _, child := range children {
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
