package gota

import (
	"github.com/carlosxfelipe/gota/core"
	"github.com/carlosxfelipe/gota/validation"
)

// Re-exported Core Types
type (
	App             = core.App
	Context         = core.Context
	Config          = core.Config
	Route           = core.Route
	RouteGroup      = core.RouteGroup
	HandlerFunc     = core.HandlerFunc
	MiddlewareFunc  = core.MiddlewareFunc
	Map             = core.Map
	Empty           = core.Empty
	HTTPError       = core.HTTPError
	ErrorResponse   = core.ErrorResponse
	ValidationError = validation.ValidationError
	ScalarTheme     = core.ScalarTheme
	CORSOptions     = core.CORSOptions
	OpenAPISpec     = core.OpenAPISpec
)

// Re-exported Scalar Theme Constants
const (
	ThemeDefault    = core.ThemeDefault
	ThemeAlternate  = core.ThemeAlternate
	ThemeMoon       = core.ThemeMoon
	ThemePurple     = core.ThemePurple
	ThemeSolarized  = core.ThemeSolarized
	ThemeBluePlanet = core.ThemeBluePlanet
	ThemeDeepSpace  = core.ThemeDeepSpace
	ThemeSaturn     = core.ThemeSaturn
	ThemeKepler     = core.ThemeKepler
	ThemeElysiaJS   = core.ThemeElysiaJS
	ThemeFastify    = core.ThemeFastify
	ThemeMars       = core.ThemeMars
	ThemeLaserwave  = core.ThemeLaserwave
	ThemeNone       = core.ThemeNone
)

// New initializes and returns a new Gota application instance.
func New(configs ...Config) *App {
	return core.New(configs...)
}

// Built-in Middleware helpers
func CORS(opts ...CORSOptions) MiddlewareFunc {
	return core.CORS(opts...)
}

func Logger() MiddlewareFunc {
	return core.Logger()
}

func Recover() MiddlewareFunc {
	return core.Recover()
}

// Built-in HTTP Error constructors
func BadRequest(message string) *HTTPError {
	return core.BadRequest(message)
}

func Unauthorized(message string) *HTTPError {
	return core.Unauthorized(message)
}

func Forbidden(message string) *HTTPError {
	return core.Forbidden(message)
}

func NotFound(message string) *HTTPError {
	return core.NotFound(message)
}

func UnprocessableEntity(message string) *HTTPError {
	return core.UnprocessableEntity(message)
}

func InternalServerError(message string) *HTTPError {
	return core.InternalServerError(message)
}
