package template

import (
	"strings"
	"unicode"
)

// ForClause is the parsed shape of a v-for expression.
type ForClause struct {
	Item        string
	Index       string
	Source      string
	SourceStart int
	SourceEnd   int
}

// ParseForClause parses '<item> in <items>' and '(item, index) in <items>'.
func ParseForClause(expression string) (*ForClause, bool) {
	in := strings.Index(expression, " in ")
	if in == -1 {
		return nil, false
	}

	target := strings.TrimSpace(expression[:in])
	if strings.HasPrefix(target, "(") && strings.HasSuffix(target, ")") {
		target = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(target, "("), ")"))
	}

	sourceStart := in + len(" in ")
	sourceEnd := len(expression)
	for sourceStart < sourceEnd && isDirectiveSpace(rune(expression[sourceStart])) {
		sourceStart++
	}
	for sourceEnd > sourceStart && isDirectiveSpace(rune(expression[sourceEnd-1])) {
		sourceEnd--
	}

	source := expression[sourceStart:sourceEnd]
	parts := strings.Split(target, ",")
	if len(parts) == 0 || len(parts) > 2 || source == "" {
		return nil, false
	}

	clause := ForClause{
		Item:        strings.TrimSpace(parts[0]),
		Source:      source,
		SourceStart: sourceStart,
		SourceEnd:   sourceEnd,
	}
	if len(parts) == 2 {
		clause.Index = strings.TrimSpace(parts[1])
	}
	if !isDirectiveIdentifier(clause.Item) || (clause.Index != "" && !isDirectiveIdentifier(clause.Index)) {
		return nil, false
	}
	return &clause, true
}

func isDirectiveIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for index, r := range name {
		if index == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isDirectiveSpace(r rune) bool {
	switch r {
	case ' ', '\n', '\r', '\t', '\f':
		return true
	default:
		return false
	}
}
