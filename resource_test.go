package tue

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

var _ Resource[string] = (*ResourceValue[string])(nil)

func TestResourceLoadsValueAndTracksLoading(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	resource := ResourceOfFunc(nil, func() (string, error) {
		close(started)
		<-release
		return "ready", nil
	})
	if err := waitClosed(started, "resource load start"); err != nil {
		t.Fatalf("wait resource load start: %v", err)
	}

	expected := resourceSnapshot[string]{Loading: true}
	if diff := cmp.Diff(expected, snapshotResource(resource)); diff != "" {
		t.Errorf("mismatch initial resource snapshot (-expected, +actual):\n%s", diff)
	}

	close(release)

	actual, err := waitForResourceSnapshot(resource, func(snapshot resourceSnapshot[string]) bool {
		return snapshot.HasValue && snapshot.Value == "ready"
	})
	if err != nil {
		t.Fatalf("wait loaded resource snapshot: %v", err)
	}
	expected = resourceSnapshot[string]{Value: "ready", HasValue: true}
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch loaded resource snapshot (-expected, +actual):\n%s", diff)
	}
}

func TestResourceStoresErrorWithoutValue(t *testing.T) {
	expectedErr := errors.New("load failed")
	resource := ResourceOfFunc(nil, func() (string, error) {
		return "", expectedErr
	})

	actual, err := waitForResourceSnapshot(resource, func(snapshot resourceSnapshot[string]) bool {
		return !snapshot.Loading && snapshot.Error != ""
	})
	if err != nil {
		t.Fatalf("wait failed resource snapshot: %v", err)
	}
	expected := resourceSnapshot[string]{Error: expectedErr.Error()}
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch failed resource snapshot (-expected, +actual):\n%s", diff)
	}
	if !errors.Is(resource.Error(), expectedErr) {
		t.Errorf("resource error actual = %v, expected %v", resource.Error(), expectedErr)
	}
}

func TestResourceReloadClearsValueAndLoadsLatestResult(t *testing.T) {
	results := make(chan string, 2)
	results <- "first"
	resource := ResourceOfFunc(nil, func() (string, error) {
		return <-results, nil
	})

	actual, err := waitForResourceSnapshot(resource, func(snapshot resourceSnapshot[string]) bool {
		return snapshot.HasValue && snapshot.Value == "first"
	})
	if err != nil {
		t.Fatalf("wait first resource snapshot: %v", err)
	}
	expected := resourceSnapshot[string]{Value: "first", HasValue: true}
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch first resource snapshot (-expected, +actual):\n%s", diff)
	}

	resource.Reload()

	expected = resourceSnapshot[string]{Loading: true}
	if diff := cmp.Diff(expected, snapshotResource(resource)); diff != "" {
		t.Errorf("mismatch reloading resource snapshot (-expected, +actual):\n%s", diff)
	}

	results <- "second"
	actual, err = waitForResourceSnapshot(resource, func(snapshot resourceSnapshot[string]) bool {
		return snapshot.HasValue && snapshot.Value == "second"
	})
	if err != nil {
		t.Fatalf("wait reloaded resource snapshot: %v", err)
	}
	expected = resourceSnapshot[string]{Value: "second", HasValue: true}
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch reloaded resource snapshot (-expected, +actual):\n%s", diff)
	}
}

func TestResourceReloadCancelsPreviousContextLoad(t *testing.T) {
	firstStarted := make(chan struct{})
	firstCanceled := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	var loads int32
	resource := ResourceOfContextFunc(nil, func(ctx context.Context) (string, error) {
		switch atomic.AddInt32(&loads, 1) {
		case 1:
			close(firstStarted)
			<-ctx.Done()
			close(firstCanceled)
			return "", ctx.Err()
		case 2:
			close(secondStarted)
			<-releaseSecond
			return "second", nil
		default:
			return "", fmt.Errorf("unexpected resource load")
		}
	})
	if err := waitClosed(firstStarted, "first resource load start"); err != nil {
		t.Fatalf("wait first resource load start: %v", err)
	}

	resource.Reload()

	if err := waitClosed(secondStarted, "second resource load start"); err != nil {
		t.Fatalf("wait second resource load start: %v", err)
	}
	if err := waitClosed(firstCanceled, "first resource load cancellation"); err != nil {
		t.Fatalf("wait first resource load cancellation: %v", err)
	}

	close(releaseSecond)
	actual, err := waitForResourceSnapshot(resource, func(snapshot resourceSnapshot[string]) bool {
		return snapshot.HasValue && snapshot.Value == "second"
	})
	if err != nil {
		t.Fatalf("wait second resource snapshot: %v", err)
	}
	expected := resourceSnapshot[string]{Value: "second", HasValue: true}
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch second resource snapshot (-expected, +actual):\n%s", diff)
	}
}

func TestResourceReloadIgnoresCanceledLoadThatFinishesLate(t *testing.T) {
	resource := &ResourceValue[string]{
		load: func(context.Context) (string, error) {
			return "", nil
		},
	}

	_, _, staleRunID, ok := resource.beginLoad()
	if !ok {
		t.Fatal("begin first resource load returned ok=false")
	}
	_, _, currentRunID, ok := resource.beginLoad()
	if !ok {
		t.Fatal("begin second resource load returned ok=false")
	}

	// This models a context-aware loader that was canceled on reload but still
	// kept running long enough to return after a newer load started.
	resource.finishLoad(staleRunID, "stale", nil)

	expected := resourceSnapshot[string]{Loading: true}
	if diff := cmp.Diff(expected, snapshotResource(resource)); diff != "" {
		t.Errorf("mismatch stale resource snapshot (-expected, +actual):\n%s", diff)
	}

	resource.finishLoad(currentRunID, "current", nil)

	expected = resourceSnapshot[string]{Value: "current", HasValue: true}
	if diff := cmp.Diff(expected, snapshotResource(resource)); diff != "" {
		t.Errorf("mismatch current resource snapshot (-expected, +actual):\n%s", diff)
	}
}

func TestResourceReloadNotifiesWatchersWhenLoadingStarts(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	finished := make(chan struct{}, 2)
	resource := ResourceOfFunc(nil, func() (string, error) {
		started <- struct{}{}
		<-release
		finished <- struct{}{}
		return "unused", nil
	})
	if err := waitClosed(started, "initial resource load start"); err != nil {
		t.Fatalf("wait initial resource load start: %v", err)
	}

	events := make(chan resourceSnapshot[string], 4)
	stop := Watch(func() {
		events <- snapshotResource(resource)
	})

	expected := resourceSnapshot[string]{Loading: true}
	actual, err := receiveResourceSnapshot(events)
	if err != nil {
		t.Fatalf("receive initial watched resource snapshot: %v", err)
	}
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch initial watched resource snapshot (-expected, +actual):\n%s", diff)
	}

	resource.Reload()

	expected = resourceSnapshot[string]{Loading: true}
	actual, err = receiveResourceSnapshot(events)
	if err != nil {
		t.Fatalf("receive reloading watched resource snapshot: %v", err)
	}
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch reloading watched resource snapshot (-expected, +actual):\n%s", diff)
	}
	stop()

	close(release)
	if err := waitClosed(finished, "initial resource load finish"); err != nil {
		t.Fatalf("wait initial resource load finish: %v", err)
	}
	if err := waitClosed(finished, "reloaded resource load finish"); err != nil {
		t.Fatalf("wait reloaded resource load finish: %v", err)
	}
}

func TestResourceUnmountIgnoresLateInFlightLoad(t *testing.T) {
	fixture := &resourceUnmountFixture{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	mounted, err := mountComponent(CompOf(fixture, func(*resourceUnmountFixture) VNode {
		return Text("resource")
	}), newStubDOMTarget())
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}
	if err := waitClosed(fixture.started, "resource load start"); err != nil {
		t.Fatalf("wait resource load start: %v", err)
	}

	expected := resourceSnapshot[string]{Loading: true}
	if diff := cmp.Diff(expected, snapshotResource(fixture.resource)); diff != "" {
		t.Errorf("mismatch mounted resource snapshot (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
	expected = resourceSnapshot[string]{}
	if diff := cmp.Diff(expected, snapshotResource(fixture.resource)); diff != "" {
		t.Errorf("mismatch stopped resource snapshot (-expected, +actual):\n%s", diff)
	}

	close(fixture.release)
	if err := waitClosed(fixture.finished, "resource load finish"); err != nil {
		t.Fatalf("wait resource load finish: %v", err)
	}

	if diff := cmp.Diff(expected, snapshotResource(fixture.resource)); diff != "" {
		t.Errorf("mismatch late resource snapshot (-expected, +actual):\n%s", diff)
	}
}

func TestResourceUnmountCancelsInFlightContextLoad(t *testing.T) {
	fixture := &resourceContextUnmountFixture{
		started:  make(chan struct{}),
		canceled: make(chan error, 1),
	}
	mounted, err := mountComponent(CompOf(fixture, func(*resourceContextUnmountFixture) VNode {
		return Text("resource")
	}), newStubDOMTarget())
	if err != nil {
		t.Fatalf("mountComponent returned error: %v", err)
	}
	if err := waitClosed(fixture.started, "resource load start"); err != nil {
		t.Fatalf("wait resource load start: %v", err)
	}

	expected := resourceSnapshot[string]{Loading: true}
	if diff := cmp.Diff(expected, snapshotResource(fixture.resource)); diff != "" {
		t.Errorf("mismatch mounted resource snapshot (-expected, +actual):\n%s", diff)
	}

	if err := mounted.Unmount(); err != nil {
		t.Fatalf("Unmount returned error: %v", err)
	}
	cancelErr, err := receiveError(fixture.canceled, "resource load cancellation")
	if err != nil {
		t.Fatalf("wait resource load cancellation: %v", err)
	}
	if !errors.Is(cancelErr, context.Canceled) {
		t.Errorf("resource load cancellation actual = %v, expected %v", cancelErr, context.Canceled)
	}
	expected = resourceSnapshot[string]{}
	if diff := cmp.Diff(expected, snapshotResource(fixture.resource)); diff != "" {
		t.Errorf("mismatch canceled resource snapshot (-expected, +actual):\n%s", diff)
	}
}

type resourceSnapshot[T any] struct {
	Value    T
	HasValue bool
	Loading  bool
	Error    string
}

func snapshotResource[T any](resource Resource[T]) resourceSnapshot[T] {
	value, ok := resource.Value()
	err := resource.Error()
	snapshot := resourceSnapshot[T]{
		Value:    value,
		HasValue: ok,
		Loading:  resource.Loading(),
	}
	if err != nil {
		snapshot.Error = err.Error()
	}
	return snapshot
}

func receiveResourceSnapshot[T any](snapshots <-chan resourceSnapshot[T]) (resourceSnapshot[T], error) {
	select {
	case snapshot := <-snapshots:
		return snapshot, nil
	case <-time.After(time.Second):
		return resourceSnapshot[T]{}, errors.New("timed out waiting for resource snapshot")
	}
}

func waitForResourceSnapshot[T any](resource Resource[T], match func(resourceSnapshot[T]) bool) (resourceSnapshot[T], error) {
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		snapshot := snapshotResource(resource)
		if match(snapshot) {
			return snapshot, nil
		}

		select {
		case <-ticker.C:
		case <-deadline:
			return resourceSnapshot[T]{}, fmt.Errorf("timed out waiting for matching resource snapshot; last snapshot = %#v", snapshot)
		}
	}
}

func waitClosed(done <-chan struct{}, subject string) error {
	select {
	case <-done:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("timed out waiting for %s", subject)
	}
}

func receiveError(done <-chan error, subject string) (error, error) {
	select {
	case err := <-done:
		return err, nil
	case <-time.After(time.Second):
		return nil, fmt.Errorf("timed out waiting for %s", subject)
	}
}

type resourceUnmountFixture struct {
	resource Resource[string]
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (f *resourceUnmountFixture) Init(ctx Context) {
	f.resource = ResourceOfFunc(ctx, func() (string, error) {
		close(f.started)
		<-f.release
		close(f.finished)
		return "late", nil
	})
}

type resourceContextUnmountFixture struct {
	resource Resource[string]
	started  chan struct{}
	canceled chan error
}

func (f *resourceContextUnmountFixture) Init(ctx Context) {
	f.resource = ResourceOfContextFunc(ctx, func(loadCtx context.Context) (string, error) {
		close(f.started)
		<-loadCtx.Done()
		f.canceled <- loadCtx.Err()
		return "late", loadCtx.Err()
	})
}
