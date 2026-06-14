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

	"github.com/norunners/vue/internal/compiler/checker"
	"github.com/norunners/vue/internal/compiler/gogen"
	scriptparser "github.com/norunners/vue/internal/compiler/script"
	"github.com/norunners/vue/internal/compiler/sfc"
	templateparser "github.com/norunners/vue/internal/compiler/template"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// Runner owns one CLI invocation. Tests can set Cwd to avoid process-global chdir.
type Runner struct {
	Stdout io.Writer
	Stderr io.Writer
	Cwd    string
}

type Project struct {
	Root  string
	Files []string
}

type loadedFile struct {
	Path     string
	Template *templateparser.Tree
	Contract *scriptparser.Contract
	Script   string
}

func Run(args []string, stdout, stderr io.Writer) int {
	return Runner{
		Stdout: stdout,
		Stderr: stderr,
	}.Run(args)
}

func (r Runner) Run(args []string) int {
	stdout := r.stdout()
	stderr := r.stderr()

	if len(args) == 0 {
		fmt.Fprint(stderr, rootUsage())
		return exitUsage
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, rootUsage())
		return exitOK
	case "check":
		return r.runCheck(args[1:])
	case "build":
		return r.runBuild(args[1:])
	case "dev", "fmt":
		return r.runStub(args[0], args[1:])
	default:
		fmt.Fprintf(stderr, "tue: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, rootUsage())
		return exitUsage
	}
}

func (r Runner) runCheck(args []string) int {
	stdout := r.stdout()
	stderr := r.stderr()

	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stderr, checkUsage())
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "tue check: expected at most one project root")
		fmt.Fprint(stderr, checkUsage())
		return exitUsage
	}

	root := "."
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}

	project, err := r.DiscoverProject(root)
	if err != nil {
		fmt.Fprintf(stderr, "tue check: %v\n", err)
		return exitError
	}

	diagnostics, err := r.parseProject(project)
	if err != nil {
		fmt.Fprintf(stderr, "tue check: %v\n", err)
		return exitError
	}
	if len(diagnostics) > 0 {
		for _, diagnostic := range diagnostics {
			fmt.Fprintf(stderr, "%s\n", diagnostic)
		}
		return exitError
	}

	fmt.Fprintf(stdout, "found %d .tue file(s) under %s\n", len(project.Files), project.Root)
	for _, file := range project.Files {
		fmt.Fprintf(stdout, "%s\n", file)
	}

	return exitOK
}

func (r Runner) runBuild(args []string) int {
	stdout := r.stdout()
	stderr := r.stderr()

	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stderr, buildUsage())
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "tue build: expected at most one project root")
		fmt.Fprint(stderr, buildUsage())
		return exitUsage
	}

	root := "."
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}

	project, err := r.DiscoverProject(root)
	if err != nil {
		fmt.Fprintf(stderr, "tue build: %v\n", err)
		return exitError
	}

	files, diagnostics, err := r.loadProject(project)
	if err != nil {
		fmt.Fprintf(stderr, "tue build: %v\n", err)
		return exitError
	}
	if len(diagnostics) == 0 {
		diagnostics = append(diagnostics, checker.Check(checkerFiles(files))...)
	}
	if len(diagnostics) > 0 {
		for _, diagnostic := range diagnostics {
			fmt.Fprintf(stderr, "%s\n", diagnostic)
		}
		return exitError
	}

	generated, err := gogen.GenerateProject(gogenFiles(files))
	if err != nil {
		var generateErr *gogen.Error
		if errors.As(err, &generateErr) {
			fmt.Fprintf(stderr, "%s\n", generateErr)
			return exitError
		}
		fmt.Fprintf(stderr, "tue build: %v\n", err)
		return exitError
	}
	if err := gogen.WriteProject(project.Root, generated); err != nil {
		fmt.Fprintf(stderr, "tue build: %v\n", err)
		return exitError
	}

	fmt.Fprintf(stdout, "generated %d file(s) under %s\n", len(generated.Files), filepath.Join(project.Root, ".tue-cache"))
	return exitOK
}

func (r Runner) runStub(command string, args []string) int {
	stderr := r.stderr()

	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(stderr, "Usage: tue %s [project-root]\n", command)
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() > 1 {
		fmt.Fprintf(stderr, "tue %s: expected at most one project root\n", command)
		flags.Usage()
		return exitUsage
	}

	fmt.Fprintf(stderr, "tue %s: not implemented yet\n", command)
	return exitError
}

func (r Runner) DiscoverProject(root string) (Project, error) {
	resolvedRoot, err := r.resolveRoot(root)
	if err != nil {
		return Project{}, err
	}

	var files []string
	err = filepath.WalkDir(resolvedRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk %s: %w", path, walkErr)
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".tue-cache", "dist", "node_modules":
				if path != resolvedRoot {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(entry.Name()) != ".tue" {
			return nil
		}

		rel, err := filepath.Rel(resolvedRoot, path)
		if err != nil {
			return fmt.Errorf("relativize %s: %w", path, err)
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return Project{}, err
	}

	sort.Strings(files)
	return Project{
		Root:  resolvedRoot,
		Files: files,
	}, nil
}

func (r Runner) parseProject(project Project) ([]sfc.Diagnostic, error) {
	files, diagnostics, err := r.loadProject(project)
	if err != nil {
		return nil, err
	}
	if len(diagnostics) == 0 {
		diagnostics = append(diagnostics, checker.Check(checkerFiles(files))...)
	}
	return diagnostics, nil
}

func (r Runner) loadProject(project Project) ([]loadedFile, []sfc.Diagnostic, error) {
	var diagnostics []sfc.Diagnostic
	var files []loadedFile
	for _, file := range project.Files {
		path := filepath.Join(project.Root, filepath.FromSlash(file))
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file, err)
		}

		parsed, err := sfc.Parse(file, src)
		if err != nil {
			var parseErr *sfc.Error
			if errors.As(err, &parseErr) {
				diagnostics = append(diagnostics, parseErr.Diagnostics...)
				continue
			}
			return nil, nil, fmt.Errorf("parse %s: %w", file, err)
		}

		tree, err := templateparser.ParseBlock(file, parsed.Template)
		if err != nil {
			var parseErr *templateparser.Error
			if errors.As(err, &parseErr) {
				diagnostics = append(diagnostics, parseErr.Diagnostics...)
				continue
			}
			return nil, nil, fmt.Errorf("parse template %s: %w", file, err)
		}

		contract, err := scriptparser.ParseBlock(file, parsed.Script)
		if err != nil {
			var parseErr *scriptparser.Error
			if errors.As(err, &parseErr) {
				diagnostics = append(diagnostics, parseErr.Diagnostics...)
				continue
			}
			return nil, nil, fmt.Errorf("parse script %s: %w", file, err)
		}

		files = append(files, loadedFile{
			Path:     file,
			Template: tree,
			Contract: contract,
			Script:   parsed.Script.Content,
		})
	}
	return files, diagnostics, nil
}

func checkerFiles(files []loadedFile) []checker.File {
	out := make([]checker.File, 0, len(files))
	for _, file := range files {
		out = append(out, checker.File{
			Path:     file.Path,
			Template: file.Template,
			Contract: file.Contract,
		})
	}
	return out
}

func gogenFiles(files []loadedFile) []gogen.File {
	out := make([]gogen.File, 0, len(files))
	for _, file := range files {
		out = append(out, gogen.File{
			Path:     file.Path,
			Template: file.Template,
			Contract: file.Contract,
			Script:   file.Script,
		})
	}
	return out
}

func (r Runner) resolveRoot(root string) (string, error) {
	if root == "" {
		root = "."
	}
	if !filepath.IsAbs(root) {
		cwd, err := r.cwd()
		if err != nil {
			return "", err
		}
		root = filepath.Join(cwd, root)
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat project root %q: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project root %q is not a directory", root)
	}

	return root, nil
}

func (r Runner) cwd() (string, error) {
	if r.Cwd != "" {
		return r.Cwd, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return cwd, nil
}

func (r Runner) stdout() io.Writer {
	if r.Stdout != nil {
		return r.Stdout
	}
	return io.Discard
}

func (r Runner) stderr() io.Writer {
	if r.Stderr != nil {
		return r.Stderr
	}
	return io.Discard
}

func rootUsage() string {
	return `Usage: tue <command> [arguments]

Commands:
  check [project-root]  discover .tue files and report diagnostics
  build [project-root]  build a project for production
  dev [project-root]    run the development server
  fmt [project-root]    format .tue files

Run "tue <command> -h" for command-specific help.
`
}

func checkUsage() string {
	return `Usage: tue check [project-root]

Discover .tue files under project-root and report diagnostics.
The project root defaults to the current directory.
`
}

func buildUsage() string {
	return `Usage: tue build [project-root]

Generate Go files for .tue components under project-root into .tue-cache.
The project root defaults to the current directory.
`
}
