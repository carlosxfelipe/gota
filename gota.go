package gota

import "github.com/carlosxfelipe/gota/repr"

// App is an alias for repr.App.
type App = repr.App

// Config is an alias for repr.Config.
type Config = repr.Config

// New creates and configures a new gota App instance.
func New(cfg Config) *App {
	return repr.New(cfg)
}
