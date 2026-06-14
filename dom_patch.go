package vue

import (
	"fmt"
	"reflect"
)

type mountTarget interface {
	document() domDocumentAccess
	root() domNodeAccess
}

type domDocumentAccess interface {
	createElement(tag string) domNodeAccess
	createTextNode(text string) domNodeAccess
}

type domNodeAccess interface {
	appendChild(child domNodeAccess)
	insertBefore(child, before domNodeAccess)
	removeChild(child domNodeAccess)
	setAttribute(name, value string)
	removeAttribute(name string)
	setTextContent(text string)
	addEventListener(name string, handler func()) func()
}

type mountedVNode struct {
	vnode     VNode
	dom       domNodeAccess
	children  []*mountedVNode
	listeners []*mountedEventListener
}

type mountedEventListener struct {
	name    string
	handler func()
	cleanup func()
}

func mountVNode(document domDocumentAccess, parent domNodeAccess, vnode VNode) (*mountedVNode, error) {
	return mountVNodeBefore(document, parent, vnode, nil)
}

func mountVNodeBefore(document domDocumentAccess, parent domNodeAccess, vnode VNode, before domNodeAccess) (*mountedVNode, error) {
	if document == nil {
		return nil, fmt.Errorf("missing DOM document")
	}
	if parent == nil {
		return nil, fmt.Errorf("missing DOM parent")
	}

	switch vnode.Type {
	case VNodeInvalid, VNodeFragment:
		mounted := &mountedVNode{vnode: vnode}
		children, err := mountChildrenBefore(document, parent, vnode.Children, before)
		if err != nil {
			return nil, err
		}
		mounted.children = children
		return mounted, nil
	case VNodeElement:
		if vnode.Tag == "" {
			return mountVNodeBefore(document, parent, Fragment(vnode.Children...), before)
		}
		dom := document.createElement(vnode.Tag)
		if dom == nil {
			return nil, fmt.Errorf("create element %q: nil DOM node", vnode.Tag)
		}
		mounted := &mountedVNode{vnode: vnode, dom: dom}
		patchAttributes(dom, nil, vnode.Props)
		mounted.listeners = attachEvents(dom, vnode.Events)
		if !isVoidElement(vnode.Tag) {
			children, err := mountChildrenBefore(document, dom, vnode.Children, nil)
			if err != nil {
				cleanupEventListeners(mounted.listeners)
				return nil, err
			}
			mounted.children = children
		}
		insertDOM(parent, dom, before)
		return mounted, nil
	case VNodeText:
		dom := document.createTextNode(vnode.Text)
		if dom == nil {
			return nil, fmt.Errorf("create text node: nil DOM node")
		}
		insertDOM(parent, dom, before)
		return &mountedVNode{vnode: vnode, dom: dom}, nil
	case VNodeComponent:
		return nil, fmt.Errorf("component VNodes are not supported by static patching")
	default:
		return nil, fmt.Errorf("unsupported VNode type %d", vnode.Type)
	}
}

func mountChildrenBefore(document domDocumentAccess, parent domNodeAccess, children []VNode, before domNodeAccess) ([]*mountedVNode, error) {
	mounted := make([]*mountedVNode, 0, len(children))
	for _, child := range children {
		mountedChild, err := mountVNodeBefore(document, parent, child, before)
		if err != nil {
			return nil, err
		}
		mounted = append(mounted, mountedChild)
	}
	return mounted, nil
}

func patchMountedVNode(document domDocumentAccess, parent domNodeAccess, current *mountedVNode, next VNode) (*mountedVNode, error) {
	if current == nil {
		return mountVNode(document, parent, next)
	}
	if !sameVNode(current.vnode, next) {
		before := firstDOM(current)
		replacement, err := mountVNodeBefore(document, parent, next, before)
		if err != nil {
			return nil, err
		}
		unmountVNode(parent, current)
		return replacement, nil
	}

	switch next.Type {
	case VNodeInvalid, VNodeFragment:
		children, err := patchChildren(document, parent, current.children, next.Children)
		if err != nil {
			return nil, err
		}
		current.children = children
	case VNodeElement:
		if next.Tag == "" {
			children, err := patchChildren(document, parent, current.children, next.Children)
			if err != nil {
				return nil, err
			}
			current.children = children
			break
		}
		patchAttributes(current.dom, current.vnode.Props, next.Props)
		current.listeners = patchEvents(current.dom, current.listeners, next.Events)
		if isVoidElement(next.Tag) {
			for _, child := range current.children {
				unmountVNode(current.dom, child)
			}
			current.children = nil
			break
		}
		children, err := patchChildren(document, current.dom, current.children, next.Children)
		if err != nil {
			return nil, err
		}
		current.children = children
	case VNodeText:
		if current.vnode.Text != next.Text {
			current.dom.setTextContent(next.Text)
		}
	case VNodeComponent:
		return nil, fmt.Errorf("component VNodes are not supported by static patching")
	default:
		return nil, fmt.Errorf("unsupported VNode type %d", next.Type)
	}

	current.vnode = next
	return current, nil
}

func patchChildren(document domDocumentAccess, parent domNodeAccess, current []*mountedVNode, next []VNode) ([]*mountedVNode, error) {
	limit := len(current)
	if len(next) < limit {
		limit = len(next)
	}

	patched := make([]*mountedVNode, 0, len(next))
	for i := 0; i < limit; i++ {
		child, err := patchMountedVNode(document, parent, current[i], next[i])
		if err != nil {
			return nil, err
		}
		patched = append(patched, child)
	}
	for i := limit; i < len(next); i++ {
		child, err := mountVNode(document, parent, next[i])
		if err != nil {
			return nil, err
		}
		patched = append(patched, child)
	}
	for i := limit; i < len(current); i++ {
		unmountVNode(parent, current[i])
	}
	return patched, nil
}

func unmountVNode(parent domNodeAccess, mounted *mountedVNode) {
	if parent == nil || mounted == nil {
		return
	}
	if mounted.dom != nil {
		cleanupMountedVNode(mounted)
		parent.removeChild(mounted.dom)
		return
	}
	for _, child := range mounted.children {
		unmountVNode(parent, child)
	}
}

func firstDOM(mounted *mountedVNode) domNodeAccess {
	if mounted == nil {
		return nil
	}
	if mounted.dom != nil {
		return mounted.dom
	}
	for _, child := range mounted.children {
		if dom := firstDOM(child); dom != nil {
			return dom
		}
	}
	return nil
}

func insertDOM(parent, child, before domNodeAccess) {
	if before != nil {
		parent.insertBefore(child, before)
		return
	}
	parent.appendChild(child)
}

func sameVNode(a, b VNode) bool {
	if a.Type != b.Type || !reflect.DeepEqual(a.Key, b.Key) {
		return false
	}
	if a.Type == VNodeElement {
		return a.Tag == b.Tag
	}
	return true
}

type domAttribute struct {
	value    string
	hasValue bool
}

func patchAttributes(dom domNodeAccess, oldAttrs, newAttrs []Attribute) {
	oldByName := attributesByName(oldAttrs)
	newByName := attributesByName(newAttrs)

	for name := range oldByName {
		if _, ok := newByName[name]; !ok {
			dom.removeAttribute(name)
		}
	}
	for name, next := range newByName {
		if old, ok := oldByName[name]; ok && old == next {
			continue
		}
		value := ""
		if next.hasValue {
			value = next.value
		}
		dom.setAttribute(name, value)
	}
}

func attributesByName(attrs []Attribute) map[string]domAttribute {
	byName := make(map[string]domAttribute, len(attrs))
	for _, attr := range attrs {
		if attr.Name == "" {
			continue
		}
		byName[attr.Name] = domAttribute{
			value:    attr.Value,
			hasValue: attr.HasValue,
		}
	}
	return byName
}

func patchEvents(dom domNodeAccess, oldListeners []*mountedEventListener, events []EventBinding) []*mountedEventListener {
	nextEvents := validEventBindings(events)
	limit := len(oldListeners)
	if len(nextEvents) < limit {
		limit = len(nextEvents)
	}

	listeners := make([]*mountedEventListener, 0, len(nextEvents))
	for i := 0; i < limit; i++ {
		next := nextEvents[i]
		if oldListeners[i].name == next.Name {
			oldListeners[i].handler = next.Handler
			listeners = append(listeners, oldListeners[i])
			continue
		}
		cleanupEventListener(oldListeners[i])
		listeners = append(listeners, attachEvent(dom, next))
	}
	for i := limit; i < len(oldListeners); i++ {
		cleanupEventListener(oldListeners[i])
	}
	for i := limit; i < len(nextEvents); i++ {
		listeners = append(listeners, attachEvent(dom, nextEvents[i]))
	}
	return listeners
}

func attachEvents(dom domNodeAccess, events []EventBinding) []*mountedEventListener {
	if dom == nil || len(events) == 0 {
		return nil
	}
	events = validEventBindings(events)
	listeners := make([]*mountedEventListener, 0, len(events))
	for _, event := range events {
		listeners = append(listeners, attachEvent(dom, event))
	}
	return listeners
}

func attachEvent(dom domNodeAccess, event EventBinding) *mountedEventListener {
	listener := &mountedEventListener{
		name:    event.Name,
		handler: event.Handler,
	}
	listener.cleanup = dom.addEventListener(event.Name, func() {
		if listener.handler != nil {
			listener.handler()
		}
	})
	return listener
}

func cleanupMountedVNode(mounted *mountedVNode) {
	if mounted == nil {
		return
	}
	cleanupEventListeners(mounted.listeners)
	mounted.listeners = nil
	for _, child := range mounted.children {
		cleanupMountedVNode(child)
	}
}

func cleanupEventListeners(listeners []*mountedEventListener) {
	for i := len(listeners) - 1; i >= 0; i-- {
		cleanupEventListener(listeners[i])
	}
}

func cleanupEventListener(listener *mountedEventListener) {
	if listener == nil || listener.cleanup == nil {
		return
	}
	listener.cleanup()
	listener.cleanup = nil
	listener.handler = nil
}

func validEventBindings(events []EventBinding) []EventBinding {
	valid := make([]EventBinding, 0, len(events))
	for _, event := range events {
		if event.Name != "" && event.Handler != nil {
			valid = append(valid, event)
		}
	}
	return valid
}
