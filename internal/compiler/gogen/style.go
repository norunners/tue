package gogen

import (
	"fmt"
	"hash/fnv"
	"io"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/norunners/tue/internal/compiler/sfc"
	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

const styleFilePath = "style.css"

// StyleFromBlock converts a parsed SFC style block into generator input.
func StyleFromBlock(block *sfc.Block) (*Style, bool) {
	if block == nil {
		return nil, false
	}
	return &Style{
		Source: block.Content,
		Scoped: block.HasAttr("scoped"),
		Span:   block.ContentSpan,
	}, true
}

func generatedStyleSource(files []File) ([]byte, bool) {
	sections := make([]string, 0, len(files))
	for _, file := range files {
		if file.Style == nil || strings.TrimSpace(file.Style.Source) == "" {
			continue
		}

		source := file.Style.Source
		if file.Style.Scoped {
			source = rewriteScopedCSS(source, scopeAttrFor(filePath(file)))
		}
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}

		sections = append(sections, fmt.Sprintf("/* %s */\n%s", filePath(file), source))
	}
	if len(sections) == 0 {
		return nil, false
	}
	return []byte(strings.Join(sections, "\n\n") + "\n"), true
}

func scopeAttrFor(path string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(filepath.ToSlash(path)))
	return fmt.Sprintf("data-tue-c-%08x", hash.Sum32())
}

func rewriteScopedCSS(source string, scopeAttr string) string {
	if scopeAttr == "" {
		return source
	}
	rewritten, ok := rewriteCSSWithParser(source, scopeAttr)
	if !ok {
		return source
	}
	return rewritten
}

func skipSelectorComment(source string, start int) int {
	end := strings.Index(source[start+2:], "*/")
	if end == -1 {
		return len(source) - 1
	}
	return start + 2 + end + 1
}

func skipSelectorString(source string, start int) int {
	quote := source[start]
	for i := start + 1; i < len(source); i++ {
		if source[i] == '\\' {
			i++
			continue
		}
		if source[i] == quote {
			return i
		}
	}
	return len(source) - 1
}

func rewriteSelectorList(prelude string, scopeAttr string) string {
	prefixEnd := selectorPreludePrefixEnd(prelude)
	prefix := prelude[:prefixEnd]
	remainder := prelude[prefixEnd:]
	trimmedRight := strings.TrimRightFunc(remainder, unicode.IsSpace)
	suffix := remainder[len(trimmedRight):]
	trimmed := strings.TrimSpace(trimmedRight)
	if trimmed == "" {
		return prelude
	}

	selectors := splitSelectorList(trimmed)
	for i, selector := range selectors {
		selectors[i] = rewriteSelector(strings.TrimSpace(selector), scopeAttr)
	}
	return prefix + strings.Join(selectors, ", ") + suffix
}

func selectorPreludePrefixEnd(value string) int {
	offset := 0
	for {
		for offset < len(value) && unicode.IsSpace(rune(value[offset])) {
			offset++
		}
		if offset+1 >= len(value) || value[offset] != '/' || value[offset+1] != '*' {
			return offset
		}
		offset = skipSelectorComment(value, offset) + 1
	}
}

func splitSelectorList(value string) []string {
	var selectors []string
	start := 0
	bracketDepth := 0
	parenDepth := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++
		case '\'', '"':
			i = skipSelectorString(value, i)
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case ',':
			if bracketDepth == 0 && parenDepth == 0 {
				selectors = append(selectors, value[start:i])
				start = i + 1
			}
		}
	}
	return append(selectors, value[start:])
}

func rewriteSelector(selector string, scopeAttr string) string {
	if selector == "" || strings.Contains(selector, "["+scopeAttr+"]") {
		return selector
	}

	start := lastSelectorCompoundStart(selector)
	insert := selectorScopeInsert(selector, start)
	return selector[:insert] + "[" + scopeAttr + "]" + selector[insert:]
}

func lastSelectorCompoundStart(selector string) int {
	start := 0
	bracketDepth := 0
	parenDepth := 0
	for i := 0; i < len(selector); i++ {
		switch selector[i] {
		case '\\':
			i++
		case '\'', '"':
			i = skipSelectorString(selector, i)
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '>', '+', '~':
			if bracketDepth == 0 && parenDepth == 0 {
				start = i + 1
			}
		default:
			if bracketDepth == 0 && parenDepth == 0 && unicode.IsSpace(rune(selector[i])) {
				start = i + 1
			}
		}
	}
	for start < len(selector) && unicode.IsSpace(rune(selector[start])) {
		start++
	}
	return start
}

func selectorScopeInsert(selector string, start int) int {
	bracketDepth := 0
	parenDepth := 0
	for i := start; i < len(selector); i++ {
		switch selector[i] {
		case '\\':
			i++
		case '\'', '"':
			i = skipSelectorString(selector, i)
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case ':':
			if bracketDepth == 0 && parenDepth == 0 && i > start {
				return i
			}
		}
	}
	return len(selector)
}

type cssReplacement struct {
	start int
	end   int
	value string
}

type cssScopeContainer struct {
	scopeRules bool
	nextStart  int
}

func rewriteCSSWithParser(source string, scopeAttr string) (string, bool) {
	parser := css.NewParser(parse.NewInputString(source), false)
	containers := []cssScopeContainer{{
		scopeRules: true,
		nextStart:  0,
	}}
	var replacements []cssReplacement

	for {
		grammar, _, data := parser.Next()
		switch grammar {
		case css.ErrorGrammar:
			if parser.Err() != io.EOF || parser.HasParseError() || len(containers) != 1 {
				return "", false
			}
			return applyCSSReplacements(source, replacements), true
		case css.CommentGrammar:
			containers[len(containers)-1].nextStart = parser.Offset()
		case css.AtRuleGrammar:
			containers[len(containers)-1].nextStart = parser.Offset()
		case css.BeginAtRuleGrammar:
			scopeRules := containers[len(containers)-1].scopeRules && isScopedCSSGroupingAtRule(data)
			containers = append(containers, cssScopeContainer{
				scopeRules: scopeRules,
				nextStart:  parser.Offset(),
			})
		case css.EndAtRuleGrammar:
			if len(containers) == 1 {
				return "", false
			}
			containers = containers[:len(containers)-1]
			containers[len(containers)-1].nextStart = parser.Offset()
		case css.BeginRulesetGrammar:
			container := &containers[len(containers)-1]
			open := parser.Offset() - 1
			if container.scopeRules && validCSSOpenBrace(source, open) && container.nextStart <= open {
				prelude := source[container.nextStart:open]
				rewritten := rewriteSelectorList(prelude, scopeAttr)
				if rewritten != prelude {
					replacements = append(replacements, cssReplacement{
						start: container.nextStart,
						end:   open,
						value: rewritten,
					})
				}
			}
		case css.EndRulesetGrammar:
			containers[len(containers)-1].nextStart = parser.Offset()
		}
	}
}

func validCSSOpenBrace(source string, offset int) bool {
	return 0 <= offset && offset < len(source) && source[offset] == '{'
}

func isScopedCSSGroupingAtRule(data []byte) bool {
	name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(string(data))), "@")
	switch name {
	case "container", "document", "layer", "media", "scope", "supports":
		return true
	default:
		return false
	}
}

func applyCSSReplacements(source string, replacements []cssReplacement) string {
	if len(replacements) == 0 {
		return source
	}

	var builder strings.Builder
	builder.Grow(len(source))
	offset := 0
	for _, replacement := range replacements {
		if replacement.start < offset || replacement.start > replacement.end || replacement.end > len(source) {
			return source
		}
		builder.WriteString(source[offset:replacement.start])
		builder.WriteString(replacement.value)
		offset = replacement.end
	}
	builder.WriteString(source[offset:])
	return builder.String()
}
