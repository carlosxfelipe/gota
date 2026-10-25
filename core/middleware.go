package core

import (
	"crypto/rand"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// MiddlewareFunc defines a function that wraps a HandlerFunc.
type MiddlewareFunc func(next HandlerFunc) HandlerFunc

// Chain wraps the final handler with the provided slice of middlewares.
func Chain(final HandlerFunc, mws []MiddlewareFunc) HandlerFunc {
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
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(cfg.MaxAge))
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
			status := ctx.StatusCode()
			if err != nil && !ctx.Written() {
				// The error handler runs after this middleware, so resolve the status it will use.
				status = statusFromError(err)
			}
			log.Printf("gota: %s %s [%d] %s", ctx.Request.Method, ctx.Request.URL.Path, status, duration)
			return err
		}
	}
}

// RequestIDOptions configures the RequestID middleware.
type RequestIDOptions struct {
	Header    string
	Generator func() string
}

func defaultUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// RequestID adds a unique identifier to each request via response header and context store.
func RequestID(opts ...RequestIDOptions) MiddlewareFunc {
	headerName := "X-Request-ID"
	generator := defaultUUIDv4

	if len(opts) > 0 {
		if opts[0].Header != "" {
			headerName = opts[0].Header
		}
		if opts[0].Generator != nil {
			generator = opts[0].Generator
		}
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			id := ctx.Header(headerName)
			if id == "" {
				id = generator()
			}
			ctx.Set("request_id", id)
			ctx.SetHeader(headerName, id)
			return next(ctx)
		}
	}
}

// SlogLoggerOptions configures the structured SlogLogger middleware.
type SlogLoggerOptions struct {
	Logger *slog.Logger
}

// SlogLogger logs each request using Go standard library's log/slog.
func SlogLogger(opts ...SlogLoggerOptions) MiddlewareFunc {
	logger := slog.Default()
	if len(opts) > 0 && opts[0].Logger != nil {
		logger = opts[0].Logger
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			start := time.Now()
			err := next(ctx)
			duration := time.Since(start)

			status := ctx.StatusCode()
			if err != nil && !ctx.Written() {
				status = statusFromError(err)
			}

			attrs := []any{
				slog.String("method", ctx.Request.Method),
				slog.String("path", ctx.Request.URL.Path),
				slog.Int("status", status),
				slog.Duration("duration", duration),
				slog.String("ip", ctx.ClientIP()),
			}
			if reqID := ctx.RequestID(); reqID != "" {
				attrs = append(attrs, slog.String("request_id", reqID))
			}
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
			}

			if status >= http.StatusInternalServerError {
				logger.ErrorContext(ctx.Request.Context(), "HTTP request", attrs...)
			} else if status >= http.StatusBadRequest {
				logger.WarnContext(ctx.Request.Context(), "HTTP request", attrs...)
			} else {
				logger.InfoContext(ctx.Request.Context(), "HTTP request", attrs...)
			}

			return err
		}
	}
}
