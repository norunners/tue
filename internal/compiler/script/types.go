package script

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
)

type tueImporter struct {
	fallback types.Importer
	tue      *types.Package
}

func newTueImporter() *tueImporter {
	return &tueImporter{
		fallback: importer.Default(),
		tue:      stubTuePackage(),
	}
}

func (i *tueImporter) Import(path string) (*types.Package, error) {
	if path == tueImportPath {
		return i.tue, nil
	}
	if i.fallback == nil {
		return nil, fmt.Errorf("import %q: no fallback importer", path)
	}
	return i.fallback.Import(path)
}

func (i *tueImporter) ImportFrom(path string, dir string, mode types.ImportMode) (*types.Package, error) {
	if path == tueImportPath {
		return i.tue, nil
	}
	if from, ok := i.fallback.(types.ImporterFrom); ok {
		return from.ImportFrom(path, dir, mode)
	}
	return i.Import(path)
}

func stubTuePackage() *types.Package {
	pkg := types.NewPackage(tueImportPath, "tue")
	scope := pkg.Scope()
	emptyInterface := types.NewInterfaceType(nil, nil).Complete()
	anyType := types.Universe.Lookup("any").Type()

	for _, name := range []string{"Prop", "Ref", "Computed", "Resource"} {
		typeName := types.NewTypeName(token.NoPos, pkg, name, nil)
		named := types.NewNamed(typeName, emptyInterface, nil)
		typeParamName := types.NewTypeName(token.NoPos, pkg, "T", nil)
		typeParam := types.NewTypeParam(typeParamName, anyType)
		named.SetTypeParams([]*types.TypeParam{typeParam})
		scope.Insert(typeName)
	}

	onTypeName := types.NewTypeName(token.NoPos, pkg, "On", nil)
	onTypeParamName := types.NewTypeName(token.NoPos, pkg, "F", nil)
	onTypeParam := types.NewTypeParam(onTypeParamName, anyType)
	onStruct := types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, pkg, "fn", onTypeParam, false),
	}, nil)
	onType := types.NewNamed(onTypeName, onStruct, nil)
	onType.SetTypeParams([]*types.TypeParam{onTypeParam})
	scope.Insert(onTypeName)

	for _, name := range []string{"Context", "Route", "RouteMatch", "Router", "TrustedHTML", "VNode"} {
		typeName := types.NewTypeName(token.NoPos, pkg, name, nil)
		types.NewNamed(typeName, emptyInterface, nil)
		scope.Insert(typeName)
	}

	pkg.MarkComplete()
	return pkg
}
