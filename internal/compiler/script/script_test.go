package script

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norunners/vue/internal/compiler/sfc"
)

func TestParseContract(t *testing.T) {
	src := []byte(`package pages

import tue "github.com/norunners/vue"

type Dashboard struct {
	userID        tue.Prop[string] ` + "`prop:\"userID,required\"`" + `
	optionalLimit tue.Prop[int]
	todos         tue.Resource[[]Todo]
	user          tue.Ref[User]
	filteredTodos tue.Computed[[]Todo]
	selectUser    func(User)
	saveUser      func(User) error
	title         string
}

func (d *Dashboard) Init(ctx tue.Context) {}
func (d *Dashboard) Reset() {}
func (d *Dashboard) Select(user User) error { return nil }
`)

	contract, err := Parse("dashboard.tue", src, sfc.Position{Offset: 100, Line: 5, Column: 1})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got, want := contract.PackageName, "pages"; got != want {
		t.Fatalf("PackageName = %q, want %q", got, want)
	}
	if got, want := contract.ComponentName, "Dashboard"; got != want {
		t.Fatalf("ComponentName = %q, want %q", got, want)
	}
	if len(contract.Imports) != 1 || contract.Imports[0].Name != "tue" || contract.Imports[0].Path != "github.com/norunners/vue" {
		t.Fatalf("Imports = %#v, want tue import", contract.Imports)
	}

	if len(contract.Props) != 2 {
		t.Fatalf("len(Props) = %d, want 2", len(contract.Props))
	}
	if got, want := contract.Props[0], (Prop{FieldName: "userID", Name: "userID", Type: "string", Required: true, Span: contract.Props[0].Span}); got != want {
		t.Fatalf("first prop = %#v, want %#v", got, want)
	}
	if got, want := contract.Props[1].Name, "optionalLimit"; got != want {
		t.Fatalf("second prop name = %q, want %q", got, want)
	}
	if contract.Props[1].Required {
		t.Fatal("optional prop Required = true, want false")
	}

	requireField(t, contract.Refs, "user", "User")
	requireField(t, contract.Computed, "filteredTodos", "[]Todo")
	requireField(t, contract.Resources, "todos", "[]Todo")
	requireField(t, contract.State, "title", "")

	if len(contract.Callbacks) != 2 {
		t.Fatalf("len(Callbacks) = %d, want 2", len(contract.Callbacks))
	}
	if got, want := contract.Callbacks[0].FieldName, "selectUser"; got != want {
		t.Fatalf("first callback field = %q, want %q", got, want)
	}
	if got, want := strings.Join(contract.Callbacks[0].Params, ","), "User"; got != want {
		t.Fatalf("first callback params = %q, want %q", got, want)
	}
	if got, want := strings.Join(contract.Callbacks[1].Results, ","), "error"; got != want {
		t.Fatalf("second callback results = %q, want %q", got, want)
	}

	if contract.Init == nil {
		t.Fatal("Init = nil, want method")
	}
	if got, want := contract.Init.ReceiverName, "d"; got != want {
		t.Fatalf("Init receiver = %q, want %q", got, want)
	}
	if len(contract.Methods) != 3 {
		t.Fatalf("len(Methods) = %d, want 3", len(contract.Methods))
	}
	if got, want := strings.Join(contract.Methods[2].Params, ","), "User"; got != want {
		t.Fatalf("Select method params = %q, want %q", got, want)
	}
	if got, want := strings.Join(contract.Methods[2].Results, ","), "error"; got != want {
		t.Fatalf("Select method results = %q, want %q", got, want)
	}
	if !contract.Allocation.HasInit {
		t.Fatal("Allocation.HasInit = false, want true")
	}
	if len(contract.Allocation.PropFields) != 2 {
		t.Fatalf("len(Allocation.PropFields) = %d, want 2", len(contract.Allocation.PropFields))
	}

	field := requireField(t, contract.Fields, "userID", "string")
	if field.Exported {
		t.Fatal("userID Exported = true, want false")
	}
	if field.Kind != FieldProp {
		t.Fatalf("userID Kind = %q, want %q", field.Kind, FieldProp)
	}
}

func TestParseBlockUsesSFCSpans(t *testing.T) {
	src := []byte(`<template></template>

<script lang="go">
package fixtures

import tue "github.com/norunners/vue"

type UserBadge struct {
	name tue.Prop[string] ` + "`prop:\"name,required\"`" + `
}
</script>`)

	file, err := sfc.Parse("src/UserBadge.tue", src)
	if err != nil {
		t.Fatalf("sfc.Parse returned error: %v", err)
	}
	contract, err := ParseBlock("src/UserBadge.tue", file.Script)
	if err != nil {
		t.Fatalf("ParseBlock returned error: %v", err)
	}

	if len(contract.Props) != 1 {
		t.Fatalf("len(Props) = %d, want 1", len(contract.Props))
	}
	prop := contract.Props[0]
	if got, want := prop.Span.Start.Offset, strings.Index(string(src), "name tue.Prop"); got != want {
		t.Fatalf("prop start offset = %d, want %d", got, want)
	}
	if got := prop.Span.Start; got.Line != 9 || got.Column != 2 {
		t.Fatalf("prop start = %#v, want line 9 column 2", got)
	}
}

func TestParseFixtureScripts(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "fixtures")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".tue" {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		file, err := sfc.Parse(filepath.ToSlash(path), src)
		if err != nil {
			t.Fatalf("sfc.Parse(%s) returned error: %v", path, err)
		}
		if _, err := ParseBlock(filepath.ToSlash(path), file.Script); err != nil {
			t.Fatalf("ParseBlock(%s) returned error: %v", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixtures: %v", err)
	}
}

func TestParseDiagnostics(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		src       string
		diagnoses []string
	}{
		{
			name: "missing component contract",
			path: "Missing.tue",
			src: `package fixtures
type Other struct{}`,
			diagnoses: []string{"script must declare component type Missing"},
		},
		{
			name: "component must be struct",
			path: "Broken.tue",
			src: `package fixtures
type Broken int`,
			diagnoses: []string{"component Broken must be a struct type"},
		},
		{
			name: "invalid init value receiver",
			path: "Broken.tue",
			src: `package fixtures
import tue "github.com/norunners/vue"
type Broken struct{}
func (b Broken) Init(ctx tue.Context) {}`,
			diagnoses: []string{"Init must have signature func (c *Broken) Init(ctx tue.Context)"},
		},
		{
			name: "invalid init params",
			path: "Broken.tue",
			src: `package fixtures
type Broken struct{}
func (b *Broken) Init() {}`,
			diagnoses: []string{"Init must have signature func (c *Broken) Init(ctx tue.Context)"},
		},
		{
			name: "invalid init result",
			path: "Broken.tue",
			src: `package fixtures
import tue "github.com/norunners/vue"
type Broken struct{}
func (b *Broken) Init(ctx tue.Context) error { return nil }`,
			diagnoses: []string{"Init must have signature func (c *Broken) Init(ctx tue.Context)"},
		},
		{
			name: "prop missing type arg",
			path: "Broken.tue",
			src: `package fixtures
import tue "github.com/norunners/vue"
type Broken struct {
	name tue.Prop
}`,
			diagnoses: []string{"tue.Prop must use exactly one type argument"},
		},
		{
			name: "prop too many type args",
			path: "Broken.tue",
			src: `package fixtures
import tue "github.com/norunners/vue"
type Broken struct {
	name tue.Prop[string, int]
}`,
			diagnoses: []string{"tue.Prop must use exactly one type argument"},
		},
		{
			name: "anonymous prop field",
			path: "Broken.tue",
			src: `package fixtures
import tue "github.com/norunners/vue"
type Broken struct {
	tue.Prop[string]
}`,
			diagnoses: []string{"prop field must be named"},
		},
		{
			name: "invalid prop tag option",
			path: "Broken.tue",
			src: `package fixtures
import tue "github.com/norunners/vue"
type Broken struct {
	name tue.Prop[string] ` + "`prop:\"name,mutable\"`" + `
}`,
			diagnoses: []string{`unsupported prop tag option "mutable"`},
		},
		{
			name: "ignored prop tag",
			path: "Broken.tue",
			src: `package fixtures
import tue "github.com/norunners/vue"
type Broken struct {
	name tue.Prop[string] ` + "`prop:\"-\"`" + `
}`,
			diagnoses: []string{`prop field name cannot use prop:"-"`},
		},
		{
			name: "duplicate prop name",
			path: "Broken.tue",
			src: `package fixtures
import tue "github.com/norunners/vue"
type Broken struct {
	first  tue.Prop[string] ` + "`prop:\"name\"`" + `
	second tue.Prop[string] ` + "`prop:\"name\"`" + `
}`,
			diagnoses: []string{`duplicate prop name "name"`, `first prop name "name" declared here`},
		},
		{
			name: "go syntax error",
			path: "Broken.tue",
			src:  `package`,
			diagnoses: []string{
				"expected 'IDENT'",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.path, []byte(test.src), sfc.Position{Line: 1, Column: 1})
			if err == nil {
				t.Fatal("Parse returned nil error")
			}
			var parseErr *Error
			if !errors.As(err, &parseErr) {
				t.Fatalf("error type = %T, want *Error", err)
			}
			got := diagnosticsText(parseErr.Diagnostics)
			for _, want := range test.diagnoses {
				if !strings.Contains(got, want) {
					t.Fatalf("diagnostics = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

func TestDiagnosticStringIncludesSourceLocation(t *testing.T) {
	src := []byte(`package fixtures
type Broken struct{}
func (b *Broken) Init() {}`)

	_, err := Parse("src/Broken.tue", src, sfc.Position{Offset: 50, Line: 10, Column: 7})
	if err == nil {
		t.Fatal("Parse returned nil error")
	}
	var parseErr *Error
	if !errors.As(err, &parseErr) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if len(parseErr.Diagnostics) == 0 {
		t.Fatal("no diagnostics")
	}

	got := parseErr.Diagnostics[0].String()
	want := "src/Broken.tue:12:18: Init must have signature func (c *Broken) Init(ctx tue.Context)"
	if got != want {
		t.Fatalf("diagnostic string = %q, want %q", got, want)
	}
}

func requireField(t *testing.T, fields []Field, name, elementType string) Field {
	t.Helper()

	for _, field := range fields {
		if field.Name != name {
			continue
		}
		if field.ElementType != elementType {
			t.Fatalf("field %s ElementType = %q, want %q", name, field.ElementType, elementType)
		}
		return field
	}
	t.Fatalf("field %s not found in %#v", name, fields)
	return Field{}
}

func diagnosticsText(diagnostics []Diagnostic) string {
	var b strings.Builder
	for _, diagnostic := range diagnostics {
		b.WriteString(diagnostic.String())
		b.WriteByte('\n')
	}
	return b.String()
}
