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

func TestMountComponentRunsLifecycleInOrder(t *testing.T) {
	events := []string{}
	component := &lifecycleFixture{events: &events, value: "first"}
	comp := CompOf(component, func(fixture *lifecycleFixture) VNode {
		*fixture.events = append(*fixture.events, "render:"+fixture.value)
		return Text(fixture.value)
	})
	target := newFakeDOMTarget()

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

func TestMountedUpdateRejectsUnmountedComponent(t *testing.T) {
	mounted, err := mountComponent(CompOf(&initFixture{}, func(*initFixture) VNode {
		return Text("value")
	}), newFakeDOMTarget())
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
		component *Comp
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Mount(tt.target, tt.component)
			if err == nil || err.Error() != tt.expected {
				t.Errorf("mismatch Mount error (-expected, +actual):\n%s", cmp.Diff(tt.expected, errorString(err)))
			}
		})
	}
}

type initFixture struct {
	value string
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

func errorString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return fmt.Sprint(err)
}
