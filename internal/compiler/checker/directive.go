package checker

import (
	"fmt"
	"go/ast"
	"strings"
	"unicode"

	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
)

func (c *fileChecker) checkCommonAttrs(node *gotemplate.Node, scope *scope, checkEvents bool) {
	for _, attr := range node.Attrs {
		switch attr.Kind {
		case gotemplate.AttrEvent:
			if checkEvents {
				c.checkEvent(attr, scope)
			}
		case gotemplate.AttrDirective:
			switch attr.Directive {
			case gotemplate.DirectiveIf:
				value := c.checkExpression(attr.Expression, attr.ExpressionSpan, scope)
				c.expectType("bool", value.Type, "v-if", attr.ExpressionSpan)
			case gotemplate.DirectiveHTML:
				value := c.checkExpression(attr.Expression, attr.ExpressionSpan, scope)
				c.expectType("string", value.Type, "v-html", attr.ExpressionSpan)
			case gotemplate.DirectiveModel:
				c.checkModel(attr, scope)
			case gotemplate.DirectiveFor, gotemplate.DirectiveElse:
			}
		}
	}
}

func (c *fileChecker) checkEvent(attr gotemplate.Attr, scope *scope) {
	expr, exprChecker, ok := c.parseExpression(attr.Expression, attr.ExpressionSpan, scope)
	if !ok {
		return
	}

	switch typed := expr.(type) {
	case *ast.Ident:
		c.expectEventMethod(typed.Name, exprChecker.nodeSpan(typed), scope, true)
	case *ast.CallExpr:
		c.checkCallEvent(typed, exprChecker, scope)
	default:
		value := exprChecker.eval(expr)
		if value.Type != unknownType && value.Type != funcType {
			c.add(fmt.Sprintf("event handler must be a method, got %s", displayType(value.Type)), exprChecker.nodeSpan(expr))
		}
	}
}

func (c *fileChecker) checkCallEvent(call *ast.CallExpr, exprChecker *exprChecker, scope *scope) {
	for _, arg := range call.Args {
		exprChecker.eval(arg)
	}

	switch callee := call.Fun.(type) {
	case *ast.Ident:
		_, ok := c.expectEventMethod(callee.Name, exprChecker.nodeSpan(callee), scope, len(call.Args) == 0)
		if ok && len(call.Args) != 0 {
			c.add(fmt.Sprintf("event handler %q does not accept arguments", callee.Name), exprChecker.nodeSpan(call))
		}
	case *ast.SelectorExpr:
		exprChecker.eval(callee)
	default:
		value := exprChecker.eval(call.Fun)
		if value.Type != unknownType && value.Type != funcType {
			c.add(fmt.Sprintf("event handler must be a method, got %s", displayType(value.Type)), exprChecker.nodeSpan(call.Fun))
		}
	}
}

func (c *fileChecker) checkFor(node *gotemplate.Node, attr gotemplate.Attr, scope *scope) *scope {
	clause, ok := parseForClause(attr.Expression)
	if !ok {
		c.add("v-for must use '<item> in <items>'", attr.ExpressionSpan)
		return scope
	}

	sourceSpan := spanWithin(attr.ExpressionSpan, attr.Expression, clause.sourceStart, clause.sourceEnd)
	source := c.checkExpression(clause.source, sourceSpan, scope)
	elementType, iterable := iterableElementType(source.Type)
	if !iterable {
		c.add(fmt.Sprintf("v-for source must be iterable, got %s", displayType(source.Type)), sourceSpan)
		elementType = unknownType
	}

	if !hasBoundKey(node) {
		c.add("v-for requires a :key attribute", attr.DirectiveSpan)
	}

	next := newScope(scope)
	next.add(symbol{Name: clause.item, Type: elementType, Writable: false})
	if clause.index != "" {
		next.add(symbol{Name: clause.index, Type: "int", Writable: false})
	}
	return next
}

func (c *fileChecker) checkModel(attr gotemplate.Attr, scope *scope) {
	expr, exprChecker, ok := c.parseExpression(attr.Expression, attr.ExpressionSpan, scope)
	if !ok {
		return
	}

	value := exprChecker.eval(expr)
	if value.Type == unknownType {
		return
	}

	target := modelTarget(expr, scope)
	if target == nil || !target.Writable {
		c.add(fmt.Sprintf("v-model target %q is not writable", attr.Expression), attr.ExpressionSpan)
	}
}

func (c *fileChecker) expectEventMethod(name string, span sfc.Span, scope *scope, requireFuncSignature bool) (symbol, bool) {
	method, ok := scope.lookup(name)
	if !ok || !method.Method {
		c.add(fmt.Sprintf("event handler %q is not a method on %s", name, c.component.Name), span)
		return symbol{}, false
	}
	if requireFuncSignature && (method.Parameters != 0 || method.Results != 0) {
		c.add(fmt.Sprintf("event handler %q must have signature func()", name), span)
		return method, false
	}
	return method, true
}

type forClause struct {
	item        string
	index       string
	source      string
	sourceStart int
	sourceEnd   int
}

func parseForClause(expression string) (forClause, bool) {
	in := strings.Index(expression, " in ")
	if in == -1 {
		return forClause{}, false
	}

	target := strings.TrimSpace(expression[:in])
	if strings.HasPrefix(target, "(") && strings.HasSuffix(target, ")") {
		target = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(target, "("), ")"))
	}

	sourceStart := in + len(" in ")
	sourceEnd := len(expression)
	for sourceStart < sourceEnd && isSpace(rune(expression[sourceStart])) {
		sourceStart++
	}
	for sourceEnd > sourceStart && isSpace(rune(expression[sourceEnd-1])) {
		sourceEnd--
	}

	source := expression[sourceStart:sourceEnd]
	parts := strings.Split(target, ",")
	if len(parts) == 0 || len(parts) > 2 || source == "" {
		return forClause{}, false
	}

	clause := forClause{
		item:        strings.TrimSpace(parts[0]),
		source:      source,
		sourceStart: sourceStart,
		sourceEnd:   sourceEnd,
	}
	if len(parts) == 2 {
		clause.index = strings.TrimSpace(parts[1])
	}
	if !isIdentifier(clause.item) || (clause.index != "" && !isIdentifier(clause.index)) {
		return forClause{}, false
	}
	return clause, true
}

func modelTarget(expr ast.Expr, scope *scope) *symbol {
	switch typed := expr.(type) {
	case *ast.Ident:
		symbol, ok := scope.lookup(typed.Name)
		if !ok {
			return nil
		}
		return &symbol
	default:
		return nil
	}
}

func directiveAttr(node *gotemplate.Node, kind gotemplate.DirectiveKind) (gotemplate.Attr, bool) {
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrDirective && attr.Directive == kind {
			return attr, true
		}
	}
	return gotemplate.Attr{}, false
}

func hasBoundKey(node *gotemplate.Node) bool {
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrBind && attr.Argument == "key" {
			return true
		}
	}
	return false
}

func isIdentifier(name string) bool {
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

func isSpace(r rune) bool {
	switch r {
	case ' ', '\n', '\r', '\t', '\f':
		return true
	default:
		return false
	}
}
