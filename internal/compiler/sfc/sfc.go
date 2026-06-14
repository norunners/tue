package sfc

// BlockKind identifies a supported top-level .tue block.
type BlockKind string

const (
	BlockTemplate BlockKind = "template"
	BlockScript   BlockKind = "script"
	BlockStyle    BlockKind = "style"
)

// Position is a byte-oriented source position. Line and Column are 1-based.
type Position struct {
	Offset int
	Line   int
	Column int
}

// Span is a half-open source range: Start is inclusive, End is exclusive.
type Span struct {
	Start Position
	End   Position
}

// Attr is an attribute on a top-level SFC block.
type Attr struct {
	Name      string
	Value     string
	HasValue  bool
	Span      Span
	NameSpan  Span
	ValueSpan Span
}

// Block is a parsed top-level .tue block.
type Block struct {
	Kind         BlockKind
	Name         string
	Attrs        []Attr
	Span         Span
	OpenTagSpan  Span
	CloseTagSpan Span
	ContentSpan  Span
	Content      string
}

// Attr returns the first attribute with name.
func (b *Block) Attr(name string) (Attr, bool) {
	for _, attr := range b.Attrs {
		if attr.Name == name {
			return attr, true
		}
	}
	return Attr{}, false
}

// HasAttr reports whether the block has an attribute with name.
func (b *Block) HasAttr(name string) bool {
	_, ok := b.Attr(name)
	return ok
}

// File is a parsed .tue single-file component.
type File struct {
	Path     string
	Blocks   []*Block
	Template *Block
	Script   *Block
	Style    *Block
}

// Diagnostic is a source-mapped SFC parse diagnostic.
type Diagnostic struct {
	Message string
	Span    Span
}
