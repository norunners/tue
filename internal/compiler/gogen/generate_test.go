package gogen

import (
	"embed"
	"encoding/json"
	"fmt"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
)

//go:embed testdata/static/*.tue testdata/dynamic/*.tue testdata/golden/*.go
var testFixtures embed.FS

func TestGenerateProjectEmitsStaticRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/static/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"App_tue.go", "App_render_tue.go"}, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}
	expectedScript, err := testFixtureString("testdata/golden/App_tue.go")
	if err != nil {
		t.Fatalf("read expected script fixture: %v", err)
	}
	actualScript, err := generatedSource(result, "App_tue.go")
	if err != nil {
		t.Fatalf("read actual generated script: %v", err)
	}
	if diff := cmp.Diff(expectedScript, string(actualScript)); diff != "" {
		t.Errorf("mismatch generated script (-expected, +actual):\n%s", diff)
	}
	expectedRender, err := testFixtureString("testdata/golden/App_render_tue.go")
	if err != nil {
		t.Fatalf("read expected render fixture: %v", err)
	}
	actualRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated render: %v", err)
	}
	if diff := cmp.Diff(expectedRender, string(actualRender)); diff != "" {
		t.Errorf("mismatch generated render (-expected, +actual):\n%s", diff)
	}

	for _, file := range result.Files {
		if _, err := goparser.ParseFile(token.NewFileSet(), file.Path, file.Source, goparser.AllErrors); err != nil {
			t.Errorf("generated file %s should parse: %v", file.Path, err)
		}
	}

	if diff := cmp.Diff(manifestSummary{
		GeneratedBy: "tue",
		Files: []manifestFileSummary{{
			Source:     "App.tue",
			Component:  "App",
			ScriptFile: "App_tue.go",
			RenderFile: "App_render_tue.go",
			Nodes: []manifestNodeSummary{
				{Kind: "text", Line: 2, Column: 39},
				{Kind: "interpolation", Line: 2, Column: 46},
				{Kind: "text", Line: 2, Column: 56},
				{Kind: "interpolation", Line: 3, Column: 8},
				{Kind: "element", Tag: "span", Line: 3, Column: 2},
				{Kind: "element", Tag: "main", Line: 2, Column: 1},
			},
		}},
	}, summarizeManifest(result.Manifest)); diff != "" {
		t.Errorf("mismatch manifest (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsUnsupportedStaticSliceConstructs(t *testing.T) {
	project, err := parseProjectFixture("testdata/dynamic/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(project)

	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "App.tue", Message: `directive "v-if" generation is not supported in the static render slice`, Line: 2, Column: 9},
		{Path: "App.tue", Message: `event attribute "@click" generation is not supported in the static render slice`, Line: 2, Column: 24},
		{Path: "App.tue", Message: `bound attribute ":class" generation is not supported in the static render slice`, Line: 2, Column: 38},
		{Path: "App.tue", Message: `component "UserBadge" generation is not supported in the static render slice`, Line: 3, Column: 2},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestWriteProjectWritesCacheFilesAndManifest(t *testing.T) {
	root := t.TempDir()
	project, err := parseProjectFixture("testdata/static/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	manifest, diagnostics, err := WriteProject(root, project)
	if err != nil {
		t.Fatalf("WriteProject returned error: %v", err)
	}
	if manifest == nil {
		t.Fatal("WriteProject manifest is nil")
	}
	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(1, len(manifest.Files)); diff != "" {
		t.Errorf("mismatch manifest file count (-expected, +actual):\n%s", diff)
	}

	for _, path := range []string{"App_tue.go", "App_render_tue.go", "manifest.json"} {
		if _, err := os.ReadFile(filepath.Join(root, CacheDir, path)); err != nil {
			t.Errorf("generated file %s should exist: %v", path, err)
		}
	}

	manifestSource, err := os.ReadFile(filepath.Join(root, CacheDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var decoded Manifest
	if err := json.Unmarshal(manifestSource, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if diff := cmp.Diff(*manifest, decoded); diff != "" {
		t.Errorf("mismatch manifest JSON (-expected, +actual):\n%s", diff)
	}
}

func parseProjectFixture(path string) (Project, error) {
	source, err := testFixture(path)
	if err != nil {
		return Project{}, err
	}
	sfcFile, sfcDiagnostics := sfc.Parse("App.tue", source)
	if len(sfcDiagnostics) != 0 {
		return Project{}, fmt.Errorf("sfc.Parse diagnostics = %#v, expected none", sfcDiagnosticMessages(sfcDiagnostics))
	}

	templateTree, templateDiagnostics := gotemplate.ParseBlock(sfcFile.Template)
	if len(templateDiagnostics) != 0 {
		return Project{}, fmt.Errorf("template.ParseBlock diagnostics = %#v, expected none", templateDiagnosticMessages(templateDiagnostics))
	}

	scriptFile, scriptDiagnostics := script.ParseSFC(sfcFile)
	if len(scriptDiagnostics) != 0 {
		return Project{}, fmt.Errorf("script.ParseSFC diagnostics = %#v, expected none", scriptDiagnosticMessages(scriptDiagnostics))
	}

	return Project{Files: []File{{
		Path:         sfcFile.Path,
		Template:     templateTree,
		Script:       scriptFile,
		ScriptSource: sfcFile.Script.Content,
	}}}, nil
}

func generatedPaths(files []GeneratedFile) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}

func generatedSource(result *Result, path string) ([]byte, error) {
	for _, file := range result.Files {
		if file.Path == path {
			return file.Source, nil
		}
	}
	return nil, fmt.Errorf("generated file %s not found in %#v", path, generatedPaths(result.Files))
}

func testFixture(path string) ([]byte, error) {
	source, err := testFixtures.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read embedded fixture %s: %w", path, err)
	}
	return source, nil
}

func testFixtureString(path string) (string, error) {
	source, err := testFixture(path)
	if err != nil {
		return "", err
	}
	return string(source), nil
}

type diagnosticSummary struct {
	Path    string
	Message string
	Line    int
	Column  int
}

func summarizeDiagnostics(diagnostics []Diagnostic) []diagnosticSummary {
	summaries := make([]diagnosticSummary, len(diagnostics))
	for i, diagnostic := range diagnostics {
		summaries[i] = diagnosticSummary{
			Path:    diagnostic.Path,
			Message: diagnostic.Message,
			Line:    diagnostic.Span.Start.Line,
			Column:  diagnostic.Span.Start.Column,
		}
	}
	return summaries
}

type manifestSummary struct {
	GeneratedBy string
	Files       []manifestFileSummary
}

type manifestFileSummary struct {
	Source     string
	Component  string
	ScriptFile string
	RenderFile string
	Nodes      []manifestNodeSummary
}

type manifestNodeSummary struct {
	Kind   string
	Tag    string
	Line   int
	Column int
}

func summarizeManifest(manifest Manifest) manifestSummary {
	summary := manifestSummary{
		GeneratedBy: manifest.GeneratedBy,
		Files:       make([]manifestFileSummary, len(manifest.Files)),
	}
	for i, file := range manifest.Files {
		summary.Files[i] = manifestFileSummary{
			Source:     file.Source,
			Component:  file.Component,
			ScriptFile: file.ScriptFile,
			RenderFile: file.RenderFile,
			Nodes:      make([]manifestNodeSummary, len(file.Nodes)),
		}
		for j, node := range file.Nodes {
			summary.Files[i].Nodes[j] = manifestNodeSummary{
				Kind:   node.Kind,
				Tag:    node.Tag,
				Line:   node.SourceSpan.Start.Line,
				Column: node.SourceSpan.Start.Column,
			}
		}
	}
	return summary
}

func sfcDiagnosticMessages(diagnostics []sfc.Diagnostic) []string {
	messages := make([]string, len(diagnostics))
	for i, diagnostic := range diagnostics {
		messages[i] = diagnostic.Message
	}
	return messages
}

func templateDiagnosticMessages(diagnostics []gotemplate.Diagnostic) []string {
	messages := make([]string, len(diagnostics))
	for i, diagnostic := range diagnostics {
		messages[i] = diagnostic.Message
	}
	return messages
}

func scriptDiagnosticMessages(diagnostics []script.Diagnostic) []string {
	messages := make([]string, len(diagnostics))
	for i, diagnostic := range diagnostics {
		messages[i] = diagnostic.Message
	}
	return messages
}
