package tue

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestOnWrapsOptionalComponentEventCallbacks(t *testing.T) {
	tests := []struct {
		name     string
		on       On[func(string)]
		expected bool
	}{
		{name: "zero value", expected: false},
		{name: "callback", on: OnOf(func(string) {}), expected: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.expected, test.on.Ok()); diff != "" {
				t.Errorf("mismatch callback presence (-expected, +actual):\n%s", diff)
			}
		})
	}
}

func TestOnFuncOkReturnsComponentEventCallbackAndPresence(t *testing.T) {
	var zero On[func(string)]
	zeroFunc, zeroOK := zero.FuncOk()
	if zeroFunc != nil {
		t.Error("zero callback actual = non-nil, expected nil")
	}
	if zeroOK {
		t.Error("zero callback actual = present, expected absent")
	}

	var actual string
	on := OnOf(func(value string) {
		actual = value
	})
	callback, ok := on.FuncOk()
	if !ok {
		t.Fatal("wrapped callback is absent")
	}
	callback("selected")

	expected := "selected"
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch callback value (-expected, +actual):\n%s", diff)
	}
}

func TestOnFuncReturnsComponentEventCallback(t *testing.T) {
	var actual string
	on := OnOf(func(value string) {
		actual = value
	})

	on.Func()("selected")

	expected := "selected"
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch callback value (-expected, +actual):\n%s", diff)
	}
}
