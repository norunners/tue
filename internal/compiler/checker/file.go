package checker

import (
	"fmt"

	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
)

type fileChecker struct {
	path        string
	component   *script.Component
	components  map[string]componentBinding
	diagnostics []Diagnostic
}

func (c *fileChecker) checkNodes(nodes []*gotemplate.Node, scope *scope) {
	for _, node := range nodes {
		c.checkNode(node, scope)
	}
}

func (c *fileChecker) checkNode(node *gotemplate.Node, scope *scope) {
	if node == nil {
		return
	}

	switch node.Kind {
	case gotemplate.NodeElement:
		c.checkElement(node, scope)
	case gotemplate.NodeInterpolation:
		c.checkExpression(node.Expression, node.ExpressionSpan, scope)
	}
}

func (c *fileChecker) checkElement(node *gotemplate.Node, scope *scope) {
	elementScope := scope
	if attr, ok := directiveAttr(node, gotemplate.DirectiveFor); ok {
		elementScope = c.checkFor(node, attr, scope)
	}

	if node.IsComponent {
		c.checkCommonAttrs(node, elementScope, false)
		c.checkComponent(node, elementScope)
	} else {
		c.checkCommonAttrs(node, elementScope, true)
		if node.Tag == "slot" {
			c.checkSlot(node)
		}
		c.checkNativeAttrs(node, elementScope)
	}
	c.checkNodes(node.Children, elementScope)
}

func (c *fileChecker) checkNativeAttrs(node *gotemplate.Node, scope *scope) {
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrBind {
			value := c.checkExpression(attr.Expression, attr.ExpressionSpan, scope)
			switch attr.Argument {
			case "class":
				c.expectType("string", value.Type, "class binding", attr.ExpressionSpan)
			case "style":
				c.expectType("string", value.Type, "style binding", attr.ExpressionSpan)
			}
		}
	}
}

func (c *fileChecker) checkSlot(node *gotemplate.Node) {
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrDirective && (attr.Directive == gotemplate.DirectiveIf || attr.Directive == gotemplate.DirectiveFor) {
			continue
		}
		if isNamedSlotAttr(attr) {
			c.add("named slots are not supported in the default slot slice", attr.Span)
		} else {
			c.add(fmt.Sprintf("slot attribute %q is not supported in the default slot slice", attr.RawName), attr.Span)
		}
	}
}

func (c *fileChecker) checkComponent(node *gotemplate.Node, scope *scope) {
	child, ok := c.components[node.Tag]
	if !ok {
		c.add(fmt.Sprintf("component %q is not registered", node.Tag), node.TagSpan)
		return
	}

	seen := make(map[string]bool)
	for _, attr := range node.Attrs {
		switch attr.Kind {
		case gotemplate.AttrStatic:
			c.checkStaticComponentProp(node, child, attr, seen)
		case gotemplate.AttrBind:
			c.checkBoundComponentProp(node, child, attr, scope, seen)
		case gotemplate.AttrEvent:
			c.checkComponentEvent(node, child, attr, scope)
		}
	}

	for _, prop := range child.component.Props {
		if prop.Required && !seen[prop.Name] {
			c.add(fmt.Sprintf("component %q requires prop %q", node.Tag, prop.Name), node.TagSpan)
		}
	}
}

func (c *fileChecker) checkStaticComponentProp(node *gotemplate.Node, child componentBinding, attr gotemplate.Attr, seen map[string]bool) {
	prop, ok := child.props[attr.Name]
	if !ok {
		c.add(fmt.Sprintf("component %q has no prop %q", node.Tag, attr.Name), attr.NameSpan)
		return
	}

	seen[prop.Name] = true
	actual := "string"
	if !attr.HasValue {
		actual = "bool"
	}
	c.expectComponentPropType(node.Tag, prop, actual, attr.ValueSpan)
}

func (c *fileChecker) checkBoundComponentProp(node *gotemplate.Node, child componentBinding, attr gotemplate.Attr, scope *scope, seen map[string]bool) {
	prop, ok := child.props[attr.Argument]
	if !ok {
		c.add(fmt.Sprintf("component %q has no prop %q", node.Tag, attr.Argument), attr.ArgumentSpan)
		return
	}

	seen[prop.Name] = true
	value := c.checkExpression(attr.Expression, attr.ExpressionSpan, scope)
	c.expectComponentPropType(node.Tag, prop, value.Type, attr.ExpressionSpan)
}

func (c *fileChecker) checkComponentEvent(node *gotemplate.Node, child componentBinding, attr gotemplate.Attr, scope *scope) {
	event, ok := child.events[attr.Argument]
	if !ok {
		c.add(fmt.Sprintf("component %q has no event %q", node.Tag, attr.Argument), attr.ArgumentSpan)
		return
	}

	if !isNoArgFunc(event.Type) {
		c.add(fmt.Sprintf("component %q event %q must have signature func()", node.Tag, attr.Argument), attr.ArgumentSpan)
		return
	}
	c.checkEvent(attr, scope)
}

func (c *fileChecker) expectType(expected string, actual string, subject string, span sfc.Span) {
	if assignable(expected, actual) {
		return
	}
	c.add(fmt.Sprintf("%s expects %s, got %s", subject, displayType(expected), displayType(actual)), span)
}

func (c *fileChecker) expectComponentPropType(componentName string, prop script.Prop, actual string, span sfc.Span) {
	expected := propType(prop)
	if assignable(expected, actual) {
		return
	}
	c.add(
		fmt.Sprintf("component %q prop %q expects %s, got %s", componentName, prop.Name, displayType(expected), displayType(actual)),
		span,
	)
}

func (c *fileChecker) add(message string, span sfc.Span) {
	c.diagnostics = append(c.diagnostics, Diagnostic{
		Path:    c.path,
		Message: message,
		Span:    span,
	})
}
