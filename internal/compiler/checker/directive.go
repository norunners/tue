package checker

import (
	"fmt"
	"go/ast"

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
				c.checkModel(node, attr, scope)
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
	clause, ok := gotemplate.ParseForClause(attr.Expression)
	if !ok {
		c.add("v-for must use '<item> in <items>'", attr.ExpressionSpan)
		return scope
	}

	sourceSpan := spanWithin(attr.ExpressionSpan, attr.Expression, clause.SourceStart, clause.SourceEnd)
	source := c.checkExpression(clause.Source, sourceSpan, scope)
	iterableTypes, iterable := iterableTypesFor(source.Type)
	if !iterable {
		c.add(fmt.Sprintf("v-for source must be iterable, got %s", displayType(source.Type)), sourceSpan)
		iterableTypes.Item = unknownType
		iterableTypes.Key = unknownType
	}

	if !hasBoundKey(node) {
		c.add("v-for requires a :key attribute", attr.DirectiveSpan)
	}

	next := newScope(scope)
	next.add(symbol{Name: clause.Item, Type: iterableTypes.Item, Writable: false})
	if clause.Index != "" {
		next.add(symbol{Name: clause.Index, Type: iterableTypes.Key, Writable: false})
	}
	return next
}

func (c *fileChecker) checkModel(node *gotemplate.Node, attr gotemplate.Attr, scope *scope) {
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

	binding, ok := nativeModelBinding(node)
	if !ok {
		c.add(modelUnsupportedMessage(node), attr.DirectiveSpan)
		return
	}
	c.expectType(binding.ValueType, value.Type, "v-model", attr.ExpressionSpan)
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

type nativeModel struct {
	ValueType string
}

func nativeModelBinding(node *gotemplate.Node) (nativeModel, bool) {
	if node == nil || node.IsComponent {
		return nativeModel{}, false
	}

	switch node.Tag {
	case "input":
		inputType, _ := staticAttrValue(node, "type")
		if isTextInputType(inputType) {
			return nativeModel{ValueType: "string"}, true
		}
		if inputType == "checkbox" {
			return nativeModel{ValueType: "bool"}, true
		}
		return nativeModel{}, false
	case "select":
		return nativeModel{ValueType: "string"}, true
	case "textarea":
		return nativeModel{ValueType: "string"}, true
	default:
		return nativeModel{}, false
	}
}

func isTextInputType(inputType string) bool {
	switch inputType {
	case "", "text", "email", "password", "search", "tel", "url":
		return true
	default:
		return false
	}
}

func modelUnsupportedMessage(node *gotemplate.Node) string {
	if node != nil && node.IsComponent {
		return "component v-model is not supported"
	}
	if node != nil && node.Tag == "input" {
		if inputType, ok := staticAttrValue(node, "type"); ok {
			return fmt.Sprintf("v-model is not supported for input type %q", inputType)
		}
	}
	return "v-model is only supported on text inputs, textareas, checkboxes, and selects"
}

func directiveAttr(node *gotemplate.Node, kind gotemplate.DirectiveKind) (gotemplate.Attr, bool) {
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrDirective && attr.Directive == kind {
			return attr, true
		}
	}
	return gotemplate.Attr{}, false
}

func hasDirective(node *gotemplate.Node, kind gotemplate.DirectiveKind) bool {
	_, ok := directiveAttr(node, kind)
	return ok
}

func hasBoundKey(node *gotemplate.Node) bool {
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrBind && attr.Argument == "key" {
			return true
		}
	}
	return false
}

func staticAttrValue(node *gotemplate.Node, name string) (string, bool) {
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrStatic && attr.Name == name && attr.HasValue {
			return attr.Value, true
		}
	}
	return "", false
}

func isNamedSlotAttr(attr gotemplate.Attr) bool {
	return (attr.Kind == gotemplate.AttrStatic && attr.Name == "name") ||
		(attr.Kind == gotemplate.AttrBind && attr.Argument == "name")
}
