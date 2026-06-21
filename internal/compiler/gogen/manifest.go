package gogen

import "github.com/norunners/tue/internal/compiler/sfc"

// Manifest records generated files and source spans for later diagnostics.
type Manifest struct {
	GeneratedBy string          `json:"generatedBy"`
	StyleFile   string          `json:"styleFile,omitempty"`
	Assets      []ManifestAsset `json:"assets,omitempty"`
	Files       []ManifestFile  `json:"files"`
}

// ManifestAsset records one copied static asset.
type ManifestAsset struct {
	Source string `json:"source"`
	Output string `json:"output"`
	Public bool   `json:"public,omitempty"`
}

// ManifestFile records the generated output for one source component.
type ManifestFile struct {
	Source        string         `json:"source"`
	Component     string         `json:"component"`
	ScriptFile    string         `json:"scriptFile"`
	ComponentFile string         `json:"componentFile,omitempty"`
	RenderFile    string         `json:"renderFile"`
	ScopeAttr     string         `json:"scopeAttr,omitempty"`
	Nodes         []ManifestNode `json:"nodes"`
}

// ManifestNode records a generated VNode's source span.
type ManifestNode struct {
	Kind       string   `json:"kind"`
	Tag        string   `json:"tag,omitempty"`
	SourceSpan sfc.Span `json:"sourceSpan"`
}
