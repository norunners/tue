package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/norunners/tue/internal/compiler/sfc"
)

func TestParseElementsAttributesInterpolationAndComponents(t *testing.T) {
	source := `<main class="page"><p>{{ greeting }}, {{ name }}!</p><button type="button" @click="increment">Increment</button><UserBadge :name="name" :isAdmin='role == "admin"' /></main>`

	tree, diagnostics := Parse([]byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("Parse diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}

	expected := strings.TrimSpace(`
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

	actual := dumpNodes(tree.Nodes)
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch AST dump (-expected, +actual):\n%s", diff)
	}

	main, err := firstElement(tree.Nodes, "main")
	if err != nil {
		t.Fatal(err)
	}
	if main.TagSpan.Start.Offset != 1 || main.TagSpan.End.Offset != 5 {
		t.Errorf("main tag span actual = %#v, expected offsets 1..5", main.TagSpan)
	}

	userBadge, err := childElement(main, "UserBadge", 0)
	if err != nil {
		t.Fatal(err)
	}
	isAdmin, err := attrByRawName(userBadge, ":isAdmin")
	if err != nil {
		t.Fatal(err)
	}
	exprOffset := strings.Index(source, `role == "admin"`)
	if isAdmin.ExpressionSpan.Start.Offset != exprOffset {
		t.Errorf(":isAdmin expression offset actual = %d, expected %d", isAdmin.ExpressionSpan.Start.Offset, exprOffset)
	}
	if isAdmin.Expression != `role == "admin"` {
		t.Errorf(":isAdmin expression actual = %q, expected %q", isAdmin.Expression, `role == "admin"`)
	}

	button, err := childElement(main, "button", 0)
	if err != nil {
		t.Fatal(err)
	}
	click, err := attrByRawName(button, "@click")
	if err != nil {
		t.Fatal(err)
	}
	clickOffset := strings.Index(source, "click")
	if click.ArgumentSpan.Start.Offset != clickOffset {
		t.Errorf("@click argument offset actual = %d, expected %d", click.ArgumentSpan.Start.Offset, clickOffset)
	}
	incrementOffset := strings.Index(source, "increment")
	if click.ExpressionSpan.Start.Offset != incrementOffset {
		t.Errorf("@click expression offset actual = %d, expected %d", click.ExpressionSpan.Start.Offset, incrementOffset)
	}
}

func TestParseDirectivesAndComments(t *testing.T) {
	source := `<section><!-- note --><p v-if="user.Admin">Admin</p><p v-else>Member</p><ul><li v-for="todo in todos" :key="todo.ID">{{ todo.Text }}</li></ul><input v-model="query" /><div v-html="trusted"></div></section>`

	tree, diagnostics := Parse([]byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("Parse diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}

	expected := strings.TrimSpace(`
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

	actual := dumpNodes(tree.Nodes)
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch AST dump (-expected, +actual):\n%s", diff)
	}

	section, err := firstElement(tree.Nodes, "section")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := childElement(section, "p", 0)
	if err != nil {
		t.Fatal(err)
	}
	vif, err := attrByRawName(admin, "v-if")
	if err != nil {
		t.Fatal(err)
	}
	vifOffset := strings.Index(source, "v-if")
	if vif.DirectiveSpan.Start.Offset != vifOffset || vif.DirectiveSpan.End.Offset != vifOffset+len("v-if") {
		t.Errorf("v-if directive span actual = %#v, expected offsets %d..%d", vif.DirectiveSpan, vifOffset, vifOffset+len("v-if"))
	}
}

func TestParseConditionalControlDirectives(t *testing.T) {
	source := `<main><p v-if="ready">Ready</p><p v-else-if="failed">Failed</p><p v-else>Unknown</p><template v-switch="status"><p v-case='"loading"'>Loading</p><p v-default>Done</p></template></main>`

	tree, diagnostics := Parse([]byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("Parse diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}

	expected := strings.TrimSpace(`
element main component=false selfClosing=false
  element p component=false selfClosing=false
    directive if expr="ready"
    text "Ready"
  element p component=false selfClosing=false
    directive else-if expr="failed"
    text "Failed"
  element p component=false selfClosing=false
    directive else
    text "Unknown"
  element template component=false selfClosing=false
    directive switch expr="status"
    element p component=false selfClosing=false
      directive case expr="\"loading\""
      text "Loading"
    element p component=false selfClosing=false
      directive default
      text "Done"
`)
	actual := dumpNodes(tree.Nodes)
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch AST dump (-expected, +actual):\n%s", diff)
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
		t.Fatalf("sfc.Parse diagnostics actual = %#v, expected none", sfcDiagnostics)
	}

	tree, diagnostics := ParseBlock(file.Template)
	if len(diagnostics) != 0 {
		t.Fatalf("ParseBlock diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}

	paragraph, err := firstElement(tree.Nodes, "p")
	if err != nil {
		t.Fatal(err)
	}
	interpolation := paragraph.Children[0]
	nameOffset := strings.Index(source, "name")
	if interpolation.ExpressionSpan.Start.Offset != nameOffset {
		t.Errorf("expression offset actual = %d, expected %d", interpolation.ExpressionSpan.Start.Offset, nameOffset)
	}
	if interpolation.ExpressionSpan.Start.Line != 2 || interpolation.ExpressionSpan.Start.Column != 7 {
		t.Errorf("expression position actual = %#v, expected line 2 column 7", interpolation.ExpressionSpan.Start)
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
			if err := parseFixture(path); err != nil {
				t.Errorf("parse fixture: %v", err)
			}
		})

		return nil
	})
	if err != nil {
		t.Errorf("walk fixtures: %v", err)
	}
}

func TestParseDiagnostics(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name:     "empty interpolation",
			source:   `<p>{{ }}</p>`,
			expected: []string{"empty interpolation expression"},
		},
		{
			name:     "unterminated interpolation",
			source:   `<p>{{ name</p>`,
			expected: []string{"unterminated interpolation"},
		},
		{
			name:     "unexpected interpolation close",
			source:   `<p>name }}</p>`,
			expected: []string{"unexpected interpolation closing braces"},
		},
		{
			name:     "empty bound attr",
			source:   `<p :="name"></p>`,
			expected: []string{"bound attribute name cannot be empty"},
		},
		{
			name:     "empty event",
			source:   `<p @="click"></p>`,
			expected: []string{"event name cannot be empty"},
		},
		{
			name:     "v-if without expression",
			source:   `<p v-if></p>`,
			expected: []string{"v-if requires an expression"},
		},
		{
			name:     "v-else-if without expression",
			source:   `<p v-else-if></p>`,
			expected: []string{"v-else-if requires an expression"},
		},
		{
			name:     "v-else with value",
			source:   `<p v-else="ok"></p>`,
			expected: []string{"v-else must not have a value"},
		},
		{
			name:     "v-switch without expression",
			source:   `<template v-switch></template>`,
			expected: []string{"v-switch requires an expression"},
		},
		{
			name:     "v-case without expression",
			source:   `<p v-case></p>`,
			expected: []string{"v-case requires an expression"},
		},
		{
			name:     "v-default with value",
			source:   `<p v-default="ok"></p>`,
			expected: []string{"v-default must not have a value"},
		},
		{
			name:     "unsupported directive",
			source:   `<p v-show="ok"></p>`,
			expected: []string{`unsupported directive "v-show"`},
		},
		{
			name:     "mismatched close",
			source:   `<p></div>`,
			expected: []string{"unexpected closing </div> tag; expected </p>", "missing closing </p> tag"},
		},
		{
			name:     "unterminated comment",
			source:   `<p><!-- broken</p>`,
			expected: []string{"unterminated comment", "missing closing </p> tag"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics := Parse([]byte(test.source))
			assertDiagnosticMessages(t, diagnostics, test.expected)
		})
	}
}

func parseFixture(path string) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read fixture: %w", err)
	}

	file, sfcDiagnostics := sfc.Parse(path, source)
	if len(sfcDiagnostics) != 0 {
		return fmt.Errorf("sfc.Parse diagnostics actual = %#v, expected none", sfcDiagnostics)
	}

	_, diagnostics := ParseBlock(file.Template)
	if len(diagnostics) != 0 {
		return fmt.Errorf("ParseBlock diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}
	return nil
}

func attrByRawName(node *Node, rawName string) (*Attr, error) {
	for index := range node.Attrs {
		if node.Attrs[index].RawName == rawName {
			return &node.Attrs[index], nil
		}
	}
	return nil, fmt.Errorf("attribute %q not found on <%s>", rawName, node.Tag)
}

func firstElement(nodes []*Node, tag string) (*Node, error) {
	for _, node := range nodes {
		if node.Kind == NodeElement && node.Tag == tag {
			return node, nil
		}
	}
	return nil, fmt.Errorf("element <%s> not found", tag)
}

func childElement(parent *Node, tag string, index int) (*Node, error) {
	seen := 0
	for _, child := range parent.Children {
		if child.Kind == NodeElement && child.Tag == tag {
			if seen == index {
				return child, nil
			}
			seen++
		}
	}
	return nil, fmt.Errorf("child element <%s> index %d not found under <%s>", tag, index, parent.Tag)
}

func assertDiagnosticMessages(t *testing.T, diagnostics []Diagnostic, expected []string) {
	t.Helper()

	actual := diagnosticMessages(diagnostics)
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
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
