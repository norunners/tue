package fixtures

import tue "github.com/norunners/tue"

type User struct{}

type Dashboard struct {
	name  tue.Prop[string] `prop:"name,required"`
	count tue.Ref[int]
	total tue.Computed[int]
	user  tue.Resource[User]
	onSave func()
	label string
}

func (d *Dashboard) Init(ctx tue.Context) {}

func (d *Dashboard) increment() {}

func (d Dashboard) snapshot() string { return "" }
