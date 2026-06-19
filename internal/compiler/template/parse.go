package template

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/norunners/tue/internal/compiler/sfc"
)

// Parse parses template source with positions relative to the start of source.
func Parse(source []byte) (*Tree, []Diagnostic) {
	return parseSource(string(source), sfc.Position{Line: 1, Column: 1})
}

// ParseBlock parses a template block returned by the SFC parser.
func ParseBlock(block *sfc.Block) (*Tree, []Diagnostic) {
	if block == nil {
		return &Tree{}, []Diagnostic{{
			Message: "missing template block",
		}}
	}
	return parseSource(block.Content, block.ContentSpan.Start)
}

type parser struct {
	source      string
	lineStarts  []int
	base        sfc.Position
	diagnostics []Diagnostic
}

type openTag struct {
	name        string
	attrs       []Attr
	span        sfc.Span
	nameSpan    sfc.Span
	contentFrom int
	selfClosing bool
}

type closeTag struct {
	name string
	span sfc.Span
}

type attrSyntax struct {
	rawName   string
	value     string
	hasValue  bool
	span      sfc.Span
	nameSpan  sfc.Span
	valueSpan sfc.Span
}

func parseSource(source string, base sfc.Position) (*Tree, []Diagnostic) {
	parser := newParser(source, base)
	nodes := parser.parseChildren("")
	return &Tree{
		Nodes: nodes,
		Span:  parser.span(0, len(source)),
	}, parser.diagnostics
}

func newParser(source string, base sfc.Position) *parser {
	if base.Line == 0 {
		base.Line = 1
	}
	if base.Column == 0 {
		base.Column = 1
	}

	lineStarts := []int{0}
	for offset, r := range source {
		if r == '\n' {
			lineStarts = append(lineStarts, offset+1)
		}
	}

	return &parser{
		source:     source,
		lineStarts: lineStarts,
		base:       base,
	}
}

func (p *parser) parseChildren(parentTag string) []*Node {
	var nodes []*Node

	for offset := 0; offset < len(p.source); {
		node, next, done := p.parseNode(offset, parentTag)
		if done {
			return nodes
		}
		if next <= offset {
			next = offset + 1
		}
		if node != nil {
			nodes = append(nodes, node)
		}
		offset = next
	}

	if parentTag != "" {
		p.addDiagnostic(fmt.Sprintf("missing closing </%s> tag", parentTag), p.span(len(p.source), len(p.source)))
	}

	return nodes
}

func (p *parser) parseNode(offset int, parentTag string) (*Node, int, bool) {
	if strings.HasPrefix(p.source[offset:], "</") {
		close, next, ok := p.parseCloseTag(offset)
		if !ok {
			return nil, next, false
		}
		if parentTag == "" {
			p.addDiagnostic(fmt.Sprintf("unexpected closing </%s> tag", close.name), close.span)
			return nil, next, false
		}
		if close.name != parentTag {
			p.addDiagnostic(fmt.Sprintf("unexpected closing </%s> tag; expected </%s>", close.name, parentTag), close.span)
			return nil, next, false
		}
		return nil, next, true
	}

	if strings.HasPrefix(p.source[offset:], "<!--") {
		node, next := p.parseComment(offset)
		return node, next, false
	}

	if p.source[offset] == '<' && offset+1 < len(p.source) && isTagNameStart(rune(p.source[offset+1])) {
		node, next := p.parseElement(offset)
		return node, next, false
	}

	node, next := p.parseText(offset)
	return node, next, false
}

func (p *parser) parseElement(start int) (*Node, int) {
	tag, diagnostic, ok := p.parseOpenTag(start)
	if !ok {
		p.diagnostics = append(p.diagnostics, diagnostic)
		return nil, p.advancePastTag(start)
	}

	node := &Node{
		Kind:        NodeElement,
		Span:        tag.span,
		ContentSpan: p.span(tag.contentFrom, tag.contentFrom),
		Tag:         tag.name,
		TagSpan:     tag.nameSpan,
		IsComponent: isComponentTag(tag.name),
		SelfClosing: tag.selfClosing,
		Attrs:       tag.attrs,
	}

	if tag.selfClosing {
		return node, tag.contentFrom
	}

	children, close, found := p.parseElementChildren(tag)
	node.Children = children
	if !found {
		p.addDiagnostic(fmt.Sprintf("missing closing </%s> tag", tag.name), tag.span)
		return node, len(p.source)
	}

	node.ContentSpan = p.span(tag.contentFrom, p.relativeOffset(close.span.Start))
	node.Span = p.span(start, p.relativeOffset(close.span.End))
	return node, p.relativeOffset(close.span.End)
}

func (p *parser) parseElementChildren(tag openTag) ([]*Node, closeTag, bool) {
	var children []*Node
	offset := tag.contentFrom

	for offset < len(p.source) {
		if strings.HasPrefix(p.source[offset:], "</") {
			close, next, ok := p.parseCloseTag(offset)
			if !ok {
				offset = next
				continue
			}
			if close.name != tag.name {
				p.addDiagnostic(fmt.Sprintf("unexpected closing </%s> tag; expected </%s>", close.name, tag.name), close.span)
				offset = next
				continue
			}
			return children, close, true
		}

		node, next, _ := p.parseNode(offset, tag.name)
		if next <= offset {
			next = offset + 1
		}
		if node != nil {
			children = append(children, node)
		}
		offset = next
	}

	return children, closeTag{}, false
}

func (p *parser) parseText(start int) (*Node, int) {
	end := len(p.source)
	if next := findNextTagStart(p.source, start+1); next != -1 {
		end = next
	}

	if strings.HasPrefix(p.source[start:end], "{{") {
		return p.parseInterpolation(start, end)
	}

	openIndex := strings.Index(p.source[start:end], "{{")
	closeIndex := strings.Index(p.source[start:end], "}}")
	if closeIndex == 0 {
		p.addDiagnostic("unexpected interpolation closing braces", p.span(start, start+2))
		return nil, start + 2
	}
	if closeIndex != -1 && (openIndex == -1 || closeIndex < openIndex) {
		return p.textNode(start, start+closeIndex), start + closeIndex
	}
	if openIndex != -1 {
		return p.textNode(start, start+openIndex), start + openIndex
	}

	return p.textNode(start, end), end
}

func (p *parser) parseInterpolation(start int, end int) (*Node, int) {
	closeIndex := strings.Index(p.source[start+2:end], "}}")
	if closeIndex == -1 {
		p.addDiagnostic("unterminated interpolation", p.span(start, end))
		return nil, end
	}

	exprRawStart := start + 2
	exprRawEnd := start + 2 + closeIndex
	exprStart, exprEnd := trimSpaceRange(p.source, exprRawStart, exprRawEnd)
	if exprStart == exprEnd {
		p.addDiagnostic("empty interpolation expression", p.span(start, exprRawEnd+2))
	}

	return &Node{
		Kind:           NodeInterpolation,
		Span:           p.span(start, exprRawEnd+2),
		ContentSpan:    p.span(exprRawStart, exprRawEnd),
		Expression:     p.source[exprStart:exprEnd],
		ExpressionSpan: p.span(exprStart, exprEnd),
	}, exprRawEnd + 2
}

func (p *parser) textNode(start int, end int) *Node {
	return &Node{
		Kind:        NodeText,
		Span:        p.span(start, end),
		ContentSpan: p.span(start, end),
		Text:        p.source[start:end],
	}
}

func (p *parser) parseComment(start int) (*Node, int) {
	contentStart := start + len("<!--")
	endIndex := strings.Index(p.source[contentStart:], "-->")
	if endIndex == -1 {
		p.addDiagnostic("unterminated comment", p.span(start, len(p.source)))
		return &Node{
			Kind:        NodeComment,
			Span:        p.span(start, len(p.source)),
			ContentSpan: p.span(contentStart, len(p.source)),
			Text:        p.source[contentStart:],
		}, len(p.source)
	}

	contentEnd := contentStart + endIndex
	end := contentEnd + len("-->")
	return &Node{
		Kind:        NodeComment,
		Span:        p.span(start, end),
		ContentSpan: p.span(contentStart, contentEnd),
		Text:        p.source[contentStart:contentEnd],
	}, end
}

func (p *parser) parseOpenTag(start int) (openTag, Diagnostic, bool) {
	end, diagnostic, ok := p.findOpenTagEnd(start)
	if !ok {
		return openTag{}, diagnostic, false
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
	if nameStart >= bodyEnd || !isTagNameStart(rune(p.source[nameStart])) {
		return openTag{}, Diagnostic{
			Message: "malformed opening tag",
			Span:    p.span(start, end),
		}, false
	}

	nameEnd := nameStart + 1
	for nameEnd < bodyEnd && isTagNameChar(rune(p.source[nameEnd])) {
		nameEnd++
	}

	attrs, attrDiagnostics := p.parseAttrs(nameEnd, bodyEnd)
	p.diagnostics = append(p.diagnostics, attrDiagnostics...)

	return openTag{
		name:        p.source[nameStart:nameEnd],
		attrs:       attrs,
		span:        p.span(start, end),
		nameSpan:    p.span(nameStart, nameEnd),
		contentFrom: end,
		selfClosing: selfClosing,
	}, Diagnostic{}, true
}

func (p *parser) parseCloseTag(start int) (closeTag, int, bool) {
	end := p.advancePastTag(start)
	if end == len(p.source) && (end == start || p.source[end-1] != '>') {
		p.addDiagnostic("unterminated closing tag", p.span(start, end))
		return closeTag{}, end, false
	}

	bodyStart := start + 2
	bodyEnd := end - 1
	bodyStart = skipSpaces(p.source, bodyStart, bodyEnd)
	bodyEnd = trimRightSpaces(p.source, bodyStart, bodyEnd)
	if bodyStart >= bodyEnd || !isTagNameStart(rune(p.source[bodyStart])) {
		p.addDiagnostic("malformed closing tag", p.span(start, end))
		return closeTag{}, end, false
	}

	nameEnd := bodyStart + 1
	for nameEnd < bodyEnd && isTagNameChar(rune(p.source[nameEnd])) {
		nameEnd++
	}
	if skipSpaces(p.source, nameEnd, bodyEnd) != bodyEnd {
		p.addDiagnostic("malformed closing tag", p.span(start, end))
		return closeTag{}, end, false
	}

	return closeTag{
		name: p.source[bodyStart:nameEnd],
		span: p.span(start, end),
	}, end, true
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

func (p *parser) parseAttrs(start int, end int) ([]Attr, []Diagnostic) {
	var attrs []Attr
	var diagnostics []Diagnostic

	for offset := skipSpaces(p.source, start, end); offset < end; offset = skipSpaces(p.source, offset, end) {
		syntax, next, diagnostic, ok := p.parseAttr(offset, end)
		if !ok {
			diagnostics = append(diagnostics, diagnostic)
			break
		}

		attr, attrDiagnostics := p.classifyAttr(syntax)
		diagnostics = append(diagnostics, attrDiagnostics...)
		attrs = append(attrs, attr)
		offset = next
	}

	return attrs, diagnostics
}

func (p *parser) parseAttr(start int, end int) (attrSyntax, int, Diagnostic, bool) {
	if !isAttrNameStart(rune(p.source[start])) {
		return attrSyntax{}, end, Diagnostic{
			Message: "malformed attribute",
			Span:    p.span(start, end),
		}, false
	}

	nameEnd := start + 1
	for nameEnd < end && isAttrNameChar(rune(p.source[nameEnd])) {
		nameEnd++
	}

	syntax := attrSyntax{
		rawName:  p.source[start:nameEnd],
		span:     p.span(start, nameEnd),
		nameSpan: p.span(start, nameEnd),
	}

	offset := skipSpaces(p.source, nameEnd, end)
	if offset >= end || p.source[offset] != '=' {
		return syntax, nameEnd, Diagnostic{}, true
	}

	syntax.hasValue = true
	valueStart := skipSpaces(p.source, offset+1, end)
	if valueStart >= end {
		return attrSyntax{}, end, Diagnostic{
			Message: "missing attribute value",
			Span:    p.span(start, end),
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
			return attrSyntax{}, end, Diagnostic{
				Message: "unterminated quoted attribute",
				Span:    p.span(start, end),
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

	syntax.value = p.source[valueStart:valueEnd]
	syntax.valueSpan = p.span(valueStart, valueEnd)
	syntax.span = p.span(start, spanEnd)
	return syntax, spanEnd, Diagnostic{}, true
}

func (p *parser) classifyAttr(syntax attrSyntax) (Attr, []Diagnostic) {
	attr := Attr{
		Kind:      AttrStatic,
		RawName:   syntax.rawName,
		Name:      syntax.rawName,
		Value:     syntax.value,
		HasValue:  syntax.hasValue,
		Span:      syntax.span,
		NameSpan:  syntax.nameSpan,
		ValueSpan: syntax.valueSpan,
	}

	var diagnostics []Diagnostic
	if strings.HasPrefix(syntax.rawName, ":") {
		attr.Kind = AttrBind
		attr.Name = strings.TrimPrefix(syntax.rawName, ":")
		attr.Argument = attr.Name
		nameStart := p.relativeOffset(syntax.nameSpan.Start)
		nameEnd := p.relativeOffset(syntax.nameSpan.End)
		attr.DirectiveSpan = p.span(nameStart, nameStart+1)
		attr.ArgumentSpan = p.span(nameStart+1, nameEnd)
		if attr.Argument == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Message: "bound attribute name cannot be empty",
				Span:    syntax.nameSpan,
			})
		}
		diagnostics = append(diagnostics, p.requireExpression(&attr, "bound attribute requires an expression")...)
		return attr, diagnostics
	}

	if strings.HasPrefix(syntax.rawName, "@") {
		attr.Kind = AttrEvent
		attr.Name = strings.TrimPrefix(syntax.rawName, "@")
		attr.Argument = attr.Name
		nameStart := p.relativeOffset(syntax.nameSpan.Start)
		nameEnd := p.relativeOffset(syntax.nameSpan.End)
		attr.DirectiveSpan = p.span(nameStart, nameStart+1)
		attr.ArgumentSpan = p.span(nameStart+1, nameEnd)
		if attr.Argument == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Message: "event name cannot be empty",
				Span:    syntax.nameSpan,
			})
		}
		diagnostics = append(diagnostics, p.requireExpression(&attr, "event handler requires an expression")...)
		return attr, diagnostics
	}

	if strings.HasPrefix(syntax.rawName, "v-") {
		attr.Kind = AttrDirective
		attr.Name = strings.TrimPrefix(syntax.rawName, "v-")
		attr.DirectiveSpan = syntax.nameSpan

		switch DirectiveKind(attr.Name) {
		case DirectiveIf:
			attr.Directive = DirectiveIf
			diagnostics = append(diagnostics, p.requireExpression(&attr, "v-if requires an expression")...)
		case DirectiveElseIf:
			attr.Directive = DirectiveElseIf
			diagnostics = append(diagnostics, p.requireExpression(&attr, "v-else-if requires an expression")...)
		case DirectiveElse:
			attr.Directive = DirectiveElse
			if attr.HasValue {
				diagnostics = append(diagnostics, Diagnostic{
					Message: "v-else must not have a value",
					Span:    attr.ValueSpan,
				})
			}
		case DirectiveFor:
			attr.Directive = DirectiveFor
			diagnostics = append(diagnostics, p.requireExpression(&attr, "v-for requires an expression")...)
		case DirectiveModel:
			attr.Directive = DirectiveModel
			diagnostics = append(diagnostics, p.requireExpression(&attr, "v-model requires an expression")...)
		case DirectiveHTML:
			attr.Directive = DirectiveHTML
			diagnostics = append(diagnostics, p.requireExpression(&attr, "v-html requires an expression")...)
		case DirectiveSwitch:
			attr.Directive = DirectiveSwitch
			diagnostics = append(diagnostics, p.requireExpression(&attr, "v-switch requires an expression")...)
		case DirectiveCase:
			attr.Directive = DirectiveCase
			diagnostics = append(diagnostics, p.requireExpression(&attr, "v-case requires an expression")...)
		case DirectiveDefault:
			attr.Directive = DirectiveDefault
			if attr.HasValue {
				diagnostics = append(diagnostics, Diagnostic{
					Message: "v-default must not have a value",
					Span:    attr.ValueSpan,
				})
			}
		default:
			diagnostics = append(diagnostics, Diagnostic{
				Message: fmt.Sprintf("unsupported directive %q", syntax.rawName),
				Span:    syntax.nameSpan,
			})
		}

		return attr, diagnostics
	}

	return attr, diagnostics
}

func (p *parser) requireExpression(attr *Attr, message string) []Diagnostic {
	if !attr.HasValue {
		return []Diagnostic{{
			Message: message,
			Span:    attr.NameSpan,
		}}
	}

	start, end := trimSpaceRange(p.source, p.relativeOffset(attr.ValueSpan.Start), p.relativeOffset(attr.ValueSpan.End))
	attr.Expression = p.source[start:end]
	attr.ExpressionSpan = p.span(start, end)
	if start == end {
		return []Diagnostic{{
			Message: message,
			Span:    attr.ValueSpan,
		}}
	}
	return nil
}

func findNextTagStart(source string, start int) int {
	for offset := start; offset < len(source); offset++ {
		if source[offset] != '<' {
			continue
		}
		if strings.HasPrefix(source[offset:], "<!--") {
			return offset
		}
		if offset+1 < len(source) && (source[offset+1] == '/' || isTagNameStart(rune(source[offset+1]))) {
			return offset
		}
	}
	return -1
}

func (p *parser) advancePastTag(start int) int {
	if end := strings.IndexByte(p.source[start:], '>'); end != -1 {
		return start + end + 1
	}
	return len(p.source)
}

func (p *parser) addDiagnostic(message string, span sfc.Span) {
	p.diagnostics = append(p.diagnostics, Diagnostic{
		Message: message,
		Span:    span,
	})
}

func trimSpaceRange(source string, start int, end int) (int, int) {
	for start < end && isSpace(source[start]) {
		start++
	}
	for end > start && isSpace(source[end-1]) {
		end--
	}
	return start, end
}

func skipSpaces(source string, start int, end int) int {
	for start < end && isSpace(source[start]) {
		start++
	}
	return start
}

func trimRightSpaces(source string, start int, end int) int {
	for end > start && isSpace(source[end-1]) {
		end--
	}
	return end
}

func isTagNameStart(r rune) bool {
	return unicode.IsLetter(r)
}

func isTagNameChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == ':'
}

func isAttrNameStart(r rune) bool {
	return unicode.IsLetter(r) || r == ':' || r == '@'
}

func isAttrNameChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == ':' || r == '@' || r == '.'
}

func isComponentTag(tag string) bool {
	r, _ := utf8.DecodeRuneInString(tag)
	return unicode.IsUpper(r)
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\n', '\r', '\t', '\f':
		return true
	default:
		return false
	}
}

func (p *parser) span(start int, end int) sfc.Span {
	return sfc.Span{
		Start: p.position(start),
		End:   p.position(end),
	}
}

func (p *parser) relativeOffset(position sfc.Position) int {
	return position.Offset - p.base.Offset
}

func (p *parser) position(offset int) sfc.Position {
	lineIndex := sort.Search(len(p.lineStarts), func(i int) bool {
		return p.lineStarts[i] > offset
	}) - 1
	if lineIndex < 0 {
		lineIndex = 0
	}

	position := sfc.Position{
		Offset: p.base.Offset + offset,
		Line:   p.base.Line + lineIndex,
		Column: offset - p.lineStarts[lineIndex] + 1,
	}
	if lineIndex == 0 {
		position.Column = p.base.Column + offset
	}
	return position
}
