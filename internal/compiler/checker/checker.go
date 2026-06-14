package checker

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"unicode"

	"github.com/norunners/vue/internal/compiler/script"
	"github.com/norunners/vue/internal/compiler/sfc"
	templateparser "github.com/norunners/vue/internal/compiler/template"
)

type File struct {
	Path     string
	Template *templateparser.Tree
	Contract *script.Contract
}

type Diagnostic = sfc.Diagnostic

func Check(files []File) []Diagnostic {
	components, diagnostics := buildComponentRegistry(files)
	for _, file := range files {
		if file.Template == nil || file.Contract == nil {
			continue
		}
		checker := newFileChecker(file, components)
		checker.check()
		diagnostics = append(diagnostics, checker.diagnostics...)
	}
	return diagnostics
}

type componentRegistration struct {
	path     string
	span     sfc.Span
	contract *script.Contract
}

func buildComponentRegistry(files []File) (map[string]componentRegistration, []Diagnostic) {
	components := make(map[string]componentRegistration)
	var diagnostics []Diagnostic

	for _, file := range files {
		if file.Contract == nil || file.Contract.ComponentName == "" {
			continue
		}
		name := file.Contract.ComponentName
		registration := componentRegistration{
			path:     file.Contract.Path,
			span:     file.Contract.Span,
			contract: file.Contract,
		}
		if previous, exists := components[name]; exists {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    registration.path,
				Message: fmt.Sprintf("duplicate component name %s", name),
				Span:    registration.span,
			})
			diagnostics = append(diagnostics, Diagnostic{
				Path:    previous.path,
				Message: fmt.Sprintf("first component name %s declared here", name),
				Span:    previous.span,
			})
			continue
		}
		components[name] = registration
	}

	return components, diagnostics
}

type fileChecker struct {
	path        string
	tree        *templateparser.Tree
	components  map[string]componentRegistration
	scopes      []scope
	diagnostics []Diagnostic
}

type scope map[string]symbol

type symbol struct {
	typeName string
	writable bool
	callable bool
	params   []string
	results  []string
}

func newFileChecker(file File, components map[string]componentRegistration) *fileChecker {
	componentScope := make(scope)
	callbacks := callbacksByField(file.Contract)
	for _, field := range file.Contract.Fields {
		componentScope[field.Name] = symbolForField(field, callbacks[field.Name])
	}
	for _, method := range file.Contract.Methods {
		componentScope[method.Name] = symbol{
			typeName: "func",
			callable: true,
			params:   method.Params,
			results:  method.Results,
		}
	}

	return &fileChecker{
		path:       file.Path,
		tree:       file.Template,
		components: components,
		scopes:     []scope{componentScope},
	}
}

func symbolForField(field script.Field, callback script.Callback) symbol {
	symbol := symbol{}
	switch field.Kind {
	case script.FieldProp:
		symbol.typeName = field.ElementType
	case script.FieldRef:
		symbol.typeName = field.ElementType
		symbol.writable = true
	case script.FieldComputed, script.FieldResource:
		symbol.typeName = field.ElementType
	case script.FieldCallback:
		symbol.typeName = "func"
		symbol.callable = true
		symbol.params = callback.Params
		symbol.results = callback.Results
	case script.FieldState:
		symbol.typeName = field.Type
		symbol.writable = true
	default:
		symbol.typeName = field.Type
		symbol.writable = true
	}
	return symbol
}

func (c *fileChecker) check() {
	for _, node := range c.tree.Nodes {
		c.checkNode(node)
	}
}

func (c *fileChecker) checkNode(node templateparser.Node) {
	switch node := node.(type) {
	case *templateparser.Element:
		c.checkElement(node)
	case *templateparser.Interpolation:
		c.checkExpression(node.Expression, node.ExpressionSpan)
	}
}

func (c *fileChecker) checkElement(element *templateparser.Element) {
	pushedScope := false
	if attr := findDirective(element, "v-for"); attr != nil {
		locals := c.checkVFor(element, *attr)
		if len(locals) > 0 {
			c.pushScope(locals)
			pushedScope = true
		}
	}

	if element.Component {
		c.checkComponentElement(element)
	} else {
		c.checkNativeElement(element)
	}

	for _, child := range element.Children {
		c.checkNode(child)
	}

	if pushedScope {
		c.popScope()
	}
}

func (c *fileChecker) checkComponentElement(element *templateparser.Element) {
	registration, ok := c.components[element.Name]
	if !ok {
		c.addDiagnostic(element.NameSpan, fmt.Sprintf("unknown component %q", element.Name))
		c.checkGenericDirectives(element)
		return
	}

	props := propsByName(registration.contract)
	callbacks := callbacksByEvent(registration.contract)
	seenProps := make(map[string]sfc.Span)

	for _, attr := range element.Attrs {
		switch attr.Kind {
		case templateparser.AttrStatic:
			if attr.Name == "key" || isFallthroughAttr(attr.Name) {
				continue
			}
			prop, ok := props[attr.Name]
			if !ok {
				c.addDiagnostic(attr.NameSpan, fmt.Sprintf("component %s has no prop %q", element.Name, attr.Name))
				continue
			}
			c.markPropSeen(seenProps, attr.Name, attr.NameSpan)
			c.checkStaticComponentProp(prop, attr)
		case templateparser.AttrBound:
			if attr.Argument == "key" {
				c.checkExpression(attr.Expression, attr.ExpressionSpan)
				continue
			}
			prop, ok := props[attr.Argument]
			if !ok {
				if isFallthroughAttr(attr.Argument) {
					c.checkExpression(attr.Expression, attr.ExpressionSpan)
					continue
				}
				c.addDiagnostic(attr.ArgumentSpan, fmt.Sprintf("component %s has no prop %q", element.Name, attr.Argument))
				continue
			}
			c.markPropSeen(seenProps, attr.Argument, attr.ArgumentSpan)
			actual := c.checkExpression(attr.Expression, attr.ExpressionSpan)
			c.expectCompatible(attr.ExpressionSpan, prop.Type, actual, fmt.Sprintf("prop %q", prop.Name))
		case templateparser.AttrEvent:
			if attr.Argument == "" {
				continue
			}
			if _, ok := callbacks[attr.Argument]; !ok {
				c.addDiagnostic(attr.ArgumentSpan, fmt.Sprintf("component %s has no event %q", element.Name, attr.Argument))
			}
			c.checkEventHandler(attr.Expression, attr.ExpressionSpan)
		case templateparser.AttrDirective:
			c.checkDirective(attr)
		}
	}

	for _, prop := range registration.contract.Props {
		if !prop.Required {
			continue
		}
		if _, ok := seenProps[prop.Name]; !ok {
			c.addDiagnostic(element.NameSpan, fmt.Sprintf("component %s requires prop %q", element.Name, prop.Name))
		}
	}
}

func (c *fileChecker) checkStaticComponentProp(prop script.Prop, attr templateparser.Attr) {
	actual := "string"
	if !attr.HasValue {
		actual = "bool"
	}
	c.expectCompatible(attr.NameSpan, prop.Type, inferred{typeName: actual}, fmt.Sprintf("prop %q", prop.Name))
}

func (c *fileChecker) checkNativeElement(element *templateparser.Element) {
	for _, attr := range element.Attrs {
		switch attr.Kind {
		case templateparser.AttrStatic:
			c.checkStaticNativeAttr(attr)
		case templateparser.AttrBound:
			if attr.Argument == "key" {
				c.checkExpression(attr.Expression, attr.ExpressionSpan)
				continue
			}
			expected := nativeAttrType(attr.Argument)
			actual := c.checkExpression(attr.Expression, attr.ExpressionSpan)
			c.expectCompatible(attr.ExpressionSpan, expected, actual, fmt.Sprintf("attribute %q", attr.Argument))
		case templateparser.AttrEvent:
			if attr.Argument == "" {
				continue
			}
			if !isNativeEvent(attr.Argument) {
				c.addDiagnostic(attr.ArgumentSpan, fmt.Sprintf("unknown native event %q", attr.Argument))
			}
			c.checkEventHandler(attr.Expression, attr.ExpressionSpan)
		case templateparser.AttrDirective:
			c.checkDirective(attr)
		}
	}
}

func (c *fileChecker) checkStaticNativeAttr(attr templateparser.Attr) {
	if attr.Name == "key" {
		return
	}
	if nativeAttrType(attr.Name) == "bool" && attr.HasValue && strings.EqualFold(attr.Value, "false") {
		c.addDiagnostic(attr.ValueSpan, fmt.Sprintf("boolean attribute %q is true when present; use :%s for dynamic false values", attr.Name, attr.Name))
	}
}

func (c *fileChecker) checkGenericDirectives(element *templateparser.Element) {
	for _, attr := range element.Attrs {
		if attr.Kind == templateparser.AttrDirective {
			c.checkDirective(attr)
		}
	}
}

func (c *fileChecker) checkDirective(attr templateparser.Attr) {
	switch attr.Name {
	case "v-if":
		actual := c.checkExpression(attr.Expression, attr.ExpressionSpan)
		c.expectCompatible(attr.ExpressionSpan, "bool", actual, "v-if expression")
	case "v-else":
	case "v-for":
	case "v-model":
		c.checkVModel(attr)
	case "v-html":
		actual := c.checkExpression(attr.Expression, attr.ExpressionSpan)
		c.expectCompatible(attr.ExpressionSpan, "string", actual, "v-html expression")
	}
}

func (c *fileChecker) checkVFor(element *templateparser.Element, attr templateparser.Attr) scope {
	if attr.Expression == "" {
		return nil
	}

	itemName, indexName, sourceExpression, ok := parseVForExpression(attr.Expression)
	if !ok {
		c.addDiagnostic(attr.ExpressionSpan, "v-for expression must use an identifier before in")
		return nil
	}

	sourceType := c.checkExpression(sourceExpression, attr.ExpressionSpan)
	itemType := collectionItemType(sourceType.typeName)
	if sourceType.typeName != "" && itemType == "" {
		c.addDiagnostic(attr.ExpressionSpan, fmt.Sprintf("v-for source must be a slice, array, or map, got %s", sourceType.typeName))
	}
	if !hasBoundKey(element) {
		c.addDiagnostic(attr.Span, "v-for elements must include :key")
	}

	locals := make(scope)
	if itemName != "" {
		locals[itemName] = symbol{typeName: itemType}
	}
	if indexName != "" {
		locals[indexName] = symbol{typeName: "int"}
	}
	return locals
}

func (c *fileChecker) checkVModel(attr templateparser.Attr) {
	parsed, ok := c.parseExpression(attr.Expression, attr.ExpressionSpan)
	if !ok {
		return
	}

	target := writableTarget(parsed.expr)
	if target == nil {
		c.addDiagnostic(attr.ExpressionSpan, "v-model target must be assignable")
		c.infer(parsed, parsed.expr)
		return
	}

	symbolName := target.Name
	symbol, ok := c.lookup(symbolName)
	if !ok {
		c.addDiagnostic(parsed.spanFor(target.Pos(), target.End()), fmt.Sprintf("unknown template symbol %q", symbolName))
		return
	}
	if !symbol.writable {
		c.addDiagnostic(attr.ExpressionSpan, fmt.Sprintf("v-model target %q is not writable", parsed.sourceFor(parsed.expr.Pos(), parsed.expr.End())))
	}
}

func writableTarget(expr ast.Expr) *ast.Ident {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr
	case *ast.SelectorExpr:
		return writableTarget(expr.X)
	case *ast.IndexExpr:
		return writableTarget(expr.X)
	case *ast.ParenExpr:
		return writableTarget(expr.X)
	default:
		return nil
	}
}

func (c *fileChecker) checkExpression(source string, span sfc.Span) inferred {
	parsed, ok := c.parseExpression(source, span)
	if !ok {
		return inferred{unresolved: true}
	}
	return c.infer(parsed, parsed.expr)
}

func (c *fileChecker) parseExpression(source string, span sfc.Span) (parsedExpression, bool) {
	if strings.TrimSpace(source) == "" {
		return parsedExpression{}, false
	}

	fset := token.NewFileSet()
	expr, err := parser.ParseExprFrom(fset, "", source, 0)
	if err != nil {
		c.addDiagnostic(span, fmt.Sprintf("invalid Go expression: %s", trimGoParseError(err.Error())))
		return parsedExpression{}, false
	}
	return parsedExpression{
		source: source,
		span:   span,
		fset:   fset,
		expr:   expr,
	}, true
}

type parsedExpression struct {
	source string
	span   sfc.Span
	fset   *token.FileSet
	expr   ast.Expr
}

func (p parsedExpression) spanFor(start, end token.Pos) sfc.Span {
	startOffset := p.fset.Position(start).Offset
	endOffset := p.fset.Position(end).Offset
	return sfc.Span{
		Start: positionWithin(p.span.Start, p.source, startOffset),
		End:   positionWithin(p.span.Start, p.source, endOffset),
	}
}

func (p parsedExpression) sourceFor(start, end token.Pos) string {
	startOffset := p.fset.Position(start).Offset
	endOffset := p.fset.Position(end).Offset
	if startOffset < 0 {
		startOffset = 0
	}
	if endOffset > len(p.source) {
		endOffset = len(p.source)
	}
	if startOffset > endOffset {
		startOffset = endOffset
	}
	return strings.TrimSpace(p.source[startOffset:endOffset])
}

type inferred struct {
	typeName   string
	unresolved bool
}

func (c *fileChecker) infer(parsed parsedExpression, expr ast.Expr) inferred {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		return inferLiteral(expr)
	case *ast.Ident:
		return c.inferIdent(parsed, expr)
	case *ast.SelectorExpr:
		base := c.infer(parsed, expr.X)
		if base.unresolved {
			return base
		}
		return inferred{}
	case *ast.ParenExpr:
		return c.infer(parsed, expr.X)
	case *ast.UnaryExpr:
		return c.inferUnary(parsed, expr)
	case *ast.BinaryExpr:
		return c.inferBinary(parsed, expr)
	case *ast.CallExpr:
		c.checkCallable(parsed, expr.Fun, "function")
		for _, arg := range expr.Args {
			c.infer(parsed, arg)
		}
		return inferred{}
	case *ast.IndexExpr:
		base := c.infer(parsed, expr.X)
		c.infer(parsed, expr.Index)
		return inferred{typeName: collectionItemType(base.typeName), unresolved: base.unresolved}
	case *ast.SliceExpr:
		base := c.infer(parsed, expr.X)
		if expr.Low != nil {
			c.infer(parsed, expr.Low)
		}
		if expr.High != nil {
			c.infer(parsed, expr.High)
		}
		if expr.Max != nil {
			c.infer(parsed, expr.Max)
		}
		return base
	default:
		return inferred{}
	}
}

func inferLiteral(lit *ast.BasicLit) inferred {
	switch lit.Kind {
	case token.STRING:
		return inferred{typeName: "string"}
	case token.INT:
		return inferred{typeName: "int"}
	case token.FLOAT:
		return inferred{typeName: "float64"}
	case token.IMAG:
		return inferred{typeName: "complex128"}
	case token.CHAR:
		return inferred{typeName: "rune"}
	default:
		return inferred{}
	}
}

func (c *fileChecker) inferIdent(parsed parsedExpression, ident *ast.Ident) inferred {
	switch ident.Name {
	case "true", "false":
		return inferred{typeName: "bool"}
	case "nil":
		return inferred{typeName: "nil"}
	}

	symbol, ok := c.lookup(ident.Name)
	if !ok {
		c.addDiagnostic(parsed.spanFor(ident.Pos(), ident.End()), fmt.Sprintf("unknown template symbol %q", ident.Name))
		return inferred{unresolved: true}
	}
	return inferred{typeName: symbol.typeName}
}

func (c *fileChecker) inferUnary(parsed parsedExpression, expr *ast.UnaryExpr) inferred {
	value := c.infer(parsed, expr.X)
	if value.unresolved {
		return value
	}

	switch expr.Op {
	case token.NOT:
		c.expectCompatible(parsed.spanFor(expr.X.Pos(), expr.X.End()), "bool", value, "operand")
		return inferred{typeName: "bool"}
	case token.ADD, token.SUB:
		if value.typeName != "" && !isNumericType(value.typeName) {
			c.addDiagnostic(parsed.spanFor(expr.X.Pos(), expr.X.End()), fmt.Sprintf("operand must be numeric, got %s", value.typeName))
		}
		return value
	default:
		return inferred{}
	}
}

func (c *fileChecker) inferBinary(parsed parsedExpression, expr *ast.BinaryExpr) inferred {
	left := c.infer(parsed, expr.X)
	right := c.infer(parsed, expr.Y)
	if left.unresolved || right.unresolved {
		return inferred{unresolved: true}
	}

	switch expr.Op {
	case token.LAND, token.LOR:
		c.expectCompatible(parsed.spanFor(expr.X.Pos(), expr.X.End()), "bool", left, "left operand")
		c.expectCompatible(parsed.spanFor(expr.Y.Pos(), expr.Y.End()), "bool", right, "right operand")
		return inferred{typeName: "bool"}
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		if left.typeName != "" && right.typeName != "" && !typesCompatible(left.typeName, right.typeName) {
			c.addDiagnostic(parsed.spanFor(expr.Pos(), expr.End()), fmt.Sprintf("mismatched operand types %s and %s", left.typeName, right.typeName))
		}
		return inferred{typeName: "bool"}
	case token.ADD:
		if left.typeName == "string" || right.typeName == "string" {
			if left.typeName != "" && right.typeName != "" && left.typeName != right.typeName {
				c.addDiagnostic(parsed.spanFor(expr.Pos(), expr.End()), fmt.Sprintf("mismatched operand types %s and %s", left.typeName, right.typeName))
			}
			return inferred{typeName: "string"}
		}
		return c.inferNumericBinary(parsed, expr, left, right)
	case token.SUB, token.MUL, token.QUO, token.REM:
		return c.inferNumericBinary(parsed, expr, left, right)
	default:
		return inferred{}
	}
}

func (c *fileChecker) inferNumericBinary(parsed parsedExpression, expr *ast.BinaryExpr, left, right inferred) inferred {
	if left.typeName != "" && !isNumericType(left.typeName) {
		c.addDiagnostic(parsed.spanFor(expr.X.Pos(), expr.X.End()), fmt.Sprintf("left operand must be numeric, got %s", left.typeName))
	}
	if right.typeName != "" && !isNumericType(right.typeName) {
		c.addDiagnostic(parsed.spanFor(expr.Y.Pos(), expr.Y.End()), fmt.Sprintf("right operand must be numeric, got %s", right.typeName))
	}
	if left.typeName == right.typeName {
		return inferred{typeName: left.typeName}
	}
	if left.typeName == "float64" || right.typeName == "float64" {
		return inferred{typeName: "float64"}
	}
	return inferred{typeName: left.typeName}
}

func (c *fileChecker) checkEventHandler(source string, span sfc.Span) {
	parsed, ok := c.parseExpression(source, span)
	if !ok {
		return
	}
	name, hasArgs, ok := eventHandlerName(parsed.expr)
	if hasArgs {
		c.addDiagnostic(parsed.spanFor(parsed.expr.Pos(), parsed.expr.End()), "event handler calls with arguments are not supported")
		return
	}
	if !ok {
		c.infer(parsed, parsed.expr)
		c.addDiagnostic(parsed.spanFor(parsed.expr.Pos(), parsed.expr.End()), "event handler must be a method or function field")
		return
	}
	symbol, ok := c.lookup(name)
	if !ok {
		c.addDiagnostic(parsed.spanFor(parsed.expr.Pos(), parsed.expr.End()), fmt.Sprintf("event handler %q does not exist", name))
		return
	}
	if !symbol.callable {
		c.addDiagnostic(parsed.spanFor(parsed.expr.Pos(), parsed.expr.End()), fmt.Sprintf("event handler %q is not callable", name))
		return
	}
	if len(symbol.params) > 0 || len(symbol.results) > 0 {
		c.addDiagnostic(parsed.spanFor(parsed.expr.Pos(), parsed.expr.End()), fmt.Sprintf("event handler %q must have signature func()", name))
	}
}

func eventHandlerName(expr ast.Expr) (string, bool, bool) {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name, false, true
	case *ast.CallExpr:
		if len(expr.Args) > 0 {
			return "", true, false
		}
		return eventHandlerName(expr.Fun)
	case *ast.ParenExpr:
		return eventHandlerName(expr.X)
	default:
		return "", false, false
	}
}

func (c *fileChecker) checkCallable(parsed parsedExpression, expr ast.Expr, label string) bool {
	switch expr := expr.(type) {
	case *ast.Ident:
		symbol, ok := c.lookup(expr.Name)
		if !ok {
			c.addDiagnostic(parsed.spanFor(expr.Pos(), expr.End()), fmt.Sprintf("%s %q does not exist", label, expr.Name))
			return false
		}
		if !symbol.callable {
			c.addDiagnostic(parsed.spanFor(expr.Pos(), expr.End()), fmt.Sprintf("%s %q is not callable", label, expr.Name))
			return false
		}
		return true
	case *ast.CallExpr:
		callable := c.checkCallable(parsed, expr.Fun, label)
		for _, arg := range expr.Args {
			c.infer(parsed, arg)
		}
		return callable
	case *ast.SelectorExpr:
		base := c.infer(parsed, expr.X)
		if base.unresolved {
			return false
		}
		return true
	case *ast.ParenExpr:
		return c.checkCallable(parsed, expr.X, label)
	default:
		c.infer(parsed, expr)
		c.addDiagnostic(parsed.spanFor(expr.Pos(), expr.End()), fmt.Sprintf("%s must be a method or function field", label))
		return false
	}
}

func (c *fileChecker) expectCompatible(span sfc.Span, expected string, actual inferred, subject string) {
	if expected == "" || actual.typeName == "" || actual.unresolved {
		return
	}
	if !typesCompatible(expected, actual.typeName) {
		c.addDiagnostic(span, fmt.Sprintf("%s expects %s, got %s", subject, expected, actual.typeName))
	}
}

func (c *fileChecker) lookup(name string) (symbol, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		symbol, ok := c.scopes[i][name]
		if ok {
			return symbol, true
		}
	}
	return symbol{}, false
}

func (c *fileChecker) pushScope(scope scope) {
	c.scopes = append(c.scopes, scope)
}

func (c *fileChecker) popScope() {
	c.scopes = c.scopes[:len(c.scopes)-1]
}

func (c *fileChecker) markPropSeen(seen map[string]sfc.Span, name string, span sfc.Span) {
	if previous, exists := seen[name]; exists {
		c.addDiagnostic(span, fmt.Sprintf("duplicate prop %q", name))
		c.addDiagnostic(previous, fmt.Sprintf("first prop %q provided here", name))
		return
	}
	seen[name] = span
}

func (c *fileChecker) addDiagnostic(span sfc.Span, message string) {
	c.diagnostics = append(c.diagnostics, Diagnostic{
		Path:    c.path,
		Message: message,
		Span:    span,
	})
}

func propsByName(contract *script.Contract) map[string]script.Prop {
	props := make(map[string]script.Prop)
	for _, prop := range contract.Props {
		props[prop.Name] = prop
	}
	return props
}

func callbacksByEvent(contract *script.Contract) map[string]script.Callback {
	callbacks := make(map[string]script.Callback)
	for _, callback := range contract.Callbacks {
		callbacks[callback.FieldName] = callback
		callbacks[kebabCase(callback.FieldName)] = callback
	}
	return callbacks
}

func callbacksByField(contract *script.Contract) map[string]script.Callback {
	callbacks := make(map[string]script.Callback)
	for _, callback := range contract.Callbacks {
		callbacks[callback.FieldName] = callback
	}
	return callbacks
}

func findDirective(element *templateparser.Element, name string) *templateparser.Attr {
	for i := range element.Attrs {
		attr := &element.Attrs[i]
		if attr.Kind == templateparser.AttrDirective && attr.Name == name {
			return attr
		}
	}
	return nil
}

func hasBoundKey(element *templateparser.Element) bool {
	for _, attr := range element.Attrs {
		if attr.Kind == templateparser.AttrBound && attr.Argument == "key" {
			return true
		}
	}
	return false
}

func parseVForExpression(expression string) (itemName, indexName, sourceExpression string, ok bool) {
	parts := strings.SplitN(expression, " in ", 2)
	if len(parts) != 2 {
		return "", "", "", false
	}

	left := strings.TrimSpace(parts[0])
	sourceExpression = strings.TrimSpace(parts[1])
	left = strings.TrimPrefix(left, "(")
	left = strings.TrimSuffix(left, ")")

	names := strings.Split(left, ",")
	if len(names) == 0 || len(names) > 2 {
		return "", "", "", false
	}
	itemName = strings.TrimSpace(names[0])
	if !isIdentifier(itemName) {
		return "", "", "", false
	}
	if len(names) == 2 {
		indexName = strings.TrimSpace(names[1])
		if !isIdentifier(indexName) {
			return "", "", "", false
		}
	}
	if sourceExpression == "" {
		return "", "", "", false
	}
	return itemName, indexName, sourceExpression, true
}

func collectionItemType(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if strings.HasPrefix(typeName, "[]") {
		return strings.TrimSpace(typeName[2:])
	}
	if strings.HasPrefix(typeName, "[") {
		if close := strings.Index(typeName, "]"); close >= 0 && close+1 < len(typeName) {
			return strings.TrimSpace(typeName[close+1:])
		}
	}
	if strings.HasPrefix(typeName, "map[") {
		if close := strings.Index(typeName, "]"); close >= 0 && close+1 < len(typeName) {
			return strings.TrimSpace(typeName[close+1:])
		}
	}
	return ""
}

func nativeAttrType(name string) string {
	if strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "aria-") {
		return ""
	}
	switch name {
	case "checked", "controls", "disabled", "hidden", "multiple", "open", "readonly", "required", "selected", "autofocus":
		return "bool"
	case "alt", "class", "for", "href", "id", "name", "placeholder", "rel", "role", "src", "style", "target", "title", "type", "value":
		return "string"
	default:
		return ""
	}
}

func isNativeEvent(name string) bool {
	switch name {
	case "blur", "change", "click", "contextmenu", "dblclick", "drag", "dragend", "dragenter", "dragleave", "dragover", "dragstart", "drop", "error", "focus", "input", "invalid", "keydown", "keypress", "keyup", "load", "mousedown", "mouseenter", "mouseleave", "mousemove", "mouseout", "mouseover", "mouseup", "reset", "scroll", "select", "submit":
		return true
	default:
		return false
	}
}

func isFallthroughAttr(name string) bool {
	return name == "class" || name == "style" || name == "id" || name == "key" || strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "aria-")
}

func typesCompatible(expected, actual string) bool {
	expected = normalizeType(expected)
	actual = normalizeType(actual)
	if expected == "" || actual == "" || expected == actual {
		return true
	}
	if expected == "any" || expected == "interface{}" || actual == "nil" {
		return true
	}
	if isNumericType(expected) && isNumericType(actual) {
		return true
	}
	return false
}

func normalizeType(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	typeName = strings.TrimPrefix(typeName, "*")
	return typeName
}

func isNumericType(typeName string) bool {
	switch normalizeType(typeName) {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune", "float32", "float64", "complex64", "complex128":
		return true
	default:
		return false
	}
}

func isIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
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

func kebabCase(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('-')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func trimGoParseError(message string) string {
	parts := strings.SplitN(message, ": ", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return message
}

func positionWithin(base sfc.Position, source string, offset int) sfc.Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}

	line := base.Line
	column := base.Column
	lineStart := 0
	for i, b := range source[:offset] {
		if b == '\n' {
			line++
			lineStart = i + 1
		}
	}
	if lineStart == 0 {
		column += offset
	} else {
		column = offset - lineStart + 1
	}
	return sfc.Position{
		Offset: base.Offset + offset,
		Line:   line,
		Column: column,
	}
}
