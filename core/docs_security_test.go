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
