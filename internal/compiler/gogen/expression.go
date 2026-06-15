package gogen

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/norunners/tue/internal/compiler/script"
)

type expressionGenerator struct {
	fields map[string]script.Field
	locals map[string]string
}

func (g expressionGenerator) render(expr ast.Expr) (jen.Code, bool) {
	switch typed := expr.(type) {
	case *ast.Ident:
		return g.ident(typed), true
	case *ast.BasicLit:
		return basicLiteral(typed)
	case *ast.BinaryExpr:
		left, ok := g.render(typed.X)
		if !ok {
			return nil, false
		}
		right, ok := g.render(typed.Y)
		if !ok {
			return nil, false
		}
		return jen.Parens(jen.Add(left).Op(typed.Op.String()).Add(right)), true
	case *ast.UnaryExpr:
		operand, ok := g.render(typed.X)
		if !ok {
			return nil, false
		}
		return jen.Op(typed.Op.String()).Add(operand), true
	case *ast.ParenExpr:
		inner, ok := g.render(typed.X)
		if !ok {
			return nil, false
		}
		return jen.Parens(inner), true
	case *ast.SelectorExpr:
		base, ok := g.render(typed.X)
		if !ok {
			return nil, false
		}
		return base.(*jen.Statement).Dot(typed.Sel.Name), true
	case *ast.IndexExpr:
		base, ok := g.render(typed.X)
		if !ok {
			return nil, false
		}
		index, ok := g.render(typed.Index)
		if !ok {
			return nil, false
		}
		return base.(*jen.Statement).Index(index), true
	default:
		return nil, false
	}
}

func (g expressionGenerator) ident(ident *ast.Ident) jen.Code {
	switch ident.Name {
	case "true":
		return jen.True()
	case "false":
		return jen.False()
	case "nil":
		return jen.Nil()
	}

	if local, ok := g.locals[ident.Name]; ok {
		return jen.Id(local)
	}

	field, ok := g.fields[ident.Name]
	if !ok {
		return jen.Id(ident.Name)
	}

	access := jen.Id("component").Dot(field.Name)
	switch field.Kind {
	case script.FieldKindProp, script.FieldKindRef, script.FieldKindComputed, script.FieldKindResource:
		return access.Dot("Get").Call()
	default:
		return access
	}
}

func basicLiteral(lit *ast.BasicLit) (jen.Code, bool) {
	switch lit.Kind {
	case token.STRING:
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return nil, false
		}
		return jen.Lit(value), true
	case token.INT:
		value, err := strconv.ParseInt(lit.Value, 0, 64)
		if err != nil {
			return nil, false
		}
		return jen.Lit(int(value)), true
	case token.FLOAT:
		value, err := strconv.ParseFloat(lit.Value, 64)
		if err != nil {
			return nil, false
		}
		return jen.Lit(value), true
	case token.CHAR:
		value, _, tail, err := strconv.UnquoteChar(lit.Value[1:len(lit.Value)-1], '\'')
		if err != nil || tail != "" {
			return nil, false
		}
		return jen.LitRune(value), true
	default:
		return nil, false
	}
}

func (g *fileGenerator) expressionType(expression string) string {
	expr, err := goparser.ParseExpr(expression)
	if err != nil {
		return "unknown"
	}
	typer := expressionTyper{
		fields:     g.fields,
		localTypes: g.localTypes,
	}
	return typer.eval(expr)
}

type expressionTyper struct {
	fields     map[string]script.Field
	localTypes map[string]string
}

func (t expressionTyper) eval(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return t.ident(typed)
	case *ast.BasicLit:
		return literalType(typed)
	case *ast.BinaryExpr:
		return t.binary(typed)
	case *ast.UnaryExpr:
		return t.unary(typed)
	case *ast.ParenExpr:
		return t.eval(typed.X)
	case *ast.SelectorExpr:
		return "unknown"
	case *ast.CallExpr:
		return "unknown"
	case *ast.IndexExpr:
		base := t.eval(typed.X)
		types, ok := iterableTypesFor(base)
		if !ok {
			return "unknown"
		}
		return types.Item
	case *ast.SliceExpr:
		return "unknown"
	default:
		return "unknown"
	}
}

func (t expressionTyper) ident(ident *ast.Ident) string {
	switch ident.Name {
	case "true", "false":
		return "bool"
	case "nil":
		return "unknown"
	}
	if typ, ok := t.localTypes[ident.Name]; ok {
		return typ
	}
	if field, ok := t.fields[ident.Name]; ok {
		return fieldValueType(field)
	}
	return "unknown"
}

func literalType(lit *ast.BasicLit) string {
	switch lit.Kind {
	case token.STRING:
		return "string"
	case token.INT:
		return "int"
	case token.FLOAT:
		return "float64"
	case token.CHAR:
		return "rune"
	default:
		return "unknown"
	}
}

func (t expressionTyper) binary(binary *ast.BinaryExpr) string {
	left := t.eval(binary.X)
	right := t.eval(binary.Y)

	switch binary.Op {
	case token.LAND, token.LOR:
		return "bool"
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return "bool"
	case token.ADD:
		if left == "string" || right == "string" {
			if assignableType("string", left) && assignableType("string", right) {
				return "string"
			}
			return "unknown"
		}
		if isNumericType(left) && isNumericType(right) {
			return left
		}
	case token.SUB, token.MUL, token.QUO, token.REM:
		if isNumericType(left) && isNumericType(right) {
			return left
		}
	}
	return "unknown"
}

func (t expressionTyper) unary(unary *ast.UnaryExpr) string {
	operand := t.eval(unary.X)
	switch unary.Op {
	case token.NOT:
		return "bool"
	case token.ADD, token.SUB:
		if operand == "unknown" || isNumericType(operand) {
			return operand
		}
	}
	return "unknown"
}

func fieldValueType(field script.Field) string {
	if field.ValueType != "" {
		return field.ValueType
	}
	if field.Type != "" {
		return field.Type
	}
	return "unknown"
}

type iterableTypes struct {
	Item string
	Key  string
}

func iterableTypesFor(typ string) (iterableTypes, bool) {
	typ = normalizeType(typ)
	if typ == "" || typ == "unknown" {
		return iterableTypes{Item: "unknown", Key: "unknown"}, true
	}
	if strings.HasPrefix(typ, "[]") {
		return iterableTypes{Item: strings.TrimSpace(strings.TrimPrefix(typ, "[]")), Key: "int"}, true
	}
	if strings.HasPrefix(typ, "[") {
		close := closingTypeBracket(typ, 0)
		if close != -1 && close+1 < len(typ) {
			return iterableTypes{Item: strings.TrimSpace(typ[close+1:]), Key: "int"}, true
		}
	}
	if strings.HasPrefix(typ, "map[") {
		close := closingTypeBracket(typ, len("map"))
		if close != -1 && close+1 < len(typ) {
			return iterableTypes{
				Item: strings.TrimSpace(typ[close+1:]),
				Key:  strings.TrimSpace(typ[len("map["):close]),
			}, true
		}
	}
	if typ == "string" {
		return iterableTypes{Item: "rune", Key: "int"}, true
	}
	return iterableTypes{}, false
}

func closingTypeBracket(typ string, open int) int {
	if open < 0 || open >= len(typ) || typ[open] != '[' {
		return -1
	}
	depth := 0
	for i := open; i < len(typ); i++ {
		switch typ[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isNumericType(typ string) bool {
	switch normalizeType(typ) {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "rune", "byte":
		return true
	default:
		return false
	}
}
