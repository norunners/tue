package vue

import (
	"html"
	"sort"
	"strings"
	"testing"
)

type fakeMountTarget struct {
	doc      *fakeDOMDocument
	rootNode *fakeDOMNode
}

func newFakeMountTarget() *fakeMountTarget {
	return &fakeMountTarget{
		doc:      &fakeDOMDocument{},
		rootNode: newFakeElement("root"),
	}
}

func (target *fakeMountTarget) document() domDocumentAccess {
	return target.doc
}

func (target *fakeMountTarget) root() domNodeAccess {
	return target.rootNode
}

func (target *fakeMountTarget) innerHTML() string {
	return renderFakeChildren(target.rootNode)
}

type fakeDOMDocument struct{}

func (document *fakeDOMDocument) createElement(tag string) domNodeAccess {
	return newFakeElement(tag)
}

func (document *fakeDOMDocument) createTextNode(text string) domNodeAccess {
	return &fakeDOMNode{kind: "text", text: text}
}

type fakeDOMNode struct {
	kind      string
	tag       string
	text      string
	attrs     map[string]string
	parent    *fakeDOMNode
	children  []*fakeDOMNode
	listeners map[string][]*fakeDOMEventListener
}

type fakeDOMEventListener struct {
	handler func()
	active  bool
}

func newFakeElement(tag string) *fakeDOMNode {
	return &fakeDOMNode{
		kind:  "element",
		tag:   tag,
		attrs: map[string]string{},
	}
}

func (node *fakeDOMNode) appendChild(child domNodeAccess) {
	childNode := asFakeDOMNode(child)
	if childNode == nil {
		return
	}
	childNode.detach()
	childNode.parent = node
	node.children = append(node.children, childNode)
}

func (node *fakeDOMNode) insertBefore(child, before domNodeAccess) {
	childNode := asFakeDOMNode(child)
	if childNode == nil {
		return
	}
	beforeNode := asFakeDOMNode(before)
	if beforeNode == nil {
		node.appendChild(childNode)
		return
	}

	childNode.detach()
	childNode.parent = node
	for i, existing := range node.children {
		if existing == beforeNode {
			node.children = append(node.children, nil)
			copy(node.children[i+1:], node.children[i:])
			node.children[i] = childNode
			return
		}
	}
	node.children = append(node.children, childNode)
}

func (node *fakeDOMNode) removeChild(child domNodeAccess) {
	childNode := asFakeDOMNode(child)
	if childNode == nil {
		return
	}
	for i, existing := range node.children {
		if existing == childNode {
			node.children = append(node.children[:i], node.children[i+1:]...)
			childNode.parent = nil
			return
		}
	}
}

func (node *fakeDOMNode) setAttribute(name, value string) {
	if node.attrs == nil {
		node.attrs = map[string]string{}
	}
	node.attrs[name] = value
}

func (node *fakeDOMNode) removeAttribute(name string) {
	delete(node.attrs, name)
}

func (node *fakeDOMNode) setTextContent(text string) {
	node.text = text
	if node.kind == "element" {
		for _, child := range node.children {
			child.parent = nil
		}
		node.children = nil
	}
}

func (node *fakeDOMNode) addEventListener(name string, handler func()) func() {
	if node == nil || name == "" || handler == nil {
		return nil
	}
	if node.listeners == nil {
		node.listeners = map[string][]*fakeDOMEventListener{}
	}
	listener := &fakeDOMEventListener{handler: handler, active: true}
	node.listeners[name] = append(node.listeners[name], listener)
	return func() {
		if !listener.active {
			return
		}
		listener.active = false
		listeners := node.listeners[name]
		for i, existing := range listeners {
			if existing == listener {
				node.listeners[name] = append(listeners[:i], listeners[i+1:]...)
				break
			}
		}
	}
}

func (node *fakeDOMNode) dispatchEvent(name string) {
	listeners := append([]*fakeDOMEventListener(nil), node.listeners[name]...)
	for _, listener := range listeners {
		if listener.active {
			listener.handler()
		}
	}
}

func (node *fakeDOMNode) listenerCount(name string) int {
	count := 0
	for _, listener := range node.listeners[name] {
		if listener.active {
			count++
		}
	}
	return count
}

func (node *fakeDOMNode) detach() {
	if node.parent != nil {
		node.parent.removeChild(node)
	}
}

func asFakeDOMNode(node domNodeAccess) *fakeDOMNode {
	if node == nil {
		return nil
	}
	fake, _ := node.(*fakeDOMNode)
	return fake
}

func renderFakeChildren(node *fakeDOMNode) string {
	var b strings.Builder
	for _, child := range node.children {
		renderFakeNode(&b, child)
	}
	return b.String()
}

func renderFakeNode(b *strings.Builder, node *fakeDOMNode) {
	switch node.kind {
	case "text":
		b.WriteString(html.EscapeString(node.text))
	case "element":
		b.WriteByte('<')
		b.WriteString(node.tag)
		names := make([]string, 0, len(node.attrs))
		for name := range node.attrs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			b.WriteByte(' ')
			b.WriteString(name)
			b.WriteString(`="`)
			b.WriteString(html.EscapeString(node.attrs[name]))
			b.WriteByte('"')
		}
		b.WriteByte('>')
		if isVoidElement(node.tag) {
			return
		}
		for _, child := range node.children {
			renderFakeNode(b, child)
		}
		b.WriteString("</")
		b.WriteString(node.tag)
		b.WriteByte('>')
	}
}

func TestStaticPatchUpdatesTextAndAttributesInPlace(t *testing.T) {
	text := "ready"
	attrs := []Attribute{
		Attr("class", "old"),
		Attr("data-remove", "yes"),
	}
	comp := CompOf(struct{}{}, func() VNode {
		return Element("p", attrs, Text(text))
	})
	target := newFakeMountTarget()

	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	element := target.rootNode.children[0]
	textNode := element.children[0]

	text = "updated"
	attrs = []Attribute{
		Attr("class", "new"),
		Attr("title", "added"),
	}
	if err := comp.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if got, want := target.innerHTML(), `<p class="new" title="added">updated</p>`; got != want {
		t.Fatalf("patched HTML = %q, want %q", got, want)
	}
	if target.rootNode.children[0] != element {
		t.Fatal("element node was replaced; want in-place patch")
	}
	if element.children[0] != textNode {
		t.Fatal("text node was replaced; want in-place patch")
	}
}

func TestStaticPatchReplacesDifferentElementType(t *testing.T) {
	tag := "p"
	comp := CompOf(struct{}{}, func() VNode {
		return Element(tag, nil, Text("body"))
	})
	target := newFakeMountTarget()

	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	first := target.rootNode.children[0]

	tag = "section"
	if err := comp.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if got, want := target.innerHTML(), `<section>body</section>`; got != want {
		t.Fatalf("patched HTML = %q, want %q", got, want)
	}
	if target.rootNode.children[0] == first {
		t.Fatal("element node was patched in place; want replacement for different tag")
	}
}

func TestStaticPatchReplacesDifferentKey(t *testing.T) {
	key := "one"
	comp := CompOf(struct{}{}, func() VNode {
		return VNode{
			Type:     VNodeElement,
			Tag:      "p",
			Key:      key,
			Children: []VNode{Text("body")},
		}
	})
	target := newFakeMountTarget()

	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	first := target.rootNode.children[0]

	key = "two"
	if err := comp.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if got, want := target.innerHTML(), `<p>body</p>`; got != want {
		t.Fatalf("patched HTML = %q, want %q", got, want)
	}
	if target.rootNode.children[0] == first {
		t.Fatal("element node was patched in place; want replacement for different key")
	}
}

func TestStaticPatchPatchesRootFragment(t *testing.T) {
	label := "one"
	comp := CompOf(struct{}{}, func() VNode {
		return Fragment(
			Text("before "),
			Element("strong", []Attribute{Attr("data-label", label)}, Text(label)),
		)
	})
	target := newFakeMountTarget()

	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	textNode := target.rootNode.children[0]
	element := target.rootNode.children[1]

	label = "two"
	if err := comp.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if got, want := target.innerHTML(), `before <strong data-label="two">two</strong>`; got != want {
		t.Fatalf("patched HTML = %q, want %q", got, want)
	}
	if len(target.rootNode.children) != 2 {
		t.Fatalf("root children = %d, want 2", len(target.rootNode.children))
	}
	if target.rootNode.children[0] != textNode || target.rootNode.children[1] != element {
		t.Fatal("root fragment children were replaced; want positional patch")
	}
}

func TestStaticPatchPatchesUnkeyedChildrenPositionally(t *testing.T) {
	items := []string{"one", "two"}
	comp := CompOf(struct{}{}, func() VNode {
		children := make([]VNode, 0, len(items))
		for _, item := range items {
			children = append(children, Element("li", nil, Text(item)))
		}
		return Element("ul", nil, children...)
	})
	target := newFakeMountTarget()

	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	list := target.rootNode.children[0]
	first := list.children[0]
	second := list.children[1]

	items = []string{"ONE", "TWO", "THREE"}
	if err := comp.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if got, want := target.innerHTML(), `<ul><li>ONE</li><li>TWO</li><li>THREE</li></ul>`; got != want {
		t.Fatalf("patched HTML after append = %q, want %q", got, want)
	}
	if list.children[0] != first || list.children[1] != second {
		t.Fatal("existing unkeyed children were replaced; want positional patch")
	}

	items = []string{"ONE"}
	if err := comp.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if got, want := target.innerHTML(), `<ul><li>ONE</li></ul>`; got != want {
		t.Fatalf("patched HTML after removal = %q, want %q", got, want)
	}
	if len(list.children) != 1 || list.children[0] != first {
		t.Fatal("unkeyed child removal did not preserve the first child")
	}
}

func TestPatchUpdatesNativeEventListeners(t *testing.T) {
	var events []string
	handler := func() {
		events = append(events, "first")
	}
	comp := CompOf(struct{}{}, func() VNode {
		return ElementWithEvents("button", nil, []EventBinding{
			On("click", handler),
		}, Text("Save"))
	})
	target := newFakeMountTarget()

	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	button := target.rootNode.children[0]
	if got, want := button.listenerCount("click"), 1; got != want {
		t.Fatalf("click listener count after mount = %d, want %d", got, want)
	}
	button.dispatchEvent("click")

	handler = func() {
		events = append(events, "second")
	}
	if err := comp.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if got, want := button.listenerCount("click"), 1; got != want {
		t.Fatalf("click listener count after update = %d, want %d", got, want)
	}
	button.dispatchEvent("click")

	if got, want := strings.Join(events, ","), "first,second"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}

func TestPatchCleansNativeEventListenersOnReplaceAndUnmount(t *testing.T) {
	var events []string
	tag := "button"
	comp := CompOf(struct{}{}, func() VNode {
		return ElementWithEvents(tag, nil, []EventBinding{
			On("click", func() {
				events = append(events, tag)
			}),
		}, Text("Go"))
	})
	target := newFakeMountTarget()

	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	button := target.rootNode.children[0]
	button.dispatchEvent("click")

	tag = "a"
	if err := comp.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if got, want := button.listenerCount("click"), 0; got != want {
		t.Fatalf("old click listener count after replace = %d, want %d", got, want)
	}
	button.dispatchEvent("click")

	link := target.rootNode.children[0]
	link.dispatchEvent("click")
	if err := comp.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
	if got, want := link.listenerCount("click"), 0; got != want {
		t.Fatalf("click listener count after unmount = %d, want %d", got, want)
	}
	link.dispatchEvent("click")

	if got, want := strings.Join(events, ","), "button,a"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}
