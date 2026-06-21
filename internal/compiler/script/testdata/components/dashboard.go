package fixtures

import tue "github.com/norunners/tue"

type User struct{}

type Dashboard struct {
	tue.Comp[struct {
		Name      string              `prop:",required"`
		Active    bool                `prop:"active"`
		UserID    string              `prop:"user-id"`
		Expanded  bool                `state:""`
		Count     int                 `state:""`
		Label     string              `state:""`
		Close     func()              `event:""`
		Select    func(name string)   `event:""`
		Range     func(name string, count int) `event:""`
		Pointer   func(*User)         `event:""`
		Variadic  func(values ...string) `event:""`
	}]
	total tue.Computed[int]
	user  tue.Resource[User]
}

func (d *Dashboard) Init(ctx tue.Context) {}

func (d *Dashboard) increment() {}

func (d Dashboard) snapshot() string { return "" }
