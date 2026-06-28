package fixtures

import (
	"context"

	tue "github.com/norunners/tue"
)

type User struct{}

type Dashboard struct {
	tue.Comp[struct {
		Name     string                       `prop:",required"`
		Active   bool                         `prop:"active"`
		UserID   string                       `prop:"user-id"`
		Expanded bool                         `state:""`
		Count    int                          `state:""`
		Label    string                       `computed:"label"`
		Total    int                          `computed:"total"`
		User     User                         `resource:"loadUser"`
		Close    func()                       `event:""`
		Select   func(name string)            `event:""`
		Range    func(name string, count int) `event:""`
		Pointer  func(*User)                  `event:""`
		Variadic func(values ...string)       `event:""`
	}]
}

func (d *Dashboard) Init(ctx tue.Context) {}

func (d *Dashboard) increment() {}

func (d Dashboard) snapshot() string { return "" }

func (d *Dashboard) label() string { return d.Name() }

func (d *Dashboard) total() int { return d.Count() }

func (d *Dashboard) loadUser(context.Context) (User, error) { return User{}, nil }
