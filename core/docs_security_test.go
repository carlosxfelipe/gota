package core

import (
	"net/http/httptest"
	"strings"
	"testing"
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
