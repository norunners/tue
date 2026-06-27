package script

// FieldKind identifies a local component field category.
type FieldKind string

const (
	FieldKindLocal    FieldKind = "local"
	FieldKindResource FieldKind = "resource"
)
