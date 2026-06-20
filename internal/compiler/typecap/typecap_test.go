package typecap

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name     string
		typ      string
		expected string
	}{
		{name: "plain", typ: "string", expected: "string"},
		{name: "whitespace", typ: "  string  ", expected: "string"},
		{name: "pointer", typ: "** User", expected: "User"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.expected, Normalize(test.typ)); diff != "" {
				t.Errorf("mismatch normalized type (-expected, +actual):\n%s", diff)
			}
		})
	}
}

func TestAssignable(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		actual   string
		result   bool
	}{
		{name: "same type", expected: "string", actual: "string", result: true},
		{name: "different type", expected: "string", actual: "bool", result: false},
		{name: "unknown expected", expected: Unknown, actual: "bool", result: true},
		{name: "unknown actual", expected: "string", actual: Unknown, result: true},
		{name: "pointer normalization", expected: "*User", actual: "User", result: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.result, Assignable(test.expected, test.actual)); diff != "" {
				t.Errorf("mismatch assignability (-expected, +actual):\n%s", diff)
			}
		})
	}
}

func TestComparable(t *testing.T) {
	declared := map[string]bool{
		"ComparableStruct":    true,
		"NonComparableStruct": false,
	}
	tests := []struct {
		name     string
		typ      string
		expected bool
	}{
		{name: "unknown", typ: Unknown, expected: true},
		{name: "pointer", typ: "*NonComparableStruct", expected: true},
		{name: "declared comparable", typ: "ComparableStruct", expected: true},
		{name: "declared non-comparable", typ: "NonComparableStruct", expected: false},
		{name: "slice", typ: "[]string", expected: false},
		{name: "map", typ: "map[string]int", expected: false},
		{name: "function", typ: "func()", expected: false},
		{name: "interface", typ: "any", expected: false},
		{name: "scalar", typ: "string", expected: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.expected, Comparable(test.typ, declared)); diff != "" {
				t.Errorf("mismatch comparability (-expected, +actual):\n%s", diff)
			}
		})
	}
}

func TestIterableFor(t *testing.T) {
	tests := []struct {
		name     string
		typ      string
		expected *Iterable
		ok       bool
	}{
		{name: "unknown", typ: Unknown, expected: &Iterable{Item: Unknown, Key: Unknown}, ok: true},
		{name: "slice", typ: "[]Todo", expected: &Iterable{Item: "Todo", Key: "int"}, ok: true},
		{name: "array", typ: "[4]Todo", expected: &Iterable{Item: "Todo", Key: "int"}, ok: true},
		{name: "map", typ: "map[string][]Todo", expected: &Iterable{Item: "[]Todo", Key: "string"}, ok: true},
		{name: "nested map key", typ: "map[[2]string]Todo", expected: &Iterable{Item: "Todo", Key: "[2]string"}, ok: true},
		{name: "string", typ: "string", expected: &Iterable{Item: "rune", Key: "int"}, ok: true},
		{name: "not iterable", typ: "bool", expected: nil, ok: false},
		{name: "malformed array", typ: "[4", expected: nil, ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, ok := IterableFor(test.typ)
			if diff := cmp.Diff(test.ok, ok); diff != "" {
				t.Errorf("mismatch iterable ok (-expected, +actual):\n%s", diff)
			}
			if diff := cmp.Diff(test.expected, actual); diff != "" {
				t.Errorf("mismatch iterable types (-expected, +actual):\n%s", diff)
			}
		})
	}
}

func TestTypeCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		actual   bool
		expected bool
	}{
		{name: "switch compatible", actual: SwitchCompatible("string", "string"), expected: true},
		{name: "switch incompatible", actual: SwitchCompatible("string", "int"), expected: false},
		{name: "trusted qualified HTML", actual: TrustedHTML("tue.TrustedHTML"), expected: true},
		{name: "trusted dot-imported HTML", actual: TrustedHTML("TrustedHTML"), expected: true},
		{name: "untrusted HTML", actual: TrustedHTML("string"), expected: false},
		{name: "numeric", actual: Numeric("int64"), expected: true},
		{name: "non-numeric", actual: Numeric("string"), expected: false},
		{name: "scalar", actual: Scalar("bool"), expected: true},
		{name: "non-scalar", actual: Scalar("[]string"), expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.expected, test.actual); diff != "" {
				t.Errorf("mismatch capability result (-expected, +actual):\n%s", diff)
			}
		})
	}
}
