package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunShowsUsageWithoutCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run(nil)

	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage: tue <command> [arguments]") {
		t.Fatalf("stderr = %q, want root usage", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"wat"})

	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `tue: unknown command "wat"`) {
		t.Fatalf("stderr = %q, want unknown command error", stderr.String())
	}
}

func TestRunDispatchesStubCommands(t *testing.T) {
	for _, command := range []string{"dev", "fmt"} {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := Runner{
				Stdout: &stdout,
				Stderr: &stderr,
			}.Run([]string{command})

			if code != exitError {
				t.Fatalf("exit code = %d, want %d", code, exitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			want := fmt.Sprintf("tue %s: not implemented yet\n", command)
			if stderr.String() != want {
				t.Fatalf("stderr = %q, want %q", stderr.String(), want)
			}
		})
	}
}

func TestBuildGeneratesTueCache(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app.tue", `<template>
  <main class="page">Hello {{ name }}</main>
</template>

<script lang="go">
package fixtures

type App struct {
	name string
}
</script>
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"build", root})

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	want := fmt.Sprintf("generated 3 file(s) under %s\n", filepath.Join(root, ".tue-cache"))
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}

	component := readFile(t, root, ".tue-cache/app_tue.go")
	render := readFile(t, root, ".tue-cache/app_render_tue.go")
	manifest := readFile(t, root, ".tue-cache/manifest.json")
	if !strings.Contains(component, "type App struct") || !strings.Contains(component, "name string") {
		t.Fatalf("component file =\n%s\nwant formatted script copy", component)
	}
	if !strings.Contains(render, "func NewApp() *vue.Comp") ||
		!strings.Contains(render, "return vue.CompOf(c, func() vue.VNode") ||
		!strings.Contains(render, "func appRender(c *App) vue.VNode") ||
		!strings.Contains(render, "vue.Text(fmt.Sprint(c.name))") {
		t.Fatalf("render file =\n%s\nwant constructor and render function with escaped interpolation", render)
	}
	if !strings.Contains(manifest, `"component": "App"`) || !strings.Contains(manifest, `"app_render_tue.go"`) {
		t.Fatalf("manifest =\n%s\nwant generated App metadata", manifest)
	}
}

func TestBuildReportsCheckerDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "broken.tue", `<template>
  <p>{{ missing }}</p>
</template>

<script lang="go">
package fixtures

type Broken struct{}
</script>
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"build", root})

	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `broken.tue:2:9: unknown template symbol "missing"`) {
		t.Fatalf("stderr = %q, want checker diagnostic", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".tue-cache")); !os.IsNotExist(err) {
		t.Fatalf(".tue-cache stat err = %v, want not exist", err)
	}
}

func TestBuildGeneratesEventHandlers(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "button.tue", `<template>
  <button @click="save">Save</button>
</template>

<script lang="go">
package fixtures

type Button struct {
	save func()
}
</script>
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"build", root})

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	want := fmt.Sprintf("generated 3 file(s) under %s\n", filepath.Join(root, ".tue-cache"))
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}

	render := readFile(t, root, ".tue-cache/button_render_tue.go")
	if !strings.Contains(render, `vue.ElementWithEvents("button", nil, []vue.EventBinding{vue.On("click", c.save)},`) {
		t.Fatalf("render file =\n%s\nwant generated event binding", render)
	}
}

func TestCheckDiscoversTueFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "page.tue", validSFC("Page"))
	writeFile(t, root, "nested/component.tue", validSFC("Component"))
	writeFile(t, root, "nested/component.go", "package nested")
	writeFile(t, root, "dist/ignored.tue", "<template></template>")
	writeFile(t, root, ".tue-cache/ignored.tue", "<template></template>")
	writeFile(t, root, "node_modules/ignored.tue", "<template></template>")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"check", root})

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	want := fmt.Sprintf("found 2 .tue file(s) under %s\nnested/component.tue\npage.tue\n", root)
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestCheckDefaultsToRunnerWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app.tue", validSFC("App"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Cwd:    root,
	}.Run([]string{"check"})

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr.String())
	}

	want := fmt.Sprintf("found 1 .tue file(s) under %s\napp.tue\n", root)
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestCheckResolvesRelativeProjectRootFromRunnerWorkingDirectory(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, "app")
	writeFile(t, workspace, "app/main.tue", validSFC("Main"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Cwd:    workspace,
	}.Run([]string{"check", "app"})

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr.String())
	}

	want := fmt.Sprintf("found 1 .tue file(s) under %s\nmain.tue\n", root)
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestCheckRejectsTooManyRoots(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"check", "one", "two"})

	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "tue check: expected at most one project root") {
		t.Fatalf("stderr = %q, want root-count error", stderr.String())
	}
}

func TestCheckRejectsMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"check", root})

	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "stat project root") {
		t.Fatalf("stderr = %q, want stat error", stderr.String())
	}
}

func TestCheckReportsSFCDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "broken.tue", "<template></template>")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"check", root})

	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "broken.tue:1:22: missing required <script lang=\"go\"> block") {
		t.Fatalf("stderr = %q, want SFC diagnostic", stderr.String())
	}
}

func TestCheckReportsTemplateDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "broken.tue", `<template>
	<p>{{ name</p>
</template>

<script lang="go">
package fixtures

type Broken struct{}
</script>
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"check", root})

	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "broken.tue:2:5: malformed interpolation: missing }}") {
		t.Fatalf("stderr = %q, want template diagnostic", stderr.String())
	}
}

func TestCheckReportsScriptDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "broken.tue", `<template>
	<p>Hello</p>
</template>

<script lang="go">
package fixtures

import tue "github.com/norunners/vue"

type Broken struct{}

func (b Broken) Init(ctx tue.Context) {}
</script>
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"check", root})

	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "broken.tue:12:17: Init must have signature func (c *Broken) Init(ctx tue.Context)") {
		t.Fatalf("stderr = %q, want script diagnostic", stderr.String())
	}
}

func TestCheckReportsTemplateTypeDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Parent.tue", `<template>
  <UserBadge :name="name" :isAdmin='"yes"' />
</template>

<script lang="go">
package fixtures

type Parent struct {
	name string
}
</script>
`)
	writeFile(t, root, "UserBadge.tue", `<template>
  <span>{{ name }}</span>
</template>

<script lang="go">
package fixtures

import tue "github.com/norunners/vue"

type UserBadge struct {
	name    tue.Prop[string] `+"`prop:\"name,required\"`"+`
	isAdmin tue.Prop[bool]   `+"`prop:\"isAdmin\"`"+`
}
</script>
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}.Run([]string{"check", root})

	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `Parent.tue:2:37: prop "isAdmin" expects bool, got string`) {
		t.Fatalf("stderr = %q, want checker diagnostic", stderr.String())
	}
}

func writeFile(t *testing.T, root, name, contents string) {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, root, name string) string {
	t.Helper()

	path := filepath.Join(root, name)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(src)
}

func validSFC(componentName string) string {
	return fmt.Sprintf(`<template></template>
<script lang="go">
package fixtures

type %s struct{}
</script>
`, componentName)
}
