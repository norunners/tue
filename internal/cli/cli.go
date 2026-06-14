package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const (
	exitOK = iota
	exitError
	exitUsage
)

// Run executes the Tue CLI with args that do not include the binary name.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitUsage
	}

	switch args[0] {
	case "-h", "--help", "help":
		printUsage(stdout)
		return exitOK
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "build", "dev", "fmt":
		return runStub(args[0], args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "tue: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return exitUsage
	}
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	root, code, ok := parseProjectRoot("check", args, stdout, stderr)
	if !ok {
		return code
	}

	files, err := discoverTueFiles(root)
	if err != nil {
		fmt.Fprintf(stderr, "tue check: %v\n", err)
		return exitError
	}

	fmt.Fprintf(stdout, "tue check: found %d .tue file(s) in %s\n", len(files), filepath.Clean(root))
	for _, file := range files {
		fmt.Fprintf(stdout, "%s\n", file)
	}

	return exitOK
}

func runStub(command string, args []string, stdout, stderr io.Writer) int {
	if _, code, ok := parseProjectRoot(command, args, stdout, stderr); !ok {
		return code
	}

	fmt.Fprintf(stderr, "tue %s: not implemented yet\n", command)
	return exitError
}

func parseProjectRoot(command string, args []string, stdout, stderr io.Writer) (string, int, bool) {
	flags := flag.NewFlagSet("tue "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printCommandUsage(command, stdout)
			return "", exitOK, false
		}
		fmt.Fprintf(stderr, "tue %s: %v\n\n", command, err)
		printCommandUsage(command, stderr)
		return "", exitUsage, false
	}

	if flags.NArg() > 1 {
		fmt.Fprintf(stderr, "tue %s: expected at most one project root\n\n", command)
		printCommandUsage(command, stderr)
		return "", exitUsage, false
	}

	root := "."
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}

	return root, exitOK, true
}

func discoverTueFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat project root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project root %q is not a directory", root)
	}

	var files []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if path != root && shouldSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if filepath.Ext(path) != ".tue" {
			return nil
		}

		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relativePath))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover .tue files: %w", err)
	}

	sort.Strings(files)
	return files, nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".tue-cache", "node_modules":
		return true
	default:
		return false
	}
}

func printUsage(out io.Writer) {
	fmt.Fprint(out, `Usage:
  tue <command> [project-root]

Commands:
  check [project-root]  Discover .tue files under a project root.
  build [project-root]  Build a Tue project. Not implemented yet.
  dev [project-root]    Start the Tue dev server. Not implemented yet.
  fmt [project-root]    Format Tue source files. Not implemented yet.
`)
}

func printCommandUsage(command string, out io.Writer) {
	fmt.Fprintf(out, "Usage:\n  tue %s [project-root]\n", command)
}
