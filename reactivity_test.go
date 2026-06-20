package tue

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRefWatchRunsImmediatelyAndStops(t *testing.T) {
	count := RefOf(0)
	values := []int{}

	stop := count.Watch(func(value int) {
		values = append(values, value)
	})
	if diff := cmp.Diff([]int{0}, values); diff != "" {
		t.Errorf("mismatch ref watch initial values (-expected, +actual):\n%s", diff)
	}

	count.Set(1)
	if diff := cmp.Diff([]int{0, 1}, values); diff != "" {
		t.Errorf("mismatch ref watch values after set (-expected, +actual):\n%s", diff)
	}

	stop()
	count.Set(2)
	if diff := cmp.Diff([]int{0, 1}, values); diff != "" {
		t.Errorf("mismatch ref watch values after stop (-expected, +actual):\n%s", diff)
	}
}

func TestWatchTracksOnlyCurrentDependencies(t *testing.T) {
	enabled := RefOf(true)
	first := RefOf("first")
	second := RefOf("second")
	values := []string{}

	Watch(func() {
		if enabled.Get() {
			values = append(values, first.Get())
			return
		}
		values = append(values, second.Get())
	})

	first.Set("first updated")
	second.Set("second before switch")
	enabled.Set(false)
	first.Set("first after switch")
	second.Set("second after switch")

	if diff := cmp.Diff([]string{
		"first",
		"first updated",
		"second before switch",
		"second after switch",
	}, values); diff != "" {
		t.Errorf("mismatch dynamic watch dependency values (-expected, +actual):\n%s", diff)
	}
}

func TestBatchDeduplicatesNestedWatcherFlushes(t *testing.T) {
	count := RefOf(0)
	values := []int{}
	Watch(func() {
		values = append(values, count.Get())
	})

	Batch(func() {
		count.Set(1)
		count.Set(2)
		Batch(func() {
			count.Set(3)
		})
		if diff := cmp.Diff([]int{0}, values); diff != "" {
			t.Errorf("mismatch batched values before flush (-expected, +actual):\n%s", diff)
		}
	})

	if diff := cmp.Diff([]int{0, 3}, values); diff != "" {
		t.Errorf("mismatch batched values after flush (-expected, +actual):\n%s", diff)
	}
}

func TestComputedIsLazyCachedAndInvalidated(t *testing.T) {
	count := RefOf(1)
	computeCount := 0
	double := ComputedOfFunc(func() int {
		computeCount++
		return count.Get() * 2
	})

	if diff := cmp.Diff(0, computeCount); diff != "" {
		t.Errorf("mismatch compute count before read (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(2, double.Get()); diff != "" {
		t.Errorf("mismatch first computed value (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(1, computeCount); diff != "" {
		t.Errorf("mismatch compute count after first read (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(2, double.Get()); diff != "" {
		t.Errorf("mismatch cached computed value (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(1, computeCount); diff != "" {
		t.Errorf("mismatch compute count after cached read (-expected, +actual):\n%s", diff)
	}

	count.Set(2)
	if diff := cmp.Diff(1, computeCount); diff != "" {
		t.Errorf("mismatch compute count after invalidation before read (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(4, double.Get()); diff != "" {
		t.Errorf("mismatch recomputed value (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(2, computeCount); diff != "" {
		t.Errorf("mismatch compute count after recompute (-expected, +actual):\n%s", diff)
	}
}

func TestComputedInvalidatesWatchers(t *testing.T) {
	count := RefOf(1)
	computeCount := 0
	double := ComputedOfFunc(func() int {
		computeCount++
		return count.Get() * 2
	})
	values := []int{}

	Watch(func() {
		values = append(values, double.Get())
	})
	count.Set(2)

	if diff := cmp.Diff([]int{2, 4}, values); diff != "" {
		t.Errorf("mismatch computed watcher values (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(2, computeCount); diff != "" {
		t.Errorf("mismatch compute count for watcher (-expected, +actual):\n%s", diff)
	}
}

func TestComponentScopedEffectsStopBeforeUserCleanup(t *testing.T) {
	events := []string{}
	component := &reactiveCleanupFixture{events: &events}
	comp := CompOf(component, func(*reactiveCleanupFixture) VNode {
		return Text("cleanup")
	})

	mounted, err := mountComponent(comp, newStubDOMTarget())
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}
	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}

	if diff := cmp.Diff([]string{"watch:0", "cleanup", "unmounted"}, events); diff != "" {
		t.Errorf("mismatch scoped cleanup events (-expected, +actual):\n%s", diff)
	}
}

type reactiveCleanupFixture struct {
	count  *RefValue[int]
	events *[]string
}

func (f *reactiveCleanupFixture) Init(ctx Context) {
	f.count = RefOf(0)
	ctx.OnCleanup(func() {
		*f.events = append(*f.events, "cleanup")
		f.count.Set(1)
	})
	Watch(func() {
		*f.events = append(*f.events, fmt.Sprintf("watch:%d", f.count.Get()))
	})
}

func (f *reactiveCleanupFixture) OnUnmounted() {
	*f.events = append(*f.events, "unmounted")
}
