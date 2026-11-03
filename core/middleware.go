package core

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log"
	"log/slog"
	"math"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
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
// It panics if AllowCredentials is combined with the "*" wildcard origin: browsers reject
// that combination, and echoing arbitrary origins with credentials would let any site
// make authenticated requests. List the allowed origins explicitly instead.
func CORS(opts ...CORSOptions) MiddlewareFunc {
	cfg := DefaultCORSOptions()
	if len(opts) > 0 {
		cfg = opts[0]
	}
	if cfg.AllowCredentials {
		for _, o := range cfg.AllowOrigins {
			if o == "*" {
				panic("gota: CORS AllowCredentials cannot be combined with the \"*\" wildcard origin; list origins explicitly")
			}
		}
	}

	methodsStr := strings.Join(cfg.AllowMethods, ", ")
	headersStr := strings.Join(cfg.AllowHeaders, ", ")
	exposeStr := strings.Join(cfg.ExposeHeaders, ", ")
	isWildcard := len(cfg.AllowOrigins) == 1 && cfg.AllowOrigins[0] == "*"
	maxAgeStr := ""
	if cfg.MaxAge > 0 {
		maxAgeStr = strconv.Itoa(cfg.MaxAge)
	}

	mw := func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			w := ctx.ResponseWriter
			r := ctx.Request

			// The response depends on the Origin request header, so caches must key on it.
			w.Header().Add("Vary", "Origin")

			origin := r.Header.Get("Origin")
			allowed := false
			if origin != "" {
				if isWildcard {
					w.Header().Set("Access-Control-Allow-Origin", "*")
					allowed = true
				} else {
					for _, o := range cfg.AllowOrigins {
						// The opaque "null" origin (sandboxed iframes, file://, redirects) is only
						// accepted when listed explicitly, never through a "*" entry.
						if o == origin || (o == "*" && origin != "null") {
							w.Header().Set("Access-Control-Allow-Origin", origin)
							allowed = true
							break
						}
					}
				}

				// CORS headers are only sent to origins that are allowed.
				if allowed {
					if cfg.AllowCredentials {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
					if exposeStr != "" {
						w.Header().Set("Access-Control-Expose-Headers", exposeStr)
					}
				}
			}

			if r.Method == http.MethodOptions {
				if allowed || origin == "" {
					if methodsStr != "" {
						w.Header().Set("Access-Control-Allow-Methods", methodsStr)
					}
					if headersStr != "" {
						w.Header().Set("Access-Control-Allow-Headers", headersStr)
					}
					if maxAgeStr != "" {
						w.Header().Set("Access-Control-Max-Age", maxAgeStr)
					}
				}
				return ctx.NoContent()
			}

			return next(ctx)
		}
	}
	return registerMiddlewareKind(mw, "CORS")
}

// Recover catches panics occurring inside handlers and returns a 500 error.
func Recover() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			defer func() {
				if r := recover(); r != nil {
					reqID := ctx.RequestID()
					if reqID != "" {
						log.Printf("gota: panic recovered [%s]: %s\n%s", reqID, quoteLog(r), debug.Stack())
					} else {
						log.Printf("gota: panic recovered: %s\n%s", quoteLog(r), debug.Stack())
					}
					if !ctx.Written() {
						_ = ctx.Status(http.StatusInternalServerError).Json(ErrorResponse{
							Error:     "internal server error",
							Status:    http.StatusInternalServerError,
							RequestID: reqID,
						})
					}
				}
			}()
			return next(ctx)
		}
	}
}

func resolveResponseStatus(ctx *Context, err error) int {
	if err != nil && !ctx.Written() {
		// The error handler runs after this middleware, so resolve the status it will use.
		return statusFromError(err)
	}
	return ctx.StatusCode()
}

// quoteLog renders client-influenced values (request paths, error and panic messages) as a
// single quoted, escaped token so CR/LF and ANSI escapes cannot forge or hide log lines.
func quoteLog(v any) string {
	return strconv.Quote(fmt.Sprint(v))
}

// Logger logs each incoming request with method, path, status, and duration.
func Logger() MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			start := time.Now()
			err := next(ctx)
			duration := time.Since(start)
			status := resolveResponseStatus(ctx, err)
			log.Printf("gota: %s %s [%d] %s", ctx.Request.Method, quoteLog(ctx.Request.URL.Path), status, duration)
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

// validRequestID reports whether a client-supplied request ID is safe to echo into
// headers, logs and error bodies: 1-128 chars of letters, digits, '.', '_', '-' or ':'.
func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == '-', c == ':':
		default:
			return false
		}
	}
	return true
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
			if !validRequestID(id) {
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
			status := resolveResponseStatus(ctx, err)

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

// BasicAuthOptions configures the BasicAuth middleware.
type BasicAuthOptions struct {
	// Realm sets the WWW-Authenticate realm (default: "Restricted").
	Realm string
	// Validator is an optional custom function to validate username and password.
	// When nil, constant-time comparison against the provided username and password is used.
	Validator func(username, password string) bool
	// ContextKey is the context store key where the authenticated username is saved (default: "user").
	ContextKey string
	// RateLimit optionally configures an in-memory RateLimiter specifically for this BasicAuth instance
	// to protect against brute-force and credential-stuffing attacks.
	RateLimit *RateLimiterOptions
	// AllowWeakCredentials suppresses security warning logs when using common or weak passwords (e.g. for testing).
	AllowWeakCredentials bool
}

// BasicAuthWarningLogger, if set, redirects BasicAuth security warning messages.
// When nil, log.Printf is used.
var BasicAuthWarningLogger func(warning string)

var commonWeakPasswords = map[string]bool{
	"admin":    true,
	"admin123": true,
	"123456":   true,
	"12345678": true,
	"password": true,
	"secret":   true,
	"root":     true,
	"guest":    true,
	"qwerty":   true,
	"pass":     true,
	"test":     true,
	"1234":     true,
}

// IsWeakPassword reports whether a password is short or matches commonly known default credentials.
func IsWeakPassword(password string) bool {
	p := strings.ToLower(strings.TrimSpace(password))
	return len(p) < 4 || commonWeakPasswords[p]
}

func emitBasicAuthWarning(msg string) {
	formatted := fmt.Sprintf("⚠️  gota [SECURITY]: %s", msg)
	if BasicAuthWarningLogger != nil {
		BasicAuthWarningLogger(formatted)
	} else {
		log.Printf("%s", formatted)
	}
}

// BasicAuth returns an HTTP Basic Authentication middleware using constant-time credential comparison.
// Automatically documents the "BasicAuth" security scheme in OpenAPI 3.1 when applied to routes or groups.
func BasicAuth(username, password string, opts ...BasicAuthOptions) MiddlewareFunc {
	realm := "Restricted"
	contextKey := "user"
	var validator func(u, p string) bool
	var rateLimiter MiddlewareFunc
	allowWeak := false

	if len(opts) > 0 {
		if opts[0].Realm != "" {
			realm = opts[0].Realm
		}
		if opts[0].ContextKey != "" {
			contextKey = opts[0].ContextKey
		}
		validator = opts[0].Validator
		if opts[0].RateLimit != nil {
			rateLimiter = RateLimiter(*opts[0].RateLimit)
		}
		allowWeak = opts[0].AllowWeakCredentials
	}

	if !allowWeak {
		if password == "" || username == "" {
			emitBasicAuthWarning("BasicAuth initialized with empty username or password! Any client may authenticate without credentials.")
		} else if strings.EqualFold(username, password) || IsWeakPassword(password) {
			if isProductionEnv() {
				emitBasicAuthWarning(fmt.Sprintf("CRITICAL: BasicAuth configured with weak or default password %q in production! Use a strong secret from os.Getenv(...) or set AllowWeakCredentials: true.", password))
			} else {
				emitBasicAuthWarning(fmt.Sprintf("BasicAuth configured with known weak/default password %q. For production security, use a strong password from os.Getenv(...) or set BasicAuthOptions.AllowWeakCredentials.", password))
			}
		}
	}

	expectedUserHash := sha256.Sum256([]byte(username))
	expectedPassHash := sha256.Sum256([]byte(password))
	wwwAuth := fmt.Sprintf("Basic realm=%q", realm)

	mw := func(next HandlerFunc) HandlerFunc {
		authHandler := func(ctx *Context) error {
			u, p, ok := ctx.Request.BasicAuth()
			if ok {
				var valid bool
				if validator != nil {
					valid = validator(u, p)
				} else {
					uHash := sha256.Sum256([]byte(u))
					pHash := sha256.Sum256([]byte(p))
					userMatch := subtle.ConstantTimeCompare(uHash[:], expectedUserHash[:]) == 1
					passMatch := subtle.ConstantTimeCompare(pHash[:], expectedPassHash[:]) == 1
					valid = userMatch && passMatch
				}
				if valid {
					ctx.Set(contextKey, u)
					return next(ctx)
				}
			}

			ctx.SetHeader("WWW-Authenticate", wwwAuth)
			return ctx.Status(http.StatusUnauthorized).Json(ErrorResponse{
				Error:     "Unauthorized",
				Status:    http.StatusUnauthorized,
				RequestID: ctx.RequestID(),
			})
		}

		if rateLimiter != nil {
			return rateLimiter(authHandler)
		}
		return authHandler
	}
	return registerMiddlewareSecurity(mw, "BasicAuth")
}

// normalizeIPKey groups IPv6 addresses by their /64 prefix (a single host or subscriber
// typically controls a whole /64, so per-address keys would be trivial to rotate).
func normalizeIPKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// RateLimiterOptions configures the in-memory RateLimiter middleware.
type RateLimiterOptions struct {
	// Max is the maximum number of requests allowed per Window (default: 60).
	Max int
	// Window is the time window for the rate limit (default: 1 minute).
	Window time.Duration
	// KeyFunc extracts the rate-limiting key from the request (default: ctx.ClientIP()).
	KeyFunc func(ctx *Context) string
	// MaxKeys caps the number of tracked keys to bound memory usage (default: 10000).
	// When the cap is reached, fully refilled (idle) buckets are dropped first, then the
	// least recently used one, so an attacker rotating keys cannot grow memory unboundedly.
	MaxKeys int
}

// DefaultRateLimiterMaxKeys is the default cap of tracked keys in RateLimiter.
const DefaultRateLimiterMaxKeys = 10000

type rateBucket struct {
	tokens     float64
	lastRefill time.Time
}

// RateLimiter returns a zero-dependency in-memory Token Bucket rate limiter middleware.
// When a client exceeds Max requests per Window, it returns HTTP 429 Too Many Requests with Retry-After.
func RateLimiter(opts ...RateLimiterOptions) MiddlewareFunc {
	maxReqs := 60
	window := time.Minute
	keyFunc := func(ctx *Context) string { return normalizeIPKey(ctx.ClientIP()) }
	maxKeys := DefaultRateLimiterMaxKeys

	if len(opts) > 0 {
		if opts[0].Max > 0 {
			maxReqs = opts[0].Max
		}
		if opts[0].Window > 0 {
			window = opts[0].Window
		}
		if opts[0].KeyFunc != nil {
			keyFunc = opts[0].KeyFunc
		}
		if opts[0].MaxKeys > 0 {
			maxKeys = opts[0].MaxKeys
		}
	}

	maxTokens := float64(maxReqs)
	refillRate := maxTokens / window.Seconds()
	limitStr := strconv.Itoa(maxReqs)

	var (
		mu          sync.Mutex
		buckets     = make(map[string]*rateBucket)
		lastCleanup = time.Now()
	)

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			key := keyFunc(ctx)
			now := time.Now()

			mu.Lock()
			// Periodic lazy eviction of idle buckets. A bucket idle for a full window is
			// completely refilled, so dropping it is equivalent to never having seen the key.
			if now.Sub(lastCleanup) > window {
				for k, b := range buckets {
					if now.Sub(b.lastRefill) >= window {
						delete(buckets, k)
					}
				}
				lastCleanup = now
			}

			b, exists := buckets[key]
			if !exists {
				if len(buckets) >= maxKeys {
					evictRateBuckets(buckets, now, window, maxKeys)
				}
				b = &rateBucket{tokens: maxTokens, lastRefill: now}
				buckets[key] = b
			} else {
				elapsed := now.Sub(b.lastRefill).Seconds()
				if elapsed > 0 {
					b.tokens = math.Min(maxTokens, b.tokens+elapsed*refillRate)
					b.lastRefill = now
				}
			}

			if b.tokens < 1.0 {
				retryAfter := int(math.Ceil((1.0 - b.tokens) / refillRate))
				if retryAfter < 1 {
					retryAfter = 1
				}
				mu.Unlock()

				ctx.SetHeader("X-RateLimit-Limit", limitStr)
				ctx.SetHeader("X-RateLimit-Remaining", "0")
				ctx.SetHeader("Retry-After", strconv.Itoa(retryAfter))
				return ctx.Status(http.StatusTooManyRequests).Json(ErrorResponse{
					Error:     "too many requests",
					Status:    http.StatusTooManyRequests,
					RequestID: ctx.RequestID(),
				})
			}

			b.tokens -= 1.0
			remaining := int(b.tokens)
			mu.Unlock()

			ctx.SetHeader("X-RateLimit-Limit", limitStr)
			ctx.SetHeader("X-RateLimit-Remaining", strconv.Itoa(remaining))
			return next(ctx)
		}
	}
}

// evictRateBuckets makes room for one new key: it drops fully refilled buckets and, if the
// map is still at capacity, the least recently used bucket.
func evictRateBuckets(buckets map[string]*rateBucket, now time.Time, window time.Duration, maxKeys int) {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, b := range buckets {
		if now.Sub(b.lastRefill) >= window {
			delete(buckets, k)
			continue
		}
		if first || b.lastRefill.Before(oldest) {
			oldestKey, oldest, first = k, b.lastRefill, false
		}
	}
	if len(buckets) >= maxKeys && !first {
		delete(buckets, oldestKey)
	}
}
