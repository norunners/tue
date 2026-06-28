package gogen

import (
	"embed"
	"encoding/json"
	"fmt"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
)

//go:embed testdata
var testFixtures embed.FS

func TestGenerateProjectEmitsStaticRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/static/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	expectedPaths := []string{"App_tue.go", "App_component_tue.go", "App_render_tue.go"}
	if diff := cmp.Diff(expectedPaths, generatedPaths(result.Files)); diff != "" {
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
	expectedComponent, err := testFixtureString("testdata/golden/App_component_tue.go")
	if err != nil {
		t.Fatalf("read expected declaration fixture: %v", err)
	}
	actualComponent, err := generatedSource(result, "App_component_tue.go")
	if err != nil {
		t.Fatalf("read actual generated component support: %v", err)
	}
	if diff := cmp.Diff(expectedComponent, string(actualComponent)); diff != "" {
		t.Errorf("mismatch generated component support (-expected, +actual):\n%s", diff)
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
			Source:        "App.tue",
			Component:     "App",
			ScriptFile:    "App_tue.go",
			ComponentFile: "App_component_tue.go",
			RenderFile:    "App_render_tue.go",
			Nodes: []manifestNodeSummary{
				{Kind: "text", Line: 2, Column: 40},
				{Kind: "interpolation", Line: 2, Column: 47},
				{Kind: "text", Line: 2, Column: 57},
				{Kind: "interpolation", Line: 3, Column: 8},
				{Kind: "element", Tag: "span", Line: 3, Column: 2},
				{Kind: "element", Tag: "main", Line: 2, Column: 2},
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

	_, diagnostics := GenerateProject(*project)

	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "App.tue", Message: `component "UserBadge" is not registered`, Line: 3, Column: 2},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectEmitsConditionalRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/conditionals/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	expectedDiagnostics := []diagnosticSummary{}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	expectedPaths := []string{"App_tue.go", "App_render_tue.go"}
	if diff := cmp.Diff(expectedPaths, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}
	expectedRender, err := testFixtureString("testdata/golden/Conditional_render_tue.go")
	if err != nil {
		t.Fatalf("read expected conditional render fixture: %v", err)
	}
	actualRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated conditional render: %v", err)
	}
	if diff := cmp.Diff(expectedRender, string(actualRender)); diff != "" {
		t.Errorf("mismatch generated conditional render (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsUnsupportedConditionalExpressions(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_conditionals/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "App.tue", Message: `v-if expression is not supported in the static render slice`, Line: 2, Column: 10},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsConditionalControlDiagnostics(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		expected []diagnosticSummary
	}{
		{
			name:    "conditional chains",
			fixture: "testdata/invalid_conditional_controls/ConditionalChain.tue",
			expected: []diagnosticSummary{
				{Path: "ConditionalChain.tue", Message: `v-else-if must follow v-if or v-else-if`, Line: 3, Column: 6},
				{Path: "ConditionalChain.tue", Message: `v-else must follow v-if or v-else-if`, Line: 4, Column: 6},
				{Path: "ConditionalChain.tue", Message: `v-if expects bool, got string`, Line: 6, Column: 12},
				{Path: "ConditionalChain.tue", Message: `v-else-if expression is not supported in the static render slice`, Line: 7, Column: 17},
			},
		},
		{
			name:    "switch placement",
			fixture: "testdata/invalid_conditional_controls/SwitchPlacement.tue",
			expected: []diagnosticSummary{
				{Path: "SwitchPlacement.tue", Message: `v-switch expression type []string is not comparable`, Line: 3, Column: 23},
				{Path: "SwitchPlacement.tue", Message: `v-switch requires at least one v-case or v-default child`, Line: 3, Column: 13},
				{Path: "SwitchPlacement.tue", Message: `v-switch is only supported on <template>`, Line: 4, Column: 8},
				{Path: "SwitchPlacement.tue", Message: `v-case must be a direct child of v-switch`, Line: 6, Column: 6},
				{Path: "SwitchPlacement.tue", Message: `v-default must be a direct child of v-switch`, Line: 7, Column: 6},
			},
		},
		{
			name:    "switch branches",
			fixture: "testdata/invalid_conditional_controls/SwitchBranches.tue",
			expected: []diagnosticSummary{
				{Path: "SwitchBranches.tue", Message: `v-switch children must use v-case or v-default`, Line: 3, Column: 3},
				{Path: "SwitchBranches.tue", Message: `v-case expects string, got int`, Line: 4, Column: 14},
				{Path: "SwitchBranches.tue", Message: `v-case must appear before v-default`, Line: 6, Column: 6},
				{Path: "SwitchBranches.tue", Message: `v-switch may only have one v-default`, Line: 7, Column: 6},
				{Path: "SwitchBranches.tue", Message: `v-switch branches cannot combine v-case or v-default with v-if, v-else-if, or v-else`, Line: 8, Column: 23},
				{Path: "SwitchBranches.tue", Message: `v-case must appear before v-default`, Line: 8, Column: 6},
			},
		},
		{
			name:    "switch comparability",
			fixture: "testdata/invalid_conditional_controls/SwitchComparability.tue",
			expected: []diagnosticSummary{
				{Path: "SwitchComparability.tue", Message: `v-switch expression type Filter is not comparable`, Line: 2, Column: 22},
				{Path: "SwitchComparability.tue", Message: `v-switch expression type bytes.Buffer is not comparable`, Line: 5, Column: 22},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project, err := parseProjectFixture(test.fixture)
			if err != nil {
				t.Fatalf("parse project fixture: %v", err)
			}

			_, diagnostics := GenerateProject(*project)
			if diff := cmp.Diff(test.expected, summarizeDiagnostics(diagnostics)); diff != "" {
				t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
			}
		})
	}
}

func TestGenerateProjectEmitsLoopRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/loops")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{
		"App_tue.go",
		"App_render_tue.go",
		"UserBadge_tue.go",
		"UserBadge_component_tue.go",
		"UserBadge_render_tue.go",
	}, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}
	expectedRender, err := testFixtureString("testdata/golden/Loop_render_tue.go")
	if err != nil {
		t.Fatalf("read expected loop render fixture: %v", err)
	}
	actualRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated loop render: %v", err)
	}
	if diff := cmp.Diff(expectedRender, string(actualRender)); diff != "" {
		t.Errorf("mismatch generated loop render (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectEmitsKeyedEmptyFragmentForFalseLoopCondition(t *testing.T) {
	project, err := parseProjectFixture("testdata/loops")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}
	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	actualRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated loop render: %v", err)
	}

	source := string(actualRender)
	for _, expected := range []string{
		"if __tueItem.Done {",
		"return tue.Fragment(nil)",
		"__tueVNode.Key = fmt.Sprint(__tueItem.ID)",
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("mismatch generated loop conditional: expected generated source to contain %q", expected)
		}
	}
}

func TestGenerateProjectEmitsMethodControlRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/method_control/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}
	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}

	actualRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated render: %v", err)
	}
	actual := string(actualRender)
	for _, expected := range []string{
		"component.visibleTodos()",
		`if __tueItem.Done`,
		`tue.Element("li", []tue.Attribute{tue.Attr("class", "done")}`,
		`tue.Element("li", []tue.Attribute{tue.Attr("class", "open")}`,
		`tue.ElementWithEvents("textarea"`,
		`tue.ElementWithEvents("input", []tue.Attribute{tue.Attr("type", "email")`,
	} {
		if !strings.Contains(actual, expected) {
			t.Errorf("generated render actual = %q, expected to contain %q", actual, expected)
		}
	}

	for _, file := range result.Files {
		if _, err := goparser.ParseFile(token.NewFileSet(), file.Path, file.Source, goparser.AllErrors); err != nil {
			t.Errorf("generated file %s should parse: %v", file.Path, err)
		}
	}
}

func TestGenerateProjectReportsUnsupportedLoopConstructs(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_loops/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "App.tue", Message: `v-for must use '<item> in <items>'`, Line: 3, Column: 13},
		{Path: "App.tue", Message: `v-for requires a :key attribute`, Line: 4, Column: 6},
		{Path: "App.tue", Message: `v-for source expression is not supported in the static render slice`, Line: 5, Column: 21},
		{Path: "App.tue", Message: `v-for key expression is not supported in the static render slice`, Line: 6, Column: 34},
		{Path: "App.tue", Message: `v-else cannot follow a conditional branch that also has v-for; use a <template v-for> wrapper`, Line: 8, Column: 6},
		{Path: "App.tue", Message: `v-for source must be iterable, got bool`, Line: 9, Column: 21},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectEmitsClassBindingRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/classes/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	expectedDiagnostics := []diagnosticSummary{}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	expectedPaths := []string{"App_tue.go", "App_render_tue.go"}
	if diff := cmp.Diff(expectedPaths, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}
	expectedRender, err := testFixtureString("testdata/golden/ClassBinding_render_tue.go")
	if err != nil {
		t.Fatalf("read expected class binding render fixture: %v", err)
	}
	actualRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated class binding render: %v", err)
	}
	if diff := cmp.Diff(expectedRender, string(actualRender)); diff != "" {
		t.Errorf("mismatch generated class binding render (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectEmitsStyleBindingRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/styles/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	expectedDiagnostics := []diagnosticSummary{}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	expectedPaths := []string{"App_tue.go", "App_render_tue.go"}
	if diff := cmp.Diff(expectedPaths, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}
	expectedRender, err := testFixtureString("testdata/golden/StyleBinding_render_tue.go")
	if err != nil {
		t.Fatalf("read expected style binding render fixture: %v", err)
	}
	actualRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated style binding render: %v", err)
	}
	if diff := cmp.Diff(expectedRender, string(actualRender)); diff != "" {
		t.Errorf("mismatch generated style binding render (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectEmitsBoundAttributeRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/bound_attrs/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	expectedDiagnostics := []diagnosticSummary{}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	render, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read generated bound attribute render: %v", err)
	}
	for _, expected := range []string{
		`tue.Attr("href", component.homeHref)`,
		`tue.Attr("title", (component.label + " link"))`,
	} {
		if !strings.Contains(string(render), expected) {
			t.Errorf("mismatch generated bound attribute render: expected source to contain %q", expected)
		}
	}

	for _, file := range result.Files {
		if _, err := goparser.ParseFile(token.NewFileSet(), file.Path, file.Source, goparser.AllErrors); err != nil {
			t.Errorf("generated file %s should parse: %v", file.Path, err)
		}
	}
}

func TestGenerateProjectEmitsHTMLBindingRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/html/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	expectedDiagnostics := []diagnosticSummary{}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	expectedPaths := []string{"App_tue.go", "App_component_tue.go", "App_render_tue.go"}
	if diff := cmp.Diff(expectedPaths, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}
	expectedRender, err := testFixtureString("testdata/golden/HTMLBinding_render_tue.go")
	if err != nil {
		t.Fatalf("read expected HTML binding render fixture: %v", err)
	}
	actualRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated HTML binding render: %v", err)
	}
	if diff := cmp.Diff(expectedRender, string(actualRender)); diff != "" {
		t.Errorf("mismatch generated HTML binding render (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectEmitsScopedStyleFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/scoped_styles")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	expectedDiagnostics := []diagnosticSummary{}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	expectedPaths := []string{
		"App_tue.go",
		"App_render_tue.go",
		"Banner_tue.go",
		"Banner_render_tue.go",
		"style.css",
	}
	if diff := cmp.Diff(expectedPaths, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}
	expectedRender, err := testFixtureString("testdata/golden/ScopedStyle_render_tue.go")
	if err != nil {
		t.Fatalf("read expected scoped style render fixture: %v", err)
	}
	actualRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated scoped style render: %v", err)
	}
	if diff := cmp.Diff(expectedRender, string(actualRender)); diff != "" {
		t.Errorf("mismatch generated scoped style render (-expected, +actual):\n%s", diff)
	}
	expectedStyle, err := testFixtureString("testdata/golden/ScopedStyle_style.css")
	if err != nil {
		t.Fatalf("read expected scoped stylesheet fixture: %v", err)
	}
	actualStyle, err := generatedSource(result, "style.css")
	if err != nil {
		t.Fatalf("read actual generated stylesheet: %v", err)
	}
	if diff := cmp.Diff(expectedStyle, string(actualStyle)); diff != "" {
		t.Errorf("mismatch generated stylesheet (-expected, +actual):\n%s", diff)
	}

	expectedManifest := manifestSummary{
		GeneratedBy: "tue",
		StyleFile:   "style.css",
		Files: []manifestFileSummary{
			{Source: "App.tue", Component: "App", ScriptFile: "App_tue.go", RenderFile: "App_render_tue.go", ScopeAttr: "data-tue-c-d8d60a14"},
			{Source: "Banner.tue", Component: "Banner", ScriptFile: "Banner_tue.go", RenderFile: "Banner_render_tue.go"},
		},
	}
	actualManifest := summarizeManifest(result.Manifest)
	actualManifest.Files[0].Nodes = nil
	actualManifest.Files[1].Nodes = nil
	if diff := cmp.Diff(expectedManifest, actualManifest); diff != "" {
		t.Errorf("mismatch scoped style manifest (-expected, +actual):\n%s", diff)
	}

	for _, file := range result.Files {
		if filepath.Ext(file.Path) != ".go" {
			continue
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), file.Path, file.Source, goparser.AllErrors); err != nil {
			t.Errorf("generated file %s should parse: %v", file.Path, err)
		}
	}
}

func TestGenerateProjectRewritesTemplateAndStyleAssets(t *testing.T) {
	root := t.TempDir()
	if err := copyTestFixtureDir(root, "testdata/assets"); err != nil {
		t.Fatalf("copy asset fixture: %v", err)
	}
	project, err := parseProjectRoot(root, []string{"App.tue"})
	if err != nil {
		t.Fatalf("parse project root: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	expectedDiagnostics := []diagnosticSummary{}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}

	logoOutput, err := assetOutput(result.Manifest, "logo.svg")
	if err != nil {
		t.Fatalf("find logo asset: %v", err)
	}
	heroOutput, err := assetOutput(result.Manifest, "hero,(1).png")
	if err != nil {
		t.Fatalf("find hero asset: %v", err)
	}
	expectedAssets := []manifestAssetSummary{
		{Source: "hero,(1).png", Output: heroOutput},
		{Source: "logo.svg", Output: logoOutput},
		{Source: "public/App_render_tue.go", Output: "public/App_render_tue.go", Public: true},
		{Source: "public/favicon.svg", Output: "public/favicon.svg", Public: true},
		{Source: "public/foo.go", Output: "public/foo.go", Public: true},
		{Source: "public/manifest.json", Output: "public/manifest.json", Public: true},
		{Source: "public/mask.svg", Output: "public/mask.svg", Public: true},
		{Source: "public/robots.txt", Output: "public/robots.txt", Public: true},
		{Source: "public/style.css", Output: "public/style.css", Public: true},
	}
	if diff := cmp.Diff(expectedAssets, summarizeManifest(result.Manifest).Assets); diff != "" {
		t.Errorf("mismatch manifest assets (-expected, +actual):\n%s", diff)
	}

	render, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read generated render: %v", err)
	}
	for _, expected := range []string{
		fmt.Sprintf(`tue.Attr("src", %q)`, logoOutput),
		`tue.Attr("href", "/favicon.svg")`,
	} {
		if !strings.Contains(string(render), expected) {
			t.Errorf("mismatch generated render asset reference: expected source to contain %q", expected)
		}
	}

	style, err := generatedSource(result, "style.css")
	if err != nil {
		t.Fatalf("read generated style: %v", err)
	}
	for _, expected := range []string{
		fmt.Sprintf(`url("%s")`, heroOutput),
		`url('/mask.svg#icon')`,
		`url("https://example.com/banner.png")`,
	} {
		if !strings.Contains(string(style), expected) {
			t.Errorf("mismatch generated style asset reference: expected source to contain %q", expected)
		}
	}

	repeatedResult, repeatedDiagnostics := GenerateProject(*project)
	if repeatedResult == nil {
		t.Fatal("repeated GenerateProject result is nil")
	}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(repeatedDiagnostics)); diff != "" {
		t.Errorf("mismatch repeated diagnostics (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(generatedPaths(result.Files), generatedPaths(repeatedResult.Files)); diff != "" {
		t.Errorf("mismatch repeated generated paths (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectEmitsModelBindingRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/models/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	expectedDiagnostics := []diagnosticSummary{}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	expectedPaths := []string{"App_tue.go", "App_component_tue.go", "App_render_tue.go"}
	if diff := cmp.Diff(expectedPaths, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}
	expectedRender, err := testFixtureString("testdata/golden/ModelBinding_render_tue.go")
	if err != nil {
		t.Fatalf("read expected model binding render fixture: %v", err)
	}
	actualRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated model binding render: %v", err)
	}
	if diff := cmp.Diff(expectedRender, string(actualRender)); diff != "" {
		t.Errorf("mismatch generated model binding render (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectEmitsDefaultSlotRenderFiles(t *testing.T) {
	project, err := parseProjectFixture("testdata/slots")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	expectedDiagnostics := []diagnosticSummary{}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	expectedPaths := []string{
		"App_tue.go",
		"App_render_tue.go",
		"Card_tue.go",
		"Card_component_tue.go",
		"Card_render_tue.go",
	}
	if diff := cmp.Diff(expectedPaths, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}

	expectedAppRender, err := testFixtureString("testdata/golden/SlotApp_render_tue.go")
	if err != nil {
		t.Fatalf("read expected slot app render fixture: %v", err)
	}
	actualAppRender, err := generatedSource(result, "App_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated slot app render: %v", err)
	}
	if diff := cmp.Diff(expectedAppRender, string(actualAppRender)); diff != "" {
		t.Errorf("mismatch generated slot app render (-expected, +actual):\n%s", diff)
	}

	expectedCardRender, err := testFixtureString("testdata/golden/SlotCard_render_tue.go")
	if err != nil {
		t.Fatalf("read expected slot card render fixture: %v", err)
	}
	actualCardRender, err := generatedSource(result, "Card_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated slot card render: %v", err)
	}
	if diff := cmp.Diff(expectedCardRender, string(actualCardRender)); diff != "" {
		t.Errorf("mismatch generated slot card render (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsUnsupportedClassBindingExpressions(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_classes/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	expected := []diagnosticSummary{
		{Path: "App.tue", Message: `class binding expression is not supported in the static render slice`, Line: 2, Column: 15},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsClassBindingTypeDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_classes/BoolClass.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	expected := []diagnosticSummary{
		{Path: "BoolClass.tue", Message: `class binding expects string, got bool`, Line: 2, Column: 15},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsUnsupportedStyleBindingExpressions(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_styles/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	expected := []diagnosticSummary{
		{Path: "App.tue", Message: `style binding expression is not supported in the static render slice`, Line: 2, Column: 16},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsStyleBindingTypeDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_styles/BoolStyle.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	expected := []diagnosticSummary{
		{Path: "BoolStyle.tue", Message: `style binding expects string, got bool`, Line: 2, Column: 16},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsBoundAttributeTypeDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_attrs/BoolHref.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	expected := []diagnosticSummary{
		{Path: "BoolHref.tue", Message: `bound attribute ":href" expects string, got bool`, Line: 2, Column: 12},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsHTMLBindingDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_html/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	expected := []diagnosticSummary{
		{Path: "App.tue", Message: `v-html expects tue.TrustedHTML, got string`, Line: 2, Column: 16},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsModelBindingDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_models/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	expected := []diagnosticSummary{
		{Path: "App.tue", Message: `v-model expects bool, got string`, Line: 3, Column: 35},
		{Path: "App.tue", Message: `v-model expects string, got bool`, Line: 4, Column: 20},
		{Path: "App.tue", Message: `v-model is not supported for input type "number"`, Line: 9, Column: 24},
		{Path: "App.tue", Message: `v-model target "query.Text" is not writable`, Line: 11, Column: 19},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsNamedSlotDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_slots/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	expected := []diagnosticSummary{
		{Path: "App.tue", Message: `named slots are not supported in the default slot slice`, Line: 3, Column: 9},
		{Path: "App.tue", Message: `named slots are not supported in the default slot slice`, Line: 4, Column: 9},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectEmitsNativeEventHandlers(t *testing.T) {
	project, err := parseProjectFixture("testdata/events/Counter.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"Counter_tue.go", "Counter_component_tue.go", "Counter_render_tue.go"}, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}
	expectedRender, err := testFixtureString("testdata/golden/Counter_render_tue.go")
	if err != nil {
		t.Fatalf("read expected counter render fixture: %v", err)
	}
	actualRender, err := generatedSource(result, "Counter_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated counter render: %v", err)
	}
	if diff := cmp.Diff(expectedRender, string(actualRender)); diff != "" {
		t.Errorf("mismatch generated counter render (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectEmitsChildComponents(t *testing.T) {
	project, err := parseProjectFixture("testdata/components")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	result, diagnostics := GenerateProject(*project)
	if result == nil {
		t.Fatal("GenerateProject result is nil")
	}

	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]string{
		"Parent_tue.go",
		"Parent_component_tue.go",
		"Parent_render_tue.go",
		"UserBadge_tue.go",
		"UserBadge_component_tue.go",
		"UserBadge_render_tue.go",
	}, generatedPaths(result.Files)); diff != "" {
		t.Errorf("mismatch generated paths (-expected, +actual):\n%s", diff)
	}

	expectedRender, err := testFixtureString("testdata/golden/Parent_render_tue.go")
	if err != nil {
		t.Fatalf("read expected parent render fixture: %v", err)
	}
	actualRender, err := generatedSource(result, "Parent_render_tue.go")
	if err != nil {
		t.Fatalf("read actual generated parent render: %v", err)
	}
	if diff := cmp.Diff(expectedRender, string(actualRender)); diff != "" {
		t.Errorf("mismatch generated parent render (-expected, +actual):\n%s", diff)
	}
	expectedComponent, err := testFixtureString("testdata/golden/UserBadge_component_tue.go")
	if err != nil {
		t.Fatalf("read expected user badge declaration fixture: %v", err)
	}
	actualComponent, err := generatedSource(result, "UserBadge_component_tue.go")
	if err != nil {
		t.Fatalf("read actual generated user badge declaration: %v", err)
	}
	if diff := cmp.Diff(expectedComponent, string(actualComponent)); diff != "" {
		t.Errorf("mismatch generated user badge declaration (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsUnsupportedEventHandlers(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_events/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "App.tue", Message: `event handler "save" does not accept arguments`, Line: 2, Column: 32},
		{Path: "App.tue", Message: `event handler "needsValue" must have signature func()`, Line: 3, Column: 32},
		{Path: "App.tue", Message: `event handler "missing" is not a method on App`, Line: 4, Column: 32},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGenerateProjectReportsUnsupportedComponentEvents(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_component_events")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	_, diagnostics := GenerateProject(*project)

	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "Parent.tue", Message: `component "UserBadge" has no event "missing"`, Line: 5, Column: 4},
		{Path: "Parent.tue", Message: `event handler "selectUser" must have signature func(string)`, Line: 6, Column: 13},
		{Path: "Parent.tue", Message: `event handler "selectUser" does not accept arguments`, Line: 7, Column: 19},
		{Path: "Parent.tue", Message: `event handler "needsValue" must have signature func()`, Line: 8, Column: 15},
		{Path: "Parent.tue", Message: `event handler "needsValue" must have signature func(int)`, Line: 9, Column: 18},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestGeneratedCounterFixtureCompilesForWASM(t *testing.T) {
	project, err := parseProjectFixture("testdata/events/Counter.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	if err := compileGeneratedProjectForWASM(t.TempDir(), *project); err != nil {
		t.Fatalf("compile generated counter fixture for WASM: %v", err)
	}
}

func TestGeneratedComponentFixtureCompilesForWASM(t *testing.T) {
	project, err := parseProjectFixture("testdata/components")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	if err := compileGeneratedProjectForWASM(t.TempDir(), *project); err != nil {
		t.Fatalf("compile generated component fixture for WASM: %v", err)
	}
}

func TestGeneratedComponentBehavior(t *testing.T) {
	project, err := parseProjectFixture("testdata/components")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	testSource := `package fixtures

import (
	"testing"
	"time"

	tue "github.com/norunners/tue"
)

func waitForProfileName(t *testing.T, child *UserBadge, expected string) {
	t.Helper()

	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		profile, ok := child.ProfileOk()
		if ok && profile.Name == expected && !child.ProfileLoading() && child.ProfileError() == nil {
			return
		}

		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("timed out waiting for profile %q; actual = %#v ok=%v loading=%v err=%v", expected, profile, ok, child.ProfileLoading(), child.ProfileError())
		}
	}
}

func TestGeneratedComponentAPI(t *testing.T) {
	parentInstance := NewParent()
	parent := parentInstance.Component.(*Parent)
	if parent.ShowBadge() {
		t.Error("ShowBadge() actual = true, expected zero value before assignment")
	}
	if parent.Selected() != "" {
		t.Errorf("Selected() actual = %q, expected zero value", parent.Selected())
	}
	parent.ShowBadgeSet(true)

	created := renderParent(parent).Children[0]
	childInstance := created.ComponentFactory()
	child := childInstance.Component.(*UserBadge)

	if actual := child.Name(); actual != "Ada" {
		t.Errorf("Name() actual = %q, expected Ada", actual)
	}
	if _, ok := child.NameOk(); !ok {
		t.Error("NameOk() did not report the supplied prop")
	}
	waitForProfileName(t, child, "Ada")
	if actual := child.Label(); actual != "Ada expanded" {
		t.Errorf("Label() actual = %q, expected Ada expanded", actual)
	}
	if actual := child.InitialLabel(); actual != "Ada expanded" {
		t.Errorf("InitialLabel() actual = %q, expected Init to read Ada expanded", actual)
	}
	if actual := child.SubtitleLabel(); actual != "none" {
		t.Errorf("SubtitleLabel() actual = %q, expected none", actual)
	}
	if actual := child.Label(); actual != "Ada expanded" {
		t.Errorf("cached Label() actual = %q, expected Ada expanded", actual)
	}
	if child.labelCalls != 1 {
		t.Errorf("label calls actual = %d, expected one cached evaluation", child.labelCalls)
	}
	if _, ok := child.SubtitleOk(); ok {
		t.Error("SubtitleOk() reported an omitted prop as supplied")
	}
	observedNames := []string{}
	stopNames := tue.Watch(func() {
		observedNames = append(observedNames, child.Name())
	})
	parent.NameSet("Marie")
	stopNames()
	if len(observedNames) != 2 || observedNames[0] != "Ada" || observedNames[1] != "Marie" {
		t.Errorf("reactive prop names actual = %#v, expected [Ada Marie]", observedNames)
	}
	if !child.Expanded() {
		t.Error("Expanded() actual = false, expected Init to observe initialized state")
	}
	child.ExpandedSet(false)
	if child.Expanded() {
		t.Error("Expanded() actual = true after ExpandedSet(false)")
	}
	if actual := child.Label(); actual != "Marie" {
		t.Errorf("Label() after state change actual = %q, expected Marie", actual)
	}
	waitForProfileName(t, child, "Marie")
	if child.labelCalls != 2 {
		t.Errorf("label calls after invalidation actual = %d, expected 2", child.labelCalls)
	}
	called := child.Select("Grace")
	if !called || parent.Selected() != "Grace" {
		t.Errorf("Select() actual = (%v, %q), expected (true, Grace)", called, parent.Selected())
	}
	if child.Dismiss("ignored") {
		t.Error("Dismiss() actual = true, expected false without a listener")
	}

	parent.NameSet("Lin")
	patched := renderParent(parent).Children[0]
	patched.ComponentUpdater(childInstance)
	if actual := child.Name(); actual != "Lin" {
		t.Errorf("Name() after patch actual = %q, expected Lin", actual)
	}
	if actual := child.Label(); actual != "Lin" {
		t.Errorf("Label() after patch actual = %q, expected Lin", actual)
	}
	waitForProfileName(t, child, "Lin")
	if child.Expanded() {
		t.Error("generated state was reset while patching parent bindings")
	}

	parent.UseAlternateSet(true)
	patched = renderParent(parent).Children[0]
	patched.ComponentUpdater(childInstance)
	if actual := child.Name(); actual != "Zoe" {
		t.Errorf("Name() after prop source swap actual = %q, expected Zoe", actual)
	}
	if actual := child.Label(); actual != "Zoe" {
		t.Errorf("Label() after prop source swap actual = %q, expected Zoe", actual)
	}
	waitForProfileName(t, child, "Zoe")
	if actual := child.SubtitleLabel(); actual != "alternate" {
		t.Errorf("SubtitleLabel() after optional prop appears actual = %q, expected alternate", actual)
	}

	parent.UseAlternateSet(false)
	patched = renderParent(parent).Children[0]
	patched.ComponentUpdater(childInstance)
	if actual := child.Name(); actual != "Lin" {
		t.Errorf("Name() after prop source swap back actual = %q, expected Lin", actual)
	}
	waitForProfileName(t, child, "Lin")
	if actual := child.SubtitleLabel(); actual != "none" {
		t.Errorf("SubtitleLabel() after optional prop disappears actual = %q, expected none", actual)
	}
}
`
	if err := runGeneratedProjectTests(t.TempDir(), *project, testSource); err != nil {
		t.Fatalf("run generated component tests: %v", err)
	}
}

func TestGeneratedConditionalFixtureCompilesForWASM(t *testing.T) {
	project, err := parseProjectFixture("testdata/conditionals/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	if err := compileGeneratedProjectForWASM(t.TempDir(), *project); err != nil {
		t.Fatalf("compile generated conditional fixture for WASM: %v", err)
	}
}

func TestGeneratedLoopFixtureCompilesForWASM(t *testing.T) {
	project, err := parseProjectFixture("testdata/loops")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	if err := compileGeneratedProjectForWASM(t.TempDir(), *project); err != nil {
		t.Fatalf("compile generated loop fixture for WASM: %v", err)
	}
}

func TestGeneratedClassBindingFixtureCompilesForWASM(t *testing.T) {
	project, err := parseProjectFixture("testdata/classes/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	if err := compileGeneratedProjectForWASM(t.TempDir(), *project); err != nil {
		t.Fatalf("compile generated class binding fixture for WASM: %v", err)
	}
}

func TestGeneratedStyleBindingFixtureCompilesForWASM(t *testing.T) {
	project, err := parseProjectFixture("testdata/styles/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	if err := compileGeneratedProjectForWASM(t.TempDir(), *project); err != nil {
		t.Fatalf("compile generated style binding fixture for WASM: %v", err)
	}
}

func TestGeneratedHTMLBindingFixtureCompilesForWASM(t *testing.T) {
	project, err := parseProjectFixture("testdata/html/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	if err := compileGeneratedProjectForWASM(t.TempDir(), *project); err != nil {
		t.Fatalf("compile generated HTML binding fixture for WASM: %v", err)
	}
}

func TestGeneratedModelBindingFixtureCompilesForWASM(t *testing.T) {
	project, err := parseProjectFixture("testdata/models/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	if err := compileGeneratedProjectForWASM(t.TempDir(), *project); err != nil {
		t.Fatalf("compile generated model binding fixture for WASM: %v", err)
	}
}

func TestGeneratedDefaultSlotFixtureCompilesForWASM(t *testing.T) {
	project, err := parseProjectFixture("testdata/slots")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	if err := compileGeneratedProjectForWASM(t.TempDir(), *project); err != nil {
		t.Fatalf("compile generated default slot fixture for WASM: %v", err)
	}
}

func TestGeneratedScopedStyleFixtureCompilesForWASM(t *testing.T) {
	project, err := parseProjectFixture("testdata/scoped_styles")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	if err := compileGeneratedProjectForWASM(t.TempDir(), *project); err != nil {
		t.Fatalf("compile generated scoped style fixture for WASM: %v", err)
	}
}

func TestWriteProjectWritesCacheFilesAndManifest(t *testing.T) {
	root := t.TempDir()
	project, err := parseProjectFixture("testdata/static/App.tue")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	manifest, diagnostics, err := WriteProject(root, *project)
	if err != nil {
		t.Fatalf("WriteProject returned error: %v", err)
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

func TestWriteProjectWritesStylesheet(t *testing.T) {
	root := t.TempDir()
	project, err := parseProjectFixture("testdata/scoped_styles")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	manifest, diagnostics, err := WriteProject(root, *project)
	if err != nil {
		t.Fatalf("WriteProject returned error: %v", err)
	}
	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff("style.css", manifest.StyleFile); diff != "" {
		t.Errorf("mismatch manifest style file (-expected, +actual):\n%s", diff)
	}

	expectedStyle, err := testFixtureString("testdata/golden/ScopedStyle_style.css")
	if err != nil {
		t.Fatalf("read expected scoped stylesheet fixture: %v", err)
	}
	actualStyle, err := os.ReadFile(filepath.Join(root, CacheDir, "style.css"))
	if err != nil {
		t.Fatalf("read generated stylesheet: %v", err)
	}
	if diff := cmp.Diff(expectedStyle, string(actualStyle)); diff != "" {
		t.Errorf("mismatch written stylesheet (-expected, +actual):\n%s", diff)
	}
}

func TestWriteProjectWritesAssets(t *testing.T) {
	root := t.TempDir()
	if err := copyTestFixtureDir(root, "testdata/assets"); err != nil {
		t.Fatalf("copy asset fixture: %v", err)
	}
	project, err := parseProjectRoot(root, []string{"App.tue"})
	if err != nil {
		t.Fatalf("parse project root: %v", err)
	}

	manifest, diagnostics, err := WriteProject(root, *project)
	if err != nil {
		t.Fatalf("WriteProject returned error: %v", err)
	}
	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}

	for _, asset := range manifest.Assets {
		actual, err := os.ReadFile(filepath.Join(root, CacheDir, filepath.FromSlash(asset.Output)))
		if err != nil {
			t.Errorf("read generated asset %s: %v", asset.Output, err)
			continue
		}
		expected, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(asset.Source)))
		if err != nil {
			t.Errorf("read source asset %s: %v", asset.Source, err)
			continue
		}
		if diff := cmp.Diff(string(expected), string(actual)); diff != "" {
			t.Errorf("mismatch copied asset %q (-expected, +actual):\n%s", asset.Output, diff)
		}
	}

	generatedStyle, err := os.ReadFile(filepath.Join(root, CacheDir, "style.css"))
	if err != nil {
		t.Fatalf("read generated stylesheet: %v", err)
	}
	if strings.Contains(string(generatedStyle), "public style collision fixture") {
		t.Errorf("generated stylesheet was overwritten by public/style.css")
	}
	if !strings.Contains(string(generatedStyle), "assets/hero,(1).") {
		t.Errorf("style.css actual = %q, expected generated stylesheet with hashed hero URL", string(generatedStyle))
	}

	generatedRender, err := os.ReadFile(filepath.Join(root, CacheDir, "App_render_tue.go"))
	if err != nil {
		t.Fatalf("read generated render file: %v", err)
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "App_render_tue.go", generatedRender, goparser.AllErrors); err != nil {
		t.Errorf("generated render file should remain valid Go after public/App_render_tue.go copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, CacheDir, "foo.go")); !os.IsNotExist(err) {
		t.Errorf("public/foo.go should not be copied into generated package root; stat error = %v", err)
	}

	manifestSource, err := os.ReadFile(filepath.Join(root, CacheDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read generated manifest: %v", err)
	}
	var decodedManifest Manifest
	if err := json.Unmarshal(manifestSource, &decodedManifest); err != nil {
		t.Fatalf("decode generated manifest: %v", err)
	}
	if diff := cmp.Diff("tue", decodedManifest.GeneratedBy); diff != "" {
		t.Errorf("mismatch generated manifest marker (-expected, +actual):\n%s", diff)
	}
}

func TestWriteProductionProjectWritesDist(t *testing.T) {
	root := t.TempDir()
	if err := copyTestFixtureDir(root, "testdata/production"); err != nil {
		t.Fatalf("copy production fixture: %v", err)
	}
	project, err := parseProjectRoot(root, []string{"App.tue"})
	if err != nil {
		t.Fatalf("parse project root: %v", err)
	}

	build, diagnostics, err := WriteProductionProject(root, *project)
	if err != nil {
		t.Fatalf("WriteProductionProject returned error: %v", err)
	}
	if build == nil {
		t.Fatal("WriteProductionProject build is nil")
	}
	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	if build.WASMSizeBytes <= 0 {
		t.Errorf("WASM size actual = %d, expected positive", build.WASMSizeBytes)
	}

	logoOutput, err := assetOutput(build.Manifest, "logo.svg")
	if err != nil {
		t.Fatalf("find logo asset: %v", err)
	}
	heroOutput, err := assetOutput(build.Manifest, "hero.png")
	if err != nil {
		t.Fatalf("find hero asset: %v", err)
	}
	faviconOutput, err := assetOutput(build.Manifest, "public/favicon.svg")
	if err != nil {
		t.Fatalf("find favicon asset: %v", err)
	}
	expectedFiles := []string{
		"app.wasm",
		heroOutput,
		logoOutput,
		faviconOutput,
		"index.html",
		"manifest.json",
		"style.css",
		"tue_loader.js",
	}
	sort.Strings(expectedFiles)
	if diff := cmp.Diff(expectedFiles, build.Files); diff != "" {
		t.Errorf("mismatch dist files (-expected, +actual):\n%s", diff)
	}

	for _, path := range expectedFiles {
		if _, err := os.Stat(filepath.Join(root, DistDir, filepath.FromSlash(path))); err != nil {
			t.Errorf("dist file %s should exist: %v", path, err)
		}
	}
	if diff := cmp.Diff("favicon.svg", faviconOutput); diff != "" {
		t.Errorf("mismatch public asset dist output (-expected, +actual):\n%s", diff)
	}

	index, err := os.ReadFile(filepath.Join(root, DistDir, "index.html"))
	if err != nil {
		t.Fatalf("read dist index: %v", err)
	}
	for _, expected := range []string{`<div id="app"></div>`, `<script src="tue_loader.js" defer></script>`} {
		if !strings.Contains(string(index), expected) {
			t.Errorf("index.html actual = %q, expected %q", string(index), expected)
		}
	}

	loader, err := os.ReadFile(filepath.Join(root, DistDir, "tue_loader.js"))
	if err != nil {
		t.Fatalf("read dist loader: %v", err)
	}
	if !strings.Contains(string(loader), `fetch("app.wasm")`) {
		t.Errorf("tue_loader.js should load app.wasm")
	}

	style, err := os.ReadFile(filepath.Join(root, DistDir, "style.css"))
	if err != nil {
		t.Fatalf("read dist stylesheet: %v", err)
	}
	if !strings.Contains(string(style), ".page[data-tue-c-d8d60a14]") {
		t.Errorf("style.css actual = %q, expected scoped selector", string(style))
	}
	if !strings.Contains(string(style), heroOutput) {
		t.Errorf("style.css actual = %q, expected hero asset %q", string(style), heroOutput)
	}

	manifestSource, err := os.ReadFile(filepath.Join(root, DistDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read dist manifest: %v", err)
	}
	var decoded Manifest
	if err := json.Unmarshal(manifestSource, &decoded); err != nil {
		t.Fatalf("decode dist manifest: %v", err)
	}
	if diff := cmp.Diff(build.Manifest, decoded); diff != "" {
		t.Errorf("mismatch dist manifest (-expected, +actual):\n%s", diff)
	}
}

func TestWriteProductionProjectWritesDistFromRelativeRoot(t *testing.T) {
	cwd := t.TempDir()
	root := filepath.Join(cwd, "app")
	if err := copyTestFixtureDir(root, "testdata/production"); err != nil {
		t.Fatalf("copy production fixture: %v", err)
	}
	t.Chdir(cwd)

	project, err := parseProjectRoot("app", []string{"App.tue"})
	if err != nil {
		t.Fatalf("parse project root: %v", err)
	}

	build, diagnostics, err := WriteProductionProject("app", *project)
	if err != nil {
		t.Fatalf("WriteProductionProject returned error: %v", err)
	}
	if build == nil {
		t.Fatal("WriteProductionProject build is nil")
	}
	expectedDiagnostics := []diagnosticSummary{}
	if diff := cmp.Diff(expectedDiagnostics, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
	if _, err := os.Stat(filepath.Join("app", DistDir, wasmFilePath)); err != nil {
		t.Errorf("relative-root build should write app.wasm under dist: %v", err)
	}
}

func TestWriteProductionProjectRejectsPublicGeneratedFileCollisions(t *testing.T) {
	for _, path := range []string{"app.wasm", "index.html", "manifest.json", "style.css", "tue_loader.js"} {
		t.Run(path, func(t *testing.T) {
			root := t.TempDir()
			if err := copyTestFixtureDir(root, "testdata/production"); err != nil {
				t.Fatalf("copy production fixture: %v", err)
			}
			if err := writeTestFile(filepath.Join(root, "public", filepath.FromSlash(path)), "collision\n"); err != nil {
				t.Fatalf("write public collision fixture: %v", err)
			}
			project, err := parseProjectRoot(root, []string{"App.tue"})
			if err != nil {
				t.Fatalf("parse project root: %v", err)
			}

			_, diagnostics, err := WriteProductionProject(root, *project)

			if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
				t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
			}
			expected := fmt.Sprintf(`public asset "public/%s" conflicts with generated production file %q`, path, path)
			if err == nil || !strings.Contains(err.Error(), expected) {
				t.Errorf("WriteProductionProject error actual = %v, expected %q", err, expected)
			}
		})
	}
}

func TestProductionModuleDependencyUsesBuildInfoVersion(t *testing.T) {
	moduleRootCalled := false
	dependency, err := resolveTueModuleDependency(&debug.BuildInfo{
		Main: debug.Module{Path: tueModulePath, Version: "v1.2.3"},
	}, "", func() (string, error) {
		moduleRootCalled = true
		return "", fmt.Errorf("module root lookup should not run")
	})
	if err != nil {
		t.Fatalf("resolve Tue module dependency: %v", err)
	}
	expected := productionModuleDependency{Version: "v1.2.3"}
	if diff := cmp.Diff(expected, dependency); diff != "" {
		t.Errorf("mismatch production module dependency (-expected, +actual):\n%s", diff)
	}
	if moduleRootCalled {
		t.Errorf("module root lookup should not run for released build info")
	}

	actualGoMod := productionGoMod(dependency)
	expectedGoMod := fmt.Sprintf("module %s\n\ngo %s\n\nrequire %s v1.2.3\n", generatedModulePath, goDirective(), tueModulePath)
	if diff := cmp.Diff(expectedGoMod, actualGoMod); diff != "" {
		t.Errorf("mismatch production go.mod (-expected, +actual):\n%s", diff)
	}
}

func TestProductionModuleDependencyUsesExplicitReplaceOverride(t *testing.T) {
	moduleRootCalled := false
	dependency, err := resolveTueModuleDependency(&debug.BuildInfo{
		Main: debug.Module{Path: tueModulePath, Version: "v1.2.3"},
	}, "/local/tue", func() (string, error) {
		moduleRootCalled = true
		return "", fmt.Errorf("module root lookup should not run")
	})
	if err != nil {
		t.Fatalf("resolve Tue module dependency: %v", err)
	}
	expected := productionModuleDependency{Version: "v0.0.0", Replace: "/local/tue"}
	if diff := cmp.Diff(expected, dependency); diff != "" {
		t.Errorf("mismatch production module dependency (-expected, +actual):\n%s", diff)
	}
	if moduleRootCalled {
		t.Errorf("module root lookup should not run for explicit replace override")
	}
}

func TestProductionModuleDependencyFallsBackToSourceTreeForDevelopmentBuild(t *testing.T) {
	dependency, err := resolveTueModuleDependency(&debug.BuildInfo{
		Main: debug.Module{Path: tueModulePath, Version: "(devel)"},
	}, "", func() (string, error) {
		return "/local/tue", nil
	})
	if err != nil {
		t.Fatalf("resolve Tue module dependency: %v", err)
	}
	expected := productionModuleDependency{Version: "v0.0.0", Replace: "/local/tue"}
	if diff := cmp.Diff(expected, dependency); diff != "" {
		t.Errorf("mismatch production module dependency (-expected, +actual):\n%s", diff)
	}

	actualGoMod := productionGoMod(dependency)
	expectedGoMod := fmt.Sprintf("module %s\n\ngo %s\n\nrequire %s v0.0.0\n\nreplace %s => /local/tue\n", generatedModulePath, goDirective(), tueModulePath, tueModulePath)
	if diff := cmp.Diff(expectedGoMod, actualGoMod); diff != "" {
		t.Errorf("mismatch production go.mod (-expected, +actual):\n%s", diff)
	}
}

func parseProjectFixture(path string) (*Project, error) {
	info, err := fs.Stat(testFixtures, path)
	if err != nil {
		return nil, fmt.Errorf("stat embedded fixture %s: %w", path, err)
	}
	if info.IsDir() {
		return parseProjectFixtureDir(path)
	}
	return parseProjectFixtureFiles([]string{path})
}

func parseProjectFixtureDir(dir string) (*Project, error) {
	entries, err := fs.ReadDir(testFixtures, dir)
	if err != nil {
		return nil, fmt.Errorf("read embedded fixture dir %s: %w", dir, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".tue" {
			continue
		}
		paths = append(paths, filepath.ToSlash(filepath.Join(dir, entry.Name())))
	}
	return parseProjectFixtureFiles(paths)
}

func parseProjectFixtureFiles(paths []string) (*Project, error) {
	project := Project{Files: make([]File, 0, len(paths))}
	for _, path := range paths {
		file, err := parseProjectFixtureFile(path)
		if err != nil {
			return nil, err
		}
		project.Files = append(project.Files, *file)
	}
	return &project, nil
}

func parseProjectFixtureFile(path string) (*File, error) {
	source, err := testFixture(path)
	if err != nil {
		return nil, err
	}
	sfcFile, sfcDiagnostics := sfc.Parse(filepath.Base(path), source)
	if len(sfcDiagnostics) != 0 {
		return nil, fmt.Errorf("sfc.Parse diagnostics = %#v, expected none", sfcDiagnosticMessages(sfcDiagnostics))
	}

	templateTree, templateDiagnostics := gotemplate.ParseBlock(sfcFile.Template)
	if len(templateDiagnostics) != 0 {
		return nil, fmt.Errorf("template.ParseBlock diagnostics = %#v, expected none", templateDiagnosticMessages(templateDiagnostics))
	}

	scriptFile, scriptDiagnostics := script.ParseSFC(sfcFile)
	if len(scriptDiagnostics) != 0 {
		return nil, fmt.Errorf("script.ParseSFC diagnostics = %#v, expected none", scriptDiagnosticMessages(scriptDiagnostics))
	}
	file := &File{
		Path:         sfcFile.Path,
		Template:     templateTree,
		Script:       scriptFile,
		ScriptSource: sfcFile.Script.Content,
	}
	if style, ok := StyleFromBlock(sfcFile.Style); ok {
		file.Style = style
	}
	return file, nil
}

func parseProjectRoot(root string, paths []string) (*Project, error) {
	project := Project{
		Root:  root,
		Files: make([]File, 0, len(paths)),
	}
	for _, path := range paths {
		file, err := parseProjectRootFile(root, path)
		if err != nil {
			return nil, err
		}
		project.Files = append(project.Files, *file)
	}
	return &project, nil
}

func parseProjectRootFile(root string, path string) (*File, error) {
	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return nil, fmt.Errorf("read project fixture %s: %w", path, err)
	}
	sfcFile, sfcDiagnostics := sfc.Parse(path, source)
	if len(sfcDiagnostics) != 0 {
		return nil, fmt.Errorf("sfc.Parse diagnostics = %#v, expected none", sfcDiagnosticMessages(sfcDiagnostics))
	}

	templateTree, templateDiagnostics := gotemplate.ParseBlock(sfcFile.Template)
	if len(templateDiagnostics) != 0 {
		return nil, fmt.Errorf("template.ParseBlock diagnostics = %#v, expected none", templateDiagnosticMessages(templateDiagnostics))
	}

	scriptFile, scriptDiagnostics := script.ParseSFC(sfcFile)
	if len(scriptDiagnostics) != 0 {
		return nil, fmt.Errorf("script.ParseSFC diagnostics = %#v, expected none", scriptDiagnosticMessages(scriptDiagnostics))
	}
	file := &File{
		Path:         sfcFile.Path,
		Template:     templateTree,
		Script:       scriptFile,
		ScriptSource: sfcFile.Script.Content,
	}
	if style, ok := StyleFromBlock(sfcFile.Style); ok {
		file.Style = style
	}
	return file, nil
}

func copyTestFixtureDir(root string, dir string) error {
	return fs.WalkDir(testFixtures, dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		source, err := testFixture(path)
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create fixture dir %s: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, source, 0o644); err != nil {
			return fmt.Errorf("write fixture %s: %w", target, err)
		}
		return nil
	})
}

func writeTestFile(path string, source string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create test file dir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		return fmt.Errorf("write test file %s: %w", path, err)
	}
	return nil
}

func compileGeneratedProjectForWASM(root string, project Project) error {
	result, diagnostics := GenerateProject(project)
	if len(diagnostics) != 0 {
		return fmt.Errorf("GenerateProject diagnostics = %#v, expected none", summarizeDiagnostics(diagnostics))
	}
	if result == nil {
		return fmt.Errorf("GenerateProject result is nil")
	}

	packageDir := filepath.Join(root, "generated")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		return fmt.Errorf("create generated package dir: %w", err)
	}
	for _, file := range result.Files {
		if err := os.WriteFile(filepath.Join(packageDir, file.Path), file.Source, 0o644); err != nil {
			return fmt.Errorf("write generated file %s: %w", file.Path, err)
		}
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}
	goMod := fmt.Sprintf("module generatedcounter\n\ngo 1.26.4\n\nrequire github.com/norunners/tue v0.0.0\n\nreplace github.com/norunners/tue => %s\n", filepath.ToSlash(repoRoot))
	if err := os.WriteFile(filepath.Join(packageDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return fmt.Errorf("write generated go.mod: %w", err)
	}

	output := filepath.Join(root, "counter.test")
	command := exec.Command("go", "test", "-c", "-o", output, ".")
	command.Dir = packageDir
	command.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	combined, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go test -c: %w\n%s", err, combined)
	}
	return nil
}

func runGeneratedProjectTests(root string, project Project, testSource string) error {
	result, diagnostics := GenerateProject(project)
	if len(diagnostics) != 0 {
		return fmt.Errorf("GenerateProject diagnostics = %#v, expected none", summarizeDiagnostics(diagnostics))
	}
	if result == nil {
		return fmt.Errorf("GenerateProject result is nil")
	}

	packageDir := filepath.Join(root, "generated")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		return fmt.Errorf("create generated package dir: %w", err)
	}
	for _, file := range result.Files {
		if err := os.WriteFile(filepath.Join(packageDir, file.Path), file.Source, 0o644); err != nil {
			return fmt.Errorf("write generated file %s: %w", file.Path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(packageDir, "component_behavior_test.go"), []byte(testSource), 0o644); err != nil {
		return fmt.Errorf("write generated component support test: %w", err)
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}
	goMod := fmt.Sprintf("module generatedcomponent\n\ngo 1.26.4\n\nrequire github.com/norunners/tue v0.0.0\n\nreplace github.com/norunners/tue => %s\n", filepath.ToSlash(repoRoot))
	if err := os.WriteFile(filepath.Join(packageDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return fmt.Errorf("write generated go.mod: %w", err)
	}

	command := exec.Command("go", "test", "./...")
	command.Dir = packageDir
	combined, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go test: %w\n%s", err, combined)
	}
	return nil
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

func assetOutput(manifest Manifest, source string) (string, error) {
	for _, asset := range manifest.Assets {
		if asset.Source == source {
			return asset.Output, nil
		}
	}
	return "", fmt.Errorf("manifest asset source %q not found in %#v", source, manifest.Assets)
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
	StyleFile   string
	Assets      []manifestAssetSummary
	Files       []manifestFileSummary
}

type manifestAssetSummary struct {
	Source string
	Output string
	Public bool
}

type manifestFileSummary struct {
	Source        string
	Component     string
	ScriptFile    string
	ComponentFile string
	RenderFile    string
	ScopeAttr     string
	Nodes         []manifestNodeSummary
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
		StyleFile:   manifest.StyleFile,
		Files:       make([]manifestFileSummary, len(manifest.Files)),
	}
	if len(manifest.Assets) != 0 {
		summary.Assets = make([]manifestAssetSummary, len(manifest.Assets))
		for i, asset := range manifest.Assets {
			summary.Assets[i] = manifestAssetSummary{
				Source: asset.Source,
				Output: asset.Output,
				Public: asset.Public,
			}
		}
	}
	for i, file := range manifest.Files {
		summary.Files[i] = manifestFileSummary{
			Source:        file.Source,
			Component:     file.Component,
			ScriptFile:    file.ScriptFile,
			ComponentFile: file.ComponentFile,
			RenderFile:    file.RenderFile,
			ScopeAttr:     file.ScopeAttr,
			Nodes:         make([]manifestNodeSummary, len(file.Nodes)),
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
