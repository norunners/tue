package template

import "github.com/norunners/tue/internal/compiler/sfc"

// NodeKind identifies the concrete shape represented by a Node.
type NodeKind string

const (
	NodeElement       NodeKind = "element"
	NodeText          NodeKind = "text"
	NodeInterpolation NodeKind = "interpolation"
	NodeComment       NodeKind = "comment"
)

// AttrKind identifies the syntactic kind of a template attribute.
type AttrKind string

const (
	AttrStatic    AttrKind = "static"
	AttrBind      AttrKind = "bind"
	AttrEvent     AttrKind = "event"
	AttrDirective AttrKind = "directive"
)

// DirectiveKind identifies a supported v-* directive.
type DirectiveKind string

const (
	DirectiveIf    DirectiveKind = "if"
	DirectiveElse  DirectiveKind = "else"
	DirectiveFor   DirectiveKind = "for"
	DirectiveModel DirectiveKind = "model"
	DirectiveHTML  DirectiveKind = "html"
)

// Tree is a parsed template AST.
type Tree struct {
	Nodes []*Node
	Span  sfc.Span
}

// Node is a concrete template AST node. Fields are populated according to Kind.
type Node struct {
	Kind           NodeKind
	Span           sfc.Span
	ContentSpan    sfc.Span
	Tag            string
	TagSpan        sfc.Span
	IsComponent    bool
	SelfClosing    bool
	Attrs          []Attr
	Children       []*Node
	Text           string
	Expression     string
	ExpressionSpan sfc.Span
}

// Attr is a parsed element attribute or directive.
type Attr struct {
	Kind           AttrKind
	Directive      DirectiveKind
	RawName        string
	Name           string
	Argument       string
	Value          string
	Expression     string
	HasValue       bool
	Span           sfc.Span
	NameSpan       sfc.Span
	DirectiveSpan  sfc.Span
	ArgumentSpan   sfc.Span
	ValueSpan      sfc.Span
	ExpressionSpan sfc.Span
}

// Diagnostic is a source-mapped template parse diagnostic.
type Diagnostic struct {
	Message string
	Span    sfc.Span
}
