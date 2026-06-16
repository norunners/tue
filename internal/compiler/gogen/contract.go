package gogen

import (
	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
)

// Project is the parsed compiler input for Go generation.
type Project struct {
	Files []File
}

// File is one parsed .tue source file plus the original script source.
type File struct {
	Path         string
	Template     *gotemplate.Tree
	Script       *script.File
	ScriptSource string
	Style        *Style
}

// Style is one parsed <style> block attached to a .tue file.
type Style struct {
	Source string
	Scoped bool
	Span   sfc.Span
}

// Result is the generated output for a project.
type Result struct {
	Files    []GeneratedFile
	Manifest Manifest
}

// GeneratedFile is a generated file path and source, relative to .tue-cache.
type GeneratedFile struct {
	Path   string
	Source []byte
}

// Diagnostic is a source-mapped generation diagnostic.
type Diagnostic struct {
	Path    string
	Message string
	Span    sfc.Span
}
