package script

// FieldKind identifies a local component field category.
type FieldKind string

const (
	FieldKindLocal    FieldKind = "local"
	FieldKindComputed FieldKind = "computed"
	FieldKindResource FieldKind = "resource"
)
