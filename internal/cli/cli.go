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

	"github.com/norunners/tue/internal/compiler/checker"
	"github.com/norunners/tue/internal/compiler/gogen"
	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	compilerTemplate "github.com/norunners/tue/internal/compiler/template"
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
	case "build":
		return runBuild(args[1:], stdout, stderr)
	case "dev", "fmt":
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

	parsedFiles, parseDiagnostics, err := parseTueFiles(root, files)
	if err != nil {
		fmt.Fprintf(stderr, "tue check: %v\n", err)
		return exitError
	}
	if len(parseDiagnostics) != 0 {
		printDiagnostics(stderr, parseDiagnostics)
		return exitError
	}

	checkDiagnostics := checker.CheckProject(checker.Project{Files: parsedFiles})
	if len(checkDiagnostics) != 0 {
		printDiagnostics(stderr, checkDiagnostics)
		return exitError
	}

	fmt.Fprintf(stdout, "tue check: checked %d .tue file(s) in %s\n", len(files), filepath.Clean(root))
	for _, file := range files {
		fmt.Fprintf(stdout, "%s\n", file)
	}

	return exitOK
}

func runBuild(args []string, stdout, stderr io.Writer) int {
	root, code, ok := parseProjectRoot("build", args, stdout, stderr)
	if !ok {
		return code
	}

	files, err := discoverTueFiles(root)
	if err != nil {
		fmt.Fprintf(stderr, "tue build: %v\n", err)
		return exitError
	}

	parsedFiles, parseDiagnostics, err := parseParsedTueFiles(root, files)
	if err != nil {
		fmt.Fprintf(stderr, "tue build: %v\n", err)
		return exitError
	}
	if len(parseDiagnostics) != 0 {
		printDiagnostics(stderr, parseDiagnostics)
		return exitError
	}

	checkFiles := make([]checker.File, len(parsedFiles))
	gogenFiles := make([]gogen.File, len(parsedFiles))
	for i, file := range parsedFiles {
		checkFiles[i] = file.CheckerFile
		gogenFiles[i] = file.gogenFile()
	}

	checkDiagnostics := checker.CheckProject(checker.Project{Files: checkFiles})
	if len(checkDiagnostics) != 0 {
		printDiagnostics(stderr, checkDiagnostics)
		return exitError
	}

	build, buildDiagnostics, err := gogen.WriteProductionProject(root, gogen.Project{
		Root:  root,
		Files: gogenFiles,
	})
	if err != nil {
		fmt.Fprintf(stderr, "tue build: %v\n", err)
		return exitError
	}
	if len(buildDiagnostics) != 0 {
		printDiagnostics(stderr, gogenDiagnosticsFor(buildDiagnostics))
		return exitError
	}

	fmt.Fprintf(stdout, "tue build: generated %d component(s) in %s\n", len(build.Manifest.Files), filepath.Join(filepath.Clean(root), gogen.DistDir))
	fmt.Fprintf(stdout, "tue build: app.wasm %d byte(s)\n", build.WASMSizeBytes)
	for _, file := range build.Files {
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

type parsedTueFile struct {
	CheckerFile  checker.File
	ScriptSource string
	Style        *gogen.Style
}

func (f parsedTueFile) gogenFile() gogen.File {
	return gogen.File{
		Path:         f.CheckerFile.Path,
		Template:     f.CheckerFile.Template,
		Script:       f.CheckerFile.Script,
		ScriptSource: f.ScriptSource,
		Style:        f.Style,
	}
}

func parseTueFiles(root string, paths []string) ([]checker.File, []checker.Diagnostic, error) {
	parsedFiles, diagnostics, err := parseParsedTueFiles(root, paths)
	if err != nil {
		return nil, nil, err
	}

	files := make([]checker.File, len(parsedFiles))
	for i, file := range parsedFiles {
		files[i] = file.CheckerFile
	}
	return files, diagnostics, nil
}

func parseParsedTueFiles(root string, paths []string) ([]parsedTueFile, []checker.Diagnostic, error) {
	files := make([]parsedTueFile, 0, len(paths))
	var diagnostics []checker.Diagnostic

	for _, path := range paths {
		file, fileDiagnostics, err := parseTueFile(root, path)
		if err != nil {
			return nil, nil, err
		}
		diagnostics = append(diagnostics, fileDiagnostics...)
		if len(fileDiagnostics) == 0 {
			if file == nil {
				return nil, nil, fmt.Errorf("parse %s: missing parsed file", path)
			}
			files = append(files, *file)
		}
	}

	return files, diagnostics, nil
}

func parseTueFile(root string, path string) (*parsedTueFile, []checker.Diagnostic, error) {
	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}

	sfcFile, sfcDiagnostics := sfc.Parse(path, source)
	if len(sfcDiagnostics) != 0 {
		return nil, sfcDiagnosticsFor(path, sfcDiagnostics), nil
	}

	templateTree, templateDiagnostics := compilerTemplate.ParseBlock(sfcFile.Template)
	scriptFile, scriptDiagnostics := script.ParseSFC(sfcFile)
	diagnostics := make([]checker.Diagnostic, 0, len(templateDiagnostics)+len(scriptDiagnostics))
	diagnostics = append(diagnostics, templateDiagnosticsFor(path, templateDiagnostics)...)
	diagnostics = append(diagnostics, scriptDiagnosticsFor(path, scriptDiagnostics)...)
	if len(diagnostics) != 0 {
		return nil, diagnostics, nil
	}

	return &parsedTueFile{
		CheckerFile: checker.File{
			Path:     path,
			Template: templateTree,
			Script:   scriptFile,
		},
		ScriptSource: sfcFile.Script.Content,
		Style:        gogen.StyleFromBlock(sfcFile.Style),
	}, nil, nil
}

func sfcDiagnosticsFor(path string, diagnostics []sfc.Diagnostic) []checker.Diagnostic {
	converted := make([]checker.Diagnostic, len(diagnostics))
	for i, diagnostic := range diagnostics {
		converted[i] = checker.Diagnostic{
			Path:    path,
			Message: diagnostic.Message,
			Span:    diagnostic.Span,
		}
	}
	return converted
}

func templateDiagnosticsFor(path string, diagnostics []compilerTemplate.Diagnostic) []checker.Diagnostic {
	converted := make([]checker.Diagnostic, len(diagnostics))
	for i, diagnostic := range diagnostics {
		converted[i] = checker.Diagnostic{
			Path:    path,
			Message: diagnostic.Message,
			Span:    diagnostic.Span,
		}
	}
	return converted
}

func scriptDiagnosticsFor(path string, diagnostics []script.Diagnostic) []checker.Diagnostic {
	converted := make([]checker.Diagnostic, len(diagnostics))
	for i, diagnostic := range diagnostics {
		converted[i] = checker.Diagnostic{
			Path:    path,
			Message: diagnostic.Message,
			Span:    diagnostic.Span,
		}
	}
	return converted
}

func gogenDiagnosticsFor(diagnostics []gogen.Diagnostic) []checker.Diagnostic {
	converted := make([]checker.Diagnostic, len(diagnostics))
	for i, diagnostic := range diagnostics {
		converted[i] = checker.Diagnostic{
			Path:    diagnostic.Path,
			Message: diagnostic.Message,
			Span:    diagnostic.Span,
		}
	}
	return converted
}

func printDiagnostics(stderr io.Writer, diagnostics []checker.Diagnostic) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Span.Start.Line > 0 && diagnostic.Span.Start.Column > 0 {
			fmt.Fprintf(stderr, "%s:%d:%d: %s\n", diagnostic.Path, diagnostic.Span.Start.Line, diagnostic.Span.Start.Column, diagnostic.Message)
			continue
		}
		if diagnostic.Path != "" {
			fmt.Fprintf(stderr, "%s: %s\n", diagnostic.Path, diagnostic.Message)
			continue
		}
		fmt.Fprintf(stderr, "%s\n", diagnostic.Message)
	}
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
  check [project-root]  Parse and check .tue files under a project root.
  build [project-root]  Generate Go files under .tue-cache.
  dev [project-root]    Start the Tue dev server. Not implemented yet.
  fmt [project-root]    Format Tue source files. Not implemented yet.
`)
}

func printCommandUsage(command string, out io.Writer) {
	fmt.Fprintf(out, "Usage:\n  tue %s [project-root]\n", command)
}
