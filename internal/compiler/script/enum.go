package script

// FieldKind identifies how a component field participates in the contract.
type FieldKind string

const (
	FieldKindState    FieldKind = "state"
	FieldKindEvent    FieldKind = "event"
	FieldKindProp     FieldKind = "prop"
	FieldKindRef      FieldKind = "ref"
	FieldKindComputed FieldKind = "computed"
	FieldKindResource FieldKind = "resource"
)
