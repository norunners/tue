package fixtures

import . "github.com/norunners/tue"

type DotImport struct {
	Comp[struct {
		Name  string `prop:",required"`
		Close func() `event:""`
		Count int    `state:""`
	}]
}

func (d *DotImport) Init(ctx Context) {}
