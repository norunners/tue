package typecap

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseFunction(t *testing.T) {
	tests := []struct {
		name       string
		typ        string
		expected   *FunctionSignature
		expectedOK bool
	}{
		{name: "no arguments", typ: "func()", expected: &FunctionSignature{}, expectedOK: true},
		{name: "one argument", typ: "func(string)", expected: &FunctionSignature{Parameters: []string{"string"}}, expectedOK: true},
		{name: "named arguments", typ: "func(value string, count int)", expected: &FunctionSignature{Parameters: []string{"string", "int"}}, expectedOK: true},
		{name: "grouped arguments", typ: "func(first, second string)", expected: &FunctionSignature{Parameters: []string{"string", "string"}}, expectedOK: true},
		{name: "variadic argument", typ: "func(...string)", expected: &FunctionSignature{Parameters: []string{"...string"}}, expectedOK: true},
		{name: "results", typ: "func(string) (int, error)", expected: &FunctionSignature{Parameters: []string{"string"}, Results: []string{"int", "error"}}, expectedOK: true},
		{name: "not function", typ: "string", expected: nil, expectedOK: false},
		{name: "malformed", typ: "func(", expected: nil, expectedOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, ok := ParseFunction(test.typ)
			if diff := cmp.Diff(test.expectedOK, ok); diff != "" {
				t.Errorf("mismatch parse ok (-expected, +actual):\n%s", diff)
			}
			if diff := cmp.Diff(test.expected, actual); diff != "" {
				t.Errorf("mismatch function signature (-expected, +actual):\n%s", diff)
			}
		})
	}
}

func TestFunctionSignatureMatches(t *testing.T) {
	signature, ok := ParseFunction("func(string, *User)")
	if !ok {
		t.Fatal("parse function signature failed")
	}

	tests := []struct {
		name       string
		parameters []string
		results    []string
		expected   bool
	}{
		{name: "exact", parameters: []string{"string", "*User"}, expected: true},
		{name: "trimmed", parameters: []string{" string ", " *User "}, expected: true},
		{name: "pointer mismatch", parameters: []string{"string", "User"}, expected: false},
		{name: "parameter count", parameters: []string{"string"}, expected: false},
		{name: "unexpected result", parameters: []string{"string", "*User"}, results: []string{"error"}, expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.expected, signature.Matches(test.parameters, test.results)); diff != "" {
				t.Errorf("mismatch signature match (-expected, +actual):\n%s", diff)
			}
		})
	}
}

func TestFunctionSignatureString(t *testing.T) {
	tests := []struct {
		name      string
		signature *FunctionSignature
		expected  string
	}{
		{name: "nil", signature: nil, expected: ""},
		{name: "no results", signature: &FunctionSignature{Parameters: []string{"string", "int"}}, expected: "func(string, int)"},
		{name: "one result", signature: &FunctionSignature{Results: []string{"error"}}, expected: "func() error"},
		{name: "multiple results", signature: &FunctionSignature{Parameters: []string{"string"}, Results: []string{"int", "error"}}, expected: "func(string) (int, error)"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.expected, test.signature.String()); diff != "" {
				t.Errorf("mismatch function signature string (-expected, +actual):\n%s", diff)
			}
		})
	}
}
