package fixtures

import tue "github.com/norunners/tue"

type App struct {
	onSave tue.On[string]
}
