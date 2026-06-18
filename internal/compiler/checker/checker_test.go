package checker

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
)

//go:embed testdata
var testFixtures embed.FS

func TestCheckProjectAcceptsValidProject(t *testing.T) {
	project, err := parseProjectFixture("testdata/valid")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	if diff := cmp.Diff([]diagnosticSummary{}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsTemplateDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "testdata/invalid/Parent.tue", Message: `unknown identifier "missing"`, Line: 3, Column: 21},
		{Path: "testdata/invalid/Parent.tue", Message: `component "UserBadge" prop "isAdmin" expects bool, got string`, Line: 3, Column: 40},
		{Path: "testdata/invalid/Parent.tue", Message: `component "UserBadge" has no prop "extra"`, Line: 3, Column: 48},
		{Path: "testdata/invalid/Parent.tue", Message: `component "UnknownCard" is not registered`, Line: 4, Column: 4},
		{Path: "testdata/invalid/Parent.tue", Message: `event handler "missingHandler" is not a method on Parent`, Line: 5, Column: 33},
		{Path: "testdata/invalid/Parent.tue", Message: `v-model target "title" is not writable`, Line: 6, Column: 19},
		{Path: "testdata/invalid/Parent.tue", Message: `v-for requires a :key attribute`, Line: 8, Column: 8},
		{Path: "testdata/invalid/Parent.tue", Message: `unknown identifier "missing"`, Line: 10, Column: 9},
		{Path: "testdata/invalid/Parent.tue", Message: `v-if expects bool, got string`, Line: 11, Column: 12},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsMissingRequiredComponentProp(t *testing.T) {
	project, err := parseProjectFixture("testdata/missing_required_prop")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "testdata/missing_required_prop/Parent.tue", Message: `component "UserBadge" requires prop "name"`, Line: 3, Column: 4},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsUnsupportedEventHandlerShapes(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_event_handler")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "testdata/invalid_event_handler/Parent.tue", Message: `event handler "increment" does not accept arguments`, Line: 2, Column: 32},
		{Path: "testdata/invalid_event_handler/Parent.tue", Message: `event handler "save" must have signature func()`, Line: 3, Column: 32},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsUnsupportedComponentEventShapes(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_component_event")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "testdata/invalid_component_event/Parent.tue", Message: `component "UserBadge" has no event "missing"`, Line: 5, Column: 5},
		{Path: "testdata/invalid_component_event/Parent.tue", Message: `component "UserBadge" event "payload" must have signature func()`, Line: 6, Column: 5},
		{Path: "testdata/invalid_component_event/Parent.tue", Message: `event handler "selectUser" does not accept arguments`, Line: 7, Column: 20},
		{Path: "testdata/invalid_component_event/Parent.tue", Message: `event handler "needsValue" must have signature func()`, Line: 8, Column: 16},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsMapKeyTypeDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_map_loop")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	if diff := cmp.Diff([]diagnosticSummary{
		{Path: "testdata/invalid_map_loop/Parent.tue", Message: `operator + requires both operands to be strings or numbers`, Line: 2, Column: 51},
	}, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsClassBindingTypeDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_class_binding")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	expected := []diagnosticSummary{
		{Path: "testdata/invalid_class_binding/Parent.tue", Message: `class binding expects string, got bool`, Line: 2, Column: 16},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsStyleBindingTypeDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_style_binding")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	expected := []diagnosticSummary{
		{Path: "testdata/invalid_style_binding/Parent.tue", Message: `style binding expects string, got bool`, Line: 2, Column: 16},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsBoundAttributeTypeDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_bound_attribute")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	expected := []diagnosticSummary{
		{Path: "testdata/invalid_bound_attribute/Parent.tue", Message: `bound attribute ":href" expects string, got bool`, Line: 2, Column: 15},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsExpressionShapeDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_expression")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	expected := []diagnosticSummary{
		{Path: "testdata/invalid_expression/Parent.tue", Message: `type User has no field "missing"`, Line: 3, Column: 14},
		{Path: "testdata/invalid_expression/Parent.tue", Message: `method call "visible" must have signature func() T`, Line: 4, Column: 12},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsControlFlowDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_control_flow")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	expected := []diagnosticSummary{
		{Path: "testdata/invalid_control_flow/Parent.tue", Message: `v-else must follow v-if`, Line: 3, Column: 6},
		{Path: "testdata/invalid_control_flow/Parent.tue", Message: `v-else cannot follow v-if on an element that also has v-for; use a <template v-for> wrapper`, Line: 6, Column: 8},
		{Path: "testdata/invalid_control_flow/Parent.tue", Message: `v-else must follow v-if`, Line: 9, Column: 8},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsNamedSlotDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_slot_binding")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	expected := []diagnosticSummary{
		{Path: "testdata/invalid_slot_binding/Parent.tue", Message: `named slots are not supported in the default slot slice`, Line: 3, Column: 9},
		{Path: "testdata/invalid_slot_binding/Parent.tue", Message: `named slots are not supported in the default slot slice`, Line: 4, Column: 9},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectReportsModelBindingDiagnostics(t *testing.T) {
	project, err := parseProjectFixture("testdata/invalid_model_binding")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	expected := []diagnosticSummary{
		{Path: "testdata/invalid_model_binding/Parent.tue", Message: `v-model expects bool, got string`, Line: 3, Column: 35},
		{Path: "testdata/invalid_model_binding/Parent.tue", Message: `v-model expects string, got bool`, Line: 4, Column: 20},
		{Path: "testdata/invalid_model_binding/Parent.tue", Message: `v-model is not supported for input type "number"`, Line: 9, Column: 24},
		{Path: "testdata/invalid_model_binding/Parent.tue", Message: `type string has no field "Text"`, Line: 11, Column: 25},
	}
	if diff := cmp.Diff(expected, summarizeDiagnostics(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func TestCheckProjectUsesExpressionSourceSpans(t *testing.T) {
	source, err := testFixture("testdata/invalid/Parent.tue")
	if err != nil {
		t.Fatalf("read span fixture: %v", err)
	}
	project, err := parseProjectFixture("testdata/invalid")
	if err != nil {
		t.Fatalf("parse project fixture: %v", err)
	}

	diagnostics := CheckProject(project)
	diagnostic, ok := findDiagnostic(diagnostics, `component "UserBadge" prop "isAdmin" expects bool, got string`)
	if !ok {
		t.Fatalf("diagnostic not found in %#v", summarizeDiagnostics(diagnostics))
	}

	expectedOffset := bytes.Index(source, []byte(`"yes"`))
	if expectedOffset == -1 {
		t.Fatal(`embedded fixture does not contain ""yes""`)
	}
	if diff := cmp.Diff(expectedOffset, diagnostic.Span.Start.Offset); diff != "" {
		t.Errorf("mismatch diagnostic start offset (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff(sfc.Position{Offset: expectedOffset, Line: 3, Column: 40}, diagnostic.Span.Start); diff != "" {
		t.Errorf("mismatch diagnostic start position (-expected, +actual):\n%s", diff)
	}
}

func parseProjectFixture(dir string) (Project, error) {
	entries, err := fs.ReadDir(testFixtures, dir)
	if err != nil {
		return Project{}, fmt.Errorf("read embedded fixture dir %s: %w", dir, err)
	}

	project := Project{Files: make([]File, 0, len(entries))}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".tue" {
			continue
		}

		path := filepath.ToSlash(filepath.Join(dir, entry.Name()))
		source, err := testFixture(path)
		if err != nil {
			return Project{}, err
		}
		sfcFile, sfcDiagnostics := sfc.Parse(path, source)
		if len(sfcDiagnostics) != 0 {
			return Project{}, fmt.Errorf("sfc.Parse(%s) diagnostics = %#v, want none", path, sfcDiagnosticMessages(sfcDiagnostics))
		}

		templateTree, templateDiagnostics := gotemplate.ParseBlock(sfcFile.Template)
		if len(templateDiagnostics) != 0 {
			return Project{}, fmt.Errorf("template.ParseBlock(%s) diagnostics = %#v, want none", path, templateDiagnosticMessages(templateDiagnostics))
		}

		scriptFile, scriptDiagnostics := script.ParseSFC(sfcFile)
		if len(scriptDiagnostics) != 0 {
			return Project{}, fmt.Errorf("script.ParseSFC(%s) diagnostics = %#v, want none", path, scriptDiagnosticMessages(scriptDiagnostics))
		}

		project.Files = append(project.Files, File{
			Path:     path,
			Template: templateTree,
			Script:   scriptFile,
		})
	}
	return project, nil
}

func testFixture(path string) ([]byte, error) {
	source, err := testFixtures.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read embedded fixture %s: %w", path, err)
	}
	return source, nil
}

func findDiagnostic(diagnostics []Diagnostic, message string) (Diagnostic, bool) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Message == message {
			return diagnostic, true
		}
	}
	return Diagnostic{}, false
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
