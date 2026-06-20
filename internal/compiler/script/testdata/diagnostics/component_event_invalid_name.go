package fixtures

import tue "github.com/norunners/tue"

type App struct {
	save tue.On[func()]
}
