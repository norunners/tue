package script

// FieldKind identifies how a component field participates in the contract.
type FieldKind string

const (
	FieldKindState    FieldKind = "state"
	FieldKindRef      FieldKind = "ref"
	FieldKindComputed FieldKind = "computed"
	FieldKindResource FieldKind = "resource"
)
