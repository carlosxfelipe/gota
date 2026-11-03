package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func docsResponse(t *testing.T, cfg Config, mws ...MiddlewareFunc) *httptest.ResponseRecorder {
	t.Helper()
	app := New(cfg)
	for _, mw := range mws {
		app.Use(mw)
	}
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/docs", nil))
	if rec.Code != 200 {
		t.Fatalf("docs status %d", rec.Code)
	}
	return rec
}

func TestDocsSecurityHeadersDefaultReportOnly(t *testing.T) {
	rec := docsResponse(t, Config{})
	h := rec.Header()
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("missing hardening headers: %v", h)
	}
	if h.Get("Content-Security-Policy") != "" {
		t.Error("CSP must be report-only by default")
	}
	csp := h.Get("Content-Security-Policy-Report-Only")
	for _, want := range []string{"script-src 'self' https://cdn.jsdelivr.net", "frame-ancestors 'none'", "object-src 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q missing %q", csp, want)
		}
	}
}

func TestDocsCSPEnforceCustomAndDisabled(t *testing.T) {
	rec := docsResponse(t, Config{DocsCSPEnforce: true, ScalarScriptURL: "/static/scalar.js"})
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" || rec.Header().Get("Content-Security-Policy-Report-Only") != "" {
		t.Fatalf("expected enforcing CSP only, got %v", rec.Header())
	}
	if strings.Contains(csp, "jsdelivr") || !strings.Contains(csp, "script-src 'self';") {
		t.Errorf("relative script URL should only allow 'self': %q", csp)
	}

	rec = docsResponse(t, Config{DocsCSP: "default-src 'none'"})
	if rec.Header().Get("Content-Security-Policy-Report-Only") != "default-src 'none'" {
		t.Errorf("custom CSP not applied: %v", rec.Header())
	}

	rec = docsResponse(t, Config{DocsCSP: "-"})
	if rec.Header().Get("Content-Security-Policy-Report-Only") != "" || rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("DocsCSP \"-\" must omit CSP")
	}
}

func TestDocsCSPOverridesGlobalSecureHeadersCSP(t *testing.T) {
	rec := docsResponse(t, Config{DocsCSPEnforce: true}, SecureHeaders(SecureHeadersOptions{ContentSecurityPolicy: ApiContentSecurityPolicy}))
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == ApiContentSecurityPolicy || !strings.Contains(csp, "cdn.jsdelivr.net") {
		t.Errorf("docs CSP should replace the API CSP, got %q", csp)
	}
}

func TestOpenAPISpecHasNosniff(t *testing.T) {
	app := New(Config{})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("expected nosniff on openapi.json")
	}
}

func TestDocsAndOpenAPINoCacheHeaders(t *testing.T) {
	app := New(Config{})
	for _, path := range []string{"/docs", "/openapi.json"} {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 200 {
			t.Fatalf("%s status %d", path, rec.Code)
		}
		cc := rec.Header().Get("Cache-Control")
		if cc != "no-store, no-cache, must-revalidate, private" {
			t.Errorf("%s: expected strict no-cache header, got %q", path, cc)
		}
		if rec.Header().Get("Pragma") != "no-cache" {
			t.Errorf("%s: expected Pragma: no-cache, got %q", path, rec.Header().Get("Pragma"))
		}
	}
}

func TestDocsNoCacheHeadersCustomOverride(t *testing.T) {
	app := New(Config{})
	app.UseDocs(func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			ctx.SetHeader("Cache-Control", "public, max-age=60")
			return next(ctx)
		}
	})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/docs", nil))
	if rec.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Errorf("expected custom Cache-Control override, got %q", rec.Header().Get("Cache-Control"))
	}
}

func TestUseDocsProtectsDocsAndOpenAPI(t *testing.T) {
	app := New(Config{})
	app.UseDocs(BasicAuth("dev", "pass123"))

	// Regular API route must NOT be affected
	app.Get("/ping", func(ctx *Context) error {
		return ctx.Send("pong")
	})

	// 1. Regular route is accessible without auth
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/ping", nil))
	if rec.Code != 200 || rec.Body.String() != "pong" {
		t.Errorf("regular route affected by UseDocs: %d %s", rec.Code, rec.Body.String())
	}

	// 2. /docs requires auth
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/docs", nil))
	if rec.Code != 401 {
		t.Errorf("/docs should be 401, got %d", rec.Code)
	}

	// 2.1 /docs with valid auth succeeds
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/docs", nil)
	req.SetBasicAuth("dev", "pass123")
	app.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("/docs with basic auth failed: %d", rec.Code)
	}

	// 3. /openapi.json requires auth
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	if rec.Code != 401 {
		t.Errorf("/openapi.json should be 401, got %d", rec.Code)
	}

	// 3.1 /openapi.json with valid auth succeeds
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/openapi.json", nil)
	req.SetBasicAuth("dev", "pass123")
	app.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("/openapi.json with basic auth failed: %d", rec.Code)
	}
}

func TestConfigDocsMiddlewares(t *testing.T) {
	app := New(Config{
		DocsMiddlewares: []MiddlewareFunc{
			BasicAuth("team", "secret"),
		},
	})
	app.Get("/health", func(ctx *Context) error { return ctx.Send("ok") })

	// Public healthcheck passes
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 200 {
		t.Errorf("health should be 200, got %d", rec.Code)
	}

	// Docs blocked without auth
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/docs", nil))
	if rec.Code != 401 {
		t.Errorf("docs should be 401, got %d", rec.Code)
	}
}

func TestUseDocsOrderWithGlobalMiddlewares(t *testing.T) {
	var order []string
	globalMW := func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			order = append(order, "global")
			return next(ctx)
		}
	}
	docsMW := func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			order = append(order, "docs")
			return next(ctx)
		}
	}

	app := New(Config{})
	app.Use(globalMW)
	app.UseDocs(docsMW)

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/docs", nil))
	if rec.Code != 200 {
		t.Fatalf("docs failed: %d", rec.Code)
	}
	if len(order) != 2 || order[0] != "global" || order[1] != "docs" {
		t.Errorf("expected [global, docs], got %v", order)
	}
}

func TestDocsRateLimitInConfig(t *testing.T) {
	app := New(Config{
		DocsRateLimit: &RateLimiterOptions{
			Max:    2,
			Window: time.Minute,
		},
	})
	app.Get("/api/data", func(ctx *Context) error { return ctx.Send("public data") })

	// Normal API route is NOT rate limited
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest("GET", "/api/data", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("api route should not be limited, got %d on attempt %d", rec.Code, i+1)
		}
	}

	// Docs route is rate-limited to 2 requests
	rec1 := httptest.NewRecorder()
	app.ServeHTTP(rec1, httptest.NewRequest("GET", "/docs", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 on first docs request, got %d", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, httptest.NewRequest("GET", "/docs", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on second docs request, got %d", rec2.Code)
	}

	// 3rd request to docs returns 429 Too Many Requests
	rec3 := httptest.NewRecorder()
	app.ServeHTTP(rec3, httptest.NewRequest("GET", "/docs", nil))
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on third docs request, got %d", rec3.Code)
	}
}

func TestUseDocsWithRateLimiterAndBasicAuth(t *testing.T) {
	app := New(Config{
		TypeScriptURL:    "/types.d.ts",
		TypeScriptSDKURL: "/sdk.ts",
	})
	app.UseDocs(
		RateLimiter(RateLimiterOptions{Max: 2, Window: time.Minute}),
		BasicAuth("admin", "secret"),
	)

	// Attempt 1 on /types.d.ts: invalid credentials -> 401
	req1 := httptest.NewRequest("GET", "/types.d.ts", nil)
	req1.SetBasicAuth("admin", "wrong")
	rec1 := httptest.NewRecorder()
	app.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("attempt 1: expected 401, got %d", rec1.Code)
	}

	// Attempt 2 on /types.d.ts: invalid credentials -> 401
	req2 := httptest.NewRequest("GET", "/types.d.ts", nil)
	req2.SetBasicAuth("admin", "wrong")
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("attempt 2: expected 401, got %d", rec2.Code)
	}

	// Attempt 3 on /types.d.ts: brute-force blocked -> 429 Too Many Requests
	req3 := httptest.NewRequest("GET", "/types.d.ts", nil)
	req3.SetBasicAuth("admin", "secret")
	rec3 := httptest.NewRecorder()
	app.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 3: expected 429 Too Many Requests, got %d", rec3.Code)
	}
}

func TestDocsCORSIsolationFromGlobalWildcard(t *testing.T) {
	app := New(Config{
		TypeScriptURL:    "/types.d.ts",
		TypeScriptSDKURL: "/sdk.ts",
	})
	app.Use(CORS()) // Global wildcard CORS

	app.Get("/api/ping", func(ctx *Context) error {
		return ctx.Send("pong")
	})

	// 1. Regular API route MUST receive CORS header
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/ping", nil)
	req.Header.Set("Origin", "https://evil.com")
	app.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("regular route should have CORS allowed, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}

	// Regular API route preflight OPTIONS must succeed with CORS
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("OPTIONS", "/api/ping", nil)
	req.Header.Set("Origin", "https://evil.com")
	app.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("regular route OPTIONS should have CORS allowed, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}

	// 2. Docs and schema endpoints MUST NOT leak CORS headers to untrusted origins
	docsPaths := []string{"/docs", "/openapi.json", "/types.d.ts", "/sdk.ts"}
	for _, path := range docsPaths {
		// GET request
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("GET", path, nil)
		req.Header.Set("Origin", "https://evil.com")
		app.ServeHTTP(rec, req)
		if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "" {
			t.Errorf("%s GET: isolated docs route leaked CORS header %q to untrusted origin", path, origin)
		}

		// OPTIONS preflight request
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("OPTIONS", path, nil)
		req.Header.Set("Origin", "https://evil.com")
		app.ServeHTTP(rec, req)
		if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "" {
			t.Errorf("%s OPTIONS: isolated docs route leaked CORS header %q to untrusted origin", path, origin)
		}
	}
}

func TestDocsCORSExplicitAllowedOrigins(t *testing.T) {
	app := New(Config{
		DocsAllowOrigins: []string{"https://portal.mycompany.com"},
		TypeScriptURL:    "/types.d.ts",
		TypeScriptSDKURL: "/sdk.ts",
	})
	app.Use(CORS()) // Global CORS

	docsPaths := []string{"/openapi.json", "/types.d.ts", "/sdk.ts"}
	for _, path := range docsPaths {
		// 1. Trusted origin receives CORS grant
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Origin", "https://portal.mycompany.com")
		app.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.mycompany.com" {
			t.Errorf("%s GET: expected CORS grant for trusted origin, got %q", path, got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("%s GET: expected Allow-Credentials: true for explicit origin, got %q", path, got)
		}

		// 2. Preflight OPTIONS from trusted origin succeeds
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("OPTIONS", path, nil)
		req.Header.Set("Origin", "https://portal.mycompany.com")
		app.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s OPTIONS: expected 204 NoContent, got %d", path, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.mycompany.com" {
			t.Errorf("%s OPTIONS: expected CORS grant for trusted origin, got %q", path, got)
		}

		// 3. Untrusted origin receives NO CORS grant
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("GET", path, nil)
		req.Header.Set("Origin", "https://evil.com")
		app.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s GET: untrusted origin must not get CORS grant, got %q", path, got)
		}

		// Untrusted origin preflight receives NO CORS grant
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("OPTIONS", path, nil)
		req.Header.Set("Origin", "https://evil.com")
		app.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s OPTIONS: untrusted origin preflight must not get CORS grant, got %q", path, got)
		}
	}
}

func TestDocsCORSWithBasicAuthProtection(t *testing.T) {
	app := New(Config{
		DocsAllowOrigins: []string{"https://portal.mycompany.com"},
	})
	app.Use(CORS())
	app.UseDocs(BasicAuth("admin", "secret"))

	// 1. Untrusted origin without auth: 401 Unauthorized with NO CORS header
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	req.Header.Set("Origin", "https://evil.com")
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("401 response must not leak CORS header to untrusted origin, got %q", got)
	}

	// 2. Preflight OPTIONS from trusted origin succeeds WITHOUT credentials
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("OPTIONS", "/openapi.json", nil)
	req.Header.Set("Origin", "https://portal.mycompany.com")
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS preflight should return 204 without requiring auth, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.mycompany.com" {
		t.Errorf("expected CORS grant on OPTIONS preflight, got %q", got)
	}
}

func TestDocsCORSExplicitWildcard(t *testing.T) {
	app := New(Config{
		DocsAllowOrigins: []string{"*"},
	})
	app.Use(CORS())

	// 1. Any regular origin matches wildcard
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	req.Header.Set("Origin", "https://external.example.com")
	app.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected wildcard CORS grant, got %q", got)
	}

	// 2. Null origin does NOT match wildcard "*" (per CORS security specifications)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/openapi.json", nil)
	req.Header.Set("Origin", "null")
	app.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("null origin must not match wildcard entry, got %q", got)
	}
}

func TestExampleSetup_ExactConfig(t *testing.T) {
	app := New(Config{
		Title:            "Gota REPR API",
		Version:          "1.0.0",
		Description:      "Go micro-framework oriented around the REPR Pattern with native interactive Scalar UI.",
		TypeScriptURL:    "/types.d.ts",
		TypeScriptSDKURL: "/sdk.ts",
	})

	app.Use(RequestID())
	app.Use(Logger())
	app.Use(Recover())
	app.Use(CORS())

	app.UseDocs(BasicAuth("admin", "admin123", BasicAuthOptions{
		Realm: "Gota API Documentation",
		RateLimit: &RateLimiterOptions{
			Max:    10,
			Window: time.Minute,
		},
		AllowWeakCredentials: true,
	}))

	for _, path := range []string{"/docs", "/openapi.json", "/types.d.ts", "/sdk.ts"} {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without credentials expected 401, got %d", path, rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") != `Basic realm="Gota API Documentation"` {
			t.Errorf("%s expected WWW-Authenticate header, got %q", path, rec.Header().Get("WWW-Authenticate"))
		}
	}
}
