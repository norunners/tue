package tue

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMountedUpdatePatchesTextInPlace(t *testing.T) {
	value := "first"
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return Text(value)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	text, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted text node: %v", err)
	}
	textID := text.id

	value = "second"
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	actual, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find patched text node: %v", err)
	}
	if diff := cmp.Diff(stubNodeSummary{ID: textID, Kind: "text", Text: "second"}, summarizeStubNode(actual)); diff != "" {
		t.Errorf("mismatch patched text node (-expected, +actual):\n%s", diff)
	}
}

func TestMountedUpdatePatchesElementAttributesInPlace(t *testing.T) {
	primary := true
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		if primary {
			return Element("button", []Attribute{
				Attr("class", "primary"),
				BoolAttr("disabled"),
			}, []VNode{Text("Save")})
		}
		return Element("button", []Attribute{
			Attr("class", "secondary"),
			Attr("title", "Ready"),
		}, []VNode{Text("Save")})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	button, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted button: %v", err)
	}
	buttonID := button.id

	primary = false
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	actual, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find patched button: %v", err)
	}
	expected := stubNodeSummary{
		ID:   buttonID,
		Kind: "element",
		Tag:  "button",
		Attrs: []Attribute{
			Attr("class", "secondary"),
			Attr("title", "Ready"),
		},
		Children: []stubNodeSummary{{Kind: "text", Text: "Save"}},
	}
	if diff := cmp.Diff(expected, summarizeStubNodeWithoutChildIDs(actual)); diff != "" {
		t.Errorf("mismatch patched element attributes (-expected, +actual):\n%s", diff)
	}
}

func TestMountedUpdateReplacesDifferentKey(t *testing.T) {
	key := "first"
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return VNode{Type: VNodeTypeElement, Key: key, Tag: "section"}
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	first, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find first keyed element: %v", err)
	}

	key = "second"
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	second, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find second keyed element: %v", err)
	}
	if second.id == first.id {
		t.Errorf("mismatch replacement node id: expected a different node from %d", first.id)
	}
	if first.parent != nil {
		t.Errorf("mismatch replaced node parent: expected nil, got %#v", first.parent)
	}
	if diff := cmp.Diff("<section></section>", target.html()); diff != "" {
		t.Errorf("mismatch replaced keyed element HTML (-expected, +actual):\n%s", diff)
	}
}

func TestMountedUpdatePatchesUnkeyedChildrenPositionally(t *testing.T) {
	items := []string{"A", "B"}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		children := make([]VNode, 0, len(items))
		for _, item := range items {
			children = append(children, Text(item))
		}
		return Element("p", nil, children)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	paragraph, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find paragraph: %v", err)
	}
	first := paragraph.children[0]
	second := paragraph.children[1]

	items = []string{"A!", "C", "D"}
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("<p>A!CD</p>", target.html()); diff != "" {
		t.Errorf("mismatch patched children HTML (-expected, +actual):\n%s", diff)
	}
	children := paragraph.children
	if diff := cmp.Diff([]stubNodeSummary{
		{ID: first.id, Kind: "text", Text: "A!"},
		{ID: second.id, Kind: "text", Text: "C"},
		{Kind: "text", Text: "D"},
	}, summarizeStubNodesWithoutNewIDs(children, map[int]bool{first.id: true, second.id: true})); diff != "" {
		t.Errorf("mismatch patched positional children (-expected, +actual):\n%s", diff)
	}

	appended := children[2]
	items = []string{"Z"}
	if err := mounted.Update(); err != nil {
		t.Fatalf("second Update returned error: %v", err)
	}

	if diff := cmp.Diff("<p>Z</p>", target.html()); diff != "" {
		t.Errorf("mismatch trimmed children HTML (-expected, +actual):\n%s", diff)
	}
	if second.parent != nil {
		t.Errorf("mismatch removed second child parent: expected nil, got %#v", second.parent)
	}
	if appended.parent != nil {
		t.Errorf("mismatch removed appended child parent: expected nil, got %#v", appended.parent)
	}
	if diff := cmp.Diff(stubNodeSummary{ID: first.id, Kind: "text", Text: "Z"}, summarizeStubNode(paragraph.children[0])); diff != "" {
		t.Errorf("mismatch retained first child after trim (-expected, +actual):\n%s", diff)
	}
}

func TestMountedUpdatePatchesFragmentChildrenBeforeEndMarker(t *testing.T) {
	values := []string{"one"}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		children := make([]VNode, 0, len(values))
		for _, value := range values {
			children = append(children, Text(value))
		}
		return Fragment(children)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}
	if diff := cmp.Diff([]string{"comment", "text", "comment"}, childKinds(target.rootNode)); diff != "" {
		t.Errorf("mismatch mounted fragment child kinds (-expected, +actual):\n%s", diff)
	}

	values = []string{"ONE", "two"}
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("ONEtwo", target.html()); diff != "" {
		t.Errorf("mismatch patched fragment HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"comment", "text", "text", "comment"}, childKinds(target.rootNode)); diff != "" {
		t.Errorf("mismatch patched fragment child kinds (-expected, +actual):\n%s", diff)
	}
}

func TestMountedEventHandlerRunsAndUpdatesInPlace(t *testing.T) {
	useSecond := false
	events := []string{}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		handler := func() {
			events = append(events, "first")
		}
		if useSecond {
			handler = func() {
				events = append(events, "second")
			}
		}
		return ElementWithEvents("button", nil, []EventBinding{On("click", handler)}, []VNode{Text("Save")})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	button, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted button: %v", err)
	}
	listener, err := onlyStubListener(button, "click")
	if err != nil {
		t.Fatalf("find mounted click listener: %v", err)
	}
	button.dispatch("click")

	useSecond = true
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	button.dispatch("click")

	if diff := cmp.Diff([]string{"first", "second"}, events); diff != "" {
		t.Errorf("mismatch event handler calls (-expected, +actual):\n%s", diff)
	}
	actual, err := onlyStubListener(button, "click")
	if err != nil {
		t.Fatalf("find patched click listener: %v", err)
	}
	if actual != listener {
		t.Errorf("mismatch retained event listener: expected %p, actual %p", listener, actual)
	}
}

func TestMountedEventListenersCleanUpWhenNodeIsReplaced(t *testing.T) {
	replace := false
	events := []string{}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		if replace {
			return ElementWithEvents("a", nil, []EventBinding{On("click", func() {
				events = append(events, "link")
			})}, []VNode{Text("Link")})
		}
		return ElementWithEvents("button", nil, []EventBinding{On("click", func() {
			events = append(events, "button")
		})}, []VNode{Text("Save")})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	button, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted button: %v", err)
	}
	button.dispatch("click")

	replace = true
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	button.dispatch("click")
	link, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find replacement link: %v", err)
	}
	link.dispatch("click")

	if diff := cmp.Diff([]string{"button", "link"}, events); diff != "" {
		t.Errorf("mismatch replaced event handler calls (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(0, button.listenerCount("click")); diff != "" {
		t.Errorf("mismatch old button listener count (-expected, +actual):\n%s", diff)
	}
}

func TestMountedEventListenersCleanUpOnUnmount(t *testing.T) {
	events := []string{}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return ElementWithEvents("button", nil, []EventBinding{On("click", func() {
			events = append(events, "click")
		})}, []VNode{Text("Save")})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	button, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted button: %v", err)
	}
	button.dispatch("click")
	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
	button.dispatch("click")

	if diff := cmp.Diff([]string{"click"}, events); diff != "" {
		t.Errorf("mismatch unmounted event handler calls (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(0, button.listenerCount("click")); diff != "" {
		t.Errorf("mismatch unmounted button listener count (-expected, +actual):\n%s", diff)
	}
}

type patchFixture struct{}

type stubDOMTarget struct {
	rootNode *stubDOMNode
	nextID   int
	clears   int
}

func newStubDOMTarget() *stubDOMTarget {
	target := &stubDOMTarget{}
	target.rootNode = target.newNode("root")
	return target
}

func (t *stubDOMTarget) root() domNode {
	return t.rootNode
}

func (t *stubDOMTarget) createElement(tag string) (domNode, error) {
	node := t.newNode("element")
	node.tag = tag
	node.attrs = map[string]Attribute{}
	return node, nil
}

func (t *stubDOMTarget) createText(text string) (domNode, error) {
	node := t.newNode("text")
	node.text = text
	return node, nil
}

func (t *stubDOMTarget) createMarker(text string) (domNode, error) {
	node := t.newNode("comment")
	node.text = text
	return node, nil
}

func (t *stubDOMTarget) appendChild(parent domNode, child domNode) error {
	parentNode, childNode, err := stubParentChild(parent, child)
	if err != nil {
		return err
	}
	childNode.detach()
	childNode.parent = parentNode
	parentNode.children = append(parentNode.children, childNode)
	return nil
}

func (t *stubDOMTarget) insertBefore(parent domNode, child domNode, before domNode) error {
	parentNode, childNode, err := stubParentChild(parent, child)
	if err != nil {
		return err
	}
	beforeNode, ok := before.(*stubDOMNode)
	if !ok {
		return fmt.Errorf("expected stub before node, got %T", before)
	}
	index := parentNode.childIndex(beforeNode)
	if index == -1 {
		return fmt.Errorf("before node %d is not a child of parent %d", beforeNode.id, parentNode.id)
	}
	childNode.detach()
	childNode.parent = parentNode
	parentNode.children = append(parentNode.children, nil)
	copy(parentNode.children[index+1:], parentNode.children[index:])
	parentNode.children[index] = childNode
	return nil
}

func (t *stubDOMTarget) removeChild(parent domNode, child domNode) error {
	parentNode, childNode, err := stubParentChild(parent, child)
	if err != nil {
		return err
	}
	index := parentNode.childIndex(childNode)
	if index == -1 {
		return fmt.Errorf("child node %d is not a child of parent %d", childNode.id, parentNode.id)
	}
	parentNode.children = append(parentNode.children[:index], parentNode.children[index+1:]...)
	childNode.parent = nil
	return nil
}

func (t *stubDOMTarget) setText(node domNode, text string) error {
	stubNode, ok := node.(*stubDOMNode)
	if !ok {
		return fmt.Errorf("expected stub text node, got %T", node)
	}
	stubNode.text = text
	return nil
}

func (t *stubDOMTarget) setAttr(node domNode, attr Attribute) error {
	stubNode, ok := node.(*stubDOMNode)
	if !ok {
		return fmt.Errorf("expected stub element node, got %T", node)
	}
	if stubNode.attrs == nil {
		stubNode.attrs = map[string]Attribute{}
	}
	stubNode.attrs[attr.Name] = attr
	return nil
}

func (t *stubDOMTarget) removeAttr(node domNode, name string) error {
	stubNode, ok := node.(*stubDOMNode)
	if !ok {
		return fmt.Errorf("expected stub element node, got %T", node)
	}
	delete(stubNode.attrs, name)
	return nil
}

func (t *stubDOMTarget) addEventListener(node domNode, name string, handler func()) (func(), error) {
	stubNode, ok := node.(*stubDOMNode)
	if !ok {
		return nil, fmt.Errorf("expected stub element node, got %T", node)
	}
	if stubNode.listeners == nil {
		stubNode.listeners = map[string][]*stubEventListener{}
	}
	listener := &stubEventListener{handler: handler}
	stubNode.listeners[name] = append(stubNode.listeners[name], listener)
	return func() {
		if listener.removed {
			return
		}
		listener.removed = true
		listeners := stubNode.listeners[name]
		for i, candidate := range listeners {
			if candidate == listener {
				stubNode.listeners[name] = append(listeners[:i], listeners[i+1:]...)
				break
			}
		}
		if len(stubNode.listeners[name]) == 0 {
			delete(stubNode.listeners, name)
		}
	}, nil
}

func (t *stubDOMTarget) clear() error {
	for _, child := range t.rootNode.children {
		child.parent = nil
	}
	t.rootNode.children = nil
	t.clears++
	return nil
}

func (t *stubDOMTarget) html() string {
	var builder strings.Builder
	for _, child := range t.rootNode.children {
		writeStubHTML(&builder, child)
	}
	return builder.String()
}

func (t *stubDOMTarget) newNode(kind string) *stubDOMNode {
	t.nextID++
	return &stubDOMNode{id: t.nextID, kind: kind}
}

type stubDOMNode struct {
	id        int
	kind      string
	tag       string
	text      string
	attrs     map[string]Attribute
	parent    *stubDOMNode
	children  []*stubDOMNode
	listeners map[string][]*stubEventListener
}

type stubEventListener struct {
	handler func()
	removed bool
}

func (n *stubDOMNode) childIndex(child *stubDOMNode) int {
	for i, candidate := range n.children {
		if candidate == child {
			return i
		}
	}
	return -1
}

func (n *stubDOMNode) detach() {
	if n.parent == nil {
		return
	}
	index := n.parent.childIndex(n)
	if index != -1 {
		n.parent.children = append(n.parent.children[:index], n.parent.children[index+1:]...)
	}
	n.parent = nil
}

func (n *stubDOMNode) dispatch(name string) {
	for _, listener := range append([]*stubEventListener(nil), n.listeners[name]...) {
		if listener.removed || listener.handler == nil {
			continue
		}
		listener.handler()
	}
}

func (n *stubDOMNode) listenerCount(name string) int {
	count := 0
	for _, listener := range n.listeners[name] {
		if !listener.removed {
			count++
		}
	}
	return count
}

func stubParentChild(parent domNode, child domNode) (*stubDOMNode, *stubDOMNode, error) {
	parentNode, ok := parent.(*stubDOMNode)
	if !ok {
		return nil, nil, fmt.Errorf("expected stub parent node, got %T", parent)
	}
	childNode, ok := child.(*stubDOMNode)
	if !ok {
		return nil, nil, fmt.Errorf("expected stub child node, got %T", child)
	}
	return parentNode, childNode, nil
}

func writeStubHTML(builder *strings.Builder, node *stubDOMNode) {
	switch node.kind {
	case "element":
		builder.WriteByte('<')
		builder.WriteString(node.tag)
		for _, attr := range sortedStubAttrs(node.attrs) {
			builder.WriteByte(' ')
			builder.WriteString(attr.Name)
			if attr.HasValue {
				builder.WriteString(`="`)
				builder.WriteString(html.EscapeString(attr.Value))
				builder.WriteByte('"')
			}
		}
		builder.WriteByte('>')
		for _, child := range node.children {
			writeStubHTML(builder, child)
		}
		builder.WriteString("</")
		builder.WriteString(node.tag)
		builder.WriteByte('>')
	case "text":
		builder.WriteString(html.EscapeString(node.text))
	case "comment":
		return
	}
}

func sortedStubAttrs(attrs map[string]Attribute) []Attribute {
	if len(attrs) == 0 {
		return nil
	}
	sorted := make([]Attribute, 0, len(attrs))
	for _, attr := range attrs {
		sorted = append(sorted, attr)
	}
	sort.Slice(sorted, func(i int, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}

func onlyVisibleChild(node *stubDOMNode) (*stubDOMNode, error) {
	children := visibleChildren(node)
	if len(children) != 1 {
		return nil, fmt.Errorf("visible children = %#v, expected exactly one", summarizeStubNodes(children))
	}
	return children[0], nil
}

func onlyStubListener(node *stubDOMNode, name string) (*stubEventListener, error) {
	if count := node.listenerCount(name); count != 1 {
		return nil, fmt.Errorf("listener count for %q = %d, expected exactly one", name, count)
	}
	for _, listener := range node.listeners[name] {
		if !listener.removed {
			return listener, nil
		}
	}
	return nil, fmt.Errorf("active listener for %q not found", name)
}

func visibleChildren(node *stubDOMNode) []*stubDOMNode {
	children := make([]*stubDOMNode, 0, len(node.children))
	for _, child := range node.children {
		if child.kind != "comment" {
			children = append(children, child)
		}
	}
	return children
}

func childKinds(node *stubDOMNode) []string {
	kinds := make([]string, len(node.children))
	for i, child := range node.children {
		kinds[i] = child.kind
	}
	return kinds
}

type stubNodeSummary struct {
	ID       int
	Kind     string
	Tag      string
	Text     string
	Attrs    []Attribute
	Children []stubNodeSummary
}

func summarizeStubNode(node *stubDOMNode) stubNodeSummary {
	children := make([]stubNodeSummary, 0, len(node.children))
	for _, child := range node.children {
		if child.kind == "comment" {
			continue
		}
		children = append(children, summarizeStubNode(child))
	}
	if len(children) == 0 {
		children = nil
	}
	return stubNodeSummary{
		ID:       node.id,
		Kind:     node.kind,
		Tag:      node.tag,
		Text:     node.text,
		Attrs:    sortedStubAttrs(node.attrs),
		Children: children,
	}
}

func summarizeStubNodeWithoutChildIDs(node *stubDOMNode) stubNodeSummary {
	summary := summarizeStubNode(node)
	for i := range summary.Children {
		summary.Children[i].ID = 0
	}
	return summary
}

func summarizeStubNodes(nodes []*stubDOMNode) []stubNodeSummary {
	summaries := make([]stubNodeSummary, len(nodes))
	for i, node := range nodes {
		summaries[i] = summarizeStubNode(node)
	}
	return summaries
}

func summarizeStubNodesWithoutNewIDs(nodes []*stubDOMNode, keep map[int]bool) []stubNodeSummary {
	summaries := make([]stubNodeSummary, len(nodes))
	for i, node := range nodes {
		summary := summarizeStubNode(node)
		if !keep[node.id] {
			summary.ID = 0
		}
		summaries[i] = summary
	}
	return summaries
}
