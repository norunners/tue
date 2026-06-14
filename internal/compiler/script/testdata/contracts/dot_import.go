package fixtures

import . "github.com/norunners/tue"

type DotImport struct {
	name Prop[string]
}

func (d *DotImport) Init(ctx Context) {}
