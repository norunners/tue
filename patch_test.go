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
	target := newFakeDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return Text(value)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	text := onlyVisibleChild(t, target.rootNode)
	textID := text.id

	value = "second"
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	actual := onlyVisibleChild(t, target.rootNode)
	if diff := cmp.Diff(fakeNodeSummary{ID: textID, Kind: "text", Text: "second"}, summarizeFakeNode(actual)); diff != "" {
		t.Errorf("mismatch patched text node (-expected, +actual):\n%s", diff)
	}
}

func TestMountedUpdatePatchesElementAttributesInPlace(t *testing.T) {
	primary := true
	target := newFakeDOMTarget()
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

	button := onlyVisibleChild(t, target.rootNode)
	buttonID := button.id

	primary = false
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	actual := onlyVisibleChild(t, target.rootNode)
	expected := fakeNodeSummary{
		ID:   buttonID,
		Kind: "element",
		Tag:  "button",
		Attrs: []Attribute{
			Attr("class", "secondary"),
			Attr("title", "Ready"),
		},
		Children: []fakeNodeSummary{{Kind: "text", Text: "Save"}},
	}
	if diff := cmp.Diff(expected, summarizeFakeNodeWithoutChildIDs(actual)); diff != "" {
		t.Errorf("mismatch patched element attributes (-expected, +actual):\n%s", diff)
	}
}

func TestMountedUpdateReplacesDifferentKey(t *testing.T) {
	key := "first"
	target := newFakeDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return VNode{Type: VNodeTypeElement, Key: key, Tag: "section"}
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	first := onlyVisibleChild(t, target.rootNode)

	key = "second"
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	second := onlyVisibleChild(t, target.rootNode)
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
	target := newFakeDOMTarget()
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

	paragraph := onlyVisibleChild(t, target.rootNode)
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
	if diff := cmp.Diff([]fakeNodeSummary{
		{ID: first.id, Kind: "text", Text: "A!"},
		{ID: second.id, Kind: "text", Text: "C"},
		{Kind: "text", Text: "D"},
	}, summarizeFakeNodesWithoutNewIDs(children, map[int]bool{first.id: true, second.id: true})); diff != "" {
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
	if diff := cmp.Diff(fakeNodeSummary{ID: first.id, Kind: "text", Text: "Z"}, summarizeFakeNode(paragraph.children[0])); diff != "" {
		t.Errorf("mismatch retained first child after trim (-expected, +actual):\n%s", diff)
	}
}

func TestMountedUpdatePatchesFragmentChildrenBeforeEndMarker(t *testing.T) {
	values := []string{"one"}
	target := newFakeDOMTarget()
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

type patchFixture struct{}

type fakeDOMTarget struct {
	rootNode *fakeDOMNode
	nextID   int
	clears   int
}

func newFakeDOMTarget() *fakeDOMTarget {
	target := &fakeDOMTarget{}
	target.rootNode = target.newNode("root")
	return target
}

func (t *fakeDOMTarget) root() domNode {
	return t.rootNode
}

func (t *fakeDOMTarget) createElement(tag string) (domNode, error) {
	node := t.newNode("element")
	node.tag = tag
	node.attrs = map[string]Attribute{}
	return node, nil
}

func (t *fakeDOMTarget) createText(text string) (domNode, error) {
	node := t.newNode("text")
	node.text = text
	return node, nil
}

func (t *fakeDOMTarget) createMarker(text string) (domNode, error) {
	node := t.newNode("comment")
	node.text = text
	return node, nil
}

func (t *fakeDOMTarget) appendChild(parent domNode, child domNode) error {
	parentNode, childNode, err := fakeParentChild(parent, child)
	if err != nil {
		return err
	}
	childNode.detach()
	childNode.parent = parentNode
	parentNode.children = append(parentNode.children, childNode)
	return nil
}

func (t *fakeDOMTarget) insertBefore(parent domNode, child domNode, before domNode) error {
	parentNode, childNode, err := fakeParentChild(parent, child)
	if err != nil {
		return err
	}
	beforeNode, ok := before.(*fakeDOMNode)
	if !ok {
		return fmt.Errorf("expected fake before node, got %T", before)
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

func (t *fakeDOMTarget) removeChild(parent domNode, child domNode) error {
	parentNode, childNode, err := fakeParentChild(parent, child)
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

func (t *fakeDOMTarget) setText(node domNode, text string) error {
	fakeNode, ok := node.(*fakeDOMNode)
	if !ok {
		return fmt.Errorf("expected fake text node, got %T", node)
	}
	fakeNode.text = text
	return nil
}

func (t *fakeDOMTarget) setAttr(node domNode, attr Attribute) error {
	fakeNode, ok := node.(*fakeDOMNode)
	if !ok {
		return fmt.Errorf("expected fake element node, got %T", node)
	}
	if fakeNode.attrs == nil {
		fakeNode.attrs = map[string]Attribute{}
	}
	fakeNode.attrs[attr.Name] = attr
	return nil
}

func (t *fakeDOMTarget) removeAttr(node domNode, name string) error {
	fakeNode, ok := node.(*fakeDOMNode)
	if !ok {
		return fmt.Errorf("expected fake element node, got %T", node)
	}
	delete(fakeNode.attrs, name)
	return nil
}

func (t *fakeDOMTarget) clear() error {
	for _, child := range t.rootNode.children {
		child.parent = nil
	}
	t.rootNode.children = nil
	t.clears++
	return nil
}

func (t *fakeDOMTarget) html() string {
	var builder strings.Builder
	for _, child := range t.rootNode.children {
		writeFakeHTML(&builder, child)
	}
	return builder.String()
}

func (t *fakeDOMTarget) newNode(kind string) *fakeDOMNode {
	t.nextID++
	return &fakeDOMNode{id: t.nextID, kind: kind}
}

type fakeDOMNode struct {
	id       int
	kind     string
	tag      string
	text     string
	attrs    map[string]Attribute
	parent   *fakeDOMNode
	children []*fakeDOMNode
}

func (n *fakeDOMNode) childIndex(child *fakeDOMNode) int {
	for i, candidate := range n.children {
		if candidate == child {
			return i
		}
	}
	return -1
}

func (n *fakeDOMNode) detach() {
	if n.parent == nil {
		return
	}
	index := n.parent.childIndex(n)
	if index != -1 {
		n.parent.children = append(n.parent.children[:index], n.parent.children[index+1:]...)
	}
	n.parent = nil
}

func fakeParentChild(parent domNode, child domNode) (*fakeDOMNode, *fakeDOMNode, error) {
	parentNode, ok := parent.(*fakeDOMNode)
	if !ok {
		return nil, nil, fmt.Errorf("expected fake parent node, got %T", parent)
	}
	childNode, ok := child.(*fakeDOMNode)
	if !ok {
		return nil, nil, fmt.Errorf("expected fake child node, got %T", child)
	}
	return parentNode, childNode, nil
}

func writeFakeHTML(builder *strings.Builder, node *fakeDOMNode) {
	switch node.kind {
	case "element":
		builder.WriteByte('<')
		builder.WriteString(node.tag)
		for _, attr := range sortedFakeAttrs(node.attrs) {
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
			writeFakeHTML(builder, child)
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

func sortedFakeAttrs(attrs map[string]Attribute) []Attribute {
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

func onlyVisibleChild(t *testing.T, node *fakeDOMNode) *fakeDOMNode {
	t.Helper()

	children := visibleChildren(node)
	if len(children) != 1 {
		t.Fatalf("visible children = %#v, want exactly one", summarizeFakeNodes(children))
	}
	return children[0]
}

func visibleChildren(node *fakeDOMNode) []*fakeDOMNode {
	children := make([]*fakeDOMNode, 0, len(node.children))
	for _, child := range node.children {
		if child.kind != "comment" {
			children = append(children, child)
		}
	}
	return children
}

func childKinds(node *fakeDOMNode) []string {
	kinds := make([]string, len(node.children))
	for i, child := range node.children {
		kinds[i] = child.kind
	}
	return kinds
}

type fakeNodeSummary struct {
	ID       int
	Kind     string
	Tag      string
	Text     string
	Attrs    []Attribute
	Children []fakeNodeSummary
}

func summarizeFakeNode(node *fakeDOMNode) fakeNodeSummary {
	children := make([]fakeNodeSummary, 0, len(node.children))
	for _, child := range node.children {
		if child.kind == "comment" {
			continue
		}
		children = append(children, summarizeFakeNode(child))
	}
	if len(children) == 0 {
		children = nil
	}
	return fakeNodeSummary{
		ID:       node.id,
		Kind:     node.kind,
		Tag:      node.tag,
		Text:     node.text,
		Attrs:    sortedFakeAttrs(node.attrs),
		Children: children,
	}
}

func summarizeFakeNodeWithoutChildIDs(node *fakeDOMNode) fakeNodeSummary {
	summary := summarizeFakeNode(node)
	for i := range summary.Children {
		summary.Children[i].ID = 0
	}
	return summary
}

func summarizeFakeNodes(nodes []*fakeDOMNode) []fakeNodeSummary {
	summaries := make([]fakeNodeSummary, len(nodes))
	for i, node := range nodes {
		summaries[i] = summarizeFakeNode(node)
	}
	return summaries
}

func summarizeFakeNodesWithoutNewIDs(nodes []*fakeDOMNode, keep map[int]bool) []fakeNodeSummary {
	summaries := make([]fakeNodeSummary, len(nodes))
	for i, node := range nodes {
		summary := summarizeFakeNode(node)
		if !keep[node.id] {
			summary.ID = 0
		}
		summaries[i] = summary
	}
	return summaries
}
