package cli

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed testdata/*.tue
var testFixtures embed.FS

func TestRunRequiresCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(nil, &stdout, &stderr)

	if code != exitUsage {
		t.Errorf("Run(nil) exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr = %q, want usage", stderr.String())
	}
}

func TestRunPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"--help"}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(--help) exit code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "tue <command>") {
		t.Errorf("stdout = %q, want top-level usage", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunPrintsCommandHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", "--help"}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("Run(check --help) exit code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "tue check [project-root]") {
		t.Errorf("stdout = %q, want command usage", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"create"}, &stdout, &stderr)

	if code != exitUsage {
		t.Errorf("Run(create) exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unknown command "create"`) {
		t.Errorf("stderr = %q, want unknown command", stderr.String())
	}
}

func TestRunStubCommandsReturnNotImplemented(t *testing.T) {
	for _, command := range []string{"build", "dev", "fmt"} {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := Run([]string{command}, &stdout, &stderr)

			if code != exitError {
				t.Errorf("Run(%s) exit code = %d, want %d", command, code, exitError)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			want := "tue " + command + ": not implemented yet"
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr = %q, want %q", stderr.String(), want)
			}
		})
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
		t.Errorf("Run(check) exit code = %d, want %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("stdout lines = %#v, want summary plus 2 files", lines)
	}
	if len(lines) > 0 && !strings.Contains(lines[0], "checked 2 .tue file(s)") {
		t.Errorf("summary = %q, want checked count", lines[0])
	}
	if len(lines) > 2 && (lines[1] != "App.tue" || lines[2] != "components/UserBadge.tue") {
		t.Errorf("file lines = %#v, want sorted relative paths", lines[1:])
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
		t.Errorf("Run(check) exit code = %d, want %d; stderr = %q", code, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "App.tue") {
		t.Errorf("stdout = %q, want default-root file", stdout.String())
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
		t.Errorf("Run(check invalid) exit code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	want := `InvalidParent.tue:2:3: component "UserBadge" requires prop "name"`
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunCheckRejectsInvalidProjectRoot(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", filepath.Join(t.TempDir(), "missing")}, &stdout, &stderr)

	if code != exitError {
		t.Errorf("Run(check missing) exit code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "stat project root") {
		t.Errorf("stderr = %q, want stat error", stderr.String())
	}
}

func TestRunCheckRejectsExtraProjectRoots(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", "one", "two"}, &stdout, &stderr)

	if code != exitUsage {
		t.Errorf("Run(check one two) exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "expected at most one project root") {
		t.Errorf("stderr = %q, want arity error", stderr.String())
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
