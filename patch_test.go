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

func TestMountedUpdatePatchesTrustedHTMLContent(t *testing.T) {
	mode := "children"
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		switch mode {
		case "html":
			return ElementWithTrustedHTML("main", nil, nil, TrustedHTML(`<strong>Ready</strong>`))
		case "back":
			return Element("main", nil, []VNode{Text("Back")})
		default:
			return Element("main", nil, []VNode{
				ElementWithEvents("button", nil, []EventBinding{EventOf("click", func() {})}, []VNode{Text("Save")}),
			})
		}
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	mainNode, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted main node: %v", err)
	}
	mainID := mainNode.id
	button := mainNode.children[0]
	if diff := cmp.Diff(1, button.listenerCount("click")); diff != "" {
		t.Errorf("mismatch initial button listener count (-expected, +actual):\n%s", diff)
	}

	mode = "html"
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	actual, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find raw HTML main node: %v", err)
	}
	if diff := cmp.Diff(stubNodeSummary{ID: mainID, Kind: "element", Tag: "main"}, summarizeStubNode(actual)); diff != "" {
		t.Errorf("mismatch raw HTML element identity (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(`<main><strong>Ready</strong></main>`, target.html()); diff != "" {
		t.Errorf("mismatch trusted HTML patch output (-expected, +actual):\n%s", diff)
	}
	if button.parent != nil {
		t.Errorf("mismatch removed raw HTML child parent: expected nil, got %#v", button.parent)
	}
	if diff := cmp.Diff(0, button.listenerCount("click")); diff != "" {
		t.Errorf("mismatch removed raw HTML child listener count (-expected, +actual):\n%s", diff)
	}

	mode = "back"
	if err := mounted.Update(); err != nil {
		t.Fatalf("second Update returned error: %v", err)
	}
	if diff := cmp.Diff(`<main>Back</main>`, target.html()); diff != "" {
		t.Errorf("mismatch trusted HTML to children output (-expected, +actual):\n%s", diff)
	}
}

func TestMountedSelectValueStateAppliesAfterOptionsMount(t *testing.T) {
	target := newStubDOMTarget()
	_, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return Element("select", []Attribute{Attr("value", "large")}, []VNode{
			Element("option", []Attribute{Attr("value", "small")}, []VNode{Text("Small")}),
			Element("option", []Attribute{Attr("value", "large")}, []VNode{Text("Large")}),
		})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	selectNode, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted select: %v", err)
	}
	var actual []stubAttrSet
	for _, attrSet := range target.attrSets {
		if attrSet.NodeID == selectNode.id && attrSet.Attribute.Name == "value" {
			actual = append(actual, attrSet)
		}
	}

	expected := []stubAttrSet{{
		NodeID:     selectNode.id,
		Attribute:  Attr("value", "large"),
		ChildCount: 2,
	}}
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch select value attr timing (-expected, +actual):\n%s", diff)
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

func TestMountedUpdateReordersKeyedElementChildren(t *testing.T) {
	items := []patchListItem{
		{Key: "a", Text: "A"},
		{Key: "b", Text: "B"},
		{Key: "c", Text: "C"},
	}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		children := make([]VNode, 0, len(items))
		for _, item := range items {
			children = append(children, VNode{
				Type:     VNodeTypeElement,
				Key:      item.Key,
				Tag:      "li",
				Children: []VNode{Text(item.Text)},
			})
		}
		return Element("ul", nil, children)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	list, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted list: %v", err)
	}
	first := list.children[0]
	second := list.children[1]
	third := list.children[2]

	items = []patchListItem{
		{Key: "c", Text: "C!"},
		{Key: "a", Text: "A!"},
		{Key: "d", Text: "D"},
	}
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("<ul><li>C!</li><li>A!</li><li>D</li></ul>", target.html()); diff != "" {
		t.Errorf("mismatch keyed element children HTML (-expected, +actual):\n%s", diff)
	}
	actual := list.children
	if actual[0] != third {
		t.Errorf("mismatch first keyed element child: expected old third node %d, actual %d", third.id, actual[0].id)
	}
	if actual[1] != first {
		t.Errorf("mismatch second keyed element child: expected old first node %d, actual %d", first.id, actual[1].id)
	}
	if actual[2] == first || actual[2] == second || actual[2] == third {
		t.Errorf("mismatch new keyed element child: expected a new node, actual existing node %d", actual[2].id)
	}
	if second.parent != nil {
		t.Errorf("mismatch removed keyed element parent: expected nil, got %#v", second.parent)
	}
}

func TestMountedUpdateKeepsMixedUnkeyedChildrenPositional(t *testing.T) {
	items := []VNode{
		Text("A"),
		VNode{Type: VNodeTypeElement, Key: "x", Tag: "span", Children: []VNode{Text("X")}},
		Text("B"),
	}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return Element("p", nil, items)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	paragraph, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted paragraph: %v", err)
	}
	firstText := paragraph.children[0]
	keyedSpan := paragraph.children[1]
	secondText := paragraph.children[2]

	items = []VNode{
		VNode{Type: VNodeTypeElement, Key: "x", Tag: "span", Children: []VNode{Text("X!")}},
		Text("B!"),
	}
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("<p><span>X!</span>B!</p>", target.html()); diff != "" {
		t.Errorf("mismatch mixed keyed children HTML (-expected, +actual):\n%s", diff)
	}
	actual := paragraph.children
	if actual[0] != keyedSpan {
		t.Errorf("mismatch mixed keyed first child: expected keyed span %d, actual %d", keyedSpan.id, actual[0].id)
	}
	if actual[1] == firstText || actual[1] == secondText {
		t.Errorf("mismatch mixed unkeyed child identity: expected a new node, actual existing node %d", actual[1].id)
	}
	if firstText.parent != nil {
		t.Errorf("mismatch removed first unkeyed parent: expected nil, got %#v", firstText.parent)
	}
	if secondText.parent != nil {
		t.Errorf("mismatch removed second unkeyed parent: expected nil, got %#v", secondText.parent)
	}
}

func TestMountedUpdateDoesNotMoveStableKeyedChildren(t *testing.T) {
	items := []patchListItem{
		{Key: "a", Text: "A"},
		{Key: "b", Text: "B"},
	}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		children := make([]VNode, 0, len(items))
		for _, item := range items {
			children = append(children, VNode{
				Type:     VNodeTypeElement,
				Key:      item.Key,
				Tag:      "li",
				Children: []VNode{Text(item.Text)},
			})
		}
		return Element("ul", nil, children)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	list, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted list: %v", err)
	}
	first := list.children[0]
	second := list.children[1]
	target.moves = 0

	items = []patchListItem{
		{Key: "a", Text: "A!"},
		{Key: "b", Text: "B!"},
	}
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("<ul><li>A!</li><li>B!</li></ul>", target.html()); diff != "" {
		t.Errorf("mismatch stable keyed children HTML (-expected, +actual):\n%s", diff)
	}
	if list.children[0] != first {
		t.Errorf("mismatch stable keyed first child: expected old first node %d, actual %d", first.id, list.children[0].id)
	}
	if list.children[1] != second {
		t.Errorf("mismatch stable keyed second child: expected old second node %d, actual %d", second.id, list.children[1].id)
	}
	if diff := cmp.Diff(0, target.moves); diff != "" {
		t.Errorf("mismatch stable keyed move count (-expected, +actual):\n%s", diff)
	}
}

func TestMountedUpdateReplacesSameKeyDifferentType(t *testing.T) {
	renderText := false
	events := []string{}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		var child VNode
		if renderText {
			child = VNode{Type: VNodeTypeText, Key: "a", Text: "Saved"}
		} else {
			child = ElementWithEvents("button", nil, []EventBinding{EventOf("click", func() {
				events = append(events, "click")
			})}, []VNode{Text("Save")})
			child.Key = "a"
		}
		return Element("div", nil, []VNode{child})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	container, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted container: %v", err)
	}
	button := container.children[0]
	button.dispatch("click")

	renderText = true
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	button.dispatch("click")

	if diff := cmp.Diff("<div>Saved</div>", target.html()); diff != "" {
		t.Errorf("mismatch same-key replacement HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"click"}, events); diff != "" {
		t.Errorf("mismatch same-key replacement event calls (-expected, +actual):\n%s", diff)
	}
	if button.parent != nil {
		t.Errorf("mismatch replaced same-key parent: expected nil, got %#v", button.parent)
	}
	if diff := cmp.Diff(0, button.listenerCount("click")); diff != "" {
		t.Errorf("mismatch replaced same-key listener count (-expected, +actual):\n%s", diff)
	}
}

func TestMountedUpdateDuplicateKeysReuseFirstOldChildOnly(t *testing.T) {
	items := []patchListItem{
		{Key: "a", Text: "First"},
		{Key: "a", Text: "Second"},
	}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		children := make([]VNode, 0, len(items))
		for _, item := range items {
			children = append(children, VNode{
				Type:     VNodeTypeElement,
				Key:      item.Key,
				Tag:      "li",
				Children: []VNode{Text(item.Text)},
			})
		}
		return Element("ul", nil, children)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	list, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted list: %v", err)
	}
	first := list.children[0]
	second := list.children[1]

	items = []patchListItem{
		{Key: "a", Text: "First!"},
		{Key: "a", Text: "Second!"},
	}
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("<ul><li>First!</li><li>Second!</li></ul>", target.html()); diff != "" {
		t.Errorf("mismatch duplicate keyed children HTML (-expected, +actual):\n%s", diff)
	}
	if list.children[0] != first {
		t.Errorf("mismatch first duplicate keyed child: expected old first node %d, actual %d", first.id, list.children[0].id)
	}
	if list.children[1] == first || list.children[1] == second {
		t.Errorf("mismatch second duplicate keyed child: expected a new node, actual existing node %d", list.children[1].id)
	}
	if second.parent != nil {
		t.Errorf("mismatch removed duplicate keyed parent: expected nil, got %#v", second.parent)
	}
}

func TestMountedUpdateReordersKeyedFragmentChildren(t *testing.T) {
	items := []patchListItem{
		{Key: "a", Text: "A"},
		{Key: "b", Text: "B"},
		{Key: "c", Text: "C"},
	}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		children := make([]VNode, 0, len(items))
		for _, item := range items {
			children = append(children, VNode{Type: VNodeTypeText, Key: item.Key, Text: item.Text})
		}
		return Fragment(children)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	children := visibleChildren(target.rootNode)
	first := children[0]
	second := children[1]
	third := children[2]

	items = []patchListItem{
		{Key: "c", Text: "C!"},
		{Key: "a", Text: "A!"},
		{Key: "d", Text: "D"},
	}
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("C!A!D", target.html()); diff != "" {
		t.Errorf("mismatch keyed fragment HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"comment", "text", "text", "text", "comment"}, childKinds(target.rootNode)); diff != "" {
		t.Errorf("mismatch keyed fragment child kinds (-expected, +actual):\n%s", diff)
	}
	actual := visibleChildren(target.rootNode)
	if actual[0] != third {
		t.Errorf("mismatch first keyed fragment child: expected old third node %d, actual %d", third.id, actual[0].id)
	}
	if actual[1] != first {
		t.Errorf("mismatch second keyed fragment child: expected old first node %d, actual %d", first.id, actual[1].id)
	}
	if actual[2] == first || actual[2] == second || actual[2] == third {
		t.Errorf("mismatch new keyed fragment child: expected a new node, actual existing node %d", actual[2].id)
	}
	if second.parent != nil {
		t.Errorf("mismatch removed keyed fragment parent: expected nil, got %#v", second.parent)
	}
}

func TestMountedUpdateReordersKeyedComponentChildren(t *testing.T) {
	renderAsElement := false
	items := []patchListItem{
		{Key: "a", Text: "A"},
		{Key: "b", Text: "B"},
		{Key: "c", Text: "C"},
	}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		children := make([]VNode, 0, len(items))
		for _, item := range items {
			vnode := Component("PatchChild", func() *Comp {
				return CompOf(&patchChildFixture{label: item.Text, renderAsElement: &renderAsElement}, renderPatchChildFixture)
			})
			vnode.Key = item.Key
			children = append(children, vnode)
		}
		return Fragment(children)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	children := visibleChildren(target.rootNode)
	first := children[0]
	second := children[1]
	third := children[2]

	items = []patchListItem{
		{Key: "c", Text: "C"},
		{Key: "a", Text: "A"},
		{Key: "d", Text: "D"},
	}
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("CAD", target.html()); diff != "" {
		t.Errorf("mismatch keyed component children HTML (-expected, +actual):\n%s", diff)
	}
	actual := visibleChildren(target.rootNode)
	if actual[0] != third {
		t.Errorf("mismatch first keyed component child: expected old third node %d, actual %d", third.id, actual[0].id)
	}
	if actual[1] != first {
		t.Errorf("mismatch second keyed component child: expected old first node %d, actual %d", first.id, actual[1].id)
	}
	if actual[2] == first || actual[2] == second || actual[2] == third {
		t.Errorf("mismatch new keyed component child: expected a new node, actual existing node %d", actual[2].id)
	}
	if second.parent != nil {
		t.Errorf("mismatch removed keyed component parent: expected nil, got %#v", second.parent)
	}

	renderAsElement = true
	if err := mounted.Update(); err != nil {
		t.Fatalf("second Update returned error: %v", err)
	}

	if diff := cmp.Diff("<span>C</span><span>A</span><span>D</span>", target.html()); diff != "" {
		t.Errorf("mismatch keyed component replacement order (-expected, +actual):\n%s", diff)
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
		return ElementWithEvents("button", nil, []EventBinding{EventOf("click", handler)}, []VNode{Text("Save")})
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

func TestMountedDuplicateEventHandlersRunModelBeforeUserHandler(t *testing.T) {
	query := "initial"
	events := []string{}
	target := newStubDOMTarget()
	_, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return ElementWithEvents("input", nil, []EventBinding{
			OnValue("input", func(value string) {
				query = value
				events = append(events, "model:"+query)
			}),
			EventOf("input", func() {
				events = append(events, "user:"+query)
			}),
		}, nil)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	input, err := onlyVisibleChild(target.rootNode)
	if err != nil {
		t.Fatalf("find mounted input: %v", err)
	}
	input.dispatchEvent("input", stubEvent{value: "query"})

	expected := []string{"model:query", "user:query"}
	if diff := cmp.Diff(expected, events); diff != "" {
		t.Errorf("mismatch duplicate event handler calls (-expected, +actual):\n%s", diff)
	}
}

func TestMountedEventListenersCleanUpWhenNodeIsReplaced(t *testing.T) {
	replace := false
	events := []string{}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		if replace {
			return ElementWithEvents("a", nil, []EventBinding{EventOf("click", func() {
				events = append(events, "link")
			})}, []VNode{Text("Link")})
		}
		return ElementWithEvents("button", nil, []EventBinding{EventOf("click", func() {
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
		return ElementWithEvents("button", nil, []EventBinding{EventOf("click", func() {
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

type patchListItem struct {
	Key  string
	Text string
}

type patchChildFixture struct {
	label           string
	renderAsElement *bool
}

func renderPatchChildFixture(component *patchChildFixture) VNode {
	if component.renderAsElement != nil && *component.renderAsElement {
		return Element("span", nil, []VNode{Text(component.label)})
	}
	return Text(component.label)
}

type stubDOMTarget struct {
	rootNode *stubDOMNode
	nextID   int
	clears   int
	moves    int
	attrSets []stubAttrSet
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
	if childNode.parent != nil {
		t.moves++
	}
	childNode.detach()
	childNode.parent = parentNode
	parentNode.innerHTML = nil
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
	if childNode == beforeNode {
		return nil
	}
	if childNode.parent != nil {
		t.moves++
	}
	childNode.detach()
	index := parentNode.childIndex(beforeNode)
	if index == -1 {
		return fmt.Errorf("before node %d is not a child of parent %d", beforeNode.id, parentNode.id)
	}
	childNode.parent = parentNode
	parentNode.innerHTML = nil
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

func (t *stubDOMTarget) setInnerHTML(node domNode, html string) error {
	stubNode, ok := node.(*stubDOMNode)
	if !ok {
		return fmt.Errorf("expected stub element node, got %T", node)
	}
	for _, child := range stubNode.children {
		child.parent = nil
	}
	stubNode.children = nil
	stubNode.innerHTML = &html
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
	t.attrSets = append(t.attrSets, stubAttrSet{
		NodeID:     stubNode.id,
		Attribute:  attr,
		ChildCount: len(stubNode.children),
	})
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

func (t *stubDOMTarget) addEventListener(node domNode, name string, handler func(Event)) (func(), error) {
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
	innerHTML *string
	attrs     map[string]Attribute
	parent    *stubDOMNode
	children  []*stubDOMNode
	listeners map[string][]*stubEventListener
}

type stubEventListener struct {
	handler func(Event)
	removed bool
}

type stubAttrSet struct {
	NodeID     int
	Attribute  Attribute
	ChildCount int
}

type stubEvent struct {
	value   string
	checked bool
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
	n.dispatchEvent(name, stubEvent{})
}

func (n *stubDOMNode) dispatchEvent(name string, event Event) {
	for _, listener := range append([]*stubEventListener(nil), n.listeners[name]...) {
		if listener.removed || listener.handler == nil {
			continue
		}
		listener.handler(event)
	}
}

func (e stubEvent) Value() string {
	return e.value
}

func (e stubEvent) Checked() bool {
	return e.checked
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
			if attr.HasBoolValue && !attr.BoolValue {
				continue
			}
			builder.WriteByte(' ')
			builder.WriteString(attr.Name)
			if attr.HasValue && !attr.HasBoolValue {
				builder.WriteString(`="`)
				builder.WriteString(html.EscapeString(attr.Value))
				builder.WriteByte('"')
			}
		}
		builder.WriteByte('>')
		if node.innerHTML != nil {
			builder.WriteString(*node.innerHTML)
		} else {
			for _, child := range node.children {
				writeStubHTML(builder, child)
			}
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
