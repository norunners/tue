package typecap

import "strings"

// Unknown is the compiler's unresolved expression type marker.
const Unknown = "unknown"

// Iterable describes the item and key types produced by a Go range expression.
type Iterable struct {
	Item string
	Key  string
}

// Normalize trims a type expression and removes pointer prefixes.
func Normalize(typ string) string {
	typ = strings.TrimSpace(typ)
	for strings.HasPrefix(typ, "*") {
		typ = strings.TrimSpace(strings.TrimPrefix(typ, "*"))
	}
	return typ
}

// Assignable reports whether the compiler's current type model accepts actual
// where expected is required.
func Assignable(expected string, actual string) bool {
	expected = Normalize(expected)
	actual = Normalize(actual)
	if expected == "" || actual == "" || expected == Unknown || actual == Unknown {
		return true
	}
	return expected == actual
}

// SwitchCompatible reports whether switch and case expressions can be compared.
func SwitchCompatible(switchType string, caseType string) bool {
	return Assignable(switchType, caseType) || Assignable(caseType, switchType)
}

// Comparable reports whether typ supports equality comparisons.
func Comparable(typ string, declared map[string]bool) bool {
	typ = strings.TrimSpace(typ)
	if strings.HasPrefix(typ, "*") {
		return true
	}
	typ = Normalize(typ)
	if typ == "" || typ == Unknown {
		return true
	}
	if comparable, ok := declared[typ]; ok {
		return comparable
	}
	return typ != "any" && typ != "interface{}" &&
		!strings.HasPrefix(typ, "[]") &&
		!strings.HasPrefix(typ, "map[") &&
		!strings.HasPrefix(typ, "func(")
}

// IterableFor returns the range item and key types for typ. When ok is false,
// the returned iterable is nil.
func IterableFor(typ string) (*Iterable, bool) {
	typ = Normalize(typ)
	if typ == "" || typ == Unknown {
		return &Iterable{Item: Unknown, Key: Unknown}, true
	}
	if strings.HasPrefix(typ, "[]") {
		return &Iterable{Item: strings.TrimSpace(strings.TrimPrefix(typ, "[]")), Key: "int"}, true
	}
	if strings.HasPrefix(typ, "[") {
		close := closingBracket(typ, 0)
		if close != -1 && close+1 < len(typ) {
			return &Iterable{Item: strings.TrimSpace(typ[close+1:]), Key: "int"}, true
		}
	}
	if strings.HasPrefix(typ, "map[") {
		close := closingBracket(typ, len("map"))
		if close != -1 && close+1 < len(typ) {
			return &Iterable{
				Item: strings.TrimSpace(typ[close+1:]),
				Key:  strings.TrimSpace(typ[len("map["):close]),
			}, true
		}
	}
	if typ == "string" {
		return &Iterable{Item: "rune", Key: "int"}, true
	}
	return nil, false
}

// TrustedHTML reports whether typ is Tue's explicit trusted HTML type.
func TrustedHTML(typ string) bool {
	switch Normalize(typ) {
	case "tue.TrustedHTML", "TrustedHTML":
		return true
	default:
		return false
	}
}

// Numeric reports whether typ is a supported Go numeric scalar.
func Numeric(typ string) bool {
	switch Normalize(typ) {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "rune", "byte":
		return true
	default:
		return false
	}
}

// Scalar reports whether typ is a supported string, bool, or numeric scalar.
func Scalar(typ string) bool {
	switch Normalize(typ) {
	case "string", "bool":
		return true
	default:
		return Numeric(typ)
	}
}

// NoArgFunc reports whether typ is exactly a no-argument function.
func NoArgFunc(typ string) bool {
	return strings.TrimSpace(typ) == "func()"
}

func closingBracket(typ string, open int) int {
	if open < 0 || open >= len(typ) || typ[open] != '[' {
		return -1
	}
	depth := 0
	for index := open; index < len(typ); index++ {
		switch typ[index] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}
