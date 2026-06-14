package sfc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		t.Fatalf("Parse diagnostics = %#v, want none", diagnosticMessages(diagnostics))
	}

	if file.Path != "hello.tue" {
		t.Fatalf("file.Path = %q, want hello.tue", file.Path)
	}
	if len(file.Blocks) != 3 {
		t.Fatalf("len(file.Blocks) = %d, want 3", len(file.Blocks))
	}

	if file.Template == nil {
		t.Fatal("file.Template is nil")
	}
	if file.Template.Kind != BlockTemplate {
		t.Fatalf("template kind = %q, want %q", file.Template.Kind, BlockTemplate)
	}
	if got, want := file.Template.Content, "\n  <p>{{ greeting }}</p>\n"; got != want {
		t.Fatalf("template content = %q, want %q", got, want)
	}

	if file.Script == nil {
		t.Fatal("file.Script is nil")
	}
	lang, ok := file.Script.Attr("lang")
	if !ok {
		t.Fatal("script lang attr missing")
	}
	if !lang.HasValue || lang.Value != "go" {
		t.Fatalf("script lang attr = %#v, want lang=\"go\"", lang)
	}
	if got, want := file.Script.Content, "\npackage fixtures\n"; got != want {
		t.Fatalf("script content = %q, want %q", got, want)
	}

	if file.Style == nil {
		t.Fatal("file.Style is nil")
	}
	if !file.Style.HasAttr("scoped") {
		t.Fatal("style scoped attr missing")
	}
	if got, want := file.Style.Content, "\np { color: red; }\n"; got != want {
		t.Fatalf("style content = %q, want %q", got, want)
	}

	scriptContentOffset := strings.Index(source, "\npackage fixtures\n")
	if file.Script.ContentSpan.Start.Offset != scriptContentOffset {
		t.Fatalf("script content start offset = %d, want %d", file.Script.ContentSpan.Start.Offset, scriptContentOffset)
	}
	if file.Script.ContentSpan.Start.Line != 4 || file.Script.ContentSpan.Start.Column != 19 {
		t.Fatalf("script content start = %#v, want line 4 column 19", file.Script.ContentSpan.Start)
	}
	if file.Script.ContentSpan.End.Line != 6 || file.Script.ContentSpan.End.Column != 1 {
		t.Fatalf("script content end = %#v, want line 6 column 1", file.Script.ContentSpan.End)
	}
}

func TestParseScriptLangMayBeUnquoted(t *testing.T) {
	source := `<template></template>
<script lang=go></script>
`

	file, diagnostics := Parse("hello.tue", []byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("Parse diagnostics = %#v, want none", diagnosticMessages(diagnostics))
	}
	if file.Script == nil {
		t.Fatal("file.Script is nil")
	}
	lang, ok := file.Script.Attr("lang")
	if !ok {
		t.Fatal("script lang attr missing")
	}
	if !lang.HasValue || lang.Value != "go" {
		t.Fatalf("script lang attr = %#v, want unquoted go value", lang)
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

			file, diagnostics := Parse(path, source)
			if len(diagnostics) != 0 {
				t.Fatalf("Parse diagnostics = %#v, want none", diagnosticMessages(diagnostics))
			}
			if file.Template == nil {
				t.Fatal("file.Template is nil")
			}
			if file.Script == nil {
				t.Fatal("file.Script is nil")
			}
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk fixtures: %v", err)
	}
}

func TestParseMissingRequiredBlocks(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "empty",
			src:  "",
			want: []string{
				"missing required <template> block",
				"missing required <script lang=\"go\"> block",
			},
		},
		{
			name: "template only",
			src:  `<template></template>`,
			want: []string{
				"missing required <script lang=\"go\"> block",
			},
		},
		{
			name: "script only",
			src:  `<script lang="go"></script>`,
			want: []string{
				"missing required <template> block",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics := Parse("missing.tue", []byte(tt.src))
			assertDiagnosticMessages(t, diagnostics, tt.want)
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

	if diagnostics[0].Span.Start.Line != 1 || diagnostics[0].Span.Start.Column != 1 {
		t.Fatalf("unexpected text position = %#v, want line 1 column 1", diagnostics[0].Span.Start)
	}
	if diagnostics[1].Span.Start.Line != 3 || diagnostics[1].Span.Start.Column != 2 {
		t.Fatalf("unsupported block position = %#v, want line 3 column 2", diagnostics[1].Span.Start)
	}
	if diagnostics[2].Span.Start.Line != 4 || diagnostics[2].Span.Start.Column != 1 {
		t.Fatalf("closing tag position = %#v, want line 4 column 1", diagnostics[2].Span.Start)
	}
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
		t.Fatalf("first valid blocks should be retained: template=%v script=%v style=%v", file.Template, file.Script, file.Style)
	}
	if len(file.Blocks) != 3 {
		t.Fatalf("len(file.Blocks) = %d, want 3 first blocks only", len(file.Blocks))
	}
}

func TestParseRejectsInvalidScriptLanguage(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "missing lang",
			src: `<template></template>
<script></script>
`,
		},
		{
			name: "wrong lang",
			src: `<template></template>
<script lang="ts"></script>
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, diagnostics := Parse("script.tue", []byte(tt.src))
			assertDiagnosticMessages(t, diagnostics, []string{
				"<script> block must set lang=\"go\"",
			})
			if file.Script != nil {
				t.Fatalf("file.Script = %#v, want nil for invalid script lang", file.Script)
			}
		})
	}
}

func TestParseRejectsMalformedTags(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "unterminated opening tag",
			src:  `<template`,
			want: "unterminated opening tag",
		},
		{
			name: "unterminated quoted attr",
			src:  `<template name="broken></template>`,
			want: "unterminated quoted attribute in opening tag",
		},
		{
			name: "missing closing tag",
			src: `<template>
  <p>Hello</p>
<script lang="go"></script>
`,
			want: "missing closing </template> tag",
		},
		{
			name: "malformed block attr",
			src:  `<template @bad></template>`,
			want: "malformed block attribute",
		},
		{
			name: "self closing required block",
			src: `<template />
<script lang="go"></script>
`,
			want: "<template> block must have a closing tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics := Parse("malformed.tue", []byte(tt.src))
			if len(diagnostics) == 0 {
				t.Fatal("Parse diagnostics = none, want malformed tag diagnostic")
			}
			if diagnostics[0].Message != tt.want {
				t.Fatalf("first diagnostic = %q, want %q; all = %#v", diagnostics[0].Message, tt.want, diagnosticMessages(diagnostics))
			}
		})
	}
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
