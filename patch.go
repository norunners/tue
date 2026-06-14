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
}

type mountedVNode struct {
	vnode    VNode
	nodes    []domNode
	children []*mountedVNode
}

func mountVNode(dom domBoundary, parent domNode, before domNode, vnode VNode) (*mountedVNode, error) {
	switch vnode.Type {
	case VNodeTypeElement:
		return mountElement(dom, parent, before, vnode)
	case VNodeTypeText:
		return mountText(dom, parent, before, vnode)
	case VNodeTypeFragment:
		return mountFragment(dom, parent, before, vnode)
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
	return &mountedVNode{vnode: vnode, nodes: []domNode{node}, children: children}, nil
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
	if old == nil {
		return mountVNode(dom, parent, nil, next)
	}
	if !sameVNode(old.vnode, next) {
		before := firstDOMNode(old)
		mounted, err := mountVNode(dom, parent, before, next)
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
	default:
		return patchText(dom, old, VNode{Type: VNodeTypeText, Text: fmt.Sprint(next.Text)})
	}
}

func patchElement(dom domBoundary, old *mountedVNode, next VNode) (*mountedVNode, error) {
	node := firstDOMNode(old)
	if err := patchAttrs(dom, node, old.vnode.Attrs, next.Attrs); err != nil {
		return nil, err
	}
	children, err := patchChildren(dom, node, nil, old.children, next.Children)
	if err != nil {
		return nil, err
	}
	old.vnode = next
	old.children = children
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

func sameVNode(old VNode, next VNode) bool {
	if old.Type != next.Type || old.Key != next.Key {
		return false
	}
	if old.Type == VNodeTypeElement {
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
	for _, node := range mounted.nodes {
		if err := dom.removeChild(parent, node); err != nil {
			return fmt.Errorf("remove node: %w", err)
		}
	}
	return nil
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
