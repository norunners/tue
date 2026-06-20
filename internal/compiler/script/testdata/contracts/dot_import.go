package fixtures

import . "github.com/norunners/tue"

type DotImport struct {
	Comp[struct {
		Name  string `prop:",required"`
		Close func() `event:""`
	}]
	count Ref[int]
}

func (d *DotImport) Init(ctx Context) {}
