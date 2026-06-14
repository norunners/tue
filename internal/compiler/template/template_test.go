package template

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norunners/vue/internal/compiler/sfc"
)

func TestParseRepresentativeTemplate(t *testing.T) {
	src := []byte(`<main class="page" :data-count="count" @click="increment">
	Hello {{ user.Name }}
	<!-- keep -->
	<UserBadge :name="user.Name" @select="selectUser" />
	<p v-if="user.Admin">Admin</p>
	<p v-else>Member</p>
	<li v-for="todo in todos" :key="todo.ID">{{ todo.Text }}</li>
	<input v-model="query" />
	<section v-html="trustedHTML"></section>
</main>`)

	tree, err := Parse("Component.tue", src, sfc.Position{Line: 1, Column: 1})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	got := formatTree(tree)
	want := strings.TrimLeft(`
element main component=false selfClosing=false
  attr static class="page"
  attr bound data-count expr="count"
  attr event click expr="increment"
  text "Hello"
  interpolation expr="user.Name"
  comment " keep "
  element UserBadge component=true selfClosing=true
    attr bound name expr="user.Name"
    attr event select expr="selectUser"
  element p component=false selfClosing=false
    attr directive v-if expr="user.Admin"
    text "Admin"
  element p component=false selfClosing=false
    attr directive v-else
    text "Member"
  element li component=false selfClosing=false
    attr directive v-for expr="todo in todos"
    attr bound key expr="todo.ID"
    interpolation expr="todo.Text"
  element input component=false selfClosing=true
    attr directive v-model expr="query"
  element section component=false selfClosing=false
    attr directive v-html expr="trustedHTML"
`, "\n")
	if got != want {
		t.Fatalf("tree =\n%s\nwant =\n%s", got, want)
	}
}

func TestParseSpansFromSFCBlock(t *testing.T) {
	src := []byte(`<template>
	<p :title=" user.Name ">{{ name }}</p>
</template>

<script lang="go">
package fixtures
</script>`)

	file, err := sfc.Parse("src/App.tue", src)
	if err != nil {
		t.Fatalf("sfc.Parse returned error: %v", err)
	}
	tree, err := ParseBlock("src/App.tue", file.Template)
	if err != nil {
		t.Fatalf("ParseBlock returned error: %v", err)
	}

	element := firstElement(t, tree.Nodes)
	if got := element.NameSpan.Start; got.Offset != strings.Index(string(src), "p :title") || got.Line != 2 || got.Column != 3 {
		t.Fatalf("element name start = %#v, want offset of p line 2 column 3", got)
	}

	if len(element.Attrs) != 1 {
		t.Fatalf("len(element.Attrs) = %d, want 1", len(element.Attrs))
	}
	attr := element.Attrs[0]
	if got, want := attr.Expression, "user.Name"; got != want {
		t.Fatalf("attr.Expression = %q, want %q", got, want)
	}
	if got, want := attr.ExpressionSpan.Start.Offset, strings.Index(string(src), "user.Name"); got != want {
		t.Fatalf("attr expression start offset = %d, want %d", got, want)
	}
	if got := attr.ExpressionSpan.Start; got.Line != 2 || got.Column != 14 {
		t.Fatalf("attr expression start = %#v, want line 2 column 14", got)
	}

	interpolation := firstInterpolation(t, element.Children)
	if got, want := interpolation.Expression, "name"; got != want {
		t.Fatalf("interpolation.Expression = %q, want %q", got, want)
	}
	if got, want := interpolation.ExpressionSpan.Start.Offset, strings.LastIndex(string(src), "name"); got != want {
		t.Fatalf("interpolation expression start offset = %d, want %d", got, want)
	}
	if got := interpolation.ExpressionSpan.Start; got.Line != 2 || got.Column != 29 {
		t.Fatalf("interpolation expression start = %#v, want line 2 column 29", got)
	}
}

func TestParseFixtures(t *testing.T) {
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
		if _, err := ParseBlock(filepath.ToSlash(path), file.Template); err != nil {
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
		src       string
		diagnoses []string
	}{
		{
			name:      "malformed interpolation",
			src:       `<p>{{ name</p>`,
			diagnoses: []string{"malformed interpolation: missing }}"},
		},
		{
			name:      "empty interpolation",
			src:       `<p>{{ }}</p>`,
			diagnoses: []string{"empty interpolation expression"},
		},
		{
			name:      "unexpected close",
			src:       `</p>`,
			diagnoses: []string{"unexpected closing </p> tag"},
		},
		{
			name:      "mismatched close",
			src:       `<div><span></div>`,
			diagnoses: []string{"expected closing </span> tag before </div>"},
		},
		{
			name:      "missing close",
			src:       `<div><span></span>`,
			diagnoses: []string{"missing closing </div> tag"},
		},
		{
			name:      "v-if needs expression",
			src:       `<p v-if></p>`,
			diagnoses: []string{"v-if requires an expression"},
		},
		{
			name:      "unsupported directive",
			src:       `<p v-show="ok"></p>`,
			diagnoses: []string{`unsupported directive "v-show"`},
		},
		{
			name:      "bound attr needs argument",
			src:       `<p :="name"></p>`,
			diagnoses: []string{"bound attribute is missing a name"},
		},
		{
			name:      "event modifiers unsupported",
			src:       `<button @click.stop="save"></button>`,
			diagnoses: []string{"event modifiers are not supported"},
		},
		{
			name:      "v-for shape",
			src:       `<li v-for="todos"></li>`,
			diagnoses: []string{`v-for expression must use "item in items" syntax`},
		},
		{
			name:      "attribute missing value",
			src:       `<p class=></p>`,
			diagnoses: []string{`attribute "class" is missing a value`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse("Broken.tue", []byte(test.src), sfc.Position{Line: 1, Column: 1})
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
	src := []byte("<p>\n\t{{ }}\n</p>")

	_, err := Parse("src/App.tue", src, sfc.Position{Offset: 50, Line: 10, Column: 7})
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
	want := "src/App.tue:11:2: empty interpolation expression"
	if got != want {
		t.Fatalf("diagnostic string = %q, want %q", got, want)
	}
}

func formatTree(tree *Tree) string {
	var b strings.Builder
	formatNodes(&b, tree.Nodes, "")
	return b.String()
}

func formatNodes(b *strings.Builder, nodes []Node, indent string) {
	for _, node := range nodes {
		switch node := node.(type) {
		case *Element:
			fmt.Fprintf(b, "%selement %s component=%t selfClosing=%t\n", indent, node.Name, node.Component, node.SelfClosing)
			for _, attr := range node.Attrs {
				formatAttr(b, attr, indent+"  ")
			}
			formatNodes(b, node.Children, indent+"  ")
		case *Text:
			text := strings.Join(strings.Fields(node.Content), " ")
			if text != "" {
				fmt.Fprintf(b, "%stext %q\n", indent, text)
			}
		case *Interpolation:
			fmt.Fprintf(b, "%sinterpolation expr=%q\n", indent, node.Expression)
		case *Comment:
			fmt.Fprintf(b, "%scomment %q\n", indent, node.Content)
		}
	}
}

func formatAttr(b *strings.Builder, attr Attr, indent string) {
	switch attr.Kind {
	case AttrStatic:
		if attr.HasValue {
			fmt.Fprintf(b, "%sattr static %s=%q\n", indent, attr.Name, attr.Value)
			return
		}
		fmt.Fprintf(b, "%sattr static %s\n", indent, attr.Name)
	case AttrBound:
		fmt.Fprintf(b, "%sattr bound %s expr=%q\n", indent, attr.Argument, attr.Expression)
	case AttrEvent:
		fmt.Fprintf(b, "%sattr event %s expr=%q\n", indent, attr.Argument, attr.Expression)
	case AttrDirective:
		if attr.Expression != "" {
			fmt.Fprintf(b, "%sattr directive %s expr=%q\n", indent, attr.Name, attr.Expression)
			return
		}
		fmt.Fprintf(b, "%sattr directive %s\n", indent, attr.Name)
	}
}

func firstElement(t *testing.T, nodes []Node) *Element {
	t.Helper()

	for _, node := range nodes {
		if element, ok := node.(*Element); ok {
			return element
		}
	}
	t.Fatalf("no element in %#v", nodes)
	return nil
}

func firstInterpolation(t *testing.T, nodes []Node) *Interpolation {
	t.Helper()

	for _, node := range nodes {
		if interpolation, ok := node.(*Interpolation); ok {
			return interpolation
		}
	}
	t.Fatalf("no interpolation in %#v", nodes)
	return nil
}

func diagnosticsText(diagnostics []Diagnostic) string {
	var b strings.Builder
	for _, diagnostic := range diagnostics {
		b.WriteString(diagnostic.String())
		b.WriteByte('\n')
	}
	return b.String()
}
