package gogen

import "github.com/norunners/tue/internal/compiler/sfc"

// Manifest records generated files and source spans for later diagnostics.
type Manifest struct {
	GeneratedBy string         `json:"generatedBy"`
	StyleFile   string         `json:"styleFile,omitempty"`
	Files       []ManifestFile `json:"files"`
}

// ManifestFile records the generated output for one source component.
type ManifestFile struct {
	Source     string         `json:"source"`
	Component  string         `json:"component"`
	ScriptFile string         `json:"scriptFile"`
	RenderFile string         `json:"renderFile"`
	ScopeAttr  string         `json:"scopeAttr,omitempty"`
	Nodes      []ManifestNode `json:"nodes"`
}

// ManifestNode records a generated VNode's source span.
type ManifestNode struct {
	Kind       string   `json:"kind"`
	Tag        string   `json:"tag,omitempty"`
	SourceSpan sfc.Span `json:"sourceSpan"`
}
