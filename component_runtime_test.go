package vue

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type lifecycleFixture struct {
	events *[]string
	label  string
}

func (fixture *lifecycleFixture) Init(ctx Context) {
	*fixture.events = append(*fixture.events, "init")
	fixture.label = "ready"
	ctx.OnMounted(func() {
		*fixture.events = append(*fixture.events, "mounted")
	})
	ctx.OnUpdated(func() {
		*fixture.events = append(*fixture.events, "updated")
	})
	ctx.OnUnmounted(func() {
		*fixture.events = append(*fixture.events, "unmounted")
	})
}

func TestCompOfMountUpdateUnmountLifecycle(t *testing.T) {
	var events []string
	fixture := &lifecycleFixture{events: &events}

	comp := CompOf(fixture, func() VNode {
		return Element("p", nil, Text(fixture.label))
	})

	if got, want := strings.Join(events, ","), "init"; got != want {
		t.Fatalf("events after CompOf = %q, want %q", got, want)
	}
	if fixture.label != "ready" {
		t.Fatalf("fixture label = %q, want Init to run before render", fixture.label)
	}

	target := newFakeMountTarget()
	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	if got, want := target.innerHTML(), "<p>ready</p>"; got != want {
		t.Fatalf("mounted HTML = %q, want %q", got, want)
	}
	if got, want := strings.Join(events, ","), "init,mounted"; got != want {
		t.Fatalf("events after mount = %q, want %q", got, want)
	}

	fixture.label = "updated"
	if err := comp.Update(); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if got, want := target.innerHTML(), "<p>updated</p>"; got != want {
		t.Fatalf("updated HTML = %q, want %q", got, want)
	}
	if got, want := strings.Join(events, ","), "init,mounted,updated"; got != want {
		t.Fatalf("events after update = %q, want %q", got, want)
	}

	if err := comp.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
	if got, want := target.innerHTML(), ""; got != want {
		t.Fatalf("unmounted HTML = %q, want %q", got, want)
	}
	if got, want := strings.Join(events, ","), "init,mounted,updated,unmounted"; got != want {
		t.Fatalf("events after unmount = %q, want %q", got, want)
	}
}

func TestComponentLifecycleErrors(t *testing.T) {
	if err := (*Comp)(nil).mount(newFakeMountTarget()); err == nil {
		t.Fatal("nil component mount returned nil error")
	}
	if err := CompOf(struct{}{}, nil).mount(nil); err == nil {
		t.Fatal("nil target mount returned nil error")
	}
	if err := CompOf(struct{}{}, nil).Update(); err == nil {
		t.Fatal("unmounted update returned nil error")
	}

	comp := CompOf(struct{}{}, func() VNode {
		return VNode{Type: VNodeComponent}
	})
	if err := comp.mount(newFakeMountTarget()); err == nil || !strings.Contains(err.Error(), "component VNodes are not supported") {
		t.Fatalf("mount error = %v, want unsupported component VNode error", err)
	}
}

type scopedReactivityFixture struct {
	count Ref[int]
	seen  *[]int
}

func (fixture *scopedReactivityFixture) Init(ctx Context) {
	fixture.count = RefOf(0)
	Watch(func() {
		*fixture.seen = append(*fixture.seen, fixture.count.Get())
	})
	ctx.OnUnmounted(func() {
		fixture.count.Set(99)
	})
}

func TestComponentScopedReactiveCleanup(t *testing.T) {
	var seen []int
	fixture := &scopedReactivityFixture{seen: &seen}
	comp := CompOf(fixture, func() VNode {
		return Text("")
	})

	if want := []int{0}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("watch values after Init = %v, want %v", seen, want)
	}

	target := newFakeMountTarget()
	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	fixture.count.Set(1)
	if want := []int{0, 1}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("watch values after Set = %v, want %v", seen, want)
	}

	if err := comp.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
	fixture.count.Set(2)
	if want := []int{0, 1}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("watch values after Unmount = %v, want %v", seen, want)
	}
}

func TestMountedComponentRerendersWhenRenderDependencyChanges(t *testing.T) {
	count := RefOf(0)
	renderCalls := 0
	comp := CompOf(struct{}{}, func() VNode {
		renderCalls++
		return Element("p", nil, Text(strconv.Itoa(count.Get())))
	})

	count.Set(1)
	if renderCalls != 0 {
		t.Fatalf("render calls before mount = %d, want 0", renderCalls)
	}

	target := newFakeMountTarget()
	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	if got, want := target.innerHTML(), "<p>1</p>"; got != want {
		t.Fatalf("mounted HTML = %q, want %q", got, want)
	}
	if renderCalls != 1 {
		t.Fatalf("render calls after mount = %d, want 1", renderCalls)
	}

	Batch(func() {
		count.Set(2)
		count.Set(3)
		if got, want := target.innerHTML(), "<p>1</p>"; got != want {
			t.Fatalf("HTML inside batch = %q, want %q", got, want)
		}
		if renderCalls != 1 {
			t.Fatalf("render calls inside batch = %d, want 1", renderCalls)
		}
	})
	if got, want := target.innerHTML(), "<p>3</p>"; got != want {
		t.Fatalf("batched HTML = %q, want %q", got, want)
	}
	if renderCalls != 2 {
		t.Fatalf("render calls after batch = %d, want 2", renderCalls)
	}

	count.Set(4)
	if got, want := target.innerHTML(), "<p>4</p>"; got != want {
		t.Fatalf("updated HTML = %q, want %q", got, want)
	}
	if renderCalls != 3 {
		t.Fatalf("render calls after Set = %d, want 3", renderCalls)
	}
}

type reactiveRenderLifecycleFixture struct {
	count      *RefValue[int]
	hookOnly   *RefValue[string]
	target     *fakeMountTarget
	updates    *[]string
	hookValues *[]string
}

func (fixture *reactiveRenderLifecycleFixture) Init(ctx Context) {
	fixture.count = RefOf(0)
	fixture.hookOnly = RefOf("initial")
	ctx.OnUpdated(func() {
		*fixture.updates = append(*fixture.updates, fixture.target.innerHTML())
		*fixture.hookValues = append(*fixture.hookValues, fixture.hookOnly.Get())
	})
}

func TestReactiveRenderRunsUpdatedAfterPatchAndStopsAfterUnmount(t *testing.T) {
	target := newFakeMountTarget()
	var updates []string
	var hookValues []string
	fixture := &reactiveRenderLifecycleFixture{
		target:     target,
		updates:    &updates,
		hookValues: &hookValues,
	}
	comp := CompOf(fixture, func() VNode {
		return Element("p", nil, Text(strconv.Itoa(fixture.count.Get())))
	})

	if err := comp.mount(target); err != nil {
		t.Fatalf("mount returned error: %v", err)
	}
	if len(updates) != 0 {
		t.Fatalf("updates after mount = %v, want none", updates)
	}

	fixture.count.Set(1)
	if want := []string{"<p>1</p>"}; !reflect.DeepEqual(updates, want) {
		t.Fatalf("updates after reactive Set = %v, want %v", updates, want)
	}
	if want := []string{"initial"}; !reflect.DeepEqual(hookValues, want) {
		t.Fatalf("hook values after reactive Set = %v, want %v", hookValues, want)
	}

	fixture.hookOnly.Set("changed")
	if want := []string{"<p>1</p>"}; !reflect.DeepEqual(updates, want) {
		t.Fatalf("updates after hook-only Set = %v, want %v", updates, want)
	}
	if want := []string{"initial"}; !reflect.DeepEqual(hookValues, want) {
		t.Fatalf("hook values after hook-only Set = %v, want %v", hookValues, want)
	}

	if err := comp.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
	fixture.count.Set(2)
	if want := []string{"<p>1</p>"}; !reflect.DeepEqual(updates, want) {
		t.Fatalf("updates after Unmount = %v, want %v", updates, want)
	}
	if got, want := target.innerHTML(), ""; got != want {
		t.Fatalf("HTML after Unmount and Set = %q, want %q", got, want)
	}
}
