package gogen

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/norunners/tue/internal/compiler/script"
	"github.com/norunners/tue/internal/compiler/typecap"
)

type expressionGenerator struct {
	fields     map[string]script.Field
	methods    map[string]script.Method
	locals     map[string]string
	localTypes map[string]string
	typeFields map[string]map[string]script.Field
}

func (g expressionGenerator) render(expr ast.Expr) (jen.Code, bool) {
	switch typed := expr.(type) {
	case *ast.Ident:
		code := g.ident(typed)
		return code, code != nil
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
		if !g.validSelector(typed) {
			return nil, false
		}
		base, ok := g.render(typed.X)
		if !ok {
			return nil, false
		}
		return base.(*jen.Statement).Dot(typed.Sel.Name), true
	case *ast.CallExpr:
		return g.call(typed)
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

func (g expressionGenerator) call(call *ast.CallExpr) (jen.Code, bool) {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return nil, false
	}
	if len(call.Args) != 0 {
		return nil, false
	}
	if _, local := g.locals[ident.Name]; local {
		return nil, false
	}
	method, ok := g.methods[ident.Name]
	if !ok || len(method.Parameters) != 0 || len(method.Results) != 1 {
		return nil, false
	}
	return jen.Id("component").Dot(method.Name).Call(), true
}

func (g expressionGenerator) validSelector(selector *ast.SelectorExpr) bool {
	baseType := expressionTyper{
		fields:     g.fields,
		methods:    g.methods,
		localTypes: g.localTypes,
		typeFields: g.typeFields,
	}.eval(selector.X)
	if baseType == "unknown" {
		return true
	}
	if _, ok := structField(g.typeFields, baseType, selector.Sel.Name); ok {
		return true
	}
	baseType = typecap.Normalize(baseType)
	if _, known := g.typeFields[baseType]; known {
		return false
	}
	return !typecap.Scalar(baseType) && !strings.HasPrefix(baseType, "[]") && !strings.HasPrefix(baseType, "map[")
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
	if method, ok := g.methods[ident.Name]; ok && method.ImplicitGetter {
		return jen.Id("component").Dot(method.Name).Call()
	}
	if _, ok := g.methods[ident.Name]; ok {
		return nil
	}

	field, ok := g.fields[ident.Name]
	if !ok {
		return jen.Id(ident.Name)
	}

	access := jen.Id("component").Dot(field.Name)
	switch field.Kind {
	case script.FieldKindComputed, script.FieldKindResource:
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
		methods:    g.methods,
		localTypes: g.localTypes,
		typeFields: g.typeFields,
	}
	return typer.eval(expr)
}

type expressionTyper struct {
	fields     map[string]script.Field
	methods    map[string]script.Method
	localTypes map[string]string
	typeFields map[string]map[string]script.Field
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
		return t.selector(typed)
	case *ast.CallExpr:
		return t.call(typed)
	case *ast.IndexExpr:
		base := t.eval(typed.X)
		types, ok := typecap.IterableFor(base)
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
	if method, ok := t.methods[ident.Name]; ok && method.ImplicitGetter && len(method.Results) == 1 {
		return method.Results[0].Type
	}
	if field, ok := t.fields[ident.Name]; ok {
		return fieldValueType(field)
	}
	return "unknown"
}

func (t expressionTyper) selector(selector *ast.SelectorExpr) string {
	baseType := t.eval(selector.X)
	field, ok := structField(t.typeFields, baseType, selector.Sel.Name)
	if !ok {
		return "unknown"
	}
	return fieldValueType(*field)
}

func (t expressionTyper) call(call *ast.CallExpr) string {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || len(call.Args) != 0 {
		return "unknown"
	}
	if _, local := t.localTypes[ident.Name]; local {
		return "unknown"
	}
	method, ok := t.methods[ident.Name]
	if !ok || len(method.Parameters) != 0 || len(method.Results) != 1 {
		return "unknown"
	}
	return method.Results[0].Type
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
			if typecap.Assignable("string", left) && typecap.Assignable("string", right) {
				return "string"
			}
			return "unknown"
		}
		if typecap.Numeric(left) && typecap.Numeric(right) {
			return left
		}
	case token.SUB, token.MUL, token.QUO, token.REM:
		if typecap.Numeric(left) && typecap.Numeric(right) {
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
		if operand == typecap.Unknown || typecap.Numeric(operand) {
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

func structField(typeFields map[string]map[string]script.Field, typeName string, fieldName string) (*script.Field, bool) {
	fields, ok := typeFields[typecap.Normalize(typeName)]
	if !ok {
		return nil, false
	}
	field, ok := fields[fieldName]
	if !ok {
		return nil, false
	}
	return &field, true
}
