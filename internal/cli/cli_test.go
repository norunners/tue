package cli

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

//go:embed testdata/*.tue
var testFixtures embed.FS

func TestRunRequiresCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(nil, &stdout, &stderr)

	if code != exitUsage {
		t.Errorf("Run(nil) exit code actual = %d, expected %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout actual = %q, expected empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr actual = %q, expected usage", stderr.String())
	}
}

func TestRunPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"--help"}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(--help) exit code actual = %d, expected %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "tue <command>") {
		t.Errorf("stdout actual = %q, expected top-level usage", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr actual = %q, expected empty", stderr.String())
	}
}

func TestRunPrintsCommandHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", "--help"}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(check --help) exit code actual = %d, expected %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "tue check [project-root]") {
		t.Errorf("stdout actual = %q, expected command usage", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr actual = %q, expected empty", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"create"}, &stdout, &stderr)

	if code != exitUsage {
		t.Errorf("Run(create) exit code actual = %d, expected %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout actual = %q, expected empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unknown command "create"`) {
		t.Errorf("stderr actual = %q, expected unknown command", stderr.String())
	}
}

func TestRunDevPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"dev", "--help"}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(dev --help) exit code actual = %d, expected %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "tue dev [flags] [project-root]") {
		t.Errorf("stdout actual = %q, expected dev usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "-addr string") {
		t.Errorf("stdout actual = %q, expected dev flags", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr actual = %q, expected empty", stderr.String())
	}
}

func TestRunFmtPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"fmt", "--help"}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(fmt --help) exit code actual = %d, expected %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "tue fmt [project-root]") {
		t.Errorf("stdout actual = %q, expected fmt usage", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr actual = %q, expected empty", stderr.String())
	}
}

func TestRunFmtFormatsTueFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "App.tue")
	if err := writeFixture(path, "testdata/FmtUnformattedApp.tue"); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}
	expected, err := cliFixture("testdata/FmtFormattedApp.tue")
	if err != nil {
		t.Fatalf("read expected fixture: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"fmt", root}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(fmt) exit code actual = %d, expected %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr actual = %q, expected empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "tue fmt: formatted 1 .tue file(s)") {
		t.Errorf("stdout actual = %q, expected formatted summary", stdout.String())
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read formatted App.tue: %v", err)
	}
	if diff := cmp.Diff(expected, string(actual)); diff != "" {
		t.Errorf("mismatch formatted App.tue (-expected, +actual):\n%s", diff)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"fmt", root}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(fmt idempotent) exit code actual = %d, expected %d; stderr = %q", code, exitOK, stderr.String())
	}
	actual, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read reformatted App.tue: %v", err)
	}
	if diff := cmp.Diff(expected, string(actual)); diff != "" {
		t.Errorf("mismatch reformatted App.tue (-expected, +actual):\n%s", diff)
	}
}

func TestRunFmtPreservesTemplateTextWhitespace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "App.tue")
	if err := writeFixture(path, "testdata/FmtWhitespaceUnformattedApp.tue"); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}
	expected, err := cliFixture("testdata/FmtWhitespaceFormattedApp.tue")
	if err != nil {
		t.Fatalf("read expected fixture: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"fmt", root}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(fmt) exit code actual = %d, expected %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr actual = %q, expected empty", stderr.String())
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read formatted App.tue: %v", err)
	}
	if diff := cmp.Diff(expected, string(actual)); diff != "" {
		t.Errorf("mismatch whitespace-sensitive formatted App.tue (-expected, +actual):\n%s", diff)
	}
}

func TestRunFmtReportsDiagnosticsWithoutWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "App.tue")
	source, err := cliFixture("testdata/FmtInvalidTemplate.tue")
	if err != nil {
		t.Fatalf("read invalid fixture: %v", err)
	}
	if err := writeFile(path, source); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"fmt", root}, &stdout, &stderr)

	if code != exitError {
		t.Errorf("Run(fmt invalid) exit code actual = %d, expected %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout actual = %q, expected empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `App.tue:2:2: missing closing </main> tag`) {
		t.Errorf("stderr actual = %q, expected template diagnostic", stderr.String())
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read invalid App.tue: %v", err)
	}
	if diff := cmp.Diff(source, string(actual)); diff != "" {
		t.Errorf("mismatch invalid App.tue after fmt (-expected, +actual):\n%s", diff)
	}
}

func TestBuildDevProjectWritesClient(t *testing.T) {
	root := t.TempDir()
	if err := writeFixture(filepath.Join(root, "App.tue"), "testdata/App.tue"); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}

	event := buildDevProject(root, devEventTypeReady)

	if event.Type != devEventTypeReady {
		t.Errorf("dev event type actual = %q, expected %q; event = %#v", event.Type, devEventTypeReady, event)
	}
	index, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatalf("read dev index: %v", err)
	}
	if !strings.Contains(string(index), `<script src="/tue_dev.js" defer></script>`) {
		t.Errorf("index.html actual = %q, expected dev client script", string(index))
	}
	client, err := os.ReadFile(filepath.Join(root, "dist", "tue_dev.js"))
	if err != nil {
		t.Fatalf("read dev client: %v", err)
	}
	if !strings.Contains(string(client), `new EventSource("/__tue/events")`) {
		t.Errorf("tue_dev.js actual = %q, expected reload stream", string(client))
	}
}

func TestBuildDevProjectWritesErrorFallback(t *testing.T) {
	root := t.TempDir()
	if err := writeFixture(filepath.Join(root, "App.tue"), "testdata/InvalidBuildApp.tue"); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}

	event := buildDevProject(root, devEventTypeReady)

	if event.Type != devEventTypeError {
		t.Errorf("dev event type actual = %q, expected %q; event = %#v", event.Type, devEventTypeError, event)
	}
	if len(event.Diagnostics) != 1 {
		t.Errorf("diagnostics actual = %#v, expected one diagnostic", event.Diagnostics)
	}
	index, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatalf("read dev error index: %v", err)
	}
	if !strings.Contains(string(index), `<script src="/tue_dev.js" defer></script>`) {
		t.Errorf("index.html actual = %q, expected dev client script", string(index))
	}
	client, err := os.ReadFile(filepath.Join(root, "dist", "tue_dev.js"))
	if err != nil {
		t.Fatalf("read dev client: %v", err)
	}
	if !strings.Contains(string(client), "Tue dev error") {
		t.Errorf("tue_dev.js actual = %q, expected error overlay client", string(client))
	}
}

func TestClassifyDevEventTypeReportsStyleOnlyChanges(t *testing.T) {
	before, err := cliFixture("testdata/StyledApp.tue")
	if err != nil {
		t.Fatalf("read styled fixture: %v", err)
	}
	after := strings.Replace(before, "color: red;", "color: blue;", 1)
	changes := []devFileChange{{
		Path:   "App.tue",
		Before: &devWatchedFile{Source: []byte(before)},
		After:  &devWatchedFile{Source: []byte(after)},
	}}

	actual := classifyDevEventType(changes)

	if actual != devEventTypeStyle {
		t.Errorf("dev event type actual = %q, expected %q", actual, devEventTypeStyle)
	}
}

func TestClassifyDevEventTypeReloadsForTemplateChanges(t *testing.T) {
	before, err := cliFixture("testdata/App.tue")
	if err != nil {
		t.Fatalf("read app fixture: %v", err)
	}
	after := strings.Replace(before, "<p>", "<main>", 1)
	after = strings.Replace(after, "</p>", "</main>", 1)
	changes := []devFileChange{{
		Path:   "App.tue",
		Before: &devWatchedFile{Source: []byte(before)},
		After:  &devWatchedFile{Source: []byte(after)},
	}}

	actual := classifyDevEventType(changes)

	if actual != devEventTypeReload {
		t.Errorf("dev event type actual = %q, expected %q", actual, devEventTypeReload)
	}
}

func TestDevBroadcasterUnsubscribeDoesNotCloseClientChannel(t *testing.T) {
	broadcaster := newDevBroadcaster()
	client, _ := broadcaster.subscribe()

	broadcaster.unsubscribe(client)

	select {
	case _, ok := <-client:
		if !ok {
			t.Errorf("client channel closed after unsubscribe")
		}
	default:
	}
}

func TestDevBroadcasterPublishWhileUnsubscribing(t *testing.T) {
	broadcaster := newDevBroadcaster()
	start := make(chan struct{})
	var publishers sync.WaitGroup
	for range 4 {
		publishers.Add(1)
		go func() {
			defer publishers.Done()
			<-start
			for range 1000 {
				broadcaster.publish(devEvent{Type: devEventTypeReload})
			}
		}()
	}

	var subscribers sync.WaitGroup
	for range 100 {
		subscribers.Add(1)
		go func() {
			defer subscribers.Done()
			<-start
			for range 100 {
				client, _ := broadcaster.subscribe()
				broadcaster.publish(devEvent{Type: devEventTypeStyle})
				broadcaster.unsubscribe(client)
			}
		}()
	}

	close(start)
	subscribers.Wait()
	publishers.Wait()
}

func TestRunBuildGeneratesDistFiles(t *testing.T) {
	root := t.TempDir()
	if err := writeFixture(filepath.Join(root, "App.tue"), "testdata/App.tue"); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"build", root}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(build) exit code actual = %d, expected %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr actual = %q, expected empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "tue build: generated 1 component(s)") {
		t.Errorf("stdout actual = %q, expected generated summary", stdout.String())
	}
	if !strings.Contains(stdout.String(), "app.wasm") || !strings.Contains(stdout.String(), "byte(s)") {
		t.Errorf("stdout actual = %q, expected WASM size report", stdout.String())
	}
	for _, path := range []string{"app.wasm", "index.html", "manifest.json", "style.css", "tue_loader.js"} {
		if _, err := os.Stat(filepath.Join(root, "dist", path)); err != nil {
			t.Errorf("dist file %s should exist: %v", path, err)
		}
	}
}

func TestRunBuildGeneratesStylesheet(t *testing.T) {
	root := t.TempDir()
	if err := writeFixture(filepath.Join(root, "App.tue"), "testdata/StyledApp.tue"); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"build", root}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(build) exit code actual = %d, expected %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr actual = %q, expected empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "style.css") {
		t.Errorf("stdout actual = %q, expected stylesheet path", stdout.String())
	}

	source, err := os.ReadFile(filepath.Join(root, "dist", "style.css"))
	if err != nil {
		t.Fatalf("read generated stylesheet: %v", err)
	}
	expected := ".page[data-tue-c-d8d60a14]"
	if !strings.Contains(string(source), expected) {
		t.Errorf("style.css actual = %q, expected scoped selector %q", string(source), expected)
	}
}

func TestRunBuildGeneratesAssets(t *testing.T) {
	root := t.TempDir()
	if err := writeFixture(filepath.Join(root, "App.tue"), "testdata/AssetApp.tue"); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}
	if err := writeFile(filepath.Join(root, "logo.svg"), "<svg>logo</svg>\n"); err != nil {
		t.Fatalf("setup logo.svg: %v", err)
	}
	if err := writeFile(filepath.Join(root, "hero.png"), "hero\n"); err != nil {
		t.Fatalf("setup hero.png: %v", err)
	}
	if err := writeFile(filepath.Join(root, "public", "favicon.svg"), "<svg>favicon</svg>\n"); err != nil {
		t.Fatalf("setup public favicon.svg: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"build", root}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(build) exit code actual = %d, expected %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr actual = %q, expected empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "assets/logo.") {
		t.Errorf("stdout actual = %q, expected hashed logo asset", stdout.String())
	}
	if !strings.Contains(stdout.String(), "favicon.svg") {
		t.Errorf("stdout actual = %q, expected public favicon asset", stdout.String())
	}

	logos, err := filepath.Glob(filepath.Join(root, "dist", "assets", "logo.*.svg"))
	if err != nil {
		t.Fatalf("glob generated logo asset: %v", err)
	}
	if len(logos) != 1 {
		t.Errorf("generated logo assets actual = %#v, expected exactly one", logos)
	}
	heroes, err := filepath.Glob(filepath.Join(root, "dist", "assets", "hero.*.png"))
	if err != nil {
		t.Fatalf("glob generated hero asset: %v", err)
	}
	if len(heroes) != 1 {
		t.Errorf("generated hero assets actual = %#v, expected exactly one", heroes)
	}
	if _, err := os.ReadFile(filepath.Join(root, "dist", "favicon.svg")); err != nil {
		t.Errorf("generated public favicon should exist: %v", err)
	}

	style, err := os.ReadFile(filepath.Join(root, "dist", "style.css"))
	if err != nil {
		t.Fatalf("read generated stylesheet: %v", err)
	}
	if !strings.Contains(string(style), "assets/hero.") {
		t.Errorf("style.css actual = %q, expected hashed hero URL", string(style))
	}
}

func TestRunBuildReportsGenerationDiagnostics(t *testing.T) {
	root := t.TempDir()
	if err := writeFile(filepath.Join(root, "App.tue"), `<template><button :title="kind">Save</button></template>
<script lang="go">
package app

type App struct {
	kind string
}
</script>
`); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"build", root}, &stdout, &stderr)

	if code != exitError {
		t.Errorf("Run(build invalid) exit code actual = %d, expected %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout actual = %q, expected empty", stdout.String())
	}
	expected := `App.tue:1:19: bound attribute ":title" generation is not supported in the static render slice`
	if !strings.Contains(stderr.String(), expected) {
		t.Errorf("stderr actual = %q, expected %q", stderr.String(), expected)
	}
}

func TestRunCheckDiscoversTueFiles(t *testing.T) {
	root := t.TempDir()
	if err := writeFixture(filepath.Join(root, "App.tue"), "testdata/App.tue"); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}
	if err := writeFile(filepath.Join(root, "notes.txt"), "not a component"); err != nil {
		t.Fatalf("setup notes.txt: %v", err)
	}
	if err := writeFixture(filepath.Join(root, "components", "UserBadge.tue"), "testdata/UserBadge.tue"); err != nil {
		t.Fatalf("setup UserBadge.tue: %v", err)
	}
	if err := writeFixture(filepath.Join(root, ".git", "ignored.tue"), "testdata/App.tue"); err != nil {
		t.Fatalf("setup ignored .git fixture: %v", err)
	}
	if err := writeFixture(filepath.Join(root, ".tue-cache", "generated.tue"), "testdata/App.tue"); err != nil {
		t.Fatalf("setup ignored .tue-cache fixture: %v", err)
	}
	if err := writeFixture(filepath.Join(root, "node_modules", "ignored.tue"), "testdata/App.tue"); err != nil {
		t.Fatalf("setup ignored node_modules fixture: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", root}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(check) exit code actual = %d, expected %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr actual = %q, expected empty", stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("stdout lines actual = %#v, expected summary plus 2 files", lines)
	}
	if len(lines) > 0 && !strings.Contains(lines[0], "checked 2 .tue file(s)") {
		t.Errorf("summary actual = %q, expected checked count", lines[0])
	}
	if len(lines) > 2 && (lines[1] != "App.tue" || lines[2] != "components/UserBadge.tue") {
		t.Errorf("file lines actual = %#v, expected sorted relative paths", lines[1:])
	}
}

func TestRunCheckDefaultsToWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := writeFixture(filepath.Join(root, "App.tue"), "testdata/App.tue"); err != nil {
		t.Fatalf("setup App.tue: %v", err)
	}
	t.Chdir(root)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check"}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(check) exit code actual = %d, expected %d; stderr = %q", code, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "App.tue") {
		t.Errorf("stdout actual = %q, expected default-root file", stdout.String())
	}
}

func TestRunCheckReportsDiagnostics(t *testing.T) {
	root := t.TempDir()
	if err := writeFixture(filepath.Join(root, "InvalidParent.tue"), "testdata/InvalidParent.tue"); err != nil {
		t.Fatalf("setup InvalidParent.tue: %v", err)
	}
	if err := writeFixture(filepath.Join(root, "UserBadge.tue"), "testdata/UserBadge.tue"); err != nil {
		t.Fatalf("setup UserBadge.tue: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", root}, &stdout, &stderr)

	if code != exitError {
		t.Errorf("Run(check invalid) exit code actual = %d, expected %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout actual = %q, expected empty", stdout.String())
	}
	expected := `InvalidParent.tue:2:3: component "UserBadge" requires prop "name"`
	if !strings.Contains(stderr.String(), expected) {
		t.Errorf("stderr actual = %q, expected %q", stderr.String(), expected)
	}
}

func TestRunCheckRejectsInvalidProjectRoot(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", filepath.Join(t.TempDir(), "missing")}, &stdout, &stderr)

	if code != exitError {
		t.Errorf("Run(check missing) exit code actual = %d, expected %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout actual = %q, expected empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "stat project root") {
		t.Errorf("stderr actual = %q, expected stat error", stderr.String())
	}
}

func TestRunCheckRejectsExtraProjectRoots(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", "one", "two"}, &stdout, &stderr)

	if code != exitUsage {
		t.Errorf("Run(check one two) exit code actual = %d, expected %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout actual = %q, expected empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "expected at most one project root") {
		t.Errorf("stderr actual = %q, expected arity error", stderr.String())
	}
}

func writeFixture(path string, fixture string) error {
	contents, err := cliFixture(fixture)
	if err != nil {
		return err
	}
	return writeFile(path, contents)
}

func writeFile(path string, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func cliFixture(path string) (string, error) {
	source, err := testFixtures.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read embedded fixture %s: %w", path, err)
	}
	return string(source), nil
}
