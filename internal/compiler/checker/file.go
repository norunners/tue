package checker

import (
	"fmt"
	"strings"

	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	gotemplate "github.com/norunners/tue/internal/compiler/template"
	"github.com/norunners/tue/internal/compiler/typecap"
)

type fileChecker struct {
	path        string
	component   *script.Component
	components  map[string]componentBinding
	structs     map[string]map[string]script.Field
	comparable  map[string]bool
	diagnostics []Diagnostic
}

func (c *fileChecker) checkNodes(nodes []*gotemplate.Node, scope *scope) {
	previousConditional := false
	previousConditionalHasFor := false
	for _, node := range nodes {
		if ignorableControlSibling(node) {
			continue
		}

		if attr, ok := directiveAttr(node, gotemplate.DirectiveCase); ok {
			c.add("v-case must be a direct child of v-switch", attr.DirectiveSpan)
		}
		if attr, ok := directiveAttr(node, gotemplate.DirectiveDefault); ok {
			c.add("v-default must be a direct child of v-switch", attr.DirectiveSpan)
		}

		if attr, ok := directiveAttr(node, gotemplate.DirectiveElseIf); ok {
			switch {
			case !previousConditional:
				c.add("v-else-if must follow v-if or v-else-if", attr.DirectiveSpan)
			case previousConditionalHasFor:
				c.add("v-else-if cannot follow a conditional branch that also has v-for; use a <template v-for> wrapper", attr.DirectiveSpan)
			}
			if hasDirective(node, gotemplate.DirectiveFor) {
				c.add("v-else-if cannot be combined with v-for; use a <template v-for> wrapper", attr.DirectiveSpan)
			}
		} else if attr, ok := directiveAttr(node, gotemplate.DirectiveElse); ok {
			switch {
			case !previousConditional:
				c.add("v-else must follow v-if or v-else-if", attr.DirectiveSpan)
			case previousConditionalHasFor:
				c.add("v-else cannot follow a conditional branch that also has v-for; use a <template v-for> wrapper", attr.DirectiveSpan)
			}
			if hasDirective(node, gotemplate.DirectiveFor) {
				c.add("v-else cannot be combined with v-for; use a <template v-for> wrapper", attr.DirectiveSpan)
			}
		}
		c.checkNode(node, scope)

		switch {
		case hasDirective(node, gotemplate.DirectiveIf):
			previousConditional = true
			previousConditionalHasFor = hasDirective(node, gotemplate.DirectiveFor)
		case hasDirective(node, gotemplate.DirectiveElseIf) && previousConditional:
			previousConditional = true
			previousConditionalHasFor = false
		default:
			previousConditional = false
			previousConditionalHasFor = false
		}
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
		elementScope = c.checkFor(node, *attr, scope)
	}
	if attr, ok := directiveAttr(node, gotemplate.DirectiveSwitch); ok {
		c.checkSwitch(node, *attr, elementScope)
		return
	}

	if node.IsComponent {
		c.checkCommonAttrs(node, elementScope, false)
		c.checkComponent(node, elementScope)
	} else if node.Tag == "template" {
		c.checkCommonAttrs(node, elementScope, false)
	} else {
		c.checkCommonAttrs(node, elementScope, true)
		if node.Tag == "slot" {
			c.checkSlot(node)
		}
		c.checkNativeAttrs(node, elementScope)
	}
	if nativeHTMLDirective(node) {
		return
	}
	c.checkNodes(node.Children, elementScope)
}

func (c *fileChecker) checkNativeAttrs(node *gotemplate.Node, scope *scope) {
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrBind {
			value := c.checkExpression(attr.Expression, attr.ExpressionSpan, scope)
			switch attr.Argument {
			case "key":
				continue
			case "class":
				c.expectType("string", value.Type, "class binding", attr.ExpressionSpan)
			case "style":
				c.expectType("string", value.Type, "style binding", attr.ExpressionSpan)
			default:
				c.expectType("string", value.Type, fmt.Sprintf("bound attribute %q", attr.RawName), attr.ExpressionSpan)
			}
		}
	}
}

func (c *fileChecker) checkSlot(node *gotemplate.Node) {
	for _, attr := range node.Attrs {
		if attr.Kind == gotemplate.AttrDirective && isControlDirective(attr.Directive) {
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

	if !typecap.NoArgFunc(event.Type) {
		c.add(fmt.Sprintf("component %q event %q must have signature func()", node.Tag, attr.Argument), attr.ArgumentSpan)
		return
	}
	c.checkEvent(attr, scope)
}

func (c *fileChecker) expectType(expected string, actual string, subject string, span sfc.Span) {
	if typecap.Assignable(expected, actual) {
		return
	}
	c.add(fmt.Sprintf("%s expects %s, got %s", subject, displayType(expected), displayType(actual)), span)
}

func (c *fileChecker) expectComponentPropType(componentName string, prop script.Prop, actual string, span sfc.Span) {
	expected := propType(prop)
	if typecap.Assignable(expected, actual) {
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

func ignorableControlSibling(node *gotemplate.Node) bool {
	if node == nil {
		return true
	}
	switch node.Kind {
	case gotemplate.NodeComment:
		return true
	case gotemplate.NodeText:
		return strings.TrimSpace(node.Text) == ""
	default:
		return false
	}
}

func nativeHTMLDirective(node *gotemplate.Node) bool {
	return node != nil && !node.IsComponent && node.Tag != "template" && node.Tag != "slot" && hasDirective(node, gotemplate.DirectiveHTML)
}
