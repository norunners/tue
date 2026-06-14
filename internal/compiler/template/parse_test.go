package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norunners/tue/internal/compiler/sfc"
)

func TestParseElementsAttributesInterpolationAndComponents(t *testing.T) {
	source := `<main class="page"><p>{{ greeting }}, {{ name }}!</p><button type="button" @click="increment">Increment</button><UserBadge :name="name" :isAdmin='role == "admin"' /></main>`

	tree, diagnostics := Parse([]byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("Parse diagnostics = %#v, want none", diagnosticMessages(diagnostics))
	}

	want := strings.TrimSpace(`
element main component=false selfClosing=false
  attr static class value="page"
  element p component=false selfClosing=false
    interpolation "greeting"
    text ", "
    interpolation "name"
    text "!"
  element button component=false selfClosing=false
    attr static type value="button"
    attr event click expr="increment"
    text "Increment"
  element UserBadge component=true selfClosing=true
    attr bind name expr="name"
    attr bind isAdmin expr="role == \"admin\""
`)

	if got := dumpNodes(tree.Nodes); got != want {
		t.Fatalf("AST dump mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}

	main := tree.Nodes[0]
	if main.TagSpan.Start.Offset != 1 || main.TagSpan.End.Offset != 5 {
		t.Fatalf("main tag span = %#v, want offsets 1..5", main.TagSpan)
	}

	userBadge := main.Children[2]
	isAdmin := attrByRawName(t, userBadge, ":isAdmin")
	exprOffset := strings.Index(source, `role == "admin"`)
	if isAdmin.ExpressionSpan.Start.Offset != exprOffset {
		t.Fatalf(":isAdmin expression offset = %d, want %d", isAdmin.ExpressionSpan.Start.Offset, exprOffset)
	}
	if isAdmin.Expression != `role == "admin"` {
		t.Fatalf(":isAdmin expression = %q", isAdmin.Expression)
	}

	button := main.Children[1]
	click := attrByRawName(t, button, "@click")
	clickOffset := strings.Index(source, "click")
	if click.ArgumentSpan.Start.Offset != clickOffset {
		t.Fatalf("@click argument offset = %d, want %d", click.ArgumentSpan.Start.Offset, clickOffset)
	}
	incrementOffset := strings.Index(source, "increment")
	if click.ExpressionSpan.Start.Offset != incrementOffset {
		t.Fatalf("@click expression offset = %d, want %d", click.ExpressionSpan.Start.Offset, incrementOffset)
	}
}

func TestParseDirectivesAndComments(t *testing.T) {
	source := `<section><!-- note --><p v-if="user.Admin">Admin</p><p v-else>Member</p><ul><li v-for="todo in todos" :key="todo.ID">{{ todo.Text }}</li></ul><input v-model="query" /><div v-html="trusted"></div></section>`

	tree, diagnostics := Parse([]byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("Parse diagnostics = %#v, want none", diagnosticMessages(diagnostics))
	}

	want := strings.TrimSpace(`
element section component=false selfClosing=false
  comment " note "
  element p component=false selfClosing=false
    directive if expr="user.Admin"
    text "Admin"
  element p component=false selfClosing=false
    directive else
    text "Member"
  element ul component=false selfClosing=false
    element li component=false selfClosing=false
      directive for expr="todo in todos"
      attr bind key expr="todo.ID"
      interpolation "todo.Text"
  element input component=false selfClosing=true
    directive model expr="query"
  element div component=false selfClosing=false
    directive html expr="trusted"
`)

	if got := dumpNodes(tree.Nodes); got != want {
		t.Fatalf("AST dump mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}

	section := tree.Nodes[0]
	admin := childElement(t, section, "p", 0)
	vif := attrByRawName(t, admin, "v-if")
	vifOffset := strings.Index(source, "v-if")
	if vif.DirectiveSpan.Start.Offset != vifOffset || vif.DirectiveSpan.End.Offset != vifOffset+len("v-if") {
		t.Fatalf("v-if directive span = %#v, want offsets %d..%d", vif.DirectiveSpan, vifOffset, vifOffset+len("v-if"))
	}
}

func TestParseBlockUsesSFCSourceSpans(t *testing.T) {
	source := `<template>
<p>{{ name }}</p>
</template>
<script lang="go"></script>
`

	file, sfcDiagnostics := sfc.Parse("component.tue", []byte(source))
	if len(sfcDiagnostics) != 0 {
		t.Fatalf("sfc.Parse diagnostics = %#v, want none", sfcDiagnostics)
	}

	tree, diagnostics := ParseBlock(file.Template)
	if len(diagnostics) != 0 {
		t.Fatalf("ParseBlock diagnostics = %#v, want none", diagnosticMessages(diagnostics))
	}

	paragraph := firstElement(t, tree.Nodes, "p")
	interpolation := paragraph.Children[0]
	nameOffset := strings.Index(source, "name")
	if interpolation.ExpressionSpan.Start.Offset != nameOffset {
		t.Fatalf("expression offset = %d, want %d", interpolation.ExpressionSpan.Start.Offset, nameOffset)
	}
	if interpolation.ExpressionSpan.Start.Line != 2 || interpolation.ExpressionSpan.Start.Column != 7 {
		t.Fatalf("expression position = %#v, want line 2 column 7", interpolation.ExpressionSpan.Start)
	}
}

func TestParseFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "fixtures")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".tue" {
			return nil
		}

		t.Run(filepath.ToSlash(path), func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			file, sfcDiagnostics := sfc.Parse(path, source)
			if len(sfcDiagnostics) != 0 {
				t.Fatalf("sfc.Parse diagnostics = %#v, want none", sfcDiagnostics)
			}

			_, diagnostics := ParseBlock(file.Template)
			if len(diagnostics) != 0 {
				t.Fatalf("ParseBlock diagnostics = %#v, want none", diagnosticMessages(diagnostics))
			}
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk fixtures: %v", err)
	}
}

func TestParseDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "empty interpolation",
			src:  `<p>{{ }}</p>`,
			want: []string{"empty interpolation expression"},
		},
		{
			name: "unterminated interpolation",
			src:  `<p>{{ name</p>`,
			want: []string{"unterminated interpolation"},
		},
		{
			name: "unexpected interpolation close",
			src:  `<p>name }}</p>`,
			want: []string{"unexpected interpolation closing braces"},
		},
		{
			name: "empty bound attr",
			src:  `<p :="name"></p>`,
			want: []string{"bound attribute name cannot be empty"},
		},
		{
			name: "empty event",
			src:  `<p @="click"></p>`,
			want: []string{"event name cannot be empty"},
		},
		{
			name: "v-if without expression",
			src:  `<p v-if></p>`,
			want: []string{"v-if requires an expression"},
		},
		{
			name: "v-else with value",
			src:  `<p v-else="ok"></p>`,
			want: []string{"v-else must not have a value"},
		},
		{
			name: "unsupported directive",
			src:  `<p v-show="ok"></p>`,
			want: []string{`unsupported directive "v-show"`},
		},
		{
			name: "mismatched close",
			src:  `<p></div>`,
			want: []string{"unexpected closing </div> tag; expected </p>", "missing closing </p> tag"},
		},
		{
			name: "unterminated comment",
			src:  `<p><!-- broken</p>`,
			want: []string{"unterminated comment", "missing closing </p> tag"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics := Parse([]byte(tt.src))
			assertDiagnosticMessages(t, diagnostics, tt.want)
		})
	}
}

func attrByRawName(t *testing.T, node *Node, rawName string) Attr {
	t.Helper()

	for _, attr := range node.Attrs {
		if attr.RawName == rawName {
			return attr
		}
	}
	t.Fatalf("attribute %q not found on <%s>", rawName, node.Tag)
	return Attr{}
}

func firstElement(t *testing.T, nodes []*Node, tag string) *Node {
	t.Helper()

	for _, node := range nodes {
		if node.Kind == NodeElement && node.Tag == tag {
			return node
		}
	}
	t.Fatalf("element <%s> not found", tag)
	return nil
}

func childElement(t *testing.T, parent *Node, tag string, index int) *Node {
	t.Helper()

	seen := 0
	for _, child := range parent.Children {
		if child.Kind == NodeElement && child.Tag == tag {
			if seen == index {
				return child
			}
			seen++
		}
	}
	t.Fatalf("child element <%s> index %d not found under <%s>", tag, index, parent.Tag)
	return nil
}

func assertDiagnosticMessages(t *testing.T, diagnostics []Diagnostic, want []string) {
	t.Helper()

	got := diagnosticMessages(diagnostics)
	if len(got) != len(want) {
		t.Fatalf("diagnostics = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("diagnostic %d = %q, want %q; all = %#v", i, got[i], want[i], got)
		}
	}
}

func diagnosticMessages(diagnostics []Diagnostic) []string {
	messages := make([]string, len(diagnostics))
	for i, diagnostic := range diagnostics {
		messages[i] = diagnostic.Message
	}
	return messages
}

func dumpNodes(nodes []*Node) string {
	var builder strings.Builder
	for _, node := range nodes {
		dumpNode(&builder, node, 0)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func dumpNode(builder *strings.Builder, node *Node, depth int) {
	indent := strings.Repeat("  ", depth)
	switch node.Kind {
	case NodeElement:
		fmt.Fprintf(builder, "%selement %s component=%t selfClosing=%t\n", indent, node.Tag, node.IsComponent, node.SelfClosing)
		for _, attr := range node.Attrs {
			dumpAttr(builder, attr, depth+1)
		}
		for _, child := range node.Children {
			dumpNode(builder, child, depth+1)
		}
	case NodeText:
		fmt.Fprintf(builder, "%stext %q\n", indent, node.Text)
	case NodeInterpolation:
		fmt.Fprintf(builder, "%sinterpolation %q\n", indent, node.Expression)
	case NodeComment:
		fmt.Fprintf(builder, "%scomment %q\n", indent, node.Text)
	default:
		fmt.Fprintf(builder, "%sunknown %q\n", indent, node.Kind)
	}
}

func dumpAttr(builder *strings.Builder, attr Attr, depth int) {
	indent := strings.Repeat("  ", depth)
	switch attr.Kind {
	case AttrStatic:
		if attr.HasValue {
			fmt.Fprintf(builder, "%sattr static %s value=%q\n", indent, attr.Name, attr.Value)
		} else {
			fmt.Fprintf(builder, "%sattr static %s\n", indent, attr.Name)
		}
	case AttrBind:
		fmt.Fprintf(builder, "%sattr bind %s expr=%q\n", indent, attr.Argument, attr.Expression)
	case AttrEvent:
		fmt.Fprintf(builder, "%sattr event %s expr=%q\n", indent, attr.Argument, attr.Expression)
	case AttrDirective:
		if attr.Expression != "" {
			fmt.Fprintf(builder, "%sdirective %s expr=%q\n", indent, attr.Directive, attr.Expression)
		} else {
			fmt.Fprintf(builder, "%sdirective %s\n", indent, attr.Directive)
		}
	}
}
