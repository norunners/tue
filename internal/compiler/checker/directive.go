package checker

import (
	"fmt"
	"go/ast"

	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
	"github.com/norunners/tue/internal/compiler/typecap"
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
			case gotemplate.DirectiveIf, gotemplate.DirectiveElseIf:
				value := c.checkExpression(attr.Expression, attr.ExpressionSpan, scope)
				c.expectType("bool", value.Type, attr.RawName, attr.ExpressionSpan)
			case gotemplate.DirectiveHTML:
				c.checkHTML(node, attr, scope)
			case gotemplate.DirectiveModel:
				c.checkModel(node, attr, scope)
			case gotemplate.DirectiveFor, gotemplate.DirectiveElse, gotemplate.DirectiveSwitch, gotemplate.DirectiveCase, gotemplate.DirectiveDefault:
			}
		}
	}
}

func (c *fileChecker) checkSwitch(node *gotemplate.Node, attr gotemplate.Attr, scope *scope) {
	hostValid := node != nil && node.Tag == "template" && !node.IsComponent
	if !hostValid {
		c.add("v-switch is only supported on <template>", attr.DirectiveSpan)
	}
	if conditionalAttr, ok := firstConditionalDirective(node); ok {
		c.add("v-switch cannot be combined with v-if, v-else-if, or v-else", conditionalAttr.DirectiveSpan)
	}
	if caseAttr, ok := directiveAttr(node, gotemplate.DirectiveCase); ok {
		c.add("v-switch cannot be combined with v-case on the same element", caseAttr.DirectiveSpan)
	}
	if defaultAttr, ok := directiveAttr(node, gotemplate.DirectiveDefault); ok {
		c.add("v-switch cannot be combined with v-default on the same element", defaultAttr.DirectiveSpan)
	}

	switchValue := c.checkExpression(attr.Expression, attr.ExpressionSpan, scope)
	if !typecap.Comparable(switchValue.Type, c.comparable) {
		c.add(fmt.Sprintf("v-switch expression type %s is not comparable", displayType(switchValue.Type)), attr.ExpressionSpan)
	}
	if !hostValid {
		return
	}

	branchCount := 0
	defaultSeen := false
	for _, branch := range node.Children {
		if ignorableControlSibling(branch) {
			continue
		}
		branchCount++

		caseAttr, hasCase := directiveAttr(branch, gotemplate.DirectiveCase)
		defaultAttr, hasDefault := directiveAttr(branch, gotemplate.DirectiveDefault)
		switch {
		case hasCase && hasDefault:
			c.add("v-switch branch cannot use both v-case and v-default", defaultAttr.DirectiveSpan)
		case hasCase:
			if defaultSeen {
				c.add("v-case must appear before v-default", caseAttr.DirectiveSpan)
			}
			caseValue := c.checkExpression(caseAttr.Expression, caseAttr.ExpressionSpan, scope)
			if !typecap.SwitchCompatible(switchValue.Type, caseValue.Type) {
				c.add(
					fmt.Sprintf("v-case expects %s, got %s", displayType(switchValue.Type), displayType(caseValue.Type)),
					caseAttr.ExpressionSpan,
				)
			}
		case hasDefault:
			if defaultSeen {
				c.add("v-switch may only have one v-default", defaultAttr.DirectiveSpan)
			}
			defaultSeen = true
		default:
			c.add("v-switch children must use v-case or v-default", branch.Span)
		}

		if conditionalAttr, ok := firstConditionalDirective(branch); ok {
			c.add("v-switch branches cannot combine v-case or v-default with v-if, v-else-if, or v-else", conditionalAttr.DirectiveSpan)
		}
		checkBranch := true
		if nestedSwitch, ok := directiveAttr(branch, gotemplate.DirectiveSwitch); ok {
			c.add("a v-switch branch must nest another v-switch inside its content", nestedSwitch.DirectiveSpan)
			checkBranch = false
		}
		if checkBranch {
			c.checkNode(branch, scope)
		}
	}
	if branchCount == 0 {
		c.add("v-switch requires at least one v-case or v-default child", attr.DirectiveSpan)
	}
}

func (c *fileChecker) checkHTML(node *gotemplate.Node, attr gotemplate.Attr, scope *scope) {
	value := c.checkExpression(attr.Expression, attr.ExpressionSpan, scope)
	if !typecap.TrustedHTML(value.Type) {
		c.add(fmt.Sprintf("v-html expects tue.TrustedHTML, got %s", displayType(value.Type)), attr.ExpressionSpan)
	}
	if node == nil || node.IsComponent || node.Tag == "template" || node.Tag == "slot" {
		c.add("v-html is only supported on native elements", attr.DirectiveSpan)
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
	iterableTypes, iterable := typecap.IterableFor(source.Type)
	if !iterable {
		c.add(fmt.Sprintf("v-for source must be iterable, got %s", displayType(source.Type)), sourceSpan)
		iterableTypes = &typecap.Iterable{Item: unknownType, Key: unknownType}
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

	target, ok := modelTarget(expr, scope)
	if !ok || !target.Writable {
		c.add(fmt.Sprintf("v-model target %q is not writable", attr.Expression), attr.ExpressionSpan)
	}

	binding, ok := nativeModelBinding(node)
	if !ok {
		c.add(modelUnsupportedMessage(node), attr.DirectiveSpan)
		return
	}
	c.expectType(binding.ValueType, value.Type, "v-model", attr.ExpressionSpan)
}

func (c *fileChecker) expectEventMethod(name string, span sfc.Span, scope *scope, requireFuncSignature bool) (*symbol, bool) {
	method, ok := scope.lookup(name)
	if !ok || !method.Method {
		c.add(fmt.Sprintf("event handler %q is not a method on %s", name, c.component.Name), span)
		return nil, false
	}
	if requireFuncSignature && (method.Parameters != 0 || method.Results != 0) {
		c.add(fmt.Sprintf("event handler %q must have signature func()", name), span)
		return nil, false
	}
	return method, true
}

func modelTarget(expr ast.Expr, scope *scope) (*symbol, bool) {
	switch typed := expr.(type) {
	case *ast.Ident:
		return scope.lookup(typed.Name)
	default:
		return nil, false
	}
}

type nativeModel struct {
	ValueType string
}

func nativeModelBinding(node *gotemplate.Node) (*nativeModel, bool) {
	if node == nil || node.IsComponent {
		return nil, false
	}

	switch node.Tag {
	case "input":
		inputType, _ := staticAttrValue(node, "type")
		if isTextInputType(inputType) {
			return &nativeModel{ValueType: "string"}, true
		}
		if inputType == "checkbox" {
			return &nativeModel{ValueType: "bool"}, true
		}
		return nil, false
	case "select":
		return &nativeModel{ValueType: "string"}, true
	case "textarea":
		return &nativeModel{ValueType: "string"}, true
	default:
		return nil, false
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

func directiveAttr(node *gotemplate.Node, kind gotemplate.DirectiveKind) (*gotemplate.Attr, bool) {
	if node == nil {
		return nil, false
	}
	for index := range node.Attrs {
		if node.Attrs[index].Kind == gotemplate.AttrDirective && node.Attrs[index].Directive == kind {
			return &node.Attrs[index], true
		}
	}
	return nil, false
}

func hasDirective(node *gotemplate.Node, kind gotemplate.DirectiveKind) bool {
	_, ok := directiveAttr(node, kind)
	return ok
}

func isControlDirective(kind gotemplate.DirectiveKind) bool {
	switch kind {
	case gotemplate.DirectiveIf,
		gotemplate.DirectiveElseIf,
		gotemplate.DirectiveElse,
		gotemplate.DirectiveFor,
		gotemplate.DirectiveSwitch,
		gotemplate.DirectiveCase,
		gotemplate.DirectiveDefault:
		return true
	default:
		return false
	}
}

func firstConditionalDirective(node *gotemplate.Node) (*gotemplate.Attr, bool) {
	for _, kind := range []gotemplate.DirectiveKind{
		gotemplate.DirectiveIf,
		gotemplate.DirectiveElseIf,
		gotemplate.DirectiveElse,
	} {
		if attr, ok := directiveAttr(node, kind); ok {
			return attr, true
		}
	}
	return nil, false
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
