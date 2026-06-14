package tue

import "fmt"

// Mounted is a live component mounted into a runtime target.
type Mounted struct {
	component *Comp
	target    mountTarget
	tree      *mountedVNode

	unmounted bool
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

	node := component.renderVNode()
	if err := target.clear(); err != nil {
		return nil, fmt.Errorf("clear mount target: %w", err)
	}
	tree, err := mountVNode(target, target.root(), nil, node)
	if err != nil {
		return nil, fmt.Errorf("render mount target: %w", err)
	}
	component.mounted()
	return &Mounted{component: component, target: target, tree: tree}, nil
}

// Update renders the component again and then calls optional OnUpdated.
func (m *Mounted) Update() error {
	if m == nil {
		return fmt.Errorf("mounted component is required")
	}
	if m.unmounted {
		return fmt.Errorf("mounted component is unmounted")
	}
	tree, err := patchVNode(m.target, m.target.root(), m.tree, m.component.renderVNode())
	if err != nil {
		return fmt.Errorf("patch mount target: %w", err)
	}
	m.tree = tree
	m.component.updated()
	return nil
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

	m.component.runCleanups()
	err := m.target.clear()
	m.component.unmounted()
	if err != nil {
		return fmt.Errorf("clear mount target: %w", err)
	}
	return nil
}
