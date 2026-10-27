package gota

import (
	"crypto/rsa"

	"github.com/carlosxfelipe/gota/core"
	"github.com/carlosxfelipe/gota/validation"
)

// Re-exported Core Types
type (
	App               = core.App
	Context           = core.Context
	Config            = core.Config
	Route             = core.Route
	RouteGroup        = core.RouteGroup
	HandlerFunc       = core.HandlerFunc
	MiddlewareFunc    = core.MiddlewareFunc
	Map               = core.Map
	Empty             = core.Empty
	HTTPError         = core.HTTPError
	ErrorResponse     = core.ErrorResponse
	ValidationError   = validation.ValidationError
	ScalarTheme       = core.ScalarTheme
	CORSOptions       = core.CORSOptions
	JWTAlgorithm      = core.JWTAlgorithm
	JWTHeader         = core.JWTHeader
	JWTOptions        = core.JWTOptions
	OpenAPISpec       = core.OpenAPISpec
	RequestIDOptions  = core.RequestIDOptions
	SlogLoggerOptions = core.SlogLoggerOptions
)

// Re-exported JWT Algorithm Constants
const (
	JWTAlgorithmHS256 = core.JWTAlgorithmHS256
	JWTAlgorithmRS256 = core.JWTAlgorithmRS256
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

	DefaultScalarVersion = core.DefaultScalarVersion
	DefaultScalarCDN     = core.DefaultScalarCDN
	DefaultScalarSRI     = core.DefaultScalarSRI

	DefaultMaxBodyBytes       = core.DefaultMaxBodyBytes
	DefaultMaxMultipartMemory = core.DefaultMaxMultipartMemory
)

// New initializes and returns a new Gota application instance.
func New(cfg Config) *App {
	if cfg.SilenceTagWarnings {
		validation.SilenceTagWarnings = true
	}
	return core.New(cfg)
}

// SilenceTagWarnings enables or disables startup warning logs for suspicious validation tags
// (such as validate:"required" on primitive bool or numeric fields).
func SilenceTagWarnings(silence bool) {
	validation.SilenceTagWarnings = silence
}

// ScalarCDNURL returns the standalone bundle URL for a given @scalar/api-reference version.
func ScalarCDNURL(version string) string {
	return core.ScalarCDNURL(version)
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

func RequestID(opts ...RequestIDOptions) MiddlewareFunc {
	return core.RequestID(opts...)
}

func SlogLogger(opts ...SlogLoggerOptions) MiddlewareFunc {
	return core.SlogLogger(opts...)
}

// Built-in JWT Authentication helpers
func JWT(secret []byte, opts ...JWTOptions) MiddlewareFunc {
	return core.JWT(secret, opts...)
}

func JWTRSA(publicKey *rsa.PublicKey, opts ...JWTOptions) MiddlewareFunc {
	return core.JWTRSA(publicKey, opts...)
}

func SignJWT(secret []byte, payload any) (string, error) {
	return core.SignJWT(secret, payload)
}

func SignJWTRSA(privateKey *rsa.PrivateKey, payload any, keyID ...string) (string, error) {
	return core.SignJWTRSA(privateKey, payload, keyID...)
}

func VerifyJWT(secret []byte, tokenString string, target any) error {
	return core.VerifyJWT(secret, tokenString, target)
}

func VerifyJWTRSA(publicKey *rsa.PublicKey, tokenString string, target any) error {
	return core.VerifyJWTRSA(publicKey, tokenString, target)
}

func ParseJWTHeader(tokenString string) (JWTHeader, error) {
	return core.ParseJWTHeader(tokenString)
}

func ParseRSAPrivateKeyFromPEM(pemBytes []byte) (*rsa.PrivateKey, error) {
	return core.ParseRSAPrivateKeyFromPEM(pemBytes)
}

func ParseRSAPublicKeyFromPEM(pemBytes []byte) (*rsa.PublicKey, error) {
	return core.ParseRSAPublicKeyFromPEM(pemBytes)
}

func JWTClaims[T any](ctx *Context, key ...string) (T, bool) {
	return core.JWTClaims[T](ctx, key...)
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
