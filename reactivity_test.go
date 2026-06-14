package vue

import (
	"reflect"
	"testing"
)

func TestPropsRefsComputedAndWatch(t *testing.T) {
	static := PropOf("Ada")
	var _ Prop[string] = static
	if got, want := static.Get(), "Ada"; got != want {
		t.Fatalf("PropOf.Get() = %q, want %q", got, want)
	}

	source := "Grace"
	bound := PropOfFunc(func() string { return source })
	var _ Prop[string] = bound
	if got, want := bound.Get(), "Grace"; got != want {
		t.Fatalf("PropOfFunc.Get() = %q, want %q", got, want)
	}
	source = "Katherine"
	if got, want := bound.Get(), "Katherine"; got != want {
		t.Fatalf("PropOfFunc.Get() after source change = %q, want %q", got, want)
	}

	count := RefOf(1)
	var _ Ref[int] = count
	count.Set(count.Get() + 1)
	if got, want := count.Get(), 2; got != want {
		t.Fatalf("Ref.Get() = %d, want %d", got, want)
	}
}

func TestComputedCachesUntilDependencyInvalidates(t *testing.T) {
	count := RefOf(2)
	computedCalls := 0
	double := ComputedOfFunc(func() int {
		computedCalls++
		return count.Get() * 2
	})
	var _ Computed[int] = double

	if got, want := double.Get(), 4; got != want {
		t.Fatalf("Computed.Get() = %d, want %d", got, want)
	}
	if got, want := double.Get(), 4; got != want {
		t.Fatalf("cached Computed.Get() = %d, want %d", got, want)
	}
	if computedCalls != 1 {
		t.Fatalf("computed calls before invalidation = %d, want 1", computedCalls)
	}

	count.Set(3)
	if computedCalls != 1 {
		t.Fatalf("computed calls after invalidation before read = %d, want 1", computedCalls)
	}
	if got, want := double.Get(), 6; got != want {
		t.Fatalf("Computed.Get() after source change = %d, want %d", got, want)
	}
	if computedCalls != 2 {
		t.Fatalf("computed calls after recompute = %d, want 2", computedCalls)
	}
}

func TestWatchTracksDependenciesAndStops(t *testing.T) {
	count := RefOf(1)
	var seen []int

	stop := Watch(func() {
		seen = append(seen, count.Get())
	})
	count.Set(2)
	stop()
	count.Set(3)

	if want := []int{1, 2}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("watch values = %v, want %v", seen, want)
	}
}

func TestBatchCoalescesWatchInvalidations(t *testing.T) {
	count := RefOf(0)
	var seen []int

	stop := Watch(func() {
		seen = append(seen, count.Get())
	})
	defer stop()

	Batch(func() {
		count.Set(1)
		count.Set(2)
		count.Set(3)
	})
	count.Set(4)

	if want := []int{0, 3, 4}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("batched watch values = %v, want %v", seen, want)
	}
}

func TestComputedWatcherInvalidatesOncePerBatch(t *testing.T) {
	count := RefOf(1)
	computedCalls := 0
	double := ComputedOfFunc(func() int {
		computedCalls++
		return count.Get() * 2
	})
	var seen []int

	stop := Watch(func() {
		seen = append(seen, double.Get())
	})
	defer stop()

	Batch(func() {
		count.Set(2)
		count.Set(3)
	})

	if want := []int{2, 6}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("computed watch values = %v, want %v", seen, want)
	}
	if computedCalls != 2 {
		t.Fatalf("computed calls = %d, want 2", computedCalls)
	}
}

func TestPropWatchTracksReactiveGetter(t *testing.T) {
	source := RefOf("Grace")
	prop := PropOfFunc(func() string {
		return source.Get()
	})
	var seen []string

	stop := prop.Watch(func(value string) {
		seen = append(seen, value)
	})
	source.Set("Katherine")
	stop()
	source.Set("Dorothy")

	if want := []string{"Katherine"}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("prop watch values = %v, want %v", seen, want)
	}
}
