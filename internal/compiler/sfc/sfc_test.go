package sfc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValidBlocks(t *testing.T) {
	src := []byte(`<template>
	<main>Hello</main>
</template>

<script lang="go">
package fixtures
</script>

<style scoped>
.page { color: red; }
</style>
`)

	file, err := Parse("Component.tue", src)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if file.Path != "Component.tue" {
		t.Fatalf("Path = %q, want Component.tue", file.Path)
	}
	if len(file.Blocks) != 3 {
		t.Fatalf("len(Blocks) = %d, want 3", len(file.Blocks))
	}
	if file.Template == nil {
		t.Fatal("Template is nil")
	}
	if file.Script == nil {
		t.Fatal("Script is nil")
	}
	if file.Style == nil {
		t.Fatal("Style is nil")
	}

	if got, want := file.Template.Content, "\n\t<main>Hello</main>\n"; got != want {
		t.Fatalf("Template.Content = %q, want %q", got, want)
	}
	if got, want := file.Script.Content, "\npackage fixtures\n"; got != want {
		t.Fatalf("Script.Content = %q, want %q", got, want)
	}
	if got, want := file.Style.Content, "\n.page { color: red; }\n"; got != want {
		t.Fatalf("Style.Content = %q, want %q", got, want)
	}

	lang, ok := file.Script.Attr("lang")
	if !ok {
		t.Fatal("script lang attr missing")
	}
	if !lang.HasValue || lang.Value != "go" {
		t.Fatalf("script lang attr = %#v, want lang=\"go\"", lang)
	}
	if !file.Style.Scoped() {
		t.Fatal("Style.Scoped() = false, want true")
	}

	if got := file.Template.OpenTagSpan.Start; got.Offset != 0 || got.Line != 1 || got.Column != 1 {
		t.Fatalf("template open start = %#v, want offset 0 line 1 column 1", got)
	}
	if got := file.Template.ContentSpan.Start; got.Offset != len("<template>") || got.Line != 1 || got.Column != 11 {
		t.Fatalf("template content start = %#v, want offset %d line 1 column 11", got, len("<template>"))
	}
	if got := file.Script.OpenTagSpan.Start; got.Line != 5 || got.Column != 1 {
		t.Fatalf("script open start = %#v, want line 5 column 1", got)
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
		if _, err := Parse(filepath.ToSlash(path), src); err != nil {
			t.Fatalf("Parse(%s) returned error: %v", path, err)
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
			name: "missing template",
			src: `<script lang="go">
package fixtures
</script>`,
			diagnoses: []string{"missing required <template> block"},
		},
		{
			name: "missing script",
			src: `<template>
	<p>Hello</p>
</template>`,
			diagnoses: []string{"missing required <script lang=\"go\"> block"},
		},
		{
			name: "duplicate template",
			src: `<template></template>
<template></template>
<script lang="go"></script>`,
			diagnoses: []string{"duplicate <template> block"},
		},
		{
			name: "duplicate script",
			src: `<template></template>
<script lang="go"></script>
<script lang="go"></script>`,
			diagnoses: []string{"duplicate <script lang=\"go\"> block"},
		},
		{
			name: "duplicate style",
			src: `<template></template>
<script lang="go"></script>
<style></style>
<style scoped></style>`,
			diagnoses: []string{"duplicate <style> block"},
		},
		{
			name: "unsupported block",
			src: `<template></template>
<route></route>
<script lang="go"></script>`,
			diagnoses: []string{"unsupported block <route>"},
		},
		{
			name: "malformed opening tag",
			src:  `<template`,
			diagnoses: []string{
				"malformed opening tag: missing >",
				"missing required <template> block",
				"missing required <script lang=\"go\"> block",
			},
		},
		{
			name: "missing closing tag",
			src: `<template>
	<p>Hello</p>
<script lang="go"></script>`,
			diagnoses: []string{"missing closing </template> tag"},
		},
		{
			name: "script missing lang",
			src: `<template></template>
<script></script>`,
			diagnoses: []string{"<script> block must use lang=\"go\""},
		},
		{
			name: "script wrong lang",
			src: `<template></template>
<script lang="ts"></script>`,
			diagnoses: []string{"<script> block must use lang=\"go\""},
		},
		{
			name: "template attr unsupported",
			src: `<template lang="html"></template>
<script lang="go"></script>`,
			diagnoses: []string{"<template> does not support attribute \"lang\""},
		},
		{
			name: "style scoped must be boolean",
			src: `<template></template>
<script lang="go"></script>
<style scoped="true"></style>`,
			diagnoses: []string{"<style scoped> must use a boolean scoped attribute"},
		},
		{
			name: "top level content",
			src: `hello
<template></template>
<script lang="go"></script>`,
			diagnoses: []string{"unexpected top-level content outside a block"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse("Broken.tue", []byte(test.src))
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
	src := []byte(`<template></template>
<route></route>
<script lang="go"></script>`)

	_, err := Parse("src/App.tue", src)
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
	want := "src/App.tue:2:2: unsupported block <route>"
	if got != want {
		t.Fatalf("diagnostic string = %q, want %q", got, want)
	}
}

func diagnosticsText(diagnostics []Diagnostic) string {
	var b strings.Builder
	for _, diagnostic := range diagnostics {
		b.WriteString(diagnostic.String())
		b.WriteByte('\n')
	}
	return b.String()
}
