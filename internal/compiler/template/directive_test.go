package template

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseForClause(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		expected   *ForClause
	}{
		{
			name:       "item",
			expression: "todo in todos",
			expected: &ForClause{
				Item:        "todo",
				Source:      "todos",
				SourceStart: 8,
				SourceEnd:   13,
			},
		},
		{
			name:       "item and index",
			expression: "(todo, index) in todos",
			expected: &ForClause{
				Item:        "todo",
				Index:       "index",
				Source:      "todos",
				SourceStart: 17,
				SourceEnd:   22,
			},
		},
		{
			name:       "trimmed source",
			expression: "todo in \t todos \n",
			expected: &ForClause{
				Item:        "todo",
				Source:      "todos",
				SourceStart: 10,
				SourceEnd:   15,
			},
		},
		{
			name:       "wrong separator",
			expression: "todo of todos",
		},
		{
			name:       "invalid item",
			expression: "todo.id in todos",
		},
		{
			name:       "empty source",
			expression: "todo in ",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, ok := ParseForClause(test.expression)
			if diff := cmp.Diff(test.expected != nil, ok); diff != "" {
				t.Errorf("mismatch clause ok (-expected, +actual):\n%s", diff)
			}
			if diff := cmp.Diff(test.expected, actual); diff != "" {
				t.Errorf("mismatch clause (-expected, +actual):\n%s", diff)
			}
		})
	}
}
