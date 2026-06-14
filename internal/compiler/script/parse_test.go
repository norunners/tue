package script

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/norunners/tue/internal/compiler/sfc"
)

//go:embed testdata/contracts/*.go testdata/diagnostics/*.go testdata/sfc/*.tue
var testFixtures embed.FS

func TestParseExtractsComponentContract(t *testing.T) {
	file, diagnostics := Parse(testFixture(t, "testdata/contracts/dashboard.go"), "Dashboard")
	if len(diagnostics) != 0 {
		t.Errorf("Parse diagnostics = %#v, want none", diagnosticMessages(diagnostics))
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

	component := requireComponent(t, file)
	if diff := cmp.Diff("Dashboard", component.Name); diff != "" {
		t.Errorf("mismatch component name (-expected, +actual):\n%s", diff)
	}

	props := requireProps(t, component, 1)
	if len(props) == 1 {
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
	}

	assertField(t, component.Refs, "count", FieldKindRef, "tue.Ref[int]", "int")
	assertField(t, component.Computed, "total", FieldKindComputed, "tue.Computed[int]", "int")
	assertField(t, component.Resources, "user", FieldKindResource, "tue.Resource[User]", "User")
	assertField(t, component.State, "label", FieldKindState, "string", "")

	init := requireInit(t, component)
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
	file, diagnostics := Parse(testFixture(t, "testdata/contracts/dot_import.go"), "DotImport")
	if len(diagnostics) != 0 {
		t.Errorf("Parse diagnostics = %#v, want none", diagnosticMessages(diagnostics))
	}

	component := requireComponent(t, file)
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
	source := testFixture(t, "testdata/sfc/user_badge.tue")
	sfcFile, sfcDiagnostics := sfc.Parse("UserBadge.tue", source)
	if len(sfcDiagnostics) != 0 {
		t.Fatalf("sfc.Parse diagnostics = %#v, want none", sfcDiagnostics)
	}

	file, diagnostics := ParseBlock(sfcFile.Script, "UserBadge")
	if len(diagnostics) != 0 {
		t.Errorf("ParseBlock diagnostics = %#v, want none", diagnosticMessages(diagnostics))
	}

	component := requireComponent(t, file)
	props := requireProps(t, component, 1)
	if len(props) != 1 {
		return
	}

	prop := props[0]
	nameOffset := bytes.Index(source, []byte("name tue.Prop"))
	if nameOffset == -1 {
		t.Fatal(`embedded SFC fixture does not contain "name tue.Prop"`)
	}
	if diff := cmp.Diff(sfc.Position{Offset: nameOffset, Line: 8, Column: 2}, prop.Field.NameSpan.Start); diff != "" {
		t.Errorf("mismatch prop name start: %q (-expected, +actual):\n%s", prop.Field.Name, diff)
	}
	packageOffset := bytes.Index(source, []byte("package fixtures"))
	if packageOffset == -1 {
		t.Fatal(`embedded SFC fixture does not contain "package fixtures"`)
	}
	if diff := cmp.Diff(sfc.Position{Offset: packageOffset, Line: 3, Column: 1}, file.PackageSpan.Start); diff != "" {
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
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			sfcFile, sfcDiagnostics := sfc.Parse(path, source)
			if len(sfcDiagnostics) != 0 {
				t.Fatalf("sfc.Parse diagnostics = %#v, want none", sfcDiagnostics)
			}

			file, diagnostics := ParseSFC(sfcFile)
			if len(diagnostics) != 0 {
				t.Errorf("ParseSFC diagnostics = %#v, want none", diagnosticMessages(diagnostics))
			}
			component := requireComponent(t, file)
			if diff := cmp.Diff(ComponentNameFromPath(path), component.Name); diff != "" {
				t.Errorf("mismatch component name (-expected, +actual):\n%s", diff)
			}
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk fixtures: %v", err)
	}
}

func TestParseDiagnostics(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		component string
		want      []string
	}{
		{
			name:      "missing component",
			fixture:   "testdata/diagnostics/missing_component.go",
			component: "Missing",
			want:      []string{`component "Missing" struct not found`},
		},
		{
			name:      "component must be struct",
			fixture:   "testdata/diagnostics/component_must_be_struct.go",
			component: "App",
			want:      []string{`component "App" must be a struct`},
		},
		{
			name:      "prop without type argument",
			fixture:   "testdata/diagnostics/prop_without_type_argument.go",
			component: "App",
			want:      []string{`field "name" must use tue.Prop[T] with exactly one type argument`},
		},
		{
			name:      "prop with too many type arguments",
			fixture:   "testdata/diagnostics/prop_with_too_many_type_arguments.go",
			component: "App",
			want:      []string{`field "name" must use tue.Prop[T] with exactly one type argument`},
		},
		{
			name:      "invalid Init value receiver",
			fixture:   "testdata/diagnostics/invalid_init_value_receiver.go",
			component: "App",
			want:      []string{`Init must have signature func (c *App) Init(tue.Context)`},
		},
		{
			name:      "invalid Init params",
			fixture:   "testdata/diagnostics/invalid_init_params.go",
			component: "App",
			want:      []string{`Init must have signature func (c *App) Init(tue.Context)`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics := Parse(testFixture(t, tt.fixture), tt.component)
			assertDiagnosticContains(t, diagnostics, tt.want)
		})
	}
}

func TestParseBlockDiagnostics(t *testing.T) {
	_, diagnostics := ParseBlock(nil, "App")
	assertDiagnosticMessages(t, diagnostics, []string{"missing script block"})
}

func TestComponentNameFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "counter.tue", want: "Counter"},
		{path: "static_hello.tue", want: "StaticHello"},
		{path: "components/user-badge.tue", want: "UserBadge"},
		{path: `windows\path\UserBadge.tue`, want: "UserBadge"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, ComponentNameFromPath(tt.path)); diff != "" {
				t.Errorf("mismatch component name from path: %q (-expected, +actual):\n%s", tt.path, diff)
			}
		})
	}
}

func testFixture(t *testing.T, path string) []byte {
	t.Helper()

	source, err := testFixtures.ReadFile(path)
	if err != nil {
		t.Fatalf("read embedded fixture %s: %v", path, err)
	}
	return source
}

func requireComponent(t *testing.T, file *File) *Component {
	t.Helper()

	if file.Component == nil {
		t.Fatal("Component is nil")
	}
	return file.Component
}

func requireProps(t *testing.T, component *Component, want int) []Prop {
	t.Helper()

	if diff := cmp.Diff(want, len(component.Props)); diff != "" {
		t.Errorf("mismatch props length (-expected, +actual):\n%s", diff)
	}
	return component.Props
}

func requireInit(t *testing.T, component *Component) *Method {
	t.Helper()

	if component.Init == nil {
		t.Fatal("Init is nil")
	}
	return component.Init
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

func assertDiagnosticMessages(t *testing.T, diagnostics []Diagnostic, want []string) {
	t.Helper()

	if diff := cmp.Diff(want, diagnosticMessages(diagnostics)); diff != "" {
		t.Errorf("mismatch diagnostics (-expected, +actual):\n%s", diff)
	}
}

func assertDiagnosticContains(t *testing.T, diagnostics []Diagnostic, want []string) {
	t.Helper()

	got := diagnosticMessages(diagnostics)
	for _, message := range want {
		found := false
		for _, gotMessage := range got {
			if gotMessage == message {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("diagnostics = %#v, want message %q", got, message)
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
