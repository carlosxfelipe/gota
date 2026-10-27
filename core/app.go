package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ServerConfig defines metadata for OpenAPI server endpoints.
type ServerConfig struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Config encapsulates settings for the Gota application, OpenAPI, and Scalar UI.
type Config struct {
	Title           string         `json:"title,omitempty"`
	Version         string         `json:"version,omitempty"`
	Description     string         `json:"description,omitempty"`
	DocsURL         string         `json:"docsUrl,omitempty"`
	OpenAPIURL      string         `json:"openApiUrl,omitempty"`
	ScalarTheme     ScalarTheme    `json:"scalarTheme,omitempty"`
	ScalarLayout    string         `json:"scalarLayout,omitempty"` // Default: "classic"
	ScalarScriptURL string         `json:"scalarScriptUrl,omitempty"`
	ScalarSRI       string         `json:"scalarSri,omitempty"`
	DisableDocs     bool           `json:"disableDocs,omitempty"`
	Servers         []ServerConfig `json:"servers,omitempty"`

	// MaxBodyBytes limits the size of request bodies. Default: 10 MiB.
	// Use a negative value to disable the limit.
	MaxBodyBytes int64 `json:"maxBodyBytes,omitempty"`

	// TrustProxyHeaders makes Context.ClientIP honor X-Forwarded-For and X-Real-IP.
	// Enable it only when the app runs behind a trusted reverse proxy that sets them.
	TrustProxyHeaders bool `json:"trustProxyHeaders,omitempty"`

	// ReadTimeout limits the duration for reading the entire request. Default: 30s.
	// Use negative value to disable.
	ReadTimeout time.Duration `json:"readTimeout,omitempty"`

	// WriteTimeout limits the duration for writing the response. Default: 30s.
	// Use negative value to disable.
	WriteTimeout time.Duration `json:"writeTimeout,omitempty"`

	// IdleTimeout is the maximum duration to wait for the next request when keep-alive is enabled. Default: 120s.
	// Use negative value to disable.
	IdleTimeout time.Duration `json:"idleTimeout,omitempty"`
}

// Default constants for server configuration.
const (
	DefaultMaxBodyBytes      int64         = 10 << 20
	DefaultReadHeaderTimeout time.Duration = 10 * time.Second
	DefaultReadTimeout       time.Duration = 30 * time.Second
	DefaultWriteTimeout      time.Duration = 30 * time.Second
	DefaultIdleTimeout       time.Duration = 120 * time.Second
)

// App represents the core engine of Gota, managing routing, middlewares, and docs.
type App struct {
	config      Config
	router      *Router
	middlewares []MiddlewareFunc

	NotFoundHandler HandlerFunc
	ErrorHandler    func(ctx *Context, err error)

	mu     sync.Mutex
	server *http.Server

	// routeCount counts user-registered routes (docs routes excluded) and docsRoutes
	// holds the built-in documentation routes, which must follow global middlewares.
	routeCount int
	docsRoutes []*Route
}

// New creates and configures a new Gota application instance.
func New(cfg Config) *App {
	if cfg.Title == "" {
		cfg.Title = "Gota API"
	}
	if cfg.Version == "" {
		cfg.Version = "1.0.0"
	}
	if cfg.DocsURL == "" {
		cfg.DocsURL = "/docs"
	}
	if cfg.OpenAPIURL == "" {
		cfg.OpenAPIURL = "/openapi.json"
	}
	if cfg.ScalarTheme == "" {
		cfg.ScalarTheme = ThemeFastify
	}
	if cfg.ScalarLayout == "" {
		cfg.ScalarLayout = "classic"
	}
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = DefaultReadTimeout
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = DefaultWriteTimeout
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}

	app := &App{
		config: cfg,
		router: NewRouter(),
	}
	app.NotFoundHandler = defaultNotFoundHandler
	app.ErrorHandler = defaultErrorHandler

	if !cfg.DisableDocs {
		app.registerDocs()
		app.routeCount = 0
	}

	return app
}

func defaultNotFoundHandler(ctx *Context) error {
	return ctx.Status(http.StatusNotFound).Json(ErrorResponse{
		Error:     fmt.Sprintf("route not found: %s %s", ctx.Request.Method, ctx.Request.URL.Path),
		Status:    http.StatusNotFound,
		RequestID: ctx.RequestID(),
	})
}

// statusError is implemented by errors that carry their own HTTP status and
// are rendered as the JSON body themselves (e.g. validation.ValidationError).
type statusError interface {
	HTTPStatus() int
}

// statusFromError resolves the HTTP status the default error handler will use.
func statusFromError(err error) int {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status
	}
	var se statusError
	if errors.As(err, &se) {
		return se.HTTPStatus()
	}
	return http.StatusInternalServerError
}

func defaultErrorHandler(ctx *Context, err error) {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		_ = ctx.Status(httpErr.Status).Json(ErrorResponse{
			Error:     httpErr.Message,
			Status:    httpErr.Status,
			RequestID: ctx.RequestID(),
		})
		return
	}

	var se statusError
	if errors.As(err, &se) {
		_ = ctx.Status(se.HTTPStatus()).Json(se)
		return
	}

	reqID := ctx.RequestID()
	if reqID != "" {
		log.Printf("gota: handler error %s %s [%s]: %v", ctx.Request.Method, ctx.Request.URL.Path, reqID, err)
	} else {
		log.Printf("gota: handler error %s %s: %v", ctx.Request.Method, ctx.Request.URL.Path, err)
	}
	_ = ctx.Status(http.StatusInternalServerError).Json(ErrorResponse{
		Error:     "internal server error",
		Status:    http.StatusInternalServerError,
		RequestID: reqID,
	})
}

// Use appends a global middleware executed across all requests.
// It must be called before registering routes or groups: middlewares are bound to a route
// when it is registered, so a late Use would silently leave existing routes unprotected.
// Use panics in that case.
func (a *App) Use(mw MiddlewareFunc) {
	if a.routeCount > 0 {
		panic("gota: App.Use must be called before registering routes; middleware would not apply to existing routes")
	}
	a.middlewares = append(a.middlewares, mw)

	// Built-in docs routes are registered in New, so refresh their chain.
	for _, rt := range a.docsRoutes {
		rt.base = append([]MiddlewareFunc(nil), a.middlewares...)
		rt.rebuild()
	}
}

// Router returns the underlying Router instance.
func (a *App) Router() *Router {
	return a.router
}

// Config returns the application's configuration.
func (a *App) Config() Config {
	return a.config
}

// Get registers an HTTP GET route.
func (a *App) Get(path string, h HandlerFunc) *Route {
	return a.RegisterRoute(http.MethodGet, path, h)
}

// Post registers an HTTP POST route.
func (a *App) Post(path string, h HandlerFunc) *Route {
	return a.RegisterRoute(http.MethodPost, path, h)
}

// Put registers an HTTP PUT route.
func (a *App) Put(path string, h HandlerFunc) *Route {
	return a.RegisterRoute(http.MethodPut, path, h)
}

// Delete registers an HTTP DELETE route.
func (a *App) Delete(path string, h HandlerFunc) *Route {
	return a.RegisterRoute(http.MethodDelete, path, h)
}

// Patch registers an HTTP PATCH route.
func (a *App) Patch(path string, h HandlerFunc) *Route {
	return a.RegisterRoute(http.MethodPatch, path, h)
}

// RegisterRoute internally binds an HTTP method and path with the global middleware chain.
func (a *App) RegisterRoute(method, path string, h HandlerFunc) *Route {
	a.routeCount++
	base := make([]MiddlewareFunc, len(a.middlewares))
	copy(base, a.middlewares)

	n := a.router.Add(method, path, Chain(h, base))
	n.RawHandler = h

	registerAutoOptions(a.router, method, path, base)
	return &Route{Node: n, router: a.router, base: base}
}

func registerAutoOptions(router *Router, method, path string, base []MiddlewareFunc) {
	if method == http.MethodOptions {
		return
	}
	optionsHandler := func(ctx *Context) error {
		return ctx.NoContent()
	}
	router.addAuto(http.MethodOptions, path, Chain(optionsHandler, base))
}

// URLFor generates a URL for a previously named route.
func (a *App) URLFor(name string, params map[string]string) (string, error) {
	return a.router.URLFor(name, params)
}

// Group initializes a logical route grouping under a common prefix.
func (a *App) Group(prefix string) *RouteGroup {
	inherited := make([]MiddlewareFunc, len(a.middlewares))
	copy(inherited, a.middlewares)
	return &RouteGroup{app: a, prefix: prefix, middlewares: inherited}
}

func (a *App) registerDocs() {
	openAPIURL := a.config.OpenAPIURL
	docsURL := a.config.DocsURL

	// OpenAPI 3.1 JSON spec
	specRoute := a.Get(openAPIURL, func(ctx *Context) error {
		gen := NewOpenAPIGenerator(a.config)
		spec := gen.Generate(a.router.Routes())
		return ctx.Json(spec)
	}).Hidden()

	// Scalar API Reference UI (Layout: classic by default)
	docsRoute := a.Get(docsURL, func(ctx *Context) error {
		html, err := RenderScalarHTML(ScalarConfig{
			Title:     a.config.Title + " - API Reference",
			SpecURL:   openAPIURL,
			Theme:     a.config.ScalarTheme,
			Layout:    a.config.ScalarLayout,
			ScriptURL: a.config.ScalarScriptURL,
			SRI:       a.config.ScalarSRI,
		})
		if err != nil {
			return ctx.Status(http.StatusInternalServerError).Send("Failed to render documentation")
		}
		ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
		return ctx.Send(html)
	}).Hidden()

	a.docsRoutes = []*Route{specRoute, docsRoute}
}

// ServeHTTP satisfies the standard net/http Handler interface.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			reqID := w.Header().Get("X-Request-ID")
			if reqID == "" {
				reqID = r.Header.Get("X-Request-ID")
			}
			if reqID != "" {
				log.Printf("gota: recovered panic in %s %s [%s]: %v\n%s", r.Method, r.URL.Path, reqID, err, debug.Stack())
			} else {
				log.Printf("gota: recovered panic in %s %s: %v\n%s", r.Method, r.URL.Path, err, debug.Stack())
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			if reqID != "" {
				_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"internal server error","status":500,"requestId":%q}`+"\n", reqID)))
			} else {
				_, _ = w.Write([]byte(`{"error":"internal server error","status":500}` + "\n"))
			}
		}
	}()

	if a.config.MaxBodyBytes > 0 && r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, a.config.MaxBodyBytes)
	}

	handler, params, ok := a.router.Match(r.Method, r.URL.Path)
	ctx := NewContext(w, r, params)
	ctx.trustProxy = a.config.TrustProxyHeaders

	if !ok {
		// A path that exists for other methods is 405, otherwise 404.
		fallback := a.NotFoundHandler
		if allowed := a.router.Allowed(r.URL.Path); len(allowed) > 0 {
			fallback = func(ctx *Context) error {
				ctx.SetHeader("Allow", strings.Join(allowed, ", "))
				return ctx.Status(http.StatusMethodNotAllowed).Json(ErrorResponse{
					Error:     fmt.Sprintf("method not allowed: %s %s", ctx.Request.Method, ctx.Request.URL.Path),
					Status:    http.StatusMethodNotAllowed,
					RequestID: ctx.RequestID(),
				})
			}
		}

		// Run global middlewares (CORS, Logger, ...) for unmatched routes too.
		if err := Chain(fallback, a.middlewares)(ctx); err != nil {
			a.ErrorHandler(ctx, err)
		}
		return
	}

	if err := handler(ctx); err != nil {
		a.ErrorHandler(ctx, err)
	}
}

// displayAddr normalizes a listen address into the address to bind and a
// host:port that can be opened in a browser. Accepts ":3000", "3000" or "host:3000".
func displayAddr(addr string) (listen string, display string) {
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return addr, net.JoinHostPort(host, port)
}

// Server returns the underlying *http.Server instance, if created by Listen.
func (a *App) Server() *http.Server {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.server
}

// Listen starts an HTTP server listening on the specified address.
func (a *App) Listen(addr string) error {
	listen, display := displayAddr(addr)
	log.Printf("💧 Gota server listening on http://%s", display)
	if !a.config.DisableDocs {
		log.Printf("📖 Scalar docs: http://%s%s", display, a.config.DocsURL)
		log.Printf("📄 OpenAPI spec: http://%s%s", display, a.config.OpenAPIURL)
	}

	readTimeout := a.config.ReadTimeout
	if readTimeout < 0 {
		readTimeout = 0
	}
	writeTimeout := a.config.WriteTimeout
	if writeTimeout < 0 {
		writeTimeout = 0
	}
	idleTimeout := a.config.IdleTimeout
	if idleTimeout < 0 {
		idleTimeout = 0
	}

	srv := &http.Server{
		Addr:              listen,
		Handler:           a,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	a.mu.Lock()
	a.server = srv
	a.mu.Unlock()

	return srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server without interrupting active connections.
func (a *App) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	srv := a.server
	a.mu.Unlock()

	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// ListenGraceful starts the server and automatically listens for shutdown signals
// (os.Interrupt, syscall.SIGTERM by default) to perform a graceful shutdown with a 10s timeout.
func (a *App) ListenGraceful(addr string, stopSignals ...os.Signal) error {
	if len(stopSignals) == 0 {
		stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
	}

	ctx, stop := signal.NotifyContext(context.Background(), stopSignals...)
	defer stop()

	errChan := make(chan error, 1)
	go func() {
		if err := a.Listen(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
		close(errChan)
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		log.Println("💧 Gota: shutdown signal received, shutting down gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return a.Shutdown(shutdownCtx)
	}
}

// Route represents a registered endpoint, allowing fluent OpenAPI and middleware chaining.
type Route struct {
	Node   *Node
	router *Router
	base   []MiddlewareFunc
}

// Middleware appends route-specific middlewares executed before the handler.
func (rt *Route) Middleware(mw ...MiddlewareFunc) *Route {
	rt.Node.Middlewares = append(rt.Node.Middlewares, mw...)
	rt.rebuild()
	return rt
}

// rebuild recomputes the full middleware chain around the raw handler.
func (rt *Route) rebuild() {
	full := make([]MiddlewareFunc, 0, len(rt.base)+len(rt.Node.Middlewares))
	full = append(full, rt.base...)
	full = append(full, rt.Node.Middlewares...)

	rt.Node.Handler = Chain(rt.Node.RawHandler, full)
}

// Name assigns a name to this route for reverse URL lookup.
func (rt *Route) Name(name string) *Route {
	rt.Node.Name = name
	rt.router.SetName(name, rt.Node)
	return rt
}

func (rt *Route) ensureDoc() *RouteDoc {
	if rt.Node.Doc == nil {
		rt.Node.Doc = &RouteDoc{
			Responses: make(map[int]*ResponseDoc),
		}
	}
	if rt.Node.Doc.Responses == nil {
		rt.Node.Doc.Responses = make(map[int]*ResponseDoc)
	}
	return rt.Node.Doc
}

// Summary sets a short summary for the OpenAPI operation.
func (rt *Route) Summary(summary string) *Route {
	rt.ensureDoc().Summary = summary
	return rt
}

// Description sets a detailed description for the OpenAPI operation.
func (rt *Route) Description(desc string) *Route {
	rt.ensureDoc().Description = desc
	return rt
}

// Tags categorizes the route into tags in OpenAPI and Scalar.
func (rt *Route) Tags(tags ...string) *Route {
	rt.ensureDoc().Tags = append(rt.ensureDoc().Tags, tags...)
	return rt
}

// Deprecated marks the route as deprecated in documentation.
func (rt *Route) Deprecated() *Route {
	rt.ensureDoc().Deprecated = true
	return rt
}

// Hidden hides the route from OpenAPI and Scalar documentation.
func (rt *Route) Hidden() *Route {
	rt.ensureDoc().Hidden = true
	return rt
}

// Body defines the request body model for documentation.
func (rt *Route) Body(model any, description ...string) *Route {
	desc := ""
	if len(description) > 0 {
		desc = description[0]
	}
	rt.ensureDoc().RequestBody = &BodyDoc{
		Model:       model,
		Description: desc,
		Required:    true,
	}
	return rt
}

// Query documents a query parameter for the route.
func (rt *Route) Query(name string, model any, description ...string) *Route {
	desc := ""
	if len(description) > 0 {
		desc = description[0]
	}
	rt.ensureDoc().Parameters = append(rt.ensureDoc().Parameters, ParamDoc{
		In:          "query",
		Name:        name,
		Model:       model,
		Description: desc,
	})
	return rt
}

// Header documents an expected request header.
func (rt *Route) Header(name string, model any, description ...string) *Route {
	desc := ""
	if len(description) > 0 {
		desc = description[0]
	}
	rt.ensureDoc().Parameters = append(rt.ensureDoc().Parameters, ParamDoc{
		In:          "header",
		Name:        name,
		Model:       model,
		Description: desc,
	})
	return rt
}

// Response documents an expected response status code and data model.
func (rt *Route) Response(statusCode int, model any, description ...string) *Route {
	desc := ""
	if len(description) > 0 {
		desc = description[0]
	}
	rt.ensureDoc().Responses[statusCode] = &ResponseDoc{
		Model:       model,
		Description: desc,
	}
	return rt
}

// Status specifies the HTTP status code of the response and moves the
// documented success response to that status.
func (rt *Route) Status(code int) *Route {
	prev := rt.Node.RawHandler
	rt.Node.RawHandler = func(ctx *Context) error {
		ctx.Status(code)
		return prev(ctx)
	}
	rt.rebuild()

	doc := rt.ensureDoc()
	if _, exists := doc.Responses[code]; exists {
		return rt
	}
	for status, resp := range doc.Responses {
		if status >= 200 && status < 300 && status != http.StatusNoContent {
			delete(doc.Responses, status)
			doc.Responses[code] = resp
			if resp.Description == http.StatusText(status) {
				resp.Description = http.StatusText(code)
			}
			return rt
		}
	}
	doc.Responses[code] = &ResponseDoc{Description: http.StatusText(code)}
	return rt
}

// Doc sets or replaces route documentation metadata.
func (rt *Route) Doc(doc RouteDoc) *Route {
	current := rt.ensureDoc()
	*current = doc
	return rt
}

// RouteGroup allows grouping routes under a common path prefix and middleware chain.
type RouteGroup struct {
	app         *App
	prefix      string
	middlewares []MiddlewareFunc
	registered  bool
}

// Use appends a middleware to this route group.
// It must be called before registering routes on the group; otherwise it panics, because
// already registered routes would silently not receive the middleware.
func (g *RouteGroup) Use(mw MiddlewareFunc) {
	if g.registered {
		panic("gota: RouteGroup.Use must be called before registering routes on the group; middleware would not apply to existing routes")
	}
	g.middlewares = append(g.middlewares, mw)
}

// Get registers a scoped HTTP GET route.
func (g *RouteGroup) Get(path string, h HandlerFunc) *Route {
	return g.RegisterRoute(http.MethodGet, path, h)
}

// Post registers a scoped HTTP POST route.
func (g *RouteGroup) Post(path string, h HandlerFunc) *Route {
	return g.RegisterRoute(http.MethodPost, path, h)
}

// Put registers a scoped HTTP PUT route.
func (g *RouteGroup) Put(path string, h HandlerFunc) *Route {
	return g.RegisterRoute(http.MethodPut, path, h)
}

// Delete registers a scoped HTTP DELETE route.
func (g *RouteGroup) Delete(path string, h HandlerFunc) *Route {
	return g.RegisterRoute(http.MethodDelete, path, h)
}

// Patch registers a scoped HTTP PATCH route.
func (g *RouteGroup) Patch(path string, h HandlerFunc) *Route {
	return g.RegisterRoute(http.MethodPatch, path, h)
}

// RegisterRoute registers a scoped route under this group prefix.
func (g *RouteGroup) RegisterRoute(method, path string, h HandlerFunc) *Route {
	full := joinPrefix(g.prefix, path)
	g.registered = true
	g.app.routeCount++
	base := make([]MiddlewareFunc, len(g.middlewares))
	copy(base, g.middlewares)

	n := g.app.router.Add(method, full, Chain(h, base))
	n.RawHandler = h

	registerAutoOptions(g.app.router, method, full, base)
	return &Route{Node: n, router: g.app.router, base: base}
}

// Group nests a new RouteGroup under this group's prefix.
func (g *RouteGroup) Group(prefix string) *RouteGroup {
	fullPrefix := joinPrefix(g.prefix, prefix)
	inherited := make([]MiddlewareFunc, len(g.middlewares))
	copy(inherited, g.middlewares)
	return &RouteGroup{app: g.app, prefix: fullPrefix, middlewares: inherited}
}

func joinPrefix(prefix, path string) string {
	if prefix == "" {
		return path
	}
	if path == "/" {
		return prefix
	}
	return prefix + path
}
