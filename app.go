package gota

import (
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
)

// ServerConfig defines metadata for OpenAPI server endpoints.
type ServerConfig struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Config encapsulates settings for the Gota application, OpenAPI, and Scalar UI.
type Config struct {
	Title        string         `json:"title,omitempty"`
	Version      string         `json:"version,omitempty"`
	Description  string         `json:"description,omitempty"`
	DocsURL      string         `json:"docsUrl,omitempty"`
	OpenAPIURL   string         `json:"openApiUrl,omitempty"`
	ScalarTheme  ScalarTheme    `json:"scalarTheme,omitempty"`
	ScalarLayout string         `json:"scalarLayout,omitempty"` // Default: "classic"
	DisableDocs  bool           `json:"disableDocs,omitempty"`
	Servers      []ServerConfig `json:"servers,omitempty"`
}

// App represents the core engine of Gota, managing routing, middlewares, and docs.
type App struct {
	config      Config
	router      *Router
	middlewares []MiddlewareFunc

	NotFoundHandler HandlerFunc
	ErrorHandler    func(ctx *Context, err error)
}

// New creates and configures a new Gota application instance.
func New(configs ...Config) *App {
	var cfg Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
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
		cfg.ScalarTheme = ThemeLaserwave
	}
	if cfg.ScalarLayout == "" {
		cfg.ScalarLayout = "classic" // Default: classic layout (Scalar/Swagger style)
	}

	app := &App{
		config: cfg,
		router: newRouter(),
	}
	app.NotFoundHandler = defaultNotFoundHandler
	app.ErrorHandler = defaultErrorHandler

	if !cfg.DisableDocs {
		app.registerDocs()
	}

	return app
}

func defaultNotFoundHandler(ctx *Context) error {
	return ctx.Status(http.StatusNotFound).Json(ErrorResponse{
		Error:  fmt.Sprintf("route not found: %s %s", ctx.Request.Method, ctx.Request.URL.Path),
		Status: http.StatusNotFound,
	})
}

func defaultErrorHandler(ctx *Context, err error) {
	if httpErr, ok := err.(*HTTPError); ok {
		_ = ctx.Status(httpErr.Status).Json(ErrorResponse{
			Error:  httpErr.Message,
			Status: httpErr.Status,
		})
		return
	}
	if valErr, ok := err.(*ValidationError); ok {
		_ = ctx.Status(valErr.Status).Json(valErr)
		return
	}

	log.Printf("gota: handler error %s %s: %v", ctx.Request.Method, ctx.Request.URL.Path, err)
	_ = ctx.Status(http.StatusInternalServerError).Json(ErrorResponse{
		Error:  "internal server error",
		Status: http.StatusInternalServerError,
	})
}

// Use appends a global middleware executed across all requests.
func (a *App) Use(mw MiddlewareFunc) {
	a.middlewares = append(a.middlewares, mw)
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
	return a.register(http.MethodGet, path, h)
}

// Post registers an HTTP POST route.
func (a *App) Post(path string, h HandlerFunc) *Route {
	return a.register(http.MethodPost, path, h)
}

// Put registers an HTTP PUT route.
func (a *App) Put(path string, h HandlerFunc) *Route {
	return a.register(http.MethodPut, path, h)
}

// Delete registers an HTTP DELETE route.
func (a *App) Delete(path string, h HandlerFunc) *Route {
	return a.register(http.MethodDelete, path, h)
}

// Patch registers an HTTP PATCH route.
func (a *App) Patch(path string, h HandlerFunc) *Route {
	return a.register(http.MethodPatch, path, h)
}

func (a *App) register(method, path string, h HandlerFunc) *Route {
	base := make([]MiddlewareFunc, len(a.middlewares))
	copy(base, a.middlewares)

	n := a.router.Add(method, path, chain(h, base))
	n.rawHandler = h

	registerAutoOptions(a.router, method, path, base)
	return &Route{node: n, router: a.router, base: base}
}

func registerAutoOptions(router *Router, method, path string, base []MiddlewareFunc) {
	if method == http.MethodOptions {
		return
	}
	optionsHandler := func(ctx *Context) error {
		return ctx.NoContent()
	}
	router.Add(http.MethodOptions, path, chain(optionsHandler, base))
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
	a.Get(openAPIURL, func(ctx *Context) error {
		gen := NewOpenAPIGenerator(a.config)
		spec := gen.Generate(a.router.Routes())
		return ctx.Json(spec)
	}).Hidden()

	// Scalar API Reference UI (Layout: classic by default)
	a.Get(docsURL, func(ctx *Context) error {
		html, err := RenderScalarHTML(ScalarConfig{
			Title:   a.config.Title + " - API Reference",
			SpecURL: openAPIURL,
			Theme:   a.config.ScalarTheme,
			Layout:  a.config.ScalarLayout,
		})
		if err != nil {
			return ctx.Status(http.StatusInternalServerError).Send("Failed to render documentation")
		}
		ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
		return ctx.Send(html)
	}).Hidden()
}

// ServeHTTP satisfies the standard net/http Handler interface.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("gota: recovered panic in %s %s: %v\n%s", r.Method, r.URL.Path, err, debug.Stack())
			http.Error(w, `{"error":"internal server error","status":500}`, http.StatusInternalServerError)
		}
	}()

	handler, params, ok := a.router.Match(r.Method, r.URL.Path)
	ctx := newContext(w, r, params)

	if !ok {
		_ = a.NotFoundHandler(ctx)
		return
	}

	if err := handler(ctx); err != nil {
		a.ErrorHandler(ctx, err)
	}
}

// Listen starts an HTTP server listening on the specified address.
func (a *App) Listen(addr string) error {
	log.Printf("💧 Gota server listening on %s", addr)
	if !a.config.DisableDocs {
		log.Printf("📖 Scalar docs available at http://localhost%s%s (classic layout)", addr, a.config.DocsURL)
	}
	return http.ListenAndServe(addr, a)
}

// Route represents a registered endpoint, allowing fluent OpenAPI and middleware chaining.
type Route struct {
	node   *node
	router *Router
	base   []MiddlewareFunc
}

// Middleware appends route-specific middlewares executed before the handler.
func (rt *Route) Middleware(mw ...MiddlewareFunc) *Route {
	rt.node.middlewares = append(rt.node.middlewares, mw...)

	full := make([]MiddlewareFunc, 0, len(rt.base)+len(rt.node.middlewares))
	full = append(full, rt.base...)
	full = append(full, rt.node.middlewares...)

	rt.node.handler = chain(rt.node.rawHandler, full)
	return rt
}

// Name assigns a name to this route for reverse URL lookup.
func (rt *Route) Name(name string) *Route {
	rt.node.name = name
	rt.router.setName(name, rt.node)
	return rt
}

func (rt *Route) ensureDoc() *RouteDoc {
	if rt.node.doc == nil {
		rt.node.doc = &RouteDoc{
			Responses: make(map[int]*ResponseDoc),
		}
	}
	if rt.node.doc.Responses == nil {
		rt.node.doc.Responses = make(map[int]*ResponseDoc)
	}
	return rt.node.doc
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

// Status specifies the default HTTP status code when returning a response.
func (rt *Route) Status(code int) *Route {
	rt.Response(code, nil)
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
}

// Use appends a middleware to this route group.
func (g *RouteGroup) Use(mw MiddlewareFunc) {
	g.middlewares = append(g.middlewares, mw)
}

// Get registers a scoped HTTP GET route.
func (g *RouteGroup) Get(path string, h HandlerFunc) *Route {
	return g.register(http.MethodGet, path, h)
}

// Post registers a scoped HTTP POST route.
func (g *RouteGroup) Post(path string, h HandlerFunc) *Route {
	return g.register(http.MethodPost, path, h)
}

// Put registers a scoped HTTP PUT route.
func (g *RouteGroup) Put(path string, h HandlerFunc) *Route {
	return g.register(http.MethodPut, path, h)
}

// Delete registers a scoped HTTP DELETE route.
func (g *RouteGroup) Delete(path string, h HandlerFunc) *Route {
	return g.register(http.MethodDelete, path, h)
}

// Patch registers a scoped HTTP PATCH route.
func (g *RouteGroup) Patch(path string, h HandlerFunc) *Route {
	return g.register(http.MethodPatch, path, h)
}

func (g *RouteGroup) register(method, path string, h HandlerFunc) *Route {
	full := joinPrefix(g.prefix, path)
	base := make([]MiddlewareFunc, len(g.middlewares))
	copy(base, g.middlewares)

	n := g.app.router.Add(method, full, chain(h, base))
	n.rawHandler = h

	registerAutoOptions(g.app.router, method, full, base)
	return &Route{node: n, router: g.app.router, base: base}
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
