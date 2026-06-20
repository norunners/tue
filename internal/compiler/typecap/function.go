package typecap

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

// FunctionSignature describes the parameter and result types of a Go function.
type FunctionSignature struct {
	Parameters []string
	Results    []string
}

// String returns the canonical Go spelling of the function signature.
func (s *FunctionSignature) String() string {
	if s == nil {
		return ""
	}
	signature := "func(" + strings.Join(s.Parameters, ", ") + ")"
	switch len(s.Results) {
	case 0:
		return signature
	case 1:
		return signature + " " + s.Results[0]
	default:
		return signature + " (" + strings.Join(s.Results, ", ") + ")"
	}
}

// ParseFunction parses typ as a Go function type. When ok is false, the
// returned signature is nil.
func ParseFunction(typ string) (*FunctionSignature, bool) {
	expression, err := parser.ParseExpr(strings.TrimSpace(typ))
	if err != nil {
		return nil, false
	}
	function, ok := expression.(*ast.FuncType)
	if !ok {
		return nil, false
	}

	parameters, ok := functionFieldTypes(function.Params)
	if !ok {
		return nil, false
	}
	results, ok := functionFieldTypes(function.Results)
	if !ok {
		return nil, false
	}
	return &FunctionSignature{Parameters: parameters, Results: results}, true
}

// Matches reports whether parameters and results exactly match the parsed
// function signature. Pointer distinctions are intentionally preserved.
func (s *FunctionSignature) Matches(parameters []string, results []string) bool {
	if s == nil || len(s.Parameters) != len(parameters) || len(s.Results) != len(results) {
		return false
	}
	for index, expected := range s.Parameters {
		if expected != strings.TrimSpace(parameters[index]) {
			return false
		}
	}
	for index, expected := range s.Results {
		if expected != strings.TrimSpace(results[index]) {
			return false
		}
	}
	return true
}

func functionFieldTypes(fields *ast.FieldList) ([]string, bool) {
	if fields == nil {
		return nil, true
	}

	var types []string
	for _, field := range fields.List {
		fieldType, ok := functionFieldType(field.Type)
		if !ok {
			return nil, false
		}
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			types = append(types, fieldType)
		}
	}
	return types, true
}

func functionFieldType(expression ast.Expr) (string, bool) {
	var source bytes.Buffer
	if err := printer.Fprint(&source, token.NewFileSet(), expression); err != nil {
		return "", false
	}
	return source.String(), true
}
