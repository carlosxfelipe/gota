package repr

import (
	"bytes"
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
	TypeScriptURL   string                           `json:"typescriptUrl,omitempty"`
	DisableDocs     bool                             `json:"disableDocs,omitempty"`
	Scalar          scalar.Config                    `json:"scalar,omitempty"`
	Servers         []OpenAPIServer                  `json:"servers,omitempty"`
	SecuritySchemes map[string]OpenAPISecurityScheme `json:"securitySchemes,omitempty"`

	// DocsMiddlewares applies standard net/http middlewares to /docs, /openapi.json, and /types.d.ts.
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

	docVersion  atomic.Uint64
	specMu      sync.RWMutex
	cachedVer   uint64
	cachedSpec  *OpenAPISpec
	cachedTSVer uint64
	cachedTS    string
}

// New creates and configures a new gota App instance.
func New(cfg Config) *App {
	if cfg.Title == "" {
		cfg.Title = "API Reference"
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
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.MaxMultipartMemory == 0 {
		cfg.MaxMultipartMemory = DefaultMaxMultipartMemory
	}
	if cfg.MaxHeaderBytes <= 0 {
		cfg.MaxHeaderBytes = DefaultMaxHeaderBytes
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
	if scalarCfg.Title == "" {
		scalarCfg.Title = a.config.Title
	}
	if scalarCfg.SpecURL == "" {
		scalarCfg.SpecURL = a.config.OpenAPIURL
	}

	a.docsMux.Get(a.config.DocsURL, scalar.Handler(scalarCfg))
	a.docsMux.Get(a.config.OpenAPIURL, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(a.OpenAPISpec())
	})

	if a.config.TypeScriptURL != "" && a.config.TypeScriptURL != "-" {
		a.docsMux.Get(a.config.TypeScriptURL, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/typescript; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(a.GenerateTypeScript()))
		})
	}
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

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		_ = ctx.Status(httpErr.Status).JSON(ErrorResponse{
			Error:     httpErr.Message,
			Status:    httpErr.Status,
			RequestID: reqID,
		})
		return
	}

	type statusError interface {
		HTTPStatus() int
	}
	var se statusError
	if errors.As(err, &se) {
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
	if p == a.config.DocsURL || p == a.config.OpenAPIURL {
		return true
	}
	if a.config.TypeScriptURL != "" && a.config.TypeScriptURL != "-" && p == a.config.TypeScriptURL {
		return true
	}
	return false
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

// HandleHTTP registers a standard net/http http.Handler and exposes it in OpenAPI, Scalar, and TypeScript.
func (a *App) HandleHTTP(method, path string, h http.Handler) *Route {
	return registerRawRoute(a, method, path, adaptStdHandler(h))
}

// Group creates a route group sharing a URL prefix and optional middlewares.
func (a *App) Group(prefix string, mws ...func(http.Handler) http.Handler) *Group {
	return &Group{
		app:         a,
		prefix:      normalizePrefix(prefix),
		middlewares: append([]func(http.Handler) http.Handler(nil), mws...),
	}
}

// Group organizes routes under a common prefix and middleware chain.
type Group struct {
	app         *App
	prefix      string
	middlewares []func(http.Handler) http.Handler
	security    []string
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

// Group creates a nested sub-group inheriting prefix, middlewares, and security schemes.
func (g *Group) Group(prefix string, mws ...func(http.Handler) http.Handler) *Group {
	combined := make([]func(http.Handler) http.Handler, 0, len(g.middlewares)+len(mws))
	combined = append(combined, g.middlewares...)
	combined = append(combined, mws...)
	return &Group{
		app:         g.app,
		prefix:      joinPaths(g.prefix, normalizePrefix(prefix)),
		middlewares: combined,
		security:    append([]string(nil), g.security...),
	}
}

func (g *Group) registerRoute(method, path string, h HandlerFunc, doc *RouteDoc) *Route {
	fullPath := joinPaths(g.prefix, path)
	if len(g.security) > 0 {
		doc.Security = append(append([]string(nil), g.security...), doc.Security...)
	}
	return g.app.registerRouteWithMiddlewares(method, fullPath, h, doc, g.middlewares)
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

// HandleHTTP registers a standard net/http http.Handler under this Group and exposes it in OpenAPI, Scalar, and TypeScript.
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

// ExportOpenAPI writes the OpenAPI 3.1 JSON specification to disk without starting a server.
func (a *App) ExportOpenAPI(outPath string) error {
	data, err := json.MarshalIndent(a.OpenAPISpec(), "", "  ")
	if err != nil {
		return err
	}
	return writeExportFile(outPath, append(data, '\n'))
}

func (a *App) cachedTSOutput(cache *string, cacheVer *uint64, build func(*TypeScriptGenerator) string) string {
	ver := a.docVersion.Load()
	a.specMu.RLock()
	if *cache != "" && *cacheVer == ver {
		res := *cache
		a.specMu.RUnlock()
		return res
	}
	a.specMu.RUnlock()

	a.specMu.Lock()
	defer a.specMu.Unlock()
	ver = a.docVersion.Load()
	if *cache != "" && *cacheVer == ver {
		return *cache
	}
	gen := NewTypeScriptGenerator(a.config)
	*cache = build(gen)
	*cacheVer = ver
	return *cache
}

// GenerateTypeScript generates complete TypeScript declarations (.d.ts) for all registered routes.
func (a *App) GenerateTypeScript() string {
	return a.cachedTSOutput(&a.cachedTS, &a.cachedTSVer, func(g *TypeScriptGenerator) string {
		return g.Generate(a.routes)
	})
}

// ExportTypeScript writes the generated TypeScript type definitions (.d.ts) to disk.
func (a *App) ExportTypeScript(outPath string) error {
	return writeExportFile(outPath, []byte(a.GenerateTypeScript()))
}

// GenerateTypeScriptSDK generates the modular TypeScript client SDK files keyed by
// slash-separated paths relative to the SDK directory (index.ts, runtime.ts, types/*.ts, endpoints/*.ts).
func (a *App) GenerateTypeScriptSDK(opts ...TypeScriptSDKOptions) (map[string]string, error) {
	a.specMu.RLock()
	routes := append([]*Route(nil), a.routes...)
	a.specMu.RUnlock()
	return NewTypeScriptGenerator(a.config).GenerateSDK(routes, opts...)
}

// ExportTypeScriptSDK writes the modular TypeScript client SDK into outDir, updating changed
// files and removing stale files previously created by Gota codegen.
func (a *App) ExportTypeScriptSDK(outDir string, opts ...TypeScriptSDKOptions) error {
	files, err := a.GenerateTypeScriptSDK(opts...)
	if err != nil {
		return err
	}
	return exportTSDir(outDir, files)
}

// CheckTypeScript verifies that the generated TypeScript definitions at outPath match the current routes.
func (a *App) CheckTypeScript(outPath string) error {
	return checkTSExportFile(outPath, a.GenerateTypeScript())
}

// CheckTypeScriptSDK verifies that outDir matches the modular TypeScript SDK for the current routes
// with no missing, modified, or stale generated files.
func (a *App) CheckTypeScriptSDK(outDir string, opts ...TypeScriptSDKOptions) error {
	files, err := a.GenerateTypeScriptSDK(opts...)
	if err != nil {
		return err
	}
	return checkTSDir(outDir, files)
}

// RunCodegen processes CLI arguments (e.g. from os.Args[1:]) to export or verify TypeScript SDK and definitions.
// Supported flags:
//
//	--ts-sdk[=<dir>]        writes modular client SDK directory to dir (default ./dist/sdk)
//	--ts-types[=<path>]     writes TypeScript definitions (.d.ts) to path (default ./dist/types.d.ts)
//	--ts-import-ext=<ext>   sets relative import suffix in --ts-sdk ("", ".js", or ".ts")
//	--check                 verifies exported files/dir are up to date without modifying disk (for CI)
//	--stdout                writes TypeScript definitions (.d.ts) to standard output
func (a *App) RunCodegen(args []string) (bool, error) {
	sdkDir := ""
	typesPath := ""
	importExt := ""
	hasImportExt := false
	stdout := false
	check := false
	matched := false

	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--ts-sdk="):
			sdkDir = strings.TrimPrefix(arg, "--ts-sdk=")
			matched = true
		case arg == "--ts-sdk":
			sdkDir = "./dist/sdk"
			matched = true
		case strings.HasPrefix(arg, "--ts-types="):
			typesPath = strings.TrimPrefix(arg, "--ts-types=")
			matched = true
		case arg == "--ts-types":
			typesPath = "./dist/types.d.ts"
			matched = true
		case strings.HasPrefix(arg, "--ts-import-ext="):
			importExt = strings.TrimPrefix(arg, "--ts-import-ext=")
			hasImportExt = true
			matched = true
		case arg == "--stdout":
			stdout = true
			matched = true
		case arg == "--check":
			check = true
			matched = true
		}
	}

	if !matched {
		return false, nil
	}

	if hasImportExt {
		if _, err := normalizeTSImportExtension(importExt); err != nil {
			return true, err
		}
	}

	if stdout {
		if check {
			return true, fmt.Errorf("gota codegen: --check cannot be combined with --stdout")
		}
		_, _ = os.Stdout.WriteString(a.GenerateTypeScript())
		return true, nil
	}

	sdkOpts := TypeScriptSDKOptions{ImportExtension: importExt}

	if check {
		if sdkDir == "" && typesPath == "" {
			return true, fmt.Errorf("gota codegen: --check requires at least one of --ts-sdk or --ts-types")
		}
		var errs []error
		if sdkDir != "" {
			if err := a.CheckTypeScriptSDK(sdkDir, sdkOpts); err != nil {
				errs = append(errs, err)
			}
		}
		if typesPath != "" {
			if err := a.CheckTypeScript(typesPath); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return true, errors.Join(errs...)
		}
		return true, nil
	}

	if sdkDir != "" {
		if err := a.ExportTypeScriptSDK(sdkDir, sdkOpts); err != nil {
			return true, fmt.Errorf("failed to export TypeScript SDK: %w", err)
		}
	}
	if typesPath != "" {
		if err := a.ExportTypeScript(typesPath); err != nil {
			return true, fmt.Errorf("failed to export TypeScript definitions: %w", err)
		}
	}

	return true, nil
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

	tsEnabled := a.config.TypeScriptURL != "" && a.config.TypeScriptURL != "-"
	if tsEnabled {
		log.Printf("🔷 TypeScript defs: http://%s%s", display, a.config.TypeScriptURL)
	}

	prod := isProductionEnv()
	if len(a.config.DocsMiddlewares) > 0 {
		log.Printf("🔒 Docs protected with %d middleware(s)", len(a.config.DocsMiddlewares))
	} else if prod {
		log.Printf("⚠️  SECURITY WARNING: Documentation is running without authentication in production! Set Config.DisableDocs = true or configure Config.DocsMiddlewares")
	} else {
		log.Printf("🔒 Docs are public: set Config.DisableDocs = true or configure Config.DocsMiddlewares if exposed to untrusted networks")
	}

	if prod && len(a.config.DocsMiddlewares) == 0 && tsEnabled {
		log.Printf("⚠️  SECURITY WARNING: TypeScript definitions are served publicly over HTTP in production. For maximum security, export offline (app.ExportTypeScriptSDK) during CI/CD and set TypeScriptURL: \"-\".")
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
