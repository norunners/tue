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

	for _, name := range []string{"Ref", "Computed", "Resource", "Comp"} {
		insertGenericStubType(scope, pkg, name, emptyInterface, anyType, 1)
	}

	for _, name := range []string{"Context", "DOMEvent", "Route", "RouteMatch", "Router", "TrustedHTML", "VNode"} {
		typeName := types.NewTypeName(token.NoPos, pkg, name, nil)
		types.NewNamed(typeName, emptyInterface, nil)
		scope.Insert(typeName)
	}

	pkg.MarkComplete()
	return pkg
}

func insertGenericStubType(scope *types.Scope, pkg *types.Package, name string, underlying types.Type, constraint types.Type, arity int) {
	typeName := types.NewTypeName(token.NoPos, pkg, name, nil)
	named := types.NewNamed(typeName, underlying, nil)
	parameters := make([]*types.TypeParam, arity)
	for index := range parameters {
		parameterName := types.NewTypeName(token.NoPos, pkg, fmt.Sprintf("T%d", index+1), nil)
		parameters[index] = types.NewTypeParam(parameterName, constraint)
	}
	named.SetTypeParams(parameters)
	scope.Insert(typeName)
}
