package gota

import (
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// MiddlewareFunc defines a function that wraps a HandlerFunc.
type MiddlewareFunc func(next HandlerFunc) HandlerFunc

// chain wraps the final handler with the provided slice of middlewares.
func chain(final HandlerFunc, mws []MiddlewareFunc) HandlerFunc {
	if len(mws) == 0 {
		return final
	}
	h := final
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// CORSOptions configures the CORS middleware.
type CORSOptions struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

// DefaultCORSOptions returns default permissive CORS configuration.
func DefaultCORSOptions() CORSOptions {
	return CORSOptions{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"},
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With"},
		MaxAge:       86400,
	}
}

// CORS returns a middleware enabling Cross-Origin Resource Sharing.
func CORS(opts ...CORSOptions) MiddlewareFunc {
	cfg := DefaultCORSOptions()
	if len(opts) > 0 {
		cfg = opts[0]
	}

	methodsStr := strings.Join(cfg.AllowMethods, ", ")
	headersStr := strings.Join(cfg.AllowHeaders, ", ")
	exposeStr := strings.Join(cfg.ExposeHeaders, ", ")

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			w := ctx.ResponseWriter
			r := ctx.Request

			origin := r.Header.Get("Origin")
			if origin != "" {
				if len(cfg.AllowOrigins) == 1 && cfg.AllowOrigins[0] == "*" {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					for _, o := range cfg.AllowOrigins {
						if o == origin || o == "*" {
							w.Header().Set("Access-Control-Allow-Origin", origin)
							w.Header().Set("Vary", "Origin")
							break
						}
					}
				}

				if cfg.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				if exposeStr != "" {
					w.Header().Set("Access-Control-Expose-Headers", exposeStr)
				}
			}

			if r.Method == http.MethodOptions {
				if methodsStr != "" {
					w.Header().Set("Access-Control-Allow-Methods", methodsStr)
				}
				if headersStr != "" {
					w.Header().Set("Access-Control-Allow-Headers", headersStr)
				}
				if cfg.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", time.Duration(cfg.MaxAge).String())
				}
				return ctx.NoContent()
			}

			return next(ctx)
		}
	}
}

// Recover catches panics occurring inside handlers and returns a 500 error.
func Recover() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("gota: panic recovered: %v\n%s", r, debug.Stack())
					if !ctx.Written() {
						_ = ctx.Status(http.StatusInternalServerError).Json(ErrorResponse{
							Error:  "internal server error",
							Status: http.StatusInternalServerError,
						})
					}
				}
			}()
			return next(ctx)
		}
	}
}

// Logger logs each incoming request with method, path, status, and duration.
func Logger() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			start := time.Now()
			err := next(ctx)
			duration := time.Since(start)
			log.Printf("gota: %s %s [%d] %s", ctx.Request.Method, ctx.Request.URL.Path, ctx.StatusCode(), duration)
			return err
		}
	}
}
