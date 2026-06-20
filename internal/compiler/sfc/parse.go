package sfc

import (
	"fmt"
	"strings"
	"unicode"
)

// Parse parses one .tue source file into source-spanned top-level blocks.
func Parse(path string, source []byte) (*File, []Diagnostic) {
	parser := newParser(path, string(source))
	return parser.parse()
}

type parser struct {
	path       string
	source     string
	lineStarts []int
}

type openTag struct {
	name        string
	attrs       []Attr
	span        Span
	nameSpan    Span
	contentFrom int
	selfClosing bool
}

func newParser(path string, source string) *parser {
	lineStarts := []int{0}
	for offset, r := range source {
		if r == '\n' {
			lineStarts = append(lineStarts, offset+1)
		}
	}

	return &parser{
		path:       path,
		source:     source,
		lineStarts: lineStarts,
	}
}

func (p *parser) parse() (*File, []Diagnostic) {
	file := &File{Path: p.path}
	var diagnostics []Diagnostic
	var fatal bool
	var sawTemplate bool
	var sawScript bool

	for offset := 0; offset < len(p.source); {
		next := strings.IndexByte(p.source[offset:], '<')
		if next == -1 {
			p.addUnexpectedText(&diagnostics, offset, len(p.source))
			break
		}

		blockStart := offset + next
		p.addUnexpectedText(&diagnostics, offset, blockStart)

		if blockStart+1 < len(p.source) && p.source[blockStart+1] == '/' {
			end := p.advancePastTag(blockStart)
			diagnostics = append(diagnostics, Diagnostic{
				Message: "unexpected closing tag",
				Span:    p.span(blockStart, end),
			})
			offset = end
			continue
		}

		tag, diagnostic, ok := p.parseOpenTag(blockStart)
		if !ok {
			diagnostics = append(diagnostics, diagnostic)
			fatal = true
			break
		}

		kind, supported := blockKind(tag.name)
		if !supported {
			diagnostics = append(diagnostics, Diagnostic{
				Message: fmt.Sprintf("unsupported top-level block <%s>", tag.name),
				Span:    tag.nameSpan,
			})
			offset = p.skipUnsupportedBlock(*tag)
			continue
		}

		switch kind {
		case BlockTemplate:
			sawTemplate = true
		case BlockScript:
			sawScript = true
		}

		if tag.selfClosing {
			diagnostics = append(diagnostics, Diagnostic{
				Message: fmt.Sprintf("<%s> block must have a closing tag", tag.name),
				Span:    tag.span,
			})
			offset = tag.contentFrom
			continue
		}

		closeStart, closeEnd, found := p.findBlockCloseTag(tag.name, tag.contentFrom)
		if !found {
			diagnostics = append(diagnostics, Diagnostic{
				Message: fmt.Sprintf("missing closing </%s> tag", tag.name),
				Span:    tag.span,
			})
			fatal = true
			break
		}

		block := &Block{
			Kind:         kind,
			Name:         tag.name,
			Attrs:        tag.attrs,
			Span:         p.span(blockStart, closeEnd),
			OpenTagSpan:  tag.span,
			CloseTagSpan: p.span(closeStart, closeEnd),
			ContentSpan:  p.span(tag.contentFrom, closeStart),
			Content:      p.source[tag.contentFrom:closeStart],
		}

		p.addBlock(file, block, &diagnostics)
		offset = closeEnd
	}

	if !fatal {
		eof := p.span(len(p.source), len(p.source))
		if !sawTemplate {
			diagnostics = append(diagnostics, Diagnostic{
				Message: "missing required <template> block",
				Span:    eof,
			})
		}
		if !sawScript {
			diagnostics = append(diagnostics, Diagnostic{
				Message: "missing required <script lang=\"go\"> block",
				Span:    eof,
			})
		}
	}

	return file, diagnostics
}

func (p *parser) addBlock(file *File, block *Block, diagnostics *[]Diagnostic) {
	switch block.Kind {
	case BlockTemplate:
		if file.Template != nil {
			*diagnostics = append(*diagnostics, Diagnostic{
				Message: "duplicate <template> block",
				Span:    block.OpenTagSpan,
			})
			return
		}
		file.Template = block
	case BlockScript:
		if file.Script != nil {
			*diagnostics = append(*diagnostics, Diagnostic{
				Message: "duplicate <script> block",
				Span:    block.OpenTagSpan,
			})
			return
		}
		if !hasAttrValue(block.Attrs, "lang", "go") {
			*diagnostics = append(*diagnostics, Diagnostic{
				Message: "<script> block must set lang=\"go\"",
				Span:    block.OpenTagSpan,
			})
			return
		}
		file.Script = block
	case BlockStyle:
		if file.Style != nil {
			*diagnostics = append(*diagnostics, Diagnostic{
				Message: "duplicate <style> block",
				Span:    block.OpenTagSpan,
			})
			return
		}
		file.Style = block
	}

	file.Blocks = append(file.Blocks, block)
}

func blockKind(name string) (BlockKind, bool) {
	switch name {
	case string(BlockTemplate):
		return BlockTemplate, true
	case string(BlockScript):
		return BlockScript, true
	case string(BlockStyle):
		return BlockStyle, true
	default:
		return "", false
	}
}

func (p *parser) parseOpenTag(start int) (*openTag, Diagnostic, bool) {
	end, diagnostic, ok := p.findOpenTagEnd(start)
	if !ok {
		return nil, diagnostic, false
	}

	bodyStart := start + 1
	bodyEnd := end - 1
	selfClosing := false
	for bodyEnd > bodyStart && isSpace(p.source[bodyEnd-1]) {
		bodyEnd--
	}
	if bodyEnd > bodyStart && p.source[bodyEnd-1] == '/' {
		selfClosing = true
		bodyEnd--
		for bodyEnd > bodyStart && isSpace(p.source[bodyEnd-1]) {
			bodyEnd--
		}
	}

	nameStart := bodyStart
	if nameStart >= bodyEnd || !isNameStart(rune(p.source[nameStart])) {
		return nil, Diagnostic{
			Message: "malformed opening tag",
			Span:    p.span(start, end),
		}, false
	}

	nameEnd := nameStart + 1
	for nameEnd < bodyEnd && isNameChar(rune(p.source[nameEnd])) {
		nameEnd++
	}

	attrs, attrDiagnostic, ok := p.parseAttrs(nameEnd, bodyEnd)
	if !ok {
		return nil, attrDiagnostic, false
	}

	return &openTag{
		name:        p.source[nameStart:nameEnd],
		attrs:       attrs,
		span:        p.span(start, end),
		nameSpan:    p.span(nameStart, nameEnd),
		contentFrom: end,
		selfClosing: selfClosing,
	}, Diagnostic{}, true
}

func (p *parser) findOpenTagEnd(start int) (int, Diagnostic, bool) {
	var quote byte
	for offset := start + 1; offset < len(p.source); offset++ {
		current := p.source[offset]
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}

		switch current {
		case '\'', '"':
			quote = current
		case '>':
			return offset + 1, Diagnostic{}, true
		}
	}

	message := "unterminated opening tag"
	if quote != 0 {
		message = "unterminated quoted attribute in opening tag"
	}

	return len(p.source), Diagnostic{
		Message: message,
		Span:    p.span(start, len(p.source)),
	}, false
}

func (p *parser) parseAttrs(start int, end int) ([]Attr, Diagnostic, bool) {
	var attrs []Attr
	for offset := skipSpaces(p.source, start, end); offset < end; offset = skipSpaces(p.source, offset, end) {
		attrStart := offset
		if !isNameStart(rune(p.source[offset])) {
			return nil, Diagnostic{
				Message: "malformed block attribute",
				Span:    p.span(offset, end),
			}, false
		}

		nameEnd := offset + 1
		for nameEnd < end && isNameChar(rune(p.source[nameEnd])) {
			nameEnd++
		}

		attr := Attr{
			Name:     p.source[attrStart:nameEnd],
			NameSpan: p.span(attrStart, nameEnd),
		}

		offset = skipSpaces(p.source, nameEnd, end)
		if offset < end && p.source[offset] == '=' {
			attr.HasValue = true
			valueStart := skipSpaces(p.source, offset+1, end)
			if valueStart >= end {
				return nil, Diagnostic{
					Message: "missing block attribute value",
					Span:    p.span(attrStart, end),
				}, false
			}

			var valueEnd int
			var spanEnd int
			if p.source[valueStart] == '"' || p.source[valueStart] == '\'' {
				quote := p.source[valueStart]
				valueStart++
				valueEnd = valueStart
				for valueEnd < end && p.source[valueEnd] != quote {
					valueEnd++
				}
				if valueEnd >= end {
					return nil, Diagnostic{
						Message: "unterminated quoted block attribute",
						Span:    p.span(attrStart, end),
					}, false
				}
				spanEnd = valueEnd + 1
			} else {
				valueEnd = valueStart
				for valueEnd < end && !isSpace(p.source[valueEnd]) {
					valueEnd++
				}
				spanEnd = valueEnd
			}

			attr.Value = p.source[valueStart:valueEnd]
			attr.ValueSpan = p.span(valueStart, valueEnd)
			attr.Span = p.span(attrStart, spanEnd)
			offset = spanEnd
		} else {
			attr.Span = p.span(attrStart, nameEnd)
			offset = nameEnd
		}

		attrs = append(attrs, attr)
	}

	return attrs, Diagnostic{}, true
}

func (p *parser) findCloseTag(name string, start int) (int, int, bool) {
	needle := "</" + name
	for searchFrom := start; searchFrom < len(p.source); {
		index := strings.Index(p.source[searchFrom:], needle)
		if index == -1 {
			return 0, 0, false
		}

		closeStart := searchFrom + index
		offset := closeStart + len(needle)
		if offset < len(p.source) && isNameChar(rune(p.source[offset])) {
			searchFrom = offset
			continue
		}

		offset = skipSpaces(p.source, offset, len(p.source))
		if offset < len(p.source) && p.source[offset] == '>' {
			return closeStart, offset + 1, true
		}

		searchFrom = offset
	}

	return 0, 0, false
}

func (p *parser) findBlockCloseTag(name string, start int) (int, int, bool) {
	if name != string(BlockTemplate) {
		return p.findCloseTag(name, start)
	}

	depth := 1
	for searchFrom := start; searchFrom < len(p.source); {
		index := strings.IndexByte(p.source[searchFrom:], '<')
		if index == -1 {
			return 0, 0, false
		}

		tagStart := searchFrom + index
		if tagStart+1 < len(p.source) && p.source[tagStart+1] == '/' {
			closeEnd, ok := p.closeTagEndAt(name, tagStart)
			if !ok {
				searchFrom = tagStart + 1
				continue
			}
			depth--
			if depth == 0 {
				return tagStart, closeEnd, true
			}
			searchFrom = closeEnd
			continue
		}

		openEnd, selfClosing, ok := p.openTagEndAt(name, tagStart)
		if !ok {
			searchFrom = p.advancePastTag(tagStart)
			continue
		}
		if !selfClosing {
			depth++
		}
		searchFrom = openEnd
	}

	return 0, 0, false
}

func (p *parser) openTagEndAt(name string, start int) (int, bool, bool) {
	if start+1+len(name) > len(p.source) || p.source[start:start+1+len(name)] != "<"+name {
		return 0, false, false
	}
	offset := start + 1 + len(name)
	if offset < len(p.source) && isNameChar(rune(p.source[offset])) {
		return 0, false, false
	}
	end, _, ok := p.findOpenTagEnd(start)
	if !ok {
		return 0, false, false
	}
	bodyEnd := end - 1
	for bodyEnd > start && isSpace(p.source[bodyEnd-1]) {
		bodyEnd--
	}
	selfClosing := bodyEnd > start && p.source[bodyEnd-1] == '/'
	return end, selfClosing, true
}

func (p *parser) closeTagEndAt(name string, start int) (int, bool) {
	if start+2+len(name) > len(p.source) || p.source[start:start+2+len(name)] != "</"+name {
		return 0, false
	}
	offset := start + 2 + len(name)
	if offset < len(p.source) && isNameChar(rune(p.source[offset])) {
		return 0, false
	}
	offset = skipSpaces(p.source, offset, len(p.source))
	if offset < len(p.source) && p.source[offset] == '>' {
		return offset + 1, true
	}
	return 0, false
}

func (p *parser) skipUnsupportedBlock(tag openTag) int {
	if tag.selfClosing {
		return tag.contentFrom
	}
	_, closeEnd, found := p.findCloseTag(tag.name, tag.contentFrom)
	if !found {
		return tag.contentFrom
	}
	return closeEnd
}

func (p *parser) addUnexpectedText(diagnostics *[]Diagnostic, start int, end int) {
	for start < end && isSpace(p.source[start]) {
		start++
	}
	for end > start && isSpace(p.source[end-1]) {
		end--
	}
	if start >= end {
		return
	}
	*diagnostics = append(*diagnostics, Diagnostic{
		Message: "unexpected text outside top-level block",
		Span:    p.span(start, end),
	})
}

func (p *parser) advancePastTag(start int) int {
	if end := strings.IndexByte(p.source[start:], '>'); end != -1 {
		return start + end + 1
	}
	return len(p.source)
}

func hasAttrValue(attrs []Attr, name string, value string) bool {
	for _, attr := range attrs {
		if attr.Name == name && attr.HasValue && attr.Value == value {
			return true
		}
	}
	return false
}

func skipSpaces(source string, start int, end int) int {
	for start < end && isSpace(source[start]) {
		start++
	}
	return start
}

func isNameStart(r rune) bool {
	return unicode.IsLetter(r)
}

func isNameChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == ':'
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\n', '\r', '\t', '\f':
		return true
	default:
		return false
	}
}

func (p *parser) span(start int, end int) Span {
	return Span{
		Start: p.position(start),
		End:   p.position(end),
	}
}

func (p *parser) position(offset int) Position {
	line := 0
	for line+1 < len(p.lineStarts) && p.lineStarts[line+1] <= offset {
		line++
	}

	return Position{
		Offset: offset,
		Line:   line + 1,
		Column: offset - p.lineStarts[line] + 1,
	}
}
