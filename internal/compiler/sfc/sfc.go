package sfc

import (
	"bytes"
	"fmt"
	"sort"
)

type File struct {
	Path     string
	Blocks   []*Block
	Template *Block
	Script   *Block
	Style    *Block
}

type Block struct {
	Name         string
	Attrs        []Attr
	Content      string
	Span         Span
	OpenTagSpan  Span
	CloseTagSpan Span
	ContentSpan  Span
}

type Attr struct {
	Name     string
	Value    string
	HasValue bool
	Span     Span
}

type Span struct {
	Start Position
	End   Position
}

type Position struct {
	Offset int
	Line   int
	Column int
}

type Diagnostic struct {
	Path    string
	Message string
	Span    Span
}

type Error struct {
	Diagnostics []Diagnostic
}

func (e *Error) Error() string {
	if len(e.Diagnostics) == 0 {
		return "parse .tue file"
	}
	if len(e.Diagnostics) == 1 {
		return e.Diagnostics[0].String()
	}
	return fmt.Sprintf("%s (and %d more diagnostics)", e.Diagnostics[0].String(), len(e.Diagnostics)-1)
}

func (d Diagnostic) String() string {
	path := d.Path
	if path == "" {
		path = "<input>"
	}
	return fmt.Sprintf("%s:%d:%d: %s", path, d.Span.Start.Line, d.Span.Start.Column, d.Message)
}

func (b Block) Attr(name string) (Attr, bool) {
	for _, attr := range b.Attrs {
		if attr.Name == name {
			return attr, true
		}
	}
	return Attr{}, false
}

func (b Block) Scoped() bool {
	_, ok := b.Attr("scoped")
	return ok
}

func Parse(path string, src []byte) (*File, error) {
	p := parser{
		path:       path,
		src:        src,
		lineStarts: lineStarts(src),
	}
	file := p.parse()
	if len(p.diagnostics) > 0 {
		return file, &Error{Diagnostics: p.diagnostics}
	}
	return file, nil
}

type parser struct {
	path        string
	src         []byte
	lineStarts  []int
	diagnostics []Diagnostic
}

type openTag struct {
	name        string
	attrs       []Attr
	start       int
	end         int
	nameStart   int
	selfClosing bool
}

type closeTag struct {
	start int
	end   int
}

func (p *parser) parse() *File {
	file := &File{Path: p.path}
	seenTemplate := false
	seenScript := false

	for offset := 0; offset < len(p.src); {
		offset = p.skipSpace(offset)
		if offset >= len(p.src) {
			break
		}
		if p.startsWith(offset, "<!--") {
			next := bytes.Index(p.src[offset+4:], []byte("-->"))
			if next < 0 {
				p.addDiagnostic(offset, len(p.src), "unterminated top-level comment")
				break
			}
			offset += 4 + next + len("-->")
			continue
		}
		if p.src[offset] != '<' {
			next := p.nextTagOffset(offset)
			p.addDiagnostic(offset, next, "unexpected top-level content outside a block")
			offset = next
			continue
		}
		if p.startsWith(offset, "</") {
			tagEnd := p.findTagEnd(offset + 2)
			if tagEnd < 0 {
				tagEnd = len(p.src)
			}
			p.addDiagnostic(offset, tagEnd, "unexpected closing tag")
			offset = tagEnd
			continue
		}

		open, ok := p.parseOpenTag(offset)
		if !ok {
			break
		}
		if open.selfClosing {
			p.addDiagnostic(open.start, open.end, fmt.Sprintf("<%s> block cannot be self-closing", open.name))
			offset = open.end
			continue
		}

		close, ok := p.findCloseTag(open.name, open.end)
		if !ok {
			p.addDiagnostic(open.start, open.end, fmt.Sprintf("missing closing </%s> tag", open.name))
			break
		}

		block := &Block{
			Name:         open.name,
			Attrs:        open.attrs,
			Content:      string(p.src[open.end:close.start]),
			Span:         p.span(open.start, close.end),
			OpenTagSpan:  p.span(open.start, open.end),
			CloseTagSpan: p.span(close.start, close.end),
			ContentSpan:  p.span(open.end, close.start),
		}

		valid := false
		switch open.name {
		case "template":
			seenTemplate = true
			valid = p.validateTemplate(block)
			if valid && file.Template != nil {
				p.addDiagnostic(open.start, open.end, "duplicate <template> block")
				valid = false
			}
		case "script":
			seenScript = true
			valid = p.validateScript(block)
			if valid && file.Script != nil {
				p.addDiagnostic(open.start, open.end, "duplicate <script lang=\"go\"> block")
				valid = false
			}
		case "style":
			valid = p.validateStyle(block)
			if valid && file.Style != nil {
				p.addDiagnostic(open.start, open.end, "duplicate <style> block")
				valid = false
			}
		default:
			p.addDiagnostic(open.nameStart, open.nameStart+len(open.name), fmt.Sprintf("unsupported block <%s>", open.name))
			offset = close.end
			continue
		}

		if valid {
			file.Blocks = append(file.Blocks, block)
			switch open.name {
			case "template":
				file.Template = block
			case "script":
				file.Script = block
			case "style":
				file.Style = block
			}
		}

		offset = close.end
	}

	if !seenTemplate {
		p.addDiagnostic(len(p.src), len(p.src), "missing required <template> block")
	}
	if !seenScript {
		p.addDiagnostic(len(p.src), len(p.src), "missing required <script lang=\"go\"> block")
	}

	return file
}

func (p *parser) parseOpenTag(start int) (openTag, bool) {
	end := p.findTagEnd(start + 1)
	if end < 0 {
		p.addDiagnostic(start, len(p.src), "malformed opening tag: missing >")
		return openTag{}, false
	}

	i := start + 1
	i = p.skipTagSpace(i, end)
	nameStart := i
	for i < end && isNameChar(p.src[i]) {
		i++
	}
	if nameStart == i {
		p.addDiagnostic(start, end+1, "malformed opening tag: missing block name")
		return openTag{}, true
	}

	name := string(p.src[nameStart:i])
	attrs, selfClosing, ok := p.parseAttrs(i, end)
	if !ok {
		return openTag{}, true
	}
	return openTag{
		name:        name,
		attrs:       attrs,
		start:       start,
		end:         end + 1,
		nameStart:   nameStart,
		selfClosing: selfClosing,
	}, true
}

func (p *parser) parseAttrs(start, end int) ([]Attr, bool, bool) {
	var attrs []Attr
	i := start
	selfClosing := false

	for {
		i = p.skipTagSpace(i, end)
		if i >= end {
			return attrs, selfClosing, true
		}
		if p.src[i] == '/' {
			i++
			i = p.skipTagSpace(i, end)
			if i != end {
				p.addDiagnostic(i, end, "malformed opening tag: unexpected content after /")
				return attrs, selfClosing, false
			}
			selfClosing = true
			return attrs, selfClosing, true
		}

		attrStart := i
		for i < end && isNameChar(p.src[i]) {
			i++
		}
		if attrStart == i {
			p.addDiagnostic(i, end, "malformed attribute")
			return attrs, selfClosing, false
		}
		name := string(p.src[attrStart:i])

		i = p.skipTagSpace(i, end)
		if i >= end || p.src[i] != '=' {
			attrs = append(attrs, Attr{
				Name: name,
				Span: p.span(attrStart, i),
			})
			continue
		}

		i++
		i = p.skipTagSpace(i, end)
		if i >= end {
			p.addDiagnostic(attrStart, end, fmt.Sprintf("attribute %q is missing a value", name))
			return attrs, selfClosing, false
		}

		valueStart := i
		var value string
		switch p.src[i] {
		case '\'', '"':
			quote := p.src[i]
			i++
			valueContentStart := i
			for i < end && p.src[i] != quote {
				i++
			}
			if i >= end {
				p.addDiagnostic(valueStart, end, fmt.Sprintf("attribute %q has an unterminated quoted value", name))
				return attrs, selfClosing, false
			}
			value = string(p.src[valueContentStart:i])
			i++
		default:
			for i < end && !isSpace(p.src[i]) && p.src[i] != '/' {
				i++
			}
			if valueStart == i {
				p.addDiagnostic(attrStart, end, fmt.Sprintf("attribute %q is missing a value", name))
				return attrs, selfClosing, false
			}
			value = string(p.src[valueStart:i])
		}

		attrs = append(attrs, Attr{
			Name:     name,
			Value:    value,
			HasValue: true,
			Span:     p.span(attrStart, i),
		})
	}
}

func (p *parser) validateTemplate(block *Block) bool {
	ok := true
	for _, attr := range block.Attrs {
		p.addDiagnostic(attr.Span.Start.Offset, attr.Span.End.Offset, fmt.Sprintf("<template> does not support attribute %q", attr.Name))
		ok = false
	}
	return ok
}

func (p *parser) validateScript(block *Block) bool {
	ok := true
	seenLang := false
	for _, attr := range block.Attrs {
		switch attr.Name {
		case "lang":
			if seenLang {
				p.addDiagnostic(attr.Span.Start.Offset, attr.Span.End.Offset, "duplicate script lang attribute")
				ok = false
			}
			seenLang = true
			if !attr.HasValue || attr.Value != "go" {
				p.addDiagnostic(attr.Span.Start.Offset, attr.Span.End.Offset, "<script> block must use lang=\"go\"")
				ok = false
			}
		default:
			p.addDiagnostic(attr.Span.Start.Offset, attr.Span.End.Offset, fmt.Sprintf("<script> does not support attribute %q", attr.Name))
			ok = false
		}
	}
	if !seenLang {
		p.addDiagnostic(block.OpenTagSpan.Start.Offset, block.OpenTagSpan.End.Offset, "<script> block must use lang=\"go\"")
		ok = false
	}
	return ok
}

func (p *parser) validateStyle(block *Block) bool {
	ok := true
	seenScoped := false
	for _, attr := range block.Attrs {
		switch attr.Name {
		case "scoped":
			if seenScoped {
				p.addDiagnostic(attr.Span.Start.Offset, attr.Span.End.Offset, "duplicate style scoped attribute")
				ok = false
			}
			seenScoped = true
			if attr.HasValue {
				p.addDiagnostic(attr.Span.Start.Offset, attr.Span.End.Offset, "<style scoped> must use a boolean scoped attribute")
				ok = false
			}
		default:
			p.addDiagnostic(attr.Span.Start.Offset, attr.Span.End.Offset, fmt.Sprintf("<style> does not support attribute %q", attr.Name))
			ok = false
		}
	}
	return ok
}

func (p *parser) findCloseTag(name string, from int) (closeTag, bool) {
	needle := []byte("</" + name)
	search := from
	for search < len(p.src) {
		idx := bytes.Index(p.src[search:], needle)
		if idx < 0 {
			return closeTag{}, false
		}
		start := search + idx
		afterName := start + len(needle)
		if afterName >= len(p.src) || (!isSpace(p.src[afterName]) && p.src[afterName] != '>') {
			search = afterName
			continue
		}

		end := p.findTagEnd(afterName)
		if end < 0 {
			p.addDiagnostic(start, len(p.src), fmt.Sprintf("malformed closing </%s> tag: missing >", name))
			return closeTag{}, false
		}
		for i := afterName; i < end; i++ {
			if !isSpace(p.src[i]) {
				p.addDiagnostic(start, end+1, fmt.Sprintf("malformed closing </%s> tag", name))
				return closeTag{}, false
			}
		}
		return closeTag{start: start, end: end + 1}, true
	}
	return closeTag{}, false
}

func (p *parser) findTagEnd(from int) int {
	var quote byte
	for i := from; i < len(p.src); i++ {
		switch {
		case quote != 0:
			if p.src[i] == quote {
				quote = 0
			}
		case p.src[i] == '\'' || p.src[i] == '"':
			quote = p.src[i]
		case p.src[i] == '>':
			return i
		}
	}
	return -1
}

func (p *parser) nextTagOffset(from int) int {
	if idx := bytes.IndexByte(p.src[from:], '<'); idx >= 0 {
		return from + idx
	}
	return len(p.src)
}

func (p *parser) skipSpace(offset int) int {
	for offset < len(p.src) && isSpace(p.src[offset]) {
		offset++
	}
	return offset
}

func (p *parser) skipTagSpace(offset, end int) int {
	for offset < end && isSpace(p.src[offset]) {
		offset++
	}
	return offset
}

func (p *parser) startsWith(offset int, prefix string) bool {
	return bytes.HasPrefix(p.src[offset:], []byte(prefix))
}

func (p *parser) addDiagnostic(start, end int, message string) {
	p.diagnostics = append(p.diagnostics, Diagnostic{
		Path:    p.path,
		Message: message,
		Span:    p.span(start, end),
	})
}

func (p *parser) span(start, end int) Span {
	return Span{
		Start: p.position(start),
		End:   p.position(end),
	}
}

func (p *parser) position(offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(p.src) {
		offset = len(p.src)
	}
	lineIdx := sort.Search(len(p.lineStarts), func(i int) bool {
		return p.lineStarts[i] > offset
	}) - 1
	if lineIdx < 0 {
		lineIdx = 0
	}
	return Position{
		Offset: offset,
		Line:   lineIdx + 1,
		Column: offset - p.lineStarts[lineIdx] + 1,
	}
}

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func isNameChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_' || b == '-' || b == ':' || b == '.'
}
