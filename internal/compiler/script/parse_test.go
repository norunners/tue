package script

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/norunners/tue/internal/compiler/sfc"
)

//go:embed testdata/components/*.go testdata/diagnostics/*.go testdata/sfc/*.tue
var testFixtures embed.FS

func TestParseExtractsComponentDeclaration(t *testing.T) {
	source, err := testFixture("testdata/components/dashboard.go")
	if err != nil {
		t.Fatalf("read dashboard fixture: %v", err)
	}
	file, diagnostics := Parse(source, "Dashboard")
	if len(diagnostics) != 0 {
		t.Errorf("Parse diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}

	if diff := cmp.Diff("fixtures", file.PackageName); diff != "" {
		t.Errorf("mismatch package name (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]importSummary{
		{Name: "context", Path: "context"},
		{Name: "tue", Path: "github.com/norunners/tue"},
	}, summarizeImports(file.Imports)); diff != "" {
		t.Errorf("mismatch imports (-expected, +actual):\n%s", diff)
	}
	expectedTypes := map[string]bool{
		"Dashboard": true,
		"User":      true,
	}
	actualTypes := make(map[string]bool, len(expectedTypes))
	for _, info := range file.Types {
		if _, ok := expectedTypes[info.Expression]; ok {
			actualTypes[info.Expression] = info.Comparable
		}
	}
	if diff := cmp.Diff(expectedTypes, actualTypes); diff != "" {
		t.Errorf("mismatch declared types (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]structSummary{
		{Name: "User", Fields: []fieldSummary{}},
		{Name: "Dashboard", Fields: []fieldSummary{}},
	}, summarizeStructs(file.Structs)); diff != "" {
		t.Errorf("mismatch structs (-expected, +actual):\n%s", diff)
	}

	component, err := requireComponent(file)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff("Dashboard", component.Name); diff != "" {
		t.Errorf("mismatch component name (-expected, +actual):\n%s", diff)
	}

	props, err := requireProps(component, 3)
	if err != nil {
		t.Fatal(err)
	}
	expectedProps := []propSummary{
		{Name: "name", GoName: "Name", Type: "string", Required: true},
		{Name: "active", GoName: "Active", Type: "bool"},
		{Name: "user-id", GoName: "UserID", Type: "string"},
	}
	if diff := cmp.Diff(expectedProps, summarizeProps(props)); diff != "" {
		t.Errorf("mismatch props (-expected, +actual):\n%s", diff)
	}
	expectedEvents := []eventSummary{
		{Name: "close", GoName: "Close", FunctionType: "func()"},
		{Name: "select", GoName: "Select", FunctionType: "func(name string)"},
		{Name: "range", GoName: "Range", FunctionType: "func(name string, count int)"},
		{Name: "pointer", GoName: "Pointer", FunctionType: "func(value *User)"},
		{Name: "variadic", GoName: "Variadic", FunctionType: "func(values ...string)"},
	}
	if diff := cmp.Diff(expectedEvents, summarizeEvents(component.Events)); diff != "" {
		t.Errorf("mismatch events (-expected, +actual):\n%s", diff)
	}
	expectedStates := []stateSummary{
		{Name: "expanded", GoName: "Expanded", Type: "bool"},
		{Name: "count", GoName: "Count", Type: "int"},
	}
	if diff := cmp.Diff(expectedStates, summarizeStates(component.States)); diff != "" {
		t.Errorf("mismatch component states (-expected, +actual):\n%s", diff)
	}

	expectedComputeds := []computedSummary{
		{Name: "label", GoName: "Label", MethodName: "label", Type: "string"},
		{Name: "total", GoName: "Total", MethodName: "total", Type: "int"},
	}
	if diff := cmp.Diff(expectedComputeds, summarizeComputeds(component.Computed)); diff != "" {
		t.Errorf("mismatch component computeds (-expected, +actual):\n%s", diff)
	}
	expectedResources := []resourceSummary{
		{Name: "user", GoName: "User", MethodName: "loadUser", Type: "User"},
	}
	if diff := cmp.Diff(expectedResources, summarizeResources(component.Resources)); diff != "" {
		t.Errorf("mismatch component resources (-expected, +actual):\n%s", diff)
	}

	init, err := requireInit(component)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(methodSummary{
		Name:            "Init",
		ReceiverName:    "Dashboard",
		PointerReceiver: true,
		Parameters: []parameterSummary{{
			Name: "ctx",
			Type: "tue.Context",
		}},
	}, summarizeMethod(init)); diff != "" {
		t.Errorf("mismatch method: %q (-expected, +actual):\n%s", init.Name, diff)
	}

	if diff := cmp.Diff([]methodSummary{
		{
			Name:            "increment",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
		},
		{
			Name:            "snapshot",
			ReceiverName:    "Dashboard",
			PointerReceiver: false,
			Results: []parameterSummary{{
				Type: "string",
			}},
		},
		{
			Name:            "label",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "string"}},
		},
		{
			Name:            "total",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "int"}},
		},
		{
			Name:            "loadUser",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Parameters:      []parameterSummary{{Type: "context.Context"}},
			Results: []parameterSummary{
				{Type: "User"},
				{Type: "error"},
			},
		},
		{
			Name:            "Name",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "string"}},
		},
		{
			Name:            "NameOk",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results: []parameterSummary{
				{Type: "string"},
				{Type: "bool"},
			},
		},
		{
			Name:            "Active",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "bool"}},
		},
		{
			Name:            "ActiveOk",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results: []parameterSummary{
				{Type: "bool"},
				{Type: "bool"},
			},
		},
		{
			Name:            "UserID",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "string"}},
		},
		{
			Name:            "UserIDOk",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results: []parameterSummary{
				{Type: "string"},
				{Type: "bool"},
			},
		},
		{
			Name:            "Close",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "bool"}},
		},
		{
			Name:            "Select",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Parameters:      []parameterSummary{{Name: "name", Type: "string"}},
			Results:         []parameterSummary{{Type: "bool"}},
		},
		{
			Name:            "Range",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Parameters: []parameterSummary{
				{Name: "name", Type: "string"},
				{Name: "count", Type: "int"},
			},
			Results: []parameterSummary{{Type: "bool"}},
		},
		{
			Name:            "Pointer",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Parameters:      []parameterSummary{{Name: "value", Type: "*User"}},
			Results:         []parameterSummary{{Type: "bool"}},
		},
		{
			Name:            "Variadic",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Parameters:      []parameterSummary{{Name: "values", Type: "...string"}},
			Results:         []parameterSummary{{Type: "bool"}},
		},
		{
			Name:            "Expanded",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "bool"}},
		},
		{
			Name:            "ExpandedSet",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Parameters:      []parameterSummary{{Name: "value", Type: "bool"}},
		},
		{
			Name:            "Count",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "int"}},
		},
		{
			Name:            "CountSet",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Parameters:      []parameterSummary{{Name: "value", Type: "int"}},
		},
		{
			Name:            "Label",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "string"}},
		},
		{
			Name:            "Total",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "int"}},
		},
		{
			Name:            "User",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "User"}},
		},
		{
			Name:            "UserOk",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results: []parameterSummary{
				{Type: "User"},
				{Type: "bool"},
			},
		},
		{
			Name:            "UserLoading",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "bool"}},
		},
		{
			Name:            "UserError",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
			Results:         []parameterSummary{{Type: "error"}},
		},
		{
			Name:            "UserReload",
			ReceiverName:    "Dashboard",
			PointerReceiver: true,
		},
	}, summarizeMethods(component.Methods)); diff != "" {
		t.Errorf("mismatch methods (-expected, +actual):\n%s", diff)
	}

	if diff := cmp.Diff(allocationSummary{
		ComponentName: "Dashboard",
		CallsInit:     true,
	}, summarizeAllocation(component.Allocation)); diff != "" {
		t.Errorf("mismatch allocation (-expected, +actual):\n%s", diff)
	}
}

func TestParseAcceptsDotImportedTueComponentFields(t *testing.T) {
	source, err := testFixture("testdata/components/dot_import.go")
	if err != nil {
		t.Fatalf("read dot-import fixture: %v", err)
	}
	file, diagnostics := Parse(source, "DotImport")
	if len(diagnostics) != 0 {
		t.Errorf("Parse diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}

	component, err := requireComponent(file)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]stateSummary{{Name: "count", GoName: "Count", Type: "int"}}, summarizeStates(component.States)); diff != "" {
		t.Errorf("mismatch component states (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]propSummary{{Name: "name", GoName: "Name", Type: "string", Required: true}}, summarizeProps(component.Props)); diff != "" {
		t.Errorf("mismatch props (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff([]eventSummary{{Name: "close", GoName: "Close", FunctionType: "func()"}}, summarizeEvents(component.Events)); diff != "" {
		t.Errorf("mismatch events (-expected, +actual):\n%s", diff)
	}
	if component.Init == nil {
		t.Error("Init is nil")
	}
}

func TestParseBlockUsesSFCSourceSpans(t *testing.T) {
	source, err := testFixture("testdata/sfc/user_badge.tue")
	if err != nil {
		t.Fatalf("read user badge fixture: %v", err)
	}
	sfcFile, sfcDiagnostics := sfc.Parse("UserBadge.tue", source)
	if len(sfcDiagnostics) != 0 {
		t.Fatalf("sfc.Parse diagnostics actual = %#v, expected none", sfcDiagnostics)
	}

	file, diagnostics := ParseSFC(sfcFile)
	if len(diagnostics) != 0 {
		t.Errorf("ParseBlock diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}

	component, err := requireComponent(file)
	if err != nil {
		t.Fatal(err)
	}
	props, err := requireProps(component, 1)
	if err != nil {
		t.Fatal(err)
	}

	prop := props[0]
	if diff := cmp.Diff(propSummary{Name: "name", GoName: "Name", Type: "string", Required: true}, summarizeProp(prop)); diff != "" {
		t.Errorf("mismatch prop: %q (-expected, +actual):\n%s", prop.Name, diff)
	}
	if diff := cmp.Diff([]eventSummary{{Name: "select", GoName: "Select", FunctionType: "func(value string)"}}, summarizeEvents(component.Events)); diff != "" {
		t.Errorf("mismatch events (-expected, +actual):\n%s", diff)
	}
	if diff := cmp.Diff("tueUserBadgeData", component.GeneratedType); diff != "" {
		t.Errorf("mismatch generated component support type (-expected, +actual):\n%s", diff)
	}
	nameOffset := bytes.Index(source, []byte("Name     string"))
	if nameOffset == -1 {
		t.Fatal(`embedded SFC fixture does not contain "Name     string"`)
	}
	if diff := cmp.Diff(sfc.Position{Offset: nameOffset, Line: 10, Column: 3}, prop.NameSpan.Start); diff != "" {
		t.Errorf("mismatch prop name start: %q (-expected, +actual):\n%s", prop.Name, diff)
	}
	packageOffset := bytes.Index(source, []byte("package fixtures"))
	if packageOffset == -1 {
		t.Fatal(`embedded SFC fixture does not contain "package fixtures"`)
	}
	if diff := cmp.Diff(sfc.Position{Offset: packageOffset, Line: 4, Column: 1}, file.PackageSpan.Start); diff != "" {
		t.Errorf("mismatch package start (-expected, +actual):\n%s", diff)
	}
}

func TestParseSFCFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "fixtures")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".tue" {
			return nil
		}

		t.Run(filepath.ToSlash(path), func(t *testing.T) {
			component, err := parseSFCFixture(path)
			if err != nil {
				t.Errorf("parse SFC fixture: %v", err)
				return
			}
			if diff := cmp.Diff(ComponentNameFromPath(path), component.Name); diff != "" {
				t.Errorf("mismatch component name (-expected, +actual):\n%s", diff)
			}
		})

		return nil
	})
	if err != nil {
		t.Errorf("walk fixtures: %v", err)
	}
}

func TestParseDiagnostics(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		component string
		expected  []string
	}{
		{
			name:      "missing component",
			fixture:   "testdata/diagnostics/missing_component.go",
			component: "Missing",
			expected:  []string{`component "Missing" struct not found`},
		},
		{
			name:      "component must be struct",
			fixture:   "testdata/diagnostics/component_must_be_struct.go",
			component: "App",
			expected:  []string{`component "App" must be a struct`},
		},
		{
			name:      "invalid Init value receiver",
			fixture:   "testdata/diagnostics/invalid_init_value_receiver.go",
			component: "App",
			expected:  []string{`Init must have signature func (c *App) Init(tue.Context)`},
		},
		{
			name:      "invalid Init params",
			fixture:   "testdata/diagnostics/invalid_init_params.go",
			component: "App",
			expected:  []string{`Init must have signature func (c *App) Init(tue.Context)`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := testFixture(test.fixture)
			if err != nil {
				t.Fatalf("read diagnostic fixture: %v", err)
			}
			_, diagnostics := Parse(source, test.component)
			assertDiagnosticContains(t, diagnostics, test.expected)
		})
	}
}

func TestParseSFCComponentDeclarationDiagnostics(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		expected []string
	}{
		{
			name:     "duplicate event",
			fixture:  "component_duplicate_event.tue",
			expected: []string{`duplicate component declaration name "select"`},
		},
		{
			name:     "duplicate prop",
			fixture:  "component_duplicate_prop.tue",
			expected: []string{`duplicate component declaration name "name"`},
		},
		{
			name:     "invalid field",
			fixture:  "component_invalid_field.tue",
			expected: []string{`component declaration field "Name": must declare exactly one prop, event, state, computed, or resource tag`},
		},
		{
			name:     "unknown tag",
			fixture:  "component_invalid_prop_marker.tue",
			expected: []string{`component declaration field "Name": unknown component tag "json"`},
		},
		{
			name:     "member collision",
			fixture:  "component_member_collision.tue",
			expected: []string{`generated prop getter Name conflicts with a component member`},
		},
		{
			name:     "reserved storage",
			fixture:  "component_reserved_storage.tue",
			expected: []string{`component member __tue is reserved for generated storage`},
		},
		{
			name:     "event result",
			fixture:  "component_invalid_event_arity.tue",
			expected: []string{`component event "range" must not return values`},
		},
		{
			name:     "default option",
			fixture:  "component_prop_default_option.tue",
			expected: []string{`component prop "Active": unknown option "default=true"`},
		},
		{
			name:     "marker must use anonymous struct",
			fixture:  "component_must_be_struct.tue",
			expected: []string{`tue.Comp type argument must be an anonymous struct`},
		},
		{
			name:     "unexported field",
			fixture:  "component_unexported_field.tue",
			expected: []string{`component declaration field "name" must be exported`},
		},
		{
			name:     "invalid event tag",
			fixture:  "component_invalid_event_tag.tue",
			expected: []string{`component event "Select": tag does not accept option "required"`},
		},
		{
			name:     "event collision",
			fixture:  "component_event_collision.tue",
			expected: []string{`generated event method Select conflicts with a component member`},
		},
		{
			name:     "presence collision",
			fixture:  "component_presence_collision.tue",
			expected: []string{`generated prop presence getter NameOk conflicts with a component member`},
		},
		{
			name:     "multiple markers",
			fixture:  "component_multiple_markers.tue",
			expected: []string{`component may embed at most one tue.Comp marker`},
		},
		{
			name:     "mixed tags",
			fixture:  "component_mixed_tags.tue",
			expected: []string{`component declaration field "Name": must declare exactly one prop, event, state, computed, or resource tag`},
		},
		{
			name:     "named marker",
			fixture:  "component_named_marker.tue",
			expected: []string{`tue.Comp marker must be embedded anonymously`},
		},
		{
			name:     "state function",
			fixture:  "component_state_function.tue",
			expected: []string{`component state "compute" must not have a function type`},
		},
		{
			name:     "state setter collision",
			fixture:  "component_state_setter_collision.tue",
			expected: []string{`generated state setter ExpandedSet conflicts with a component member`},
		},
		{
			name:     "duplicate category name",
			fixture:  "component_duplicate_name.tue",
			expected: []string{`duplicate component declaration name "value"`},
		},
		{
			name:     "invalid marker arity",
			fixture:  "component_invalid_arity.tue",
			expected: []string{`tue.Comp marker requires exactly one anonymous struct type argument`},
		},
		{
			name:     "function prop",
			fixture:  "component_prop_function.tue",
			expected: []string{`component prop "format" must not have a function type`},
		},
		{
			name:     "state outside marker",
			fixture:  "component_external_state.tue",
			expected: []string{`component state "Count" must be declared inside embedded tue.Comp`},
		},
		{
			name:     "computed source is required",
			fixture:  "component_computed_empty_source.tue",
			expected: []string{`component computed "label" must name its source method`},
		},
		{
			name:     "computed source is an identifier",
			fixture:  "component_computed_invalid_source.tue",
			expected: []string{`component computed "label" has invalid source method "label-value"`},
		},
		{
			name:     "computed source exists",
			fixture:  "component_computed_missing_method.tue",
			expected: []string{`component computed "label" source method label was not found`},
		},
		{
			name:     "computed source has no parameters",
			fixture:  "component_computed_signature.tue",
			expected: []string{`component computed "label" source method label must have signature func() string`},
		},
		{
			name:     "computed source result matches",
			fixture:  "component_computed_result.tue",
			expected: []string{`component computed "label" source method label must have signature func() string`},
		},
		{
			name:     "computed source returns one value",
			fixture:  "component_computed_no_result.tue",
			expected: []string{`component computed "label" source method label must have signature func() string`},
		},
		{
			name:     "computed getter collision",
			fixture:  "component_computed_collision.tue",
			expected: []string{`generated computed getter Label conflicts with a component member`},
		},
		{
			name:     "computed value is not a function",
			fixture:  "component_computed_function.tue",
			expected: []string{`component computed "format" must not have a function type`},
		},
		{
			name:     "computed outside marker",
			fixture:  "component_external_computed.tue",
			expected: []string{`component computed "Label" must be declared inside embedded tue.Comp`},
		},
		{
			name:     "resource loader is required",
			fixture:  "component_resource_empty_loader.tue",
			expected: []string{`component resource "user" must name its loader method`},
		},
		{
			name:     "resource loader is an identifier",
			fixture:  "component_resource_invalid_loader.tue",
			expected: []string{`component resource "user" has invalid loader method "load-user"`},
		},
		{
			name:     "resource loader exists",
			fixture:  "component_resource_missing_loader.tue",
			expected: []string{`component resource "user" loader method loadUser was not found`},
		},
		{
			name:     "resource loader signature",
			fixture:  "component_resource_signature.tue",
			expected: []string{`component resource "user" loader method loadUser must have signature func(context.Context) (User, error)`},
		},
		{
			name:     "resource getter collision",
			fixture:  "component_resource_collision.tue",
			expected: []string{`generated resource getter User conflicts with a component member`},
		},
		{
			name:     "resource value is not a function",
			fixture:  "component_resource_function.tue",
			expected: []string{`component resource "load" must not have a function type`},
		},
		{
			name:     "resource outside marker",
			fixture:  "component_external_resource.tue",
			expected: []string{`component resource "User" must be declared inside embedded tue.Comp`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := "testdata/sfc/" + test.fixture
			source, err := testFixture(path)
			if err != nil {
				t.Fatalf("read component declaration diagnostic fixture: %v", err)
			}
			sfcFile, sfcDiagnostics := sfc.Parse(test.fixture, source)
			if len(sfcDiagnostics) != 0 {
				t.Fatalf("sfc.Parse diagnostics actual = %#v, expected none", sfcDiagnostics)
			}
			_, diagnostics := ParseSFC(sfcFile)
			assertDiagnosticContains(t, diagnostics, test.expected)
		})
	}
}

func TestParseBlockDiagnostics(t *testing.T) {
	_, diagnostics := ParseBlock(nil, "App")
	assertDiagnosticMessages(t, diagnostics, []string{"missing script block"})
}

func TestComponentNameFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{path: "counter.tue", expected: "Counter"},
		{path: "static_hello.tue", expected: "StaticHello"},
		{path: "components/user-badge.tue", expected: "UserBadge"},
		{path: `windows\path\UserBadge.tue`, expected: "UserBadge"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			if diff := cmp.Diff(test.expected, ComponentNameFromPath(test.path)); diff != "" {
				t.Errorf("mismatch component name from path: %q (-expected, +actual):\n%s", test.path, diff)
			}
		})
	}
}

func TestLowerIdentifierPreservesGoInitialisms(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{
			name:     "Name",
			expected: "name",
		},
		{
			name:     "UserID",
			expected: "userID",
		},
		{
			name:     "URL",
			expected: "url",
		},
		{
			name:     "URLValue",
			expected: "urlValue",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if diff := cmp.Diff(test.expected, lowerIdentifier(test.name)); diff != "" {
				t.Errorf("mismatch lower identifier: %q (-expected, +actual):\n%s", test.name, diff)
			}
		})
	}
}

func parseSFCFixture(path string) (*Component, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture: %w", err)
	}

	sfcFile, sfcDiagnostics := sfc.Parse(path, source)
	if len(sfcDiagnostics) != 0 {
		return nil, fmt.Errorf("sfc.Parse diagnostics actual = %#v, expected none", sfcDiagnostics)
	}

	file, diagnostics := ParseSFC(sfcFile)
	if len(diagnostics) != 0 {
		return nil, fmt.Errorf("ParseSFC diagnostics actual = %#v, expected none", diagnosticMessages(diagnostics))
	}
	component, err := requireComponent(file)
	if err != nil {
		return nil, err
	}
	return component, nil
}

func testFixture(path string) ([]byte, error) {
	source, err := testFixtures.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read embedded fixture %s: %w", path, err)
	}
	return source, nil
}

func requireComponent(file *File) (*Component, error) {
	if file == nil || file.Component == nil {
		return nil, fmt.Errorf("component is nil")
	}
	return file.Component, nil
}

func requireProps(component *Component, expected int) ([]Prop, error) {
	if component == nil {
		return nil, fmt.Errorf("component is nil")
	}
	if len(component.Props) != expected {
		return nil, fmt.Errorf("props length actual = %d, expected %d", len(component.Props), expected)
	}
	return component.Props, nil
}

func requireInit(component *Component) (*Method, error) {
	if component == nil || component.Init == nil {
		return nil, fmt.Errorf("Init is nil")
	}
	return component.Init, nil
}

func assertDiagnosticMessages(t *testing.T, diagnostics []Diagnostic, expected []string) {
	t.Helper()

	if diff := cmp.Diff(expected, diagnosticMessages(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func assertDiagnosticContains(t *testing.T, diagnostics []Diagnostic, expected []string) {
	t.Helper()

	actual := diagnosticMessages(diagnostics)
	for _, message := range expected {
		found := false
		for _, actualMessage := range actual {
			if actualMessage == message {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("diagnostics actual = %#v, expected message %q", actual, message)
		}
	}
}

func diagnosticMessages(diagnostics []Diagnostic) []string {
	messages := make([]string, len(diagnostics))
	for i, diagnostic := range diagnostics {
		messages[i] = diagnostic.Message
	}
	return messages
}

type importSummary struct {
	Name string
	Path string
}

type propSummary struct {
	Name     string
	GoName   string
	Type     string
	Required bool
}

type eventSummary struct {
	Name         string
	GoName       string
	FunctionType string
}

type stateSummary struct {
	Name   string
	GoName string
	Type   string
}

type computedSummary struct {
	Name       string
	GoName     string
	MethodName string
	Type       string
}

type resourceSummary struct {
	Name       string
	GoName     string
	MethodName string
	Type       string
}

type fieldSummary struct {
	Kind      FieldKind
	Name      string
	Exported  bool
	Type      string
	ValueType string
}

type structSummary struct {
	Name   string
	Fields []fieldSummary
}

type methodSummary struct {
	Name            string
	ReceiverName    string
	PointerReceiver bool
	Parameters      []parameterSummary
	Results         []parameterSummary
}

type parameterSummary struct {
	Name string
	Type string
}

type allocationSummary struct {
	ComponentName string
	CallsInit     bool
}

func summarizeImports(imports []Import) []importSummary {
	summaries := make([]importSummary, len(imports))
	for i, item := range imports {
		summaries[i] = importSummary{
			Name: item.Name,
			Path: item.Path,
		}
	}
	return summaries
}

func summarizeProps(props []Prop) []propSummary {
	summaries := make([]propSummary, len(props))
	for i, prop := range props {
		summaries[i] = summarizeProp(prop)
	}
	return summaries
}

func summarizeProp(prop Prop) propSummary {
	return propSummary{
		Name:     prop.Name,
		GoName:   prop.GoName,
		Type:     prop.Type,
		Required: prop.Required,
	}
}

func summarizeEvents(events []Event) []eventSummary {
	summaries := make([]eventSummary, len(events))
	for index, event := range events {
		summaries[index] = eventSummary{Name: event.Name, GoName: event.GoName, FunctionType: event.FunctionType()}
	}
	return summaries
}

func summarizeStates(states []State) []stateSummary {
	summaries := make([]stateSummary, len(states))
	for index, state := range states {
		summaries[index] = stateSummary{Name: state.Name, GoName: state.GoName, Type: state.Type}
	}
	return summaries
}

func summarizeComputeds(computeds []Computed) []computedSummary {
	summaries := make([]computedSummary, len(computeds))
	for index, computed := range computeds {
		summaries[index] = computedSummary{
			Name:       computed.Name,
			GoName:     computed.GoName,
			MethodName: computed.MethodName,
			Type:       computed.Type,
		}
	}
	return summaries
}

func summarizeResources(resources []Resource) []resourceSummary {
	summaries := make([]resourceSummary, len(resources))
	for index, resource := range resources {
		summaries[index] = resourceSummary{
			Name:       resource.Name,
			GoName:     resource.GoName,
			MethodName: resource.MethodName,
			Type:       resource.Type,
		}
	}
	return summaries
}

func summarizeFields(fields []Field) []fieldSummary {
	summaries := make([]fieldSummary, len(fields))
	for i, field := range fields {
		summaries[i] = summarizeField(field)
	}
	return summaries
}

func summarizeStructs(structs []Struct) []structSummary {
	summaries := make([]structSummary, len(structs))
	for i, structure := range structs {
		summaries[i] = structSummary{
			Name:   structure.Name,
			Fields: summarizeFields(structure.Fields),
		}
	}
	return summaries
}

func summarizeField(field Field) fieldSummary {
	return fieldSummary{
		Kind:      field.Kind,
		Name:      field.Name,
		Exported:  field.Exported,
		Type:      field.Type,
		ValueType: field.ValueType,
	}
}

func summarizeMethods(methods []Method) []methodSummary {
	summaries := make([]methodSummary, len(methods))
	for i, method := range methods {
		summaries[i] = summarizeMethod(&method)
	}
	return summaries
}

func summarizeMethod(method *Method) methodSummary {
	return methodSummary{
		Name:            method.Name,
		ReceiverName:    method.ReceiverName,
		PointerReceiver: method.PointerReceiver,
		Parameters:      summarizeParameters(method.Parameters),
		Results:         summarizeParameters(method.Results),
	}
}

func summarizeParameters(parameters []Parameter) []parameterSummary {
	if len(parameters) == 0 {
		return nil
	}

	summaries := make([]parameterSummary, len(parameters))
	for i, parameter := range parameters {
		summaries[i] = parameterSummary{
			Name: parameter.Name,
			Type: parameter.Type,
		}
	}
	return summaries
}

func summarizeAllocation(allocation Allocation) allocationSummary {
	return allocationSummary{
		ComponentName: allocation.ComponentName,
		CallsInit:     allocation.CallsInit,
	}
}
