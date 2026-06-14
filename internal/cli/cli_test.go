package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRequiresCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(nil, &stdout, &stderr)

	if code != exitUsage {
		t.Fatalf("Run(nil) exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestRunPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"--help"}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("Run(--help) exit code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "tue <command>") {
		t.Fatalf("stdout = %q, want top-level usage", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunPrintsCommandHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", "--help"}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("Run(check --help) exit code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "tue check [project-root]") {
		t.Fatalf("stdout = %q, want command usage", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"create"}, &stdout, &stderr)

	if code != exitUsage {
		t.Fatalf("Run(create) exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unknown command "create"`) {
		t.Fatalf("stderr = %q, want unknown command", stderr.String())
	}
}

func TestRunStubCommandsReturnNotImplemented(t *testing.T) {
	for _, command := range []string{"build", "dev", "fmt"} {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := Run([]string{command}, &stdout, &stderr)

			if code != exitError {
				t.Fatalf("Run(%s) exit code = %d, want %d", command, code, exitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			want := "tue " + command + ": not implemented yet"
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), want)
			}
		})
	}
}

func TestRunCheckDiscoversTueFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "App.tue"), "<template></template>")
	writeFile(t, filepath.Join(root, "notes.txt"), "not a component")
	writeFile(t, filepath.Join(root, "components", "UserBadge.tue"), "<template></template>")
	writeFile(t, filepath.Join(root, ".git", "ignored.tue"), "<template></template>")
	writeFile(t, filepath.Join(root, ".tue-cache", "generated.tue"), "<template></template>")
	writeFile(t, filepath.Join(root, "node_modules", "ignored.tue"), "<template></template>")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", root}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("Run(check) exit code = %d, want %d; stderr = %q", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("stdout lines = %#v, want summary plus 2 files", lines)
	}
	if !strings.Contains(lines[0], "found 2 .tue file(s)") {
		t.Fatalf("summary = %q, want discovered count", lines[0])
	}
	if lines[1] != "App.tue" || lines[2] != "components/UserBadge.tue" {
		t.Fatalf("file lines = %#v, want sorted relative paths", lines[1:])
	}
}

func TestRunCheckDefaultsToWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "App.tue"), "<template></template>")
	t.Chdir(root)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check"}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("Run(check) exit code = %d, want %d; stderr = %q", code, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "App.tue") {
		t.Fatalf("stdout = %q, want default-root file", stdout.String())
	}
}

func TestRunCheckRejectsInvalidProjectRoot(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", filepath.Join(t.TempDir(), "missing")}, &stdout, &stderr)

	if code != exitError {
		t.Fatalf("Run(check missing) exit code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "stat project root") {
		t.Fatalf("stderr = %q, want stat error", stderr.String())
	}
}

func TestRunCheckRejectsExtraProjectRoots(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"check", "one", "two"}, &stdout, &stderr)

	if code != exitUsage {
		t.Fatalf("Run(check one two) exit code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "expected at most one project root") {
		t.Fatalf("stderr = %q, want arity error", stderr.String())
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
