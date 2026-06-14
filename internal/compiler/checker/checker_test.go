package checker

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/norunners/vue/internal/compiler/script"
	"github.com/norunners/vue/internal/compiler/sfc"
	templateparser "github.com/norunners/vue/internal/compiler/template"
)

func TestCheckAcceptsRepresentativeComponentTree(t *testing.T) {
	diagnostics := checkSources(t, map[string]string{
		"Parent.tue": `<template>
  <main>
    <UserBadge :name="name" :isAdmin='role == "admin"' @select="increment" />
    <button type="button" :disabled="saving" @click="increment">Increment</button>
    <input v-model="name" />
    <li v-for="todo in todos" :key="todo.ID">{{ todo.Text }}</li>
  </main>
</template>

<script lang="go">
package fixtures

import tue "github.com/norunners/vue"

type Todo struct {
	ID   int
	Text string
}

type Parent struct {
	name      string
	role      string
	saving    bool
	todos     []Todo
	increment func()
}

func (p *Parent) Init(ctx tue.Context) {
	p.increment = func() {}
}
</script>`,
		"UserBadge.tue": `<template>
  <span>{{ name }}</span>
</template>

<script lang="go">
package fixtures

import tue "github.com/norunners/vue"

type UserBadge struct {
	name    tue.Prop[string] ` + "`prop:\"name,required\"`" + `
	isAdmin tue.Prop[bool]   ` + "`prop:\"isAdmin\"`" + `
	Select  func()
}
</script>`,
	})

	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics =\n%s\nwant none", diagnosticsText(diagnostics))
	}
}

func TestCheckReportsTemplateTypeDiagnostics(t *testing.T) {
	tests := []struct {
		name    string
		sources map[string]string
		want    []string
	}{
		{
			name: "unknown interpolation symbol",
			sources: map[string]string{
				"Broken.tue": `<template>
  <p>{{ missing }}</p>
</template>

<script lang="go">
package fixtures

type Broken struct {
	name string
}
</script>`,
			},
			want: []string{`unknown template symbol "missing"`},
		},
		{
			name: "invalid interpolation expression",
			sources: map[string]string{
				"Broken.tue": `<template>
  <p>{{ name + }}</p>
</template>

<script lang="go">
package fixtures

type Broken struct {
	name string
}
</script>`,
			},
			want: []string{`invalid Go expression`},
		},
		{
			name: "component prop type mismatch",
			sources: map[string]string{
				"Parent.tue": `<template>
  <UserBadge :name="name" :isAdmin='"yes"' />
</template>

<script lang="go">
package fixtures

type Parent struct {
	name string
}
</script>`,
				"UserBadge.tue": userBadgeSource(),
			},
			want: []string{`prop "isAdmin" expects bool, got string`},
		},
		{
			name: "unknown component prop",
			sources: map[string]string{
				"Parent.tue": `<template>
  <UserBadge :missing="name" />
</template>

<script lang="go">
package fixtures

type Parent struct {
	name string
}
</script>`,
				"UserBadge.tue": userBadgeSource(),
			},
			want: []string{`component UserBadge has no prop "missing"`, `component UserBadge requires prop "name"`},
		},
		{
			name: "missing required component prop",
			sources: map[string]string{
				"Parent.tue": `<template>
  <UserBadge />
</template>

<script lang="go">
package fixtures

type Parent struct{}
</script>`,
				"UserBadge.tue": userBadgeSource(),
			},
			want: []string{`component UserBadge requires prop "name"`},
		},
		{
			name: "missing component event",
			sources: map[string]string{
				"Parent.tue": `<template>
  <UserBadge :name="name" @missing="save" />
</template>

<script lang="go">
package fixtures

type Parent struct {
	name string
	save func()
}
</script>`,
				"UserBadge.tue": userBadgeSource(),
			},
			want: []string{`component UserBadge has no event "missing"`},
		},
		{
			name: "missing native event handler",
			sources: map[string]string{
				"Broken.tue": `<template>
  <button @click="save">Save</button>
</template>

<script lang="go">
package fixtures

type Broken struct{}
</script>`,
			},
			want: []string{`event handler "save" does not exist`},
		},
		{
			name: "native bound attribute mismatch",
			sources: map[string]string{
				"Broken.tue": `<template>
  <button :disabled="name">Save</button>
</template>

<script lang="go">
package fixtures

type Broken struct {
	name string
}
</script>`,
			},
			want: []string{`attribute "disabled" expects bool, got string`},
		},
		{
			name: "v-model prop target is not writable",
			sources: map[string]string{
				"Broken.tue": `<template>
  <input v-model="name" />
</template>

<script lang="go">
package fixtures

import tue "github.com/norunners/vue"

type Broken struct {
	name tue.Prop[string]
}
</script>`,
			},
			want: []string{`v-model target "name" is not writable`},
		},
		{
			name: "v-for requires key",
			sources: map[string]string{
				"Broken.tue": `<template>
  <li v-for="item in items">{{ item }}</li>
</template>

<script lang="go">
package fixtures

type Broken struct {
	items []string
}
</script>`,
			},
			want: []string{`v-for elements must include :key`},
		},
		{
			name: "v-for source must be collection",
			sources: map[string]string{
				"Broken.tue": `<template>
  <li v-for="item in title" :key="item">{{ item }}</li>
</template>

<script lang="go">
package fixtures

type Broken struct {
	title string
}
</script>`,
			},
			want: []string{`v-for source must be a slice, array, or map, got string`},
		},
		{
			name: "native event name",
			sources: map[string]string{
				"Broken.tue": `<template>
  <button @explode="save">Save</button>
</template>

<script lang="go">
package fixtures

type Broken struct {
	save func()
}
</script>`,
			},
			want: []string{`unknown native event "explode"`},
		},
		{
			name: "native event handler signature",
			sources: map[string]string{
				"Broken.tue": `<template>
  <button @click="save">Save</button>
  <button @click="reset()">Reset</button>
</template>

<script lang="go">
package fixtures

type Broken struct {
	save func(string)
}

func (b *Broken) reset() error { return nil }
</script>`,
			},
			want: []string{`event handler "save" must have signature func()`, `event handler "reset" must have signature func()`},
		},
		{
			name: "native event handler arguments",
			sources: map[string]string{
				"Broken.tue": `<template>
  <button @click="save(name)">Save</button>
</template>

<script lang="go">
package fixtures

type Broken struct {
	name string
	save func()
}
</script>`,
			},
			want: []string{`event handler calls with arguments are not supported`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostics := checkSources(t, test.sources)
			got := diagnosticsText(diagnostics)
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Fatalf("diagnostics =\n%s\nwant to contain %q", got, want)
				}
			}
		})
	}
}

func TestCheckFixtures(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		diagnoses []string
	}{
		{
			name: "static hello",
			path: filepath.Join("..", "..", "..", "testdata", "fixtures", "static_hello.tue"),
		},
		{
			name: "interpolation",
			path: filepath.Join("..", "..", "..", "testdata", "fixtures", "interpolation.tue"),
		},
		{
			name: "counter",
			path: filepath.Join("..", "..", "..", "testdata", "fixtures", "counter.tue"),
		},
		{
			name: "parent child props",
			path: filepath.Join("..", "..", "..", "testdata", "fixtures", "parent_child_props"),
		},
		{
			name:      "invalid prop type",
			path:      filepath.Join("..", "..", "..", "testdata", "fixtures", "invalid_prop_type"),
			diagnoses: []string{`prop "isAdmin" expects bool, got string`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostics := checkSources(t, readFixtureSources(t, test.path))
			got := diagnosticsText(diagnostics)
			if len(test.diagnoses) == 0 {
				if len(diagnostics) != 0 {
					t.Fatalf("diagnostics =\n%s\nwant none", got)
				}
				return
			}
			for _, want := range test.diagnoses {
				if !strings.Contains(got, want) {
					t.Fatalf("diagnostics =\n%s\nwant to contain %q", got, want)
				}
			}
		})
	}
}

func TestCheckDiagnosticSpanSupportsCarets(t *testing.T) {
	source := `<template>
  <p>{{ missing }}</p>
</template>

<script lang="go">
package fixtures

type Broken struct{}
</script>`
	diagnostics := checkSources(t, map[string]string{"Broken.tue": source})
	if len(diagnostics) != 1 {
		t.Fatalf("len(diagnostics) = %d, want 1:\n%s", len(diagnostics), diagnosticsText(diagnostics))
	}

	if got, want := diagnostics[0].String(), `Broken.tue:2:9: unknown template symbol "missing"`; got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
	if got, want := sourceLineWithCaret(source, diagnostics[0].Span), "  <p>{{ missing }}</p>\n        ^^^^^^^"; got != want {
		t.Fatalf("source caret =\n%s\nwant =\n%s", got, want)
	}
}

func checkSources(t *testing.T, sources map[string]string) []Diagnostic {
	t.Helper()

	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	files := make([]File, 0, len(paths))
	for _, path := range paths {
		src := []byte(sources[path])
		parsed, err := sfc.Parse(path, src)
		if err != nil {
			var parseErr *sfc.Error
			if errors.As(err, &parseErr) {
				t.Fatalf("sfc.Parse(%s) diagnostics:\n%s", path, diagnosticsText(parseErr.Diagnostics))
			}
			t.Fatalf("sfc.Parse(%s): %v", path, err)
		}

		tree, err := templateparser.ParseBlock(path, parsed.Template)
		if err != nil {
			var parseErr *templateparser.Error
			if errors.As(err, &parseErr) {
				t.Fatalf("template.ParseBlock(%s) diagnostics:\n%s", path, diagnosticsText(parseErr.Diagnostics))
			}
			t.Fatalf("template.ParseBlock(%s): %v", path, err)
		}

		contract, err := script.ParseBlock(path, parsed.Script)
		if err != nil {
			var parseErr *script.Error
			if errors.As(err, &parseErr) {
				t.Fatalf("script.ParseBlock(%s) diagnostics:\n%s", path, diagnosticsText(parseErr.Diagnostics))
			}
			t.Fatalf("script.ParseBlock(%s): %v", path, err)
		}

		files = append(files, File{
			Path:     path,
			Template: tree,
			Contract: contract,
		})
	}

	return Check(files)
}

func readFixtureSources(t *testing.T, fixturePath string) map[string]string {
	t.Helper()

	sources := make(map[string]string)
	info, err := os.Stat(fixturePath)
	if err != nil {
		t.Fatalf("stat fixture %s: %v", fixturePath, err)
	}
	if !info.IsDir() {
		src, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatalf("read fixture %s: %v", fixturePath, err)
		}
		sources[filepath.ToSlash(fixturePath)] = string(src)
		return sources
	}

	err = filepath.WalkDir(fixturePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".tue" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sources[filepath.ToSlash(path)] = string(src)
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture %s: %v", fixturePath, err)
	}
	return sources
}

func userBadgeSource() string {
	return `<template>
  <span>{{ name }}</span>
</template>

<script lang="go">
package fixtures

import tue "github.com/norunners/vue"

type UserBadge struct {
	name    tue.Prop[string] ` + "`prop:\"name,required\"`" + `
	isAdmin tue.Prop[bool]   ` + "`prop:\"isAdmin\"`" + `
}
</script>`
}

func diagnosticsText(diagnostics []Diagnostic) string {
	var b strings.Builder
	for _, diagnostic := range diagnostics {
		b.WriteString(diagnostic.String())
		b.WriteByte('\n')
	}
	return b.String()
}

func sourceLineWithCaret(source string, span sfc.Span) string {
	lines := strings.Split(source, "\n")
	if span.Start.Line <= 0 || span.Start.Line > len(lines) {
		return ""
	}
	line := lines[span.Start.Line-1]
	width := span.End.Column - span.Start.Column
	if width <= 0 {
		width = 1
	}
	return line + "\n" + strings.Repeat(" ", span.Start.Column-1) + strings.Repeat("^", width)
}
