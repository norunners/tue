package tue

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRenderHTMLEscapesTextAndAttributes(t *testing.T) {
	node := Element("main", []Attribute{
		Attr("title", `A "quoted" & <tag>`),
		BoolAttr("hidden"),
	}, []VNode{
		Text(`Hello <Tue> & "friends"`),
	})

	if diff := cmp.Diff(`<main title="A &#34;quoted&#34; &amp; &lt;tag&gt;" hidden>Hello &lt;Tue&gt; &amp; &#34;friends&#34;</main>`, RenderHTML(node)); diff != "" {
		t.Errorf("mismatch rendered HTML (-expected, +actual):\n%s", diff)
	}
}

func TestRenderHTMLWritesTrustedHTMLWithoutEscaping(t *testing.T) {
	node := ElementWithTrustedHTML("section", []Attribute{
		Attr("data-state", "ready"),
	}, nil, TrustedHTML(`<strong>Ready</strong><span data-raw="&">Now</span>`))

	expected := `<section data-state="ready"><strong>Ready</strong><span data-raw="&">Now</span></section>`
	if diff := cmp.Diff(expected, RenderHTML(node)); diff != "" {
		t.Errorf("mismatch rendered trusted HTML (-expected, +actual):\n%s", diff)
	}
}

func TestClassAttrNormalizesStaticAndBoundClasses(t *testing.T) {
	actual := ClassAttr(" page  active ", "", " selected ", "wide")

	expected := Attribute{Name: "class", Value: "page active selected wide", HasValue: true}
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch class attribute (-expected, +actual):\n%s", diff)
	}
}

func TestStyleAttrNormalizesStaticAndBoundStyles(t *testing.T) {
	actual := StyleAttr(" color: red; ", "", " display: block; ", "font-weight: bold")

	expected := Attribute{Name: "style", Value: "color: red; display: block; font-weight: bold", HasValue: true}
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch style attribute (-expected, +actual):\n%s", diff)
	}
}

func TestBoolStateAttrRendersOnlyWhenTrue(t *testing.T) {
	node := Element("input", []Attribute{
		BoolStateAttr("checked", false),
		BoolStateAttr("disabled", true),
	}, nil)

	expected := "<input disabled></input>"
	if diff := cmp.Diff(expected, RenderHTML(node)); diff != "" {
		t.Errorf("mismatch rendered boolean state attributes (-expected, +actual):\n%s", diff)
	}
}

func TestEventValueHelpersReadEventTargetState(t *testing.T) {
	values := []string{}
	checks := []bool{}

	OnValue("input", func(value string) {
		values = append(values, value)
	}).Handler(stubEvent{value: "query"})
	OnChecked("change", func(checked bool) {
		checks = append(checks, checked)
	}).Handler(stubEvent{checked: true})

	if diff := cmp.Diff([]string{"query"}, values); diff != "" {
		t.Errorf("mismatch value event values (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]bool{true}, checks); diff != "" {
		t.Errorf("mismatch checked event values (-expected, +actual):\n%s", diff)
	}
}

func TestCompOfCallsOptionalInit(t *testing.T) {
	component := &initFixture{}

	comp := CompOf(component, func(fixture *initFixture) VNode {
		return Text(fixture.value)
	})

	if diff := cmp.Diff("initialized", component.value); diff != "" {
		t.Errorf("mismatch component value (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff("initialized", comp.Render().Text); diff != "" {
		t.Errorf("mismatch rendered text (-expected, +actual):\n%s", diff)
	}
}

func TestCompOfRunsGeneratedInitializersBeforeInit(t *testing.T) {
	component := &generatedInitFixture{}

	CompOf(component, nil, func() {
		component.value = "generated"
	})

	if diff := cmp.Diff("generated", component.initializedWith); diff != "" {
		t.Errorf("mismatch value observed by Init (-expected, +actual):\n%s", diff)
	}
}

func TestMountComponentRunsLifecycleInOrder(t *testing.T) {
	events := []string{}
	component := &lifecycleFixture{events: &events, value: "first"}
	comp := CompOf(component, func(fixture *lifecycleFixture) VNode {
		*fixture.events = append(*fixture.events, "render:"+fixture.value)
		return Text(fixture.value)
	})
	target := newStubDOMTarget()

	mounted, err := mountComponent(comp, target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}
	if diff := cmp.Diff("first", target.html()); diff != "" {
		t.Errorf("mismatch mounted target HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"init", "render:first", "mounted"}, events); diff != "" {
		t.Errorf("mismatch lifecycle after mount (-expected, +actual):\n%s", diff)
	}

	component.value = "second"
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if diff := cmp.Diff("second", target.html()); diff != "" {
		t.Errorf("mismatch mounted target HTML after update (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"init", "render:first", "mounted", "render:second", "updated"}, events); diff != "" {
		t.Errorf("mismatch lifecycle after update (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
	if diff := cmp.Diff("", target.html()); diff != "" {
		t.Errorf("mismatch mounted target HTML after unmount (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"init", "render:first", "mounted", "render:second", "updated", "cleanup", "unmounted"}, events); diff != "" {
		t.Errorf("mismatch lifecycle after unmount (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Errorf("second Unmount returned error: %v", err)
	}
	if diff := cmp.Diff("", target.html()); diff != "" {
		t.Errorf("mismatch mounted target HTML after second unmount (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"init", "render:first", "mounted", "render:second", "updated", "cleanup", "unmounted"}, events); diff != "" {
		t.Errorf("mismatch lifecycle after second unmount (-expected, +actual):\n%s", diff)
	}
}

func TestMountedReactiveRerendersWhenRenderDependencyChanges(t *testing.T) {
	count := StateOf(0)
	renderCount := 0
	component := &reactiveRenderFixture{}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(component, func(*reactiveRenderFixture) VNode {
		renderCount++
		return Text(fmt.Sprintf("count:%d", count.Get()))
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	if diff := cmp.Diff("count:0", target.html()); diff != "" {
		t.Errorf("mismatch initial reactive render HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(1, renderCount); diff != "" {
		t.Errorf("mismatch initial reactive render count (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(0, component.updateCount); diff != "" {
		t.Errorf("mismatch initial update count (-expected, +actual):\n%s", diff)
	}

	count.Set(1)

	if diff := cmp.Diff("count:1", target.html()); diff != "" {
		t.Errorf("mismatch updated reactive render HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(2, renderCount); diff != "" {
		t.Errorf("mismatch updated reactive render count (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(1, component.updateCount); diff != "" {
		t.Errorf("mismatch updated lifecycle count (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
}

func TestMountedReactiveRerendersCoalesceInsideBatch(t *testing.T) {
	count := StateOf(0)
	renderCount := 0
	component := &reactiveRenderFixture{}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(component, func(*reactiveRenderFixture) VNode {
		renderCount++
		return Text(fmt.Sprint(count.Get()))
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	Batch(func() {
		count.Set(1)
		count.Set(2)
		count.Set(3)

		if diff := cmp.Diff("0", target.html()); diff != "" {
			t.Errorf("mismatch batched reactive render HTML before flush (-expected, +actual):\n%s", diff)
		}
		if diff := cmp.Diff(1, renderCount); diff != "" {
			t.Errorf("mismatch batched reactive render count before flush (-expected, +actual):\n%s", diff)
		}
		if diff := cmp.Diff(0, component.updateCount); diff != "" {
			t.Errorf("mismatch batched update count before flush (-expected, +actual):\n%s", diff)
		}
	})

	if diff := cmp.Diff("3", target.html()); diff != "" {
		t.Errorf("mismatch batched reactive render HTML after flush (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(2, renderCount); diff != "" {
		t.Errorf("mismatch batched reactive render count after flush (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(1, component.updateCount); diff != "" {
		t.Errorf("mismatch batched update count after flush (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
}

func TestMountedReactiveRerenderStopsOnUnmount(t *testing.T) {
	count := StateOf(0)
	renderCount := 0
	component := &reactiveRenderFixture{}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(component, func(*reactiveRenderFixture) VNode {
		renderCount++
		return Text(fmt.Sprint(count.Get()))
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	Batch(func() {
		count.Set(1)
		if err := mounted.Unmount(); err != nil {
			t.Fatalf("Unmount returned error: %v", err)
		}
	})
	count.Set(2)

	if diff := cmp.Diff("", target.html()); diff != "" {
		t.Errorf("mismatch unmounted reactive render HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(1, renderCount); diff != "" {
		t.Errorf("mismatch unmounted reactive render count (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(0, component.updateCount); diff != "" {
		t.Errorf("mismatch unmounted update count (-expected, +actual):\n%s", diff)
	}
}

func TestMountedReactiveRerenderDoesNotTrackUpdatedHookReads(t *testing.T) {
	count := StateOf(0)
	hookValue := StateOf("initial")
	events := []string{}
	component := &reactiveUpdatedHookFixture{
		events:    &events,
		hookValue: hookValue,
	}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(component, func(*reactiveUpdatedHookFixture) VNode {
		events = append(events, fmt.Sprintf("render:%d", count.Get()))
		return Text(fmt.Sprint(count.Get()))
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	count.Set(1)
	hookValue.Set("changed")

	if diff := cmp.Diff("1", target.html()); diff != "" {
		t.Errorf("mismatch hook-read reactive render HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{
		"render:0",
		"render:1",
		"updated:initial",
	}, events); diff != "" {
		t.Errorf("mismatch hook-read reactive render events (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
}

func TestMountedComponentVNodeRunsChildLifecycleAndUpdates(t *testing.T) {
	value := "first"
	events := []string{}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return Element("main", nil, []VNode{
			Component("Child", func() *CompInstance {
				child := &componentVNodeFixture{
					events: &events,
					value:  func() string { return value },
				}
				return CompOf(child, func(child *componentVNodeFixture) VNode {
					*child.events = append(*child.events, "render:"+child.value())
					return Text(child.value())
				})
			}),
		})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	if diff := cmp.Diff("<main>first</main>", target.html()); diff != "" {
		t.Errorf("mismatch mounted component HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"init:first", "render:first", "mounted"}, events); diff != "" {
		t.Errorf("mismatch child lifecycle after mount (-expected, +actual):\n%s", diff)
	}

	value = "second"
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("<main>second</main>", target.html()); diff != "" {
		t.Errorf("mismatch updated component HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"init:first", "render:first", "mounted", "render:second", "updated"}, events); diff != "" {
		t.Errorf("mismatch child lifecycle after update (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}

	if diff := cmp.Diff("", target.html()); diff != "" {
		t.Errorf("mismatch unmounted component HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"init:first", "render:first", "mounted", "render:second", "updated", "cleanup", "unmounted"}, events); diff != "" {
		t.Errorf("mismatch child lifecycle after unmount (-expected, +actual):\n%s", diff)
	}
}

func TestMountedComponentVNodeAppliesInheritedScopeAttrsToRoot(t *testing.T) {
	scopeAttrs := []string{"data-tue-c-parent"}
	childTag := "section"
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		child := Component("Child", func() *CompInstance {
			return CompOf(&patchFixture{}, func(*patchFixture) VNode {
				return Element(childTag, []Attribute{Attr("class", "banner")}, []VNode{
					Element("p", nil, []VNode{Text("Child")}),
				})
			})
		})
		return WithScopeAttrs(child, scopeAttrs...)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	expected := `<section class="banner" data-tue-c-parent><p>Child</p></section>`
	if diff := cmp.Diff(expected, target.html()); diff != "" {
		t.Errorf("mismatch mounted inherited scope HTML (-expected, +actual):\n%s", diff)
	}

	scopeAttrs = nil
	childTag = "article"
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	expected = `<article class="banner"><p>Child</p></article>`
	if diff := cmp.Diff(expected, target.html()); diff != "" {
		t.Errorf("mismatch patched inherited scope HTML (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
}

func TestMountedComponentVNodeAppendsNestedInheritedScopeAttrsToRoot(t *testing.T) {
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		parent := Component("Parent", func() *CompInstance {
			return CompOf(&patchFixture{}, func(*patchFixture) VNode {
				child := Component("Child", func() *CompInstance {
					return CompOf(&patchFixture{}, func(*patchFixture) VNode {
						return Element("section", []Attribute{
							Attr("class", "child-root"),
							BoolAttr("data-tue-c-child"),
						}, []VNode{Text("Child")})
					})
				})
				return WithScopeAttrs(child, "data-tue-c-parent")
			})
		})
		return WithScopeAttrs(parent, "data-tue-c-grandparent")
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	expected := `<section class="child-root" data-tue-c-child data-tue-c-grandparent data-tue-c-parent>Child</section>`
	if diff := cmp.Diff(expected, target.html()); diff != "" {
		t.Errorf("mismatch nested inherited scope HTML (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
}

func TestMountedComponentVNodeTracksChildReactiveReads(t *testing.T) {
	count := StateOf(0)
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return Component("Child", func() *CompInstance {
			return CompOf(&patchFixture{}, func(*patchFixture) VNode {
				return Text(fmt.Sprint(count.Get()))
			})
		})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	count.Set(1)

	if diff := cmp.Diff("1", target.html()); diff != "" {
		t.Errorf("mismatch child reactive render HTML (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
}

func TestMountedComponentVNodeRendersDefaultSlot(t *testing.T) {
	value := StateOf("first")
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return Component("Child", func() *CompInstance {
			child := CompOf(&patchFixture{}, func(*patchFixture) VNode {
				return Element("section", nil, []VNode{Slot(Text("fallback"))})
			})
			child.DefaultSlot = func() VNode {
				return Element("strong", nil, []VNode{Text(value.Get())})
			}
			return child
		})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	expected := "<section><strong>first</strong></section>"
	if diff := cmp.Diff(expected, target.html()); diff != "" {
		t.Errorf("mismatch mounted default slot HTML (-expected, +actual):\n%s", diff)
	}

	value.Set("second")

	expected = "<section><strong>second</strong></section>"
	if diff := cmp.Diff(expected, target.html()); diff != "" {
		t.Errorf("mismatch updated default slot HTML (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
}

func TestMountedComponentVNodeRefreshesDefaultSlotOnPatch(t *testing.T) {
	label := "expanded"
	slotMode := "expanded"
	initCount := 0
	defaultSlot := func() func() VNode {
		switch slotMode {
		case "expanded":
			return func() VNode {
				return Element("strong", nil, []VNode{Text("Expanded")})
			}
		case "collapsed":
			return func() VNode {
				return Element("em", nil, []VNode{Text("Collapsed")})
			}
		default:
			return nil
		}
	}
	updateChild := func(childComp *CompInstance) {
		child := childComp.Component.(*componentSlotPatchFixture)
		child.label = func() string { return label }
		childComp.DefaultSlot = defaultSlot()
	}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return ComponentWithUpdate("Child", func() *CompInstance {
			child := &componentSlotPatchFixture{
				label:     func() string { return label },
				initCount: &initCount,
			}
			childComp := CompOf(child, func(fixture *componentSlotPatchFixture) VNode {
				return Element("section", nil, []VNode{
					Element("h2", nil, []VNode{Text(fixture.label())}),
					Slot(Text("fallback")),
				})
			})
			childComp.DefaultSlot = defaultSlot()
			return childComp
		}, updateChild)
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	expected := "<section><h2>expanded</h2><strong>Expanded</strong></section>"
	if diff := cmp.Diff(expected, target.html()); diff != "" {
		t.Errorf("mismatch mounted refreshed slot HTML (-expected, +actual):\n%s", diff)
	}

	label = "collapsed"
	slotMode = "collapsed"
	if err := mounted.Update(); err != nil {
		t.Fatalf("first Update returned error: %v", err)
	}

	expected = "<section><h2>collapsed</h2><em>Collapsed</em></section>"
	if diff := cmp.Diff(expected, target.html()); diff != "" {
		t.Errorf("mismatch patched refreshed slot HTML (-expected, +actual):\n%s", diff)
	}

	label = "empty"
	slotMode = ""
	if err := mounted.Update(); err != nil {
		t.Fatalf("second Update returned error: %v", err)
	}

	expected = "<section><h2>empty</h2>fallback</section>"
	if diff := cmp.Diff(expected, target.html()); diff != "" {
		t.Errorf("mismatch patched cleared slot HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(1, initCount); diff != "" {
		t.Errorf("mismatch child init count (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
}

func TestMountedComponentVNodeBatchesUpdaterInvalidations(t *testing.T) {
	label := "first"
	renderCount := 0
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return ComponentWithUpdate("Child", func() *CompInstance {
			child := &componentUpdaterInvalidationFixture{
				label:       func() string { return label },
				version:     StateOf(0),
				renderCount: &renderCount,
			}
			return CompOf(child, func(child *componentUpdaterInvalidationFixture) VNode {
				*child.renderCount++
				_ = child.version.Get()
				return Text(child.label())
			})
		}, func(childComp *CompInstance) {
			child := childComp.Component.(*componentUpdaterInvalidationFixture)
			child.label = func() string { return label }
			child.version.Set(child.version.Get() + 1)
		})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	if diff := cmp.Diff("first", target.html()); diff != "" {
		t.Errorf("mismatch mounted component HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(1, renderCount); diff != "" {
		t.Errorf("mismatch mounted child render count (-expected, +actual):\n%s", diff)
	}

	label = "second"
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("second", target.html()); diff != "" {
		t.Errorf("mismatch patched component HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(2, renderCount); diff != "" {
		t.Errorf("mismatch patched child render count (-expected, +actual):\n%s", diff)
	}
}

func TestMountedComponentVNodeRendersSlotFallback(t *testing.T) {
	target := newStubDOMTarget()
	_, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		return Component("Child", func() *CompInstance {
			return CompOf(&patchFixture{}, func(*patchFixture) VNode {
				return Element("section", nil, []VNode{Slot(Text("fallback"))})
			})
		})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	expected := "<section>fallback</section>"
	if diff := cmp.Diff(expected, target.html()); diff != "" {
		t.Errorf("mismatch default slot fallback HTML (-expected, +actual):\n%s", diff)
	}
}

func TestMountedComponentVNodeUnmountsWhenReplaced(t *testing.T) {
	showChild := true
	events := []string{}
	target := newStubDOMTarget()
	mounted, err := mountComponent(CompOf(&patchFixture{}, func(*patchFixture) VNode {
		if !showChild {
			return Text("gone")
		}
		return Component("Child", func() *CompInstance {
			child := &componentVNodeFixture{
				events: &events,
				value:  func() string { return "shown" },
			}
			return CompOf(child, func(child *componentVNodeFixture) VNode {
				return Text(child.value())
			})
		})
	}), target)
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}

	showChild = false
	if err := mounted.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if diff := cmp.Diff("gone", target.html()); diff != "" {
		t.Errorf("mismatch replaced component HTML (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"init:shown", "mounted", "cleanup", "unmounted"}, events); diff != "" {
		t.Errorf("mismatch replaced child lifecycle (-expected, +actual):\n%s", diff)
	}
}

func TestMountedUpdateRejectsUnmountedComponent(t *testing.T) {
	mounted, err := mountComponent(CompOf(&initFixture{}, func(*initFixture) VNode {
		return Text("value")
	}), newStubDOMTarget())
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}
	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}

	err = mounted.Update()

	if err == nil || err.Error() != "mounted component is unmounted" {
		t.Errorf("mismatch update error (-expected, +actual):\n%s", cmp.Diff("mounted component is unmounted", errorString(err)))
	}
}

func TestMountValidatesInputBeforePlatformBoundary(t *testing.T) {
	component := CompOf(&initFixture{}, func(*initFixture) VNode {
		return Text("value")
	})
	tests := []struct {
		name      string
		target    string
		component *CompInstance
		expected  string
	}{
		{
			name:      "missing target",
			component: component,
			expected:  "mount target is required",
		},
		{
			name:     "missing component",
			target:   "#app",
			expected: "component is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Mount(test.target, test.component)
			if err == nil || err.Error() != test.expected {
				t.Errorf("mismatch Mount error (-expected, +actual):\n%s", cmp.Diff(test.expected, errorString(err)))
			}
		})
	}
}

type initFixture struct {
	value string
}

type generatedInitFixture struct {
	value           string
	initializedWith string
}

func (f *generatedInitFixture) Init(Context) {
	f.initializedWith = f.value
}

func (f *initFixture) Init(Context) {
	f.value = "initialized"
}

type lifecycleFixture struct {
	events *[]string
	value  string
}

func (f *lifecycleFixture) Init(ctx Context) {
	*f.events = append(*f.events, "init")
	ctx.OnCleanup(func() {
		*f.events = append(*f.events, "cleanup")
	})
}

func (f *lifecycleFixture) OnMounted() {
	*f.events = append(*f.events, "mounted")
}

func (f *lifecycleFixture) OnUpdated() {
	*f.events = append(*f.events, "updated")
}

func (f *lifecycleFixture) OnUnmounted() {
	*f.events = append(*f.events, "unmounted")
}

type reactiveRenderFixture struct {
	updateCount int
}

func (f *reactiveRenderFixture) OnUpdated() {
	f.updateCount++
}

type reactiveUpdatedHookFixture struct {
	events    *[]string
	hookValue *StateValue[string]
}

func (f *reactiveUpdatedHookFixture) OnUpdated() {
	*f.events = append(*f.events, "updated:"+f.hookValue.Get())
}

type componentVNodeFixture struct {
	events *[]string
	value  func() string
}

func (f *componentVNodeFixture) Init(ctx Context) {
	*f.events = append(*f.events, "init:"+f.value())
	ctx.OnCleanup(func() {
		*f.events = append(*f.events, "cleanup")
	})
}

func (f *componentVNodeFixture) OnMounted() {
	*f.events = append(*f.events, "mounted")
}

func (f *componentVNodeFixture) OnUpdated() {
	*f.events = append(*f.events, "updated")
}

func (f *componentVNodeFixture) OnUnmounted() {
	*f.events = append(*f.events, "unmounted")
}

type componentSlotPatchFixture struct {
	label     func() string
	initCount *int
}

type componentUpdaterInvalidationFixture struct {
	label       func() string
	version     State[int]
	renderCount *int
}

func (f *componentSlotPatchFixture) Init(Context) {
	if f.initCount != nil {
		*f.initCount = *f.initCount + 1
	}
}

func errorString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return fmt.Sprint(err)
}
