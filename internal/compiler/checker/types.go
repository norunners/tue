package checker

import (
	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
)

// Project is the parsed compiler input for template checking.
type Project struct {
	Files []File
}

// File is one parsed .tue source file.
type File struct {
	Path     string
	Template *gotemplate.Tree
	Script   *script.File
}

// Diagnostic is a source-mapped template checker diagnostic.
type Diagnostic struct {
	Path    string
	Message string
	Span    sfc.Span
}
