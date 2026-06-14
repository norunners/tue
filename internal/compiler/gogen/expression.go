package gogen

import (
	"go/ast"
	"go/token"
	"strconv"

	"github.com/dave/jennifer/jen"
	"github.com/norunners/tue/internal/compiler/script"
)

type expressionGenerator struct {
	fields map[string]script.Field
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
