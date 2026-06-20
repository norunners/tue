package sfc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseValidBlocks(t *testing.T) {
	source := `<template>
  <p>{{ greeting }}</p>
</template>
<script lang="go">
package fixtures
</script>
<style scoped>
p { color: red; }
</style>
`

	file, diagnostics := Parse("hello.tue", []byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("Parse diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}

	if diff := cmp.Diff("hello.tue", file.Path); diff != "" {
		t.Errorf("mismatch file path (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(3, len(file.Blocks)); diff != "" {
		t.Errorf("mismatch block count (-expected, +actual):\n%s", diff)
	}

	if file.Template == nil {
		t.Fatal("file.Template is nil")
	}
	if file.Template.Kind != BlockTemplate {
		t.Errorf("template kind actual = %q, expected %q", file.Template.Kind, BlockTemplate)
	}
	if diff := cmp.Diff("\n  <p>{{ greeting }}</p>\n", file.Template.Content); diff != "" {
		t.Errorf("mismatch template content (-expected, +actual):\n%s", diff)
	}
	if file.Script == nil {
		t.Fatal("file.Script is nil")
	}
	lang, ok := file.Script.Attr("lang")
	if !ok {
		t.Fatal("script lang attr missing")
	}
	if !lang.HasValue || lang.Value != "go" {
		t.Errorf("script lang attr actual = %#v, expected lang=\"go\"", lang)
	}
	if diff := cmp.Diff("\npackage fixtures\n", file.Script.Content); diff != "" {
		t.Errorf("mismatch script content (-expected, +actual):\n%s", diff)
	}

	if file.Style == nil {
		t.Fatal("file.Style is nil")
	}
	if !file.Style.HasAttr("scoped") {
		t.Error("style scoped attr missing")
	}
	if diff := cmp.Diff("\np { color: red; }\n", file.Style.Content); diff != "" {
		t.Errorf("mismatch style content (-expected, +actual):\n%s", diff)
	}

	scriptContentOffset := strings.Index(source, "\npackage fixtures\n")
	if file.Script.ContentSpan.Start.Offset != scriptContentOffset {
		t.Errorf("script content start offset actual = %d, expected %d", file.Script.ContentSpan.Start.Offset, scriptContentOffset)
	}
	if file.Script.ContentSpan.Start.Line != 4 || file.Script.ContentSpan.Start.Column != 19 {
		t.Errorf("script content start actual = %#v, expected line 4 column 19", file.Script.ContentSpan.Start)
	}
	if file.Script.ContentSpan.End.Line != 6 || file.Script.ContentSpan.End.Column != 1 {
		t.Errorf("script content end actual = %#v, expected line 6 column 1", file.Script.ContentSpan.End)
	}
}

func TestParseScriptLangMayBeUnquoted(t *testing.T) {
	source := `<template></template>
<script lang=go></script>
`

	file, diagnostics := Parse("hello.tue", []byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("Parse diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}
	if file.Script == nil {
		t.Fatal("file.Script is nil")
	}
	lang, ok := file.Script.Attr("lang")
	if !ok {
		t.Fatal("script lang attr missing")
	}
	if !lang.HasValue || lang.Value != "go" {
		t.Errorf("script lang attr actual = %#v, expected unquoted go value", lang)
	}
}

func TestBlockAttr(t *testing.T) {
	lang := Attr{Name: "lang", Value: "go", HasValue: true}
	block := &Block{Attrs: []Attr{lang}}
	tests := []struct {
		name     string
		block    *Block
		attrName string
		expected *Attr
		ok       bool
	}{
		{name: "present", block: block, attrName: "lang", expected: &lang, ok: true},
		{name: "missing", block: block, attrName: "scoped", expected: nil, ok: false},
		{name: "nil block", block: nil, attrName: "lang", expected: nil, ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, ok := test.block.Attr(test.attrName)
			if diff := cmp.Diff(test.ok, ok); diff != "" {
				t.Errorf("mismatch attribute ok (-expected, +actual):\n%s", diff)
			}
			if diff := cmp.Diff(test.expected, actual); diff != "" {
				t.Errorf("mismatch attribute (-expected, +actual):\n%s", diff)
			}
		})
	}
}

func TestParseTemplateAllowsNestedTemplateTags(t *testing.T) {
	source := `<template>
	<ul>
		<template v-for="todo in todos">
			<li>{{ todo.Text }}</li>
		</template>
	</ul>
</template>
<script lang="go">
package fixtures
</script>
`

	file, diagnostics := Parse("nested.tue", []byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("Parse diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}
	if file.Template == nil {
		t.Fatal("file.Template is nil")
	}
	if !strings.Contains(file.Template.Content, `<template v-for="todo in todos">`) {
		t.Errorf("template content actual = %q, expected nested template tag", file.Template.Content)
	}
	if strings.Contains(file.Template.Content, `<script`) {
		t.Errorf("template content = %q, should not include script block", file.Template.Content)
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

func TestParseMissingRequiredBlocks(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected []string
	}{
		{
			name:   "empty",
			source: "",
			expected: []string{
				"missing required <template> block",
				"missing required <script lang=\"go\"> block",
			},
		},
		{
			name:   "template only",
			source: `<template></template>`,
			expected: []string{
				"missing required <script lang=\"go\"> block",
			},
		},
		{
			name:   "script only",
			source: `<script lang="go"></script>`,
			expected: []string{
				"missing required <template> block",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics := Parse("missing.tue", []byte(test.source))
			assertDiagnosticMessages(t, diagnostics, test.expected)
		})
	}
}

func TestParseRejectsUnsupportedAndUnexpectedTopLevelSyntax(t *testing.T) {
	source := `intro
<template></template>
<route></route>
</style>
<script lang="go"></script>
`

	_, diagnostics := Parse("unsupported.tue", []byte(source))
	assertDiagnosticMessages(t, diagnostics, []string{
		"unexpected text outside top-level block",
		"unsupported top-level block <route>",
		"unexpected closing tag",
	})
	if len(diagnostics) == 3 {
		type lineColumn struct {
			Line   int
			Column int
		}
		expected := []lineColumn{
			{Line: 1, Column: 1},
			{Line: 3, Column: 2},
			{Line: 4, Column: 1},
		}
		actual := []lineColumn{
			{Line: diagnostics[0].Span.Start.Line, Column: diagnostics[0].Span.Start.Column},
			{Line: diagnostics[1].Span.Start.Line, Column: diagnostics[1].Span.Start.Column},
			{Line: diagnostics[2].Span.Start.Line, Column: diagnostics[2].Span.Start.Column},
		}
		if diff := cmp.Diff(expected, actual); diff != "" {
			t.Errorf("mismatch diagnostic positions (-expected, +actual):\n%s", diff)
		}
	}
}

func TestParseRejectsUnsupportedContractBlock(t *testing.T) {
	source := `<template></template>
<contract></contract>
<script lang="go"></script>
`

	_, diagnostics := Parse("unsupported-contract.tue", []byte(source))
	assertDiagnosticMessages(t, diagnostics, []string{
		"unsupported top-level block <contract>",
	})
}

func TestParseRejectsDuplicateBlocks(t *testing.T) {
	source := `<template></template>
<template></template>
<script lang="go"></script>
<script lang="go"></script>
<style></style>
<style scoped></style>
`

	file, diagnostics := Parse("duplicates.tue", []byte(source))
	assertDiagnosticMessages(t, diagnostics, []string{
		"duplicate <template> block",
		"duplicate <script> block",
		"duplicate <style> block",
	})

	if file.Template == nil || file.Script == nil || file.Style == nil {
		t.Errorf("first valid blocks should be retained: template=%v script=%v style=%v", file.Template, file.Script, file.Style)
	}
	if len(file.Blocks) != 3 {
		t.Errorf("block count actual = %d, expected 3 first blocks only", len(file.Blocks))
	}
}

func TestParseRejectsInvalidScriptLanguage(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "missing lang",
			source: `<template></template>
<script></script>
`,
		},
		{
			name: "wrong lang",
			source: `<template></template>
<script lang="ts"></script>
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file, diagnostics := Parse("script.tue", []byte(test.source))
			assertDiagnosticMessages(t, diagnostics, []string{
				"<script> block must set lang=\"go\"",
			})
			if file.Script != nil {
				t.Errorf("file.Script actual = %#v, expected nil for invalid script lang", file.Script)
			}
		})
	}
}

func TestParseRejectsMalformedTags(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected string
	}{
		{
			name:     "unterminated opening tag",
			source:   `<template`,
			expected: "unterminated opening tag",
		},
		{
			name:     "unterminated quoted attr",
			source:   `<template name="broken></template>`,
			expected: "unterminated quoted attribute in opening tag",
		},
		{
			name: "missing closing tag",
			source: `<template>
  <p>Hello</p>
<script lang="go"></script>
`,
			expected: "missing closing </template> tag",
		},
		{
			name:     "malformed block attr",
			source:   `<template @bad></template>`,
			expected: "malformed block attribute",
		},
		{
			name: "self closing required block",
			source: `<template />
<script lang="go"></script>
`,
			expected: "<template> block must have a closing tag",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics := Parse("malformed.tue", []byte(test.source))
			if len(diagnostics) == 0 {
				t.Error("Parse diagnostics actual = none, expected malformed tag diagnostic")
				return
			}
			if diff := cmp.Diff(test.expected, diagnostics[0].Message); diff != "" {
				t.Errorf("mismatch first diagnostic (-expected, +actual):\n%s", diff)
			}
		})
	}
}

func parseFixture(path string) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read fixture: %w", err)
	}

	file, diagnostics := Parse(path, source)
	if len(diagnostics) != 0 {
		return fmt.Errorf("Parse diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}
	if file.Template == nil {
		return fmt.Errorf("file.Template is nil")
	}
	if file.Script == nil {
		return fmt.Errorf("file.Script is nil")
	}
	return nil
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
