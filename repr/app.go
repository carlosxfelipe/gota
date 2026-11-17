package repr

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/carlosxfelipe/gota/scalar"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const (
	DefaultMaxHeaderBytes    int           = 1 << 20
	DefaultReadHeaderTimeout time.Duration = 10 * time.Second
	DefaultReadTimeout       time.Duration = 30 * time.Second
	DefaultWriteTimeout      time.Duration = 30 * time.Second
	DefaultIdleTimeout       time.Duration = 120 * time.Second
)

// Config configures the gota application, OpenAPI specification, and Scalar docs.
type Config struct {
	Title           string                           `json:"title,omitempty"`
	Version         string                           `json:"version,omitempty"`
	Description     string                           `json:"description,omitempty"`
	DocsURL         string                           `json:"docsUrl,omitempty"`
	OpenAPIURL      string                           `json:"openApiUrl,omitempty"`
	DisableDocs     bool                             `json:"disableDocs,omitempty"`
	Scalar          scalar.Config                    `json:"scalar,omitempty"`
	Servers         []OpenAPIServer                  `json:"servers,omitempty"`
	SecuritySchemes map[string]OpenAPISecurityScheme `json:"securitySchemes,omitempty"`

	// DocsMiddlewares applies standard net/http middlewares to /docs and /openapi.json.
	// Docs routes are isolated from App.Use middlewares (such as CORS) by design.
	DocsMiddlewares []func(http.Handler) http.Handler `json:"-"`

	MaxBodyBytes          int64         `json:"maxBodyBytes,omitempty"`
	MaxMultipartMemory    int64         `json:"maxMultipartMemory,omitempty"`
	MaxHeaderBytes        int           `json:"maxHeaderBytes,omitempty"`
	ReadTimeout           time.Duration `json:"readTimeout,omitempty"`
	WriteTimeout          time.Duration `json:"writeTimeout,omitempty"`
	IdleTimeout           time.Duration `json:"idleTimeout,omitempty"`
	DisallowUnknownFields bool          `json:"disallowUnknownFields,omitempty"`
	SilenceTagWarnings    bool          `json:"silenceTagWarnings,omitempty"`
}

// App is the core gota engine backed by chi and net/http.
type App struct {
	config       Config
	mux          *chi.Mux
	docsMux      *chi.Mux
	routes       []*Route
	ErrorHandler func(ctx *Context, err error)

	docVersion atomic.Uint64
	specMu     sync.RWMutex
	cachedVer  uint64
	cachedSpec *OpenAPISpec
}

// New creates and configures a new gota App instance.
func New(cfg Config) *App {
	cfg.Title = cmp.Or(cfg.Title, "API Reference")
	cfg.Version = cmp.Or(cfg.Version, "1.0.0")
	cfg.DocsURL = cmp.Or(cfg.DocsURL, "/docs")
	cfg.OpenAPIURL = cmp.Or(cfg.OpenAPIURL, "/openapi.json")
	cfg.MaxBodyBytes = cmp.Or(cfg.MaxBodyBytes, DefaultMaxBodyBytes)
	cfg.MaxMultipartMemory = cmp.Or(cfg.MaxMultipartMemory, DefaultMaxMultipartMemory)
	if cfg.MaxHeaderBytes <= 0 {
		cfg.MaxHeaderBytes = DefaultMaxHeaderBytes
	}
	cfg.ReadTimeout = cmp.Or(cfg.ReadTimeout, DefaultReadTimeout)
	cfg.WriteTimeout = cmp.Or(cfg.WriteTimeout, DefaultWriteTimeout)
	cfg.IdleTimeout = cmp.Or(cfg.IdleTimeout, DefaultIdleTimeout)

	app := &App{
		config:       cfg,
		mux:          chi.NewRouter(),
		docsMux:      chi.NewRouter(),
		ErrorHandler: defaultErrorHandler,
	}

	app.mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		ctx := app.newContext(w, r)
		_ = ctx.Status(http.StatusNotFound).JSON(ErrorResponse{
			Error:     fmt.Sprintf("route not found: %s %s", r.Method, r.URL.Path),
			Status:    http.StatusNotFound,
			RequestID: ctx.RequestID(),
		})
	})

	if !cfg.DisableDocs {
		app.registerDocsRoutes()
	}
	return app
}

func (a *App) registerDocsRoutes() {
	a.docsMux.Use(middleware.Recoverer)
	for _, mw := range a.config.DocsMiddlewares {
		if mw != nil {
			a.docsMux.Use(mw)
		}
	}

	scalarCfg := a.config.Scalar
	scalarCfg.Title = cmp.Or(scalarCfg.Title, a.config.Title)
	scalarCfg.SpecURL = cmp.Or(scalarCfg.SpecURL, a.config.OpenAPIURL)

	a.docsMux.Get(a.config.DocsURL, scalar.Handler(scalarCfg))
	a.docsMux.Get(a.config.OpenAPIURL, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(a.OpenAPISpec())
	})
}

func defaultErrorHandler(ctx *Context, err error) {
	reqID := ctx.RequestID()
	if reqID != "" {
		log.Printf("gota: handler error %s %s [%s]: %v", ctx.Request.Method, ctx.Request.URL.Path, reqID, err)
	} else {
		log.Printf("gota: handler error %s %s: %v", ctx.Request.Method, ctx.Request.URL.Path, err)
	}

	if ctx.Written() {
		return
	}

	if httpErr, ok := errors.AsType[*HTTPError](err); ok {
		_ = ctx.Status(httpErr.Status).JSON(ErrorResponse{
			Error:     httpErr.Message,
			Status:    httpErr.Status,
			RequestID: reqID,
		})
		return
	}

	type statusError interface {
		error
		HTTPStatus() int
	}
	if se, ok := errors.AsType[statusError](err); ok {
		_ = ctx.Status(se.HTTPStatus()).JSON(se)
		return
	}

	_ = ctx.Status(http.StatusInternalServerError).JSON(ErrorResponse{
		Error:     "internal server error",
		Status:    http.StatusInternalServerError,
		RequestID: reqID,
	})
}

// Use appends standard net/http middlewares to the application router.
// Follows chi's rule: call Use before registering routes.
func (a *App) Use(middlewares ...func(http.Handler) http.Handler) {
	a.mux.Use(middlewares...)
}

// Router exposes the underlying chi.Mux for advanced net/http interoperability.
func (a *App) Router() *chi.Mux {
	return a.mux
}

func (a *App) isDocsPath(p string) bool {
	return p == a.config.DocsURL || p == a.config.OpenAPIURL
}

// ServeHTTP implements http.Handler.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !a.config.DisableDocs && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		if a.isDocsPath(r.URL.Path) {
			a.docsMux.ServeHTTP(w, r)
			return
		}
	}
	a.mux.ServeHTTP(w, r)
}

func (a *App) newContext(w http.ResponseWriter, r *http.Request) *Context {
	ctx := NewContext(w, r)
	ctx.disallowUnknownFields = a.config.DisallowUnknownFields
	ctx.maxBodyBytes = a.config.MaxBodyBytes
	ctx.maxMultipartMemory = a.config.MaxMultipartMemory
	return ctx
}

func (a *App) adaptHandler(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := a.newContext(w, r)
		if err := h(ctx); err != nil {
			a.ErrorHandler(ctx, err)
		}
	}
}

func (a *App) registerRoute(method, path string, h HandlerFunc, doc *RouteDoc) *Route {
	return a.registerRouteWithMiddlewares(method, path, h, doc, nil)
}

func (a *App) registerRouteWithMiddlewares(method, path string, h HandlerFunc, doc *RouteDoc, mws []func(http.Handler) http.Handler) *Route {
	if path == "" {
		path = "/"
	}
	rt := &Route{
		app:    a,
		method: method,
		path:   path,
		doc:    doc,
	}
	a.routes = append(a.routes, rt)
	a.invalidateSpec()

	r := chi.Router(a.mux)
	if len(mws) > 0 {
		r = a.mux.With(mws...)
	}
	r.Method(method, path, a.adaptHandler(h))
	return rt
}

// Handle registers an undocumented standard http.Handler on the App router.
func (a *App) Handle(method, path string, h http.Handler) {
	a.mux.Method(method, path, h)
}

func newNonREPRRouteDoc() *RouteDoc {
	return &RouteDoc{
		Responses: map[int]*ResponseDoc{
			http.StatusOK: {Description: http.StatusText(http.StatusOK)},
		},
	}
}

func registerRawRoute(target RouterTarget, method, path string, h HandlerFunc) *Route {
	if h == nil {
		panic("gota: route handler cannot be nil")
	}
	return target.registerRoute(method, path, h, newNonREPRRouteDoc())
}

func adaptStdHandler(h http.Handler) HandlerFunc {
	if h == nil {
		panic("gota: http.Handler cannot be nil")
	}
	return func(ctx *Context) error {
		h.ServeHTTP(ctx.Writer, ctx.Request)
		return nil
	}
}

// Route registers a documented non-REPR HandlerFunc for the given HTTP method and path.
func (a *App) Route(method, path string, h HandlerFunc) *Route {
	return registerRawRoute(a, method, path, h)
}

// Get registers a documented non-REPR GET HandlerFunc.
func (a *App) Get(path string, h HandlerFunc) *Route {
	return registerRawRoute(a, http.MethodGet, path, h)
}

// Post registers a documented non-REPR POST HandlerFunc.
func (a *App) Post(path string, h HandlerFunc) *Route {
	return registerRawRoute(a, http.MethodPost, path, h)
}

// Put registers a documented non-REPR PUT HandlerFunc.
func (a *App) Put(path string, h HandlerFunc) *Route {
	return registerRawRoute(a, http.MethodPut, path, h)
}

// Patch registers a documented non-REPR PATCH HandlerFunc.
func (a *App) Patch(path string, h HandlerFunc) *Route {
	return registerRawRoute(a, http.MethodPatch, path, h)
}

// Delete registers a documented non-REPR DELETE HandlerFunc.
func (a *App) Delete(path string, h HandlerFunc) *Route {
	return registerRawRoute(a, http.MethodDelete, path, h)
}

// HandleHTTP registers a standard net/http http.Handler and exposes its route metadata in OpenAPI, Scalar, and SDK codegen.
func (a *App) HandleHTTP(method, path string, h http.Handler) *Route {
	return registerRawRoute(a, method, path, adaptStdHandler(h))
}

// Group creates a route group sharing a URL prefix and optional middlewares.
func (a *App) Group(prefix string, mws ...func(http.Handler) http.Handler) *Group {
	return &Group{
		app:         a,
		prefix:      normalizePrefix(prefix),
		middlewares: slices.Clone(mws),
	}
}

// Group organizes routes under a common prefix and middleware chain.
type Group struct {
	app         *App
	prefix      string
	middlewares []func(http.Handler) http.Handler
	security    []string
	ownTags     []string
	parentTags  []string
}

// Use appends standard net/http middlewares to this Group (must be called before adding routes).
func (g *Group) Use(middlewares ...func(http.Handler) http.Handler) {
	g.middlewares = append(g.middlewares, middlewares...)
}

// Security declares OpenAPI security scheme names inherited by all routes in this Group.
func (g *Group) Security(schemes ...string) *Group {
	g.security = append(g.security, schemes...)
	return g
}

// Tags declares OpenAPI tags inherited by routes in this Group. Child group tags and route-level
// Tags calls take precedence ahead of parent group tags for module grouping.
func (g *Group) Tags(tags ...string) *Group {
	g.ownTags = append(g.ownTags, tags...)
	return g
}

func (g *Group) effectiveTags() []string {
	if len(g.ownTags) == 0 && len(g.parentTags) == 0 {
		return nil
	}
	out := make([]string, 0, len(g.ownTags)+len(g.parentTags))
	out = append(out, g.ownTags...)
	out = append(out, g.parentTags...)
	return out
}

// Group creates a nested sub-group inheriting prefix, middlewares, security schemes, and tags.
func (g *Group) Group(prefix string, mws ...func(http.Handler) http.Handler) *Group {
	return &Group{
		app:         g.app,
		prefix:      joinPaths(g.prefix, normalizePrefix(prefix)),
		middlewares: slices.Concat(g.middlewares, mws),
		security:    slices.Clone(g.security),
		parentTags:  g.effectiveTags(),
	}
}

func (g *Group) registerRoute(method, path string, h HandlerFunc, doc *RouteDoc) *Route {
	fullPath := joinPaths(g.prefix, path)
	if len(g.security) > 0 {
		doc.Security = slices.Concat(g.security, doc.Security)
	}
	effective := g.effectiveTags()
	if len(effective) > 0 {
		doc.Tags = append(doc.Tags, effective...)
	}
	rt := g.app.registerRouteWithMiddlewares(method, fullPath, h, doc, g.middlewares)
	rt.inheritedTags = len(effective)
	return rt
}

// Route registers a documented non-REPR HandlerFunc under this Group.
func (g *Group) Route(method, path string, h HandlerFunc) *Route {
	return registerRawRoute(g, method, path, h)
}

// Get registers a documented non-REPR GET HandlerFunc under this Group.
func (g *Group) Get(path string, h HandlerFunc) *Route {
	return registerRawRoute(g, http.MethodGet, path, h)
}

// Post registers a documented non-REPR POST HandlerFunc under this Group.
func (g *Group) Post(path string, h HandlerFunc) *Route {
	return registerRawRoute(g, http.MethodPost, path, h)
}

// Put registers a documented non-REPR PUT HandlerFunc under this Group.
func (g *Group) Put(path string, h HandlerFunc) *Route {
	return registerRawRoute(g, http.MethodPut, path, h)
}

// Patch registers a documented non-REPR PATCH HandlerFunc under this Group.
func (g *Group) Patch(path string, h HandlerFunc) *Route {
	return registerRawRoute(g, http.MethodPatch, path, h)
}

// Delete registers a documented non-REPR DELETE HandlerFunc under this Group.
func (g *Group) Delete(path string, h HandlerFunc) *Route {
	return registerRawRoute(g, http.MethodDelete, path, h)
}

// HandleHTTP registers a standard net/http http.Handler under this Group and exposes its route metadata in OpenAPI, Scalar, and SDK codegen.
func (g *Group) HandleHTTP(method, path string, h http.Handler) *Route {
	return registerRawRoute(g, method, path, adaptStdHandler(h))
}

func normalizePrefix(p string) string {
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}

func joinPaths(prefix, path string) string {
	if path == "" || path == "/" {
		if prefix == "" {
			return "/"
		}
		return prefix
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return prefix + path
}

func (a *App) invalidateSpec() {
	a.docVersion.Add(1)
}

// OpenAPISpec returns the cached OpenAPI 3.1 specification for all registered routes.
func (a *App) OpenAPISpec() *OpenAPISpec {
	ver := a.docVersion.Load()
	a.specMu.RLock()
	if a.cachedSpec != nil && a.cachedVer == ver {
		spec := a.cachedSpec
		a.specMu.RUnlock()
		return spec
	}
	a.specMu.RUnlock()

	a.specMu.Lock()
	defer a.specMu.Unlock()
	ver = a.docVersion.Load()
	if a.cachedSpec != nil && a.cachedVer == ver {
		return a.cachedSpec
	}
	gen := newOpenAPIGenerator(a.config)
	a.cachedSpec = gen.generate(a.routes)
	a.cachedVer = ver
	return a.cachedSpec
}

func writeExportFile(outPath string, data []byte) error {
	if existing, err := os.ReadFile(outPath); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(outPath, data, 0o644)
}

// Config returns a copy of the application's configuration.
func (a *App) Config() Config {
	return a.config
}

// Routes returns a snapshot slice of all registered routes on the application.
func (a *App) Routes() []*Route {
	a.specMu.RLock()
	defer a.specMu.RUnlock()
	return slices.Clone(a.routes)
}

// ExportOpenAPI writes the OpenAPI 3.1 JSON specification to disk without starting a server.
func (a *App) ExportOpenAPI(outPath string) error {
	data, err := json.MarshalIndent(a.OpenAPISpec(), "", "  ")
	if err != nil {
		return err
	}
	return writeExportFile(outPath, append(data, '\n'))
}

func (a *App) newHTTPServer(addr string) *http.Server {
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
	return &http.Server{
		Addr:              addr,
		Handler:           a,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    a.config.MaxHeaderBytes,
	}
}

// displayAddr normalizes a listen address into the address to bind and a
// host:port that can be opened in a browser. Accepts ":3000", "3000", or "host:3000".
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

func isProductionEnv() bool {
	for _, key := range []string{"ENV", "APP_ENV", "NODE_ENV"} {
		v := strings.ToLower(os.Getenv(key))
		if v == "production" || v == "prod" {
			return true
		}
	}
	return strings.ToLower(os.Getenv("GIN_MODE")) == "release"
}

func (a *App) logStartupBanner(display string) {
	log.Printf("💧 Gota server listening on http://%s", display)
	if a.config.DisableDocs {
		return
	}

	log.Printf("📖 Scalar docs: http://%s%s", display, a.config.DocsURL)
	log.Printf("📄 OpenAPI spec: http://%s%s", display, a.config.OpenAPIURL)

	prod := isProductionEnv()
	if len(a.config.DocsMiddlewares) > 0 {
		log.Printf("🔒 Docs protected with %d middleware(s)", len(a.config.DocsMiddlewares))
	} else if prod {
		log.Printf("⚠️  SECURITY WARNING: Documentation is running without authentication in production! Set Config.DisableDocs = true or configure Config.DocsMiddlewares")
	} else {
		log.Printf("🔒 Docs are public: set Config.DisableDocs = true or configure Config.DocsMiddlewares if exposed to untrusted networks")
	}
}

// Listen starts the HTTP server on addr.
func (a *App) Listen(addr string) error {
	listen, display := displayAddr(addr)
	srv := a.newHTTPServer(listen)
	a.logStartupBanner(display)
	return srv.ListenAndServe()
}

// ListenGraceful starts the HTTP server and gracefully shuts down on SIGINT/SIGTERM.
func (a *App) ListenGraceful(addr string, shutdownTimeout ...time.Duration) error {
	timeout := 10 * time.Second
	if len(shutdownTimeout) > 0 && shutdownTimeout[0] > 0 {
		timeout = shutdownTimeout[0]
	}
	listen, display := displayAddr(addr)
	srv := a.newHTTPServer(listen)

	errCh := make(chan error, 1)
	go func() {
		a.logStartupBanner(display)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case err := <-errCh:
		return err
	case <-quit:
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}
