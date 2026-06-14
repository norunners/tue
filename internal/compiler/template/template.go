package template

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/norunners/vue/internal/compiler/sfc"
)

type Tree struct {
	Path  string
	Nodes []Node
	Span  sfc.Span
}

type Node interface {
	node()
	NodeSpan() sfc.Span
}

type Element struct {
	Name         string
	Component    bool
	SelfClosing  bool
	Attrs        []Attr
	Children     []Node
	Span         sfc.Span
	OpenTagSpan  sfc.Span
	CloseTagSpan sfc.Span
	NameSpan     sfc.Span
}

type Text struct {
	Content string
	Span    sfc.Span
}

type Interpolation struct {
	Expression     string
	Span           sfc.Span
	ExpressionSpan sfc.Span
}

type Comment struct {
	Content string
	Span    sfc.Span
}

type AttrKind string

const (
	AttrStatic    AttrKind = "static"
	AttrBound     AttrKind = "bound"
	AttrEvent     AttrKind = "event"
	AttrDirective AttrKind = "directive"
)

type Attr struct {
	Kind           AttrKind
	Name           string
	Argument       string
	Value          string
	Expression     string
	HasValue       bool
	Span           sfc.Span
	NameSpan       sfc.Span
	DirectiveSpan  sfc.Span
	ArgumentSpan   sfc.Span
	ValueSpan      sfc.Span
	ExpressionSpan sfc.Span
}

type Diagnostic = sfc.Diagnostic

type Error struct {
	Diagnostics []Diagnostic
}

func (e *Error) Error() string {
	if len(e.Diagnostics) == 0 {
		return "parse template"
	}
	if len(e.Diagnostics) == 1 {
		return e.Diagnostics[0].String()
	}
	return fmt.Sprintf("%s (and %d more diagnostics)", e.Diagnostics[0].String(), len(e.Diagnostics)-1)
}

func ParseBlock(path string, block *sfc.Block) (*Tree, error) {
	if block == nil {
		return nil, fmt.Errorf("parse template block: nil block")
	}
	return Parse(path, []byte(block.Content), block.ContentSpan.Start)
}

func Parse(path string, src []byte, base sfc.Position) (*Tree, error) {
	p := parser{
		path:       path,
		src:        src,
		base:       normalizeBase(base),
		lineStarts: lineStarts(src),
	}
	tree := p.parse()
	if len(p.diagnostics) > 0 {
		return tree, &Error{Diagnostics: p.diagnostics}
	}
	return tree, nil
}

func (e *Element) node() {}

func (e *Element) NodeSpan() sfc.Span {
	return e.Span
}

func (t *Text) node() {}

func (t *Text) NodeSpan() sfc.Span {
	return t.Span
}

func (i *Interpolation) node() {}

func (i *Interpolation) NodeSpan() sfc.Span {
	return i.Span
}

func (c *Comment) node() {}

func (c *Comment) NodeSpan() sfc.Span {
	return c.Span
}

type parser struct {
	path        string
	src         []byte
	base        sfc.Position
	lineStarts  []int
	pos         int
	diagnostics []Diagnostic
}

type closeTag struct {
	name  string
	start int
	end   int
}

func (p *parser) parse() *Tree {
	nodes, _, _ := p.parseNodes("")
	return &Tree{
		Path:  p.path,
		Nodes: nodes,
		Span:  p.span(0, len(p.src)),
	}
}

func (p *parser) parseNodes(parent string) ([]Node, closeTag, bool) {
	var nodes []Node

	for p.pos < len(p.src) {
		switch {
		case p.startsWith(p.pos, "<!--"):
			if comment := p.parseComment(); comment != nil {
				nodes = append(nodes, comment)
			}
		case p.startsWith(p.pos, "{{"):
			if interpolation := p.parseInterpolation(); interpolation != nil {
				nodes = append(nodes, interpolation)
			}
		case p.startsWith(p.pos, "}}"):
			p.addDiagnostic(p.pos, p.pos+len("}}"), "unexpected interpolation close")
			p.pos += len("}}")
		case p.startsWith(p.pos, "</"):
			close, ok := p.parseCloseTag(p.pos)
			if !ok {
				p.pos = p.recoverTag(p.pos)
				continue
			}
			if parent == "" {
				p.addDiagnostic(close.start, close.end, fmt.Sprintf("unexpected closing </%s> tag", close.name))
				p.pos = close.end
				continue
			}
			if close.name != parent {
				p.addDiagnostic(close.start, close.end, fmt.Sprintf("expected closing </%s> tag before </%s>", parent, close.name))
				return nodes, closeTag{}, false
			}
			p.pos = close.end
			return nodes, close, true
		case p.src[p.pos] == '<':
			if p.pos+1 < len(p.src) && isTagNameStart(p.src[p.pos+1]) {
				if element := p.parseElement(); element != nil {
					nodes = append(nodes, element)
				}
				continue
			}
			end := p.recoverTag(p.pos)
			p.addDiagnostic(p.pos, end, "malformed element tag")
			p.pos = end
		default:
			if text := p.parseText(); text != nil {
				nodes = append(nodes, text)
			}
		}
	}

	return nodes, closeTag{}, parent == ""
}

func (p *parser) parseElement() *Element {
	start := p.pos
	end := p.findTagEnd(start + 1)
	if end < 0 {
		p.addDiagnostic(start, len(p.src), "malformed opening tag: missing >")
		p.pos = len(p.src)
		return nil
	}

	i := start + 1
	nameStart := i
	for i < end && isTagNameChar(p.src[i]) {
		i++
	}
	nameEnd := i
	if nameStart == nameEnd {
		p.addDiagnostic(start, end+1, "malformed opening tag: missing element name")
		p.pos = end + 1
		return nil
	}

	attrs, selfClosing := p.parseAttrs(i, end)
	openEnd := end + 1
	name := string(p.src[nameStart:nameEnd])
	element := &Element{
		Name:        name,
		Component:   isComponentName(name),
		SelfClosing: selfClosing,
		Attrs:       attrs,
		OpenTagSpan: p.span(start, openEnd),
		NameSpan:    p.span(nameStart, nameEnd),
	}

	p.pos = openEnd
	if selfClosing {
		element.Span = p.span(start, openEnd)
		return element
	}

	children, close, closed := p.parseNodes(name)
	element.Children = children
	if closed {
		element.CloseTagSpan = p.span(close.start, close.end)
		element.Span = p.span(start, close.end)
		return element
	}

	if p.pos >= len(p.src) {
		p.addDiagnostic(start, openEnd, fmt.Sprintf("missing closing </%s> tag", name))
	}
	element.Span = p.span(start, p.pos)
	return element
}

func (p *parser) parseAttrs(start, end int) ([]Attr, bool) {
	var attrs []Attr
	i := start
	selfClosing := false

	for {
		i = p.skipSpace(i, end)
		if i >= end {
			return attrs, selfClosing
		}
		if p.src[i] == '/' {
			slash := i
			i++
			i = p.skipSpace(i, end)
			if i != end {
				p.addDiagnostic(slash, end, "malformed opening tag: unexpected content after /")
			}
			selfClosing = true
			return attrs, selfClosing
		}

		attrStart := i
		for i < end && isAttrNameChar(p.src[i]) {
			i++
		}
		if attrStart == i {
			p.addDiagnostic(i, end, "malformed attribute")
			return attrs, selfClosing
		}
		nameEnd := i
		name := string(p.src[attrStart:nameEnd])

		i = p.skipSpace(i, end)
		valueStart := 0
		valueEnd := 0
		hasValue := false
		if i < end && p.src[i] == '=' {
			i++
			i = p.skipSpace(i, end)
			if i >= end || p.src[i] == '/' {
				p.addDiagnostic(attrStart, end, fmt.Sprintf("attribute %q is missing a value", name))
				return attrs, selfClosing
			}

			hasValue = true
			switch p.src[i] {
			case '\'', '"':
				quote := p.src[i]
				i++
				valueStart = i
				for i < end && p.src[i] != quote {
					i++
				}
				if i >= end {
					p.addDiagnostic(valueStart-1, end, fmt.Sprintf("attribute %q has an unterminated quoted value", name))
					return attrs, selfClosing
				}
				valueEnd = i
				i++
			default:
				valueStart = i
				for i < end && !isSpace(p.src[i]) && p.src[i] != '/' {
					i++
				}
				if valueStart == i {
					p.addDiagnostic(attrStart, end, fmt.Sprintf("attribute %q is missing a value", name))
					return attrs, selfClosing
				}
				valueEnd = i
			}
		}

		attr := Attr{
			Kind:     AttrStatic,
			Name:     name,
			HasValue: hasValue,
			Span:     p.span(attrStart, i),
			NameSpan: p.span(attrStart, nameEnd),
		}
		if hasValue {
			attr.Value = string(p.src[valueStart:valueEnd])
			attr.ValueSpan = p.span(valueStart, valueEnd)
		}
		p.classifyAttr(&attr, attrStart, nameEnd, valueStart, valueEnd)
		attrs = append(attrs, attr)
	}
}

func (p *parser) classifyAttr(attr *Attr, nameStart, nameEnd, valueStart, valueEnd int) {
	switch {
	case strings.HasPrefix(attr.Name, ":"):
		attr.Kind = AttrBound
		attr.DirectiveSpan = p.span(nameStart, nameStart+1)
		attr.Argument = attr.Name[1:]
		attr.ArgumentSpan = p.span(nameStart+1, nameEnd)
		p.setExpression(attr, valueStart, valueEnd)
		if attr.Argument == "" {
			p.addDiagnostic(nameStart, nameEnd, "bound attribute is missing a name")
		}
		if strings.Contains(attr.Argument, ".") {
			p.addDiagnostic(nameStart+1, nameEnd, "bound attribute modifiers are not supported")
		}
		if attr.Expression == "" {
			p.addDiagnosticSpan(attr.Span, fmt.Sprintf("bound attribute %q requires an expression", attr.Name))
		}
	case strings.HasPrefix(attr.Name, "@"):
		attr.Kind = AttrEvent
		attr.DirectiveSpan = p.span(nameStart, nameStart+1)
		attr.Argument = attr.Name[1:]
		attr.ArgumentSpan = p.span(nameStart+1, nameEnd)
		p.setExpression(attr, valueStart, valueEnd)
		if attr.Argument == "" {
			p.addDiagnostic(nameStart, nameEnd, "event handler is missing an event name")
		}
		if strings.Contains(attr.Argument, ".") {
			p.addDiagnostic(nameStart+1, nameEnd, "event modifiers are not supported")
		}
		if attr.Expression == "" {
			p.addDiagnosticSpan(attr.Span, fmt.Sprintf("event handler %q requires an expression", attr.Name))
		}
	case strings.HasPrefix(attr.Name, "v-"):
		attr.Kind = AttrDirective
		attr.DirectiveSpan = attr.NameSpan
		p.setExpression(attr, valueStart, valueEnd)
		p.validateDirective(attr, nameStart, nameEnd)
	}
}

func (p *parser) validateDirective(attr *Attr, nameStart, nameEnd int) {
	switch attr.Name {
	case "v-if":
		p.requireDirectiveExpression(attr)
	case "v-else":
		if attr.HasValue {
			p.addDiagnosticSpan(attr.ValueSpan, "v-else must not have a value")
		}
	case "v-for":
		if !p.requireDirectiveExpression(attr) {
			return
		}
		if !strings.Contains(attr.Expression, " in ") {
			p.addDiagnosticSpan(attr.ExpressionSpan, "v-for expression must use \"item in items\" syntax")
		}
	case "v-model", "v-html":
		p.requireDirectiveExpression(attr)
	default:
		p.addDiagnostic(nameStart, nameEnd, fmt.Sprintf("unsupported directive %q", attr.Name))
	}
}

func (p *parser) requireDirectiveExpression(attr *Attr) bool {
	if attr.Expression != "" {
		return true
	}
	p.addDiagnosticSpan(attr.Span, fmt.Sprintf("%s requires an expression", attr.Name))
	return false
}

func (p *parser) setExpression(attr *Attr, valueStart, valueEnd int) {
	if !attr.HasValue {
		return
	}
	exprStart, exprEnd := trimSpaceRange(p.src, valueStart, valueEnd)
	attr.Expression = string(p.src[exprStart:exprEnd])
	attr.ExpressionSpan = p.span(exprStart, exprEnd)
}

func (p *parser) parseComment() *Comment {
	start := p.pos
	next := bytes.Index(p.src[start+len("<!--"):], []byte("-->"))
	if next < 0 {
		p.addDiagnostic(start, len(p.src), "unterminated comment")
		p.pos = len(p.src)
		return nil
	}

	contentStart := start + len("<!--")
	contentEnd := contentStart + next
	end := contentEnd + len("-->")
	p.pos = end
	return &Comment{
		Content: string(p.src[contentStart:contentEnd]),
		Span:    p.span(start, end),
	}
}

func (p *parser) parseInterpolation() *Interpolation {
	start := p.pos
	contentStart := start + len("{{")
	next := bytes.Index(p.src[contentStart:], []byte("}}"))
	if next < 0 {
		p.addDiagnostic(start, len(p.src), "malformed interpolation: missing }}")
		p.pos = len(p.src)
		return nil
	}

	contentEnd := contentStart + next
	end := contentEnd + len("}}")
	exprStart, exprEnd := trimSpaceRange(p.src, contentStart, contentEnd)
	if exprStart == exprEnd {
		p.addDiagnostic(start, end, "empty interpolation expression")
		p.pos = end
		return nil
	}

	p.pos = end
	return &Interpolation{
		Expression:     string(p.src[exprStart:exprEnd]),
		Span:           p.span(start, end),
		ExpressionSpan: p.span(exprStart, exprEnd),
	}
}

func (p *parser) parseText() *Text {
	start := p.pos
	next := len(p.src)
	for _, marker := range []string{"<", "{{", "}}"} {
		if idx := bytes.Index(p.src[start:], []byte(marker)); idx >= 0 && start+idx < next {
			next = start + idx
		}
	}
	if next == start {
		p.pos++
		return nil
	}

	p.pos = next
	return &Text{
		Content: string(p.src[start:next]),
		Span:    p.span(start, next),
	}
}

func (p *parser) parseCloseTag(start int) (closeTag, bool) {
	end := p.findTagEnd(start + len("</"))
	if end < 0 {
		p.addDiagnostic(start, len(p.src), "malformed closing tag: missing >")
		return closeTag{}, false
	}

	i := p.skipSpace(start+len("</"), end)
	nameStart := i
	for i < end && isTagNameChar(p.src[i]) {
		i++
	}
	nameEnd := i
	if nameStart == nameEnd {
		p.addDiagnostic(start, end+1, "malformed closing tag: missing element name")
		return closeTag{}, false
	}

	i = p.skipSpace(i, end)
	if i != end {
		p.addDiagnostic(start, end+1, "malformed closing tag")
	}

	return closeTag{
		name:  string(p.src[nameStart:nameEnd]),
		start: start,
		end:   end + 1,
	}, true
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

func (p *parser) recoverTag(start int) int {
	if end := p.findTagEnd(start + 1); end >= 0 {
		return end + 1
	}
	return len(p.src)
}

func (p *parser) startsWith(offset int, prefix string) bool {
	return bytes.HasPrefix(p.src[offset:], []byte(prefix))
}

func (p *parser) skipSpace(offset, end int) int {
	for offset < end && isSpace(p.src[offset]) {
		offset++
	}
	return offset
}

func (p *parser) addDiagnostic(start, end int, message string) {
	p.diagnostics = append(p.diagnostics, Diagnostic{
		Path:    p.path,
		Message: message,
		Span:    p.span(start, end),
	})
}

func (p *parser) addDiagnosticSpan(span sfc.Span, message string) {
	p.diagnostics = append(p.diagnostics, Diagnostic{
		Path:    p.path,
		Message: message,
		Span:    span,
	})
}

func (p *parser) span(start, end int) sfc.Span {
	return sfc.Span{
		Start: p.position(start),
		End:   p.position(end),
	}
}

func (p *parser) position(offset int) sfc.Position {
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

	column := offset - p.lineStarts[lineIdx] + 1
	if lineIdx == 0 {
		column = p.base.Column + offset
	}
	return sfc.Position{
		Offset: p.base.Offset + offset,
		Line:   p.base.Line + lineIdx,
		Column: column,
	}
}

func normalizeBase(base sfc.Position) sfc.Position {
	if base.Line == 0 {
		base.Line = 1
	}
	if base.Column == 0 {
		base.Column = 1
	}
	return base
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

func trimSpaceRange(src []byte, start, end int) (int, int) {
	for start < end && isSpace(src[start]) {
		start++
	}
	for end > start && isSpace(src[end-1]) {
		end--
	}
	return start, end
}

func isComponentName(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func isTagNameStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

func isTagNameChar(b byte) bool {
	return isTagNameStart(b) || (b >= '0' && b <= '9') || b == '-' || b == ':' || b == '.'
}

func isAttrNameChar(b byte) bool {
	return b != 0 &&
		!isSpace(b) &&
		b != '=' &&
		b != '/' &&
		b != '<' &&
		b != '>' &&
		b != '\'' &&
		b != '"'
}
