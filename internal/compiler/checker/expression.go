package checker

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/sfc"
	"github.com/norunners/tue/internal/compiler/typecap"
)

func (c *fileChecker) checkExpression(expression string, span sfc.Span, scope *scope) value {
	expr, exprChecker, ok := c.parseExpression(expression, span, scope)
	if !ok {
		return value{Type: unknownType}
	}
	return exprChecker.eval(expr)
}

func (c *fileChecker) parseExpression(expression string, span sfc.Span, scope *scope) (ast.Expr, *exprChecker, bool) {
	fset := token.NewFileSet()
	expr, err := goparser.ParseExprFrom(fset, "", expression, 0)
	exprChecker := &exprChecker{
		fileChecker: c,
		fset:        fset,
		source:      expression,
		base:        span,
		scope:       scope,
	}
	if err != nil {
		c.add(fmt.Sprintf("invalid expression: %s", err), span)
		return nil, exprChecker, false
	}
	return expr, exprChecker, true
}

type exprChecker struct {
	fileChecker *fileChecker
	fset        *token.FileSet
	source      string
	base        sfc.Span
	scope       *scope
}

type value struct {
	Type   string
	Symbol *symbol
}

func (c *exprChecker) eval(expr ast.Expr) value {
	switch typed := expr.(type) {
	case *ast.Ident:
		return c.ident(typed)
	case *ast.BasicLit:
		return c.basicLit(typed)
	case *ast.BinaryExpr:
		return c.binary(typed)
	case *ast.UnaryExpr:
		return c.unary(typed)
	case *ast.ParenExpr:
		return c.eval(typed.X)
	case *ast.SelectorExpr:
		return c.selector(typed)
	case *ast.CallExpr:
		return c.call(typed)
	case *ast.IndexExpr:
		return c.index(typed)
	case *ast.SliceExpr:
		c.eval(typed.X)
		if typed.Low != nil {
			c.eval(typed.Low)
		}
		if typed.High != nil {
			c.eval(typed.High)
		}
		if typed.Max != nil {
			c.eval(typed.Max)
		}
		return value{Type: unknownType}
	default:
		return value{Type: unknownType}
	}
}

func (c *exprChecker) ident(ident *ast.Ident) value {
	switch ident.Name {
	case "true", "false":
		return value{Type: "bool"}
	case "nil":
		return value{Type: unknownType}
	}

	symbol, ok := c.scope.lookup(ident.Name)
	if !ok {
		c.fileChecker.add(fmt.Sprintf("unknown identifier %q", ident.Name), c.nodeSpan(ident))
		return value{Type: unknownType}
	}
	return value{Type: symbol.Type, Symbol: symbol}
}

func (c *exprChecker) basicLit(lit *ast.BasicLit) value {
	switch lit.Kind {
	case token.STRING:
		if _, err := strconv.Unquote(lit.Value); err != nil {
			c.fileChecker.add("invalid string literal", c.nodeSpan(lit))
		}
		return value{Type: "string"}
	case token.INT:
		return value{Type: "int"}
	case token.FLOAT:
		return value{Type: "float64"}
	case token.CHAR:
		return value{Type: "rune"}
	default:
		return value{Type: unknownType}
	}
}

func (c *exprChecker) binary(binary *ast.BinaryExpr) value {
	left := c.eval(binary.X)
	right := c.eval(binary.Y)

	switch binary.Op {
	case token.LAND, token.LOR:
		c.expectOperand("bool", left, binary.X)
		c.expectOperand("bool", right, binary.Y)
		return value{Type: "bool"}
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		if !typecap.Assignable(left.Type, right.Type) && !typecap.Assignable(right.Type, left.Type) {
			c.fileChecker.add(
				fmt.Sprintf("cannot compare %s and %s", displayType(left.Type), displayType(right.Type)),
				c.nodeSpan(binary),
			)
		}
		return value{Type: "bool"}
	case token.ADD:
		if left.Type == "string" || right.Type == "string" {
			if !typecap.Assignable("string", left.Type) || !typecap.Assignable("string", right.Type) {
				c.fileChecker.add("operator + requires both operands to be strings or numbers", c.nodeSpan(binary))
				return value{Type: unknownType}
			}
			return value{Type: "string"}
		}
		if typecap.Numeric(left.Type) && typecap.Numeric(right.Type) {
			return value{Type: left.Type}
		}
	case token.SUB, token.MUL, token.QUO, token.REM:
		if typecap.Numeric(left.Type) && typecap.Numeric(right.Type) {
			return value{Type: left.Type}
		}
	}

	if left.Type == unknownType || right.Type == unknownType {
		return value{Type: unknownType}
	}
	c.fileChecker.add(fmt.Sprintf("operator %s is not defined for %s and %s", binary.Op, displayType(left.Type), displayType(right.Type)), c.nodeSpan(binary))
	return value{Type: unknownType}
}

func (c *exprChecker) unary(unary *ast.UnaryExpr) value {
	operand := c.eval(unary.X)
	switch unary.Op {
	case token.NOT:
		c.expectOperand("bool", operand, unary.X)
		return value{Type: "bool"}
	case token.ADD, token.SUB:
		if operand.Type == unknownType || typecap.Numeric(operand.Type) {
			return operand
		}
		c.fileChecker.add(fmt.Sprintf("operator %s is not defined for %s", unary.Op, displayType(operand.Type)), c.nodeSpan(unary))
	}
	return value{Type: unknownType}
}

func (c *exprChecker) selector(selector *ast.SelectorExpr) value {
	base := c.eval(selector.X)
	if base.Type == unknownType {
		return value{Type: unknownType}
	}
	if field, ok := c.fileChecker.structField(base.Type, selector.Sel.Name); ok {
		return value{Type: fieldType(*field)}
	}
	if _, ok := c.fileChecker.structs[typecap.Normalize(base.Type)]; ok {
		c.fileChecker.add(fmt.Sprintf("type %s has no field %q", displayType(base.Type), selector.Sel.Name), c.nodeSpan(selector.Sel))
		return value{Type: unknownType}
	}
	if typecap.Scalar(base.Type) || strings.HasPrefix(typecap.Normalize(base.Type), "[]") || strings.HasPrefix(typecap.Normalize(base.Type), "map[") {
		c.fileChecker.add(fmt.Sprintf("type %s has no field %q", displayType(base.Type), selector.Sel.Name), c.nodeSpan(selector.Sel))
	}
	return value{Type: unknownType}
}

func (c *exprChecker) call(call *ast.CallExpr) value {
	for _, arg := range call.Args {
		c.eval(arg)
	}

	if ident, ok := call.Fun.(*ast.Ident); ok {
		symbol, found := c.scope.lookup(ident.Name)
		if !found {
			c.fileChecker.add(fmt.Sprintf("unknown identifier %q", ident.Name), c.nodeSpan(ident))
			return value{Type: unknownType}
		}
		if !symbol.Method {
			return value{Type: unknownType}
		}
		if len(call.Args) != 0 || len(symbol.Parameters) != 0 || len(symbol.Results) != 1 {
			c.fileChecker.add(fmt.Sprintf("method call %q must have signature func() T", ident.Name), c.nodeSpan(call))
			return value{Type: unknownType}
		}
		return value{Type: symbol.ResultType}
	}

	c.eval(call.Fun)
	return value{Type: unknownType}
}

func (c *exprChecker) index(index *ast.IndexExpr) value {
	base := c.eval(index.X)
	c.eval(index.Index)
	if types, ok := typecap.IterableFor(base.Type); ok {
		return value{Type: types.Item}
	}
	return value{Type: unknownType}
}

func (c *exprChecker) expectOperand(expected string, actual value, expr ast.Expr) {
	if typecap.Assignable(expected, actual.Type) {
		return
	}
	c.fileChecker.add(fmt.Sprintf("operand expects %s, got %s", displayType(expected), displayType(actual.Type)), c.nodeSpan(expr))
}

func (c *fileChecker) structField(typeName string, fieldName string) (*script.Field, bool) {
	fields, ok := c.structs[typecap.Normalize(typeName)]
	if !ok {
		return nil, false
	}
	field, ok := fields[fieldName]
	if !ok {
		return nil, false
	}
	return &field, true
}

func (c *exprChecker) nodeSpan(node ast.Node) sfc.Span {
	start := c.offset(node.Pos())
	end := c.offset(node.End())
	return spanWithin(c.base, c.source, start, end)
}

func (c *exprChecker) offset(position token.Pos) int {
	file := c.fset.File(position)
	if file == nil || !position.IsValid() {
		return len(c.source)
	}
	offset := file.Offset(position)
	if offset < 0 {
		return 0
	}
	if offset > len(c.source) {
		return len(c.source)
	}
	return offset
}
