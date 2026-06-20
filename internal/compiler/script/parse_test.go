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

//go:embed testdata/contracts/*.go testdata/diagnostics/*.go testdata/sfc/*.tue
var testFixtures embed.FS

func TestParseExtractsComponentContract(t *testing.T) {
	source, err := testFixture("testdata/contracts/dashboard.go")
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
	if diff := cmp.Diff([]importSummary{{
		Name: "tue",
		Path: "github.com/norunners/tue",
	}}, summarizeImports(file.Imports)); diff != "" {
		t.Errorf("mismatch imports (-expected, +actual):\n%s", diff)
	}
	expectedTypes := map[string]bool{
		"Dashboard": false,
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
		{Name: "Dashboard", Fields: []fieldSummary{
			{Kind: FieldKindState, Name: "name", Type: "tue.Prop[string]"},
			{Kind: FieldKindState, Name: "count", Type: "tue.Ref[int]"},
			{Kind: FieldKindState, Name: "total", Type: "tue.Computed[int]"},
			{Kind: FieldKindState, Name: "user", Type: "tue.Resource[User]"},
			{Kind: FieldKindState, Name: "onSave", Type: "tue.On[func()]"},
			{Kind: FieldKindState, Name: "label", Type: "string"},
		}},
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

	props, err := requireProps(component, 1)
	if err != nil {
		t.Fatal(err)
	}
	prop := props[0]
	if diff := cmp.Diff(propSummary{
		FieldName:  "name",
		Name:       "name",
		Required:   true,
		Exported:   false,
		Type:       "tue.Prop[string]",
		ValueType:  "string",
		FieldKind:  FieldKindProp,
		StructTag:  `prop:"name,required"`,
		HasTagSpan: true,
	}, summarizeProp(prop)); diff != "" {
		t.Errorf("mismatch prop: %q (-expected, +actual):\n%s", prop.Field.Name, diff)
	}

	assertField(t, component.Refs, "count", FieldKindRef, "tue.Ref[int]", "int")
	assertField(t, component.Computed, "total", FieldKindComputed, "tue.Computed[int]", "int")
	assertField(t, component.Resources, "user", FieldKindResource, "tue.Resource[User]", "User")
	assertField(t, component.Events, "onSave", FieldKindEvent, "tue.On[func()]", "func()")
	assertField(t, component.State, "label", FieldKindState, "string", "")

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
	}, summarizeMethods(component.Methods)); diff != "" {
		t.Errorf("mismatch methods (-expected, +actual):\n%s", diff)
	}

	if diff := cmp.Diff(allocationSummary{
		ComponentName: "Dashboard",
		PropFields:    []string{"name"},
		CallsInit:     true,
	}, summarizeAllocation(component.Allocation)); diff != "" {
		t.Errorf("mismatch allocation (-expected, +actual):\n%s", diff)
	}
}

func TestParseAcceptsDotImportedTueContracts(t *testing.T) {
	source, err := testFixture("testdata/contracts/dot_import.go")
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
	if diff := cmp.Diff([]propSummary{{
		FieldName: "name",
		Name:      "name",
		Type:      "Prop[string]",
		ValueType: "string",
		FieldKind: FieldKindProp,
	}}, summarizeProps(component.Props)); diff != "" {
		t.Errorf("mismatch props (-expected, +actual):\n%s", diff)
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

	file, diagnostics := ParseBlock(sfcFile.Script, "UserBadge")
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
	nameOffset := bytes.Index(source, []byte("name tue.Prop"))
	if nameOffset == -1 {
		t.Fatal(`embedded SFC fixture does not contain "name tue.Prop"`)
	}
	if diff := cmp.Diff(sfc.Position{Offset: nameOffset, Line: 9, Column: 2}, prop.Field.NameSpan.Start); diff != "" {
		t.Errorf("mismatch prop name start: %q (-expected, +actual):\n%s", prop.Field.Name, diff)
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
			name:      "prop without type argument",
			fixture:   "testdata/diagnostics/prop_without_type_argument.go",
			component: "App",
			expected:  []string{`field "name" must use tue.Prop[T] with exactly one type argument`},
		},
		{
			name:      "prop with too many type arguments",
			fixture:   "testdata/diagnostics/prop_with_too_many_type_arguments.go",
			component: "App",
			expected:  []string{`field "name" must use tue.Prop[T] with exactly one type argument`},
		},
		{
			name:      "legacy component event",
			fixture:   "testdata/diagnostics/legacy_component_event.go",
			component: "App",
			expected:  []string{`component event field "onSave" must use tue.On[func(...)]`},
		},
		{
			name:      "component event requires function type",
			fixture:   "testdata/diagnostics/component_event_non_function.go",
			component: "App",
			expected:  []string{`field "onSave" must use tue.On[F] with a function type`},
		},
		{
			name:      "component event requires event field name",
			fixture:   "testdata/diagnostics/component_event_invalid_name.go",
			component: "App",
			expected:  []string{`component event field "save" must start with on followed by an uppercase event name`},
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

func assertField(t *testing.T, fields []Field, name string, kind FieldKind, fieldType string, valueType string) {
	t.Helper()

	for _, field := range fields {
		if field.Name == name {
			if diff := cmp.Diff(fieldSummary{
				Kind:      kind,
				Name:      name,
				Type:      fieldType,
				ValueType: valueType,
				Exported:  false,
			}, summarizeField(field)); diff != "" {
				t.Errorf("mismatch field: %q (-expected, +actual):\n%s", name, diff)
			}
			return
		}
	}
	t.Errorf("field %q not found in %#v", name, summarizeFields(fields))
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
	FieldName  string
	Name       string
	Required   bool
	Exported   bool
	Type       string
	ValueType  string
	FieldKind  FieldKind
	StructTag  string
	HasTagSpan bool
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
	PropFields    []string
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
		FieldName:  prop.Field.Name,
		Name:       prop.Name,
		Required:   prop.Required,
		Exported:   prop.Field.Exported,
		Type:       prop.Field.Type,
		ValueType:  prop.Field.ValueType,
		FieldKind:  prop.Field.Kind,
		StructTag:  prop.Field.Tag,
		HasTagSpan: prop.Field.TagSpan.Start.Offset != 0 || prop.Field.TagSpan.End.Offset != 0,
	}
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
		PropFields:    allocation.PropFields,
		CallsInit:     allocation.CallsInit,
	}
}
