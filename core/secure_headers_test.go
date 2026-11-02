package core

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func runSecure(t *testing.T, opts SecureHeadersOptions, mod func(*Context)) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx := NewContext(rec, httptest.NewRequest("GET", "/", nil), nil)
	if mod != nil {
		mod(ctx)
	}
	h := SecureHeaders(opts)(func(c *Context) error { return c.Send("ok") })
	if err := h(ctx); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestSecureHeadersDefaults(t *testing.T) {
	rec := runSecure(t, SecureHeadersOptions{}, nil)
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	for _, k := range []string{"Strict-Transport-Security", "Content-Security-Policy", "Permissions-Policy"} {
		if rec.Header().Get(k) != "" {
			t.Errorf("%s must not be set by default over plain HTTP", k)
		}
	}
}

func TestSecureHeadersHSTS(t *testing.T) {
	opts := SecureHeadersOptions{HSTSIncludeSubdomains: true, HSTSPreload: true}
	rec := runSecure(t, opts, func(c *Context) { c.Request.TLS = &tls.ConnectionState{} })
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains; preload" {
		t.Errorf("HSTS = %q", got)
	}

	// X-Forwarded-Proto is ignored unless proxy headers are trusted.
	fwd := func(trust bool) func(*Context) {
		return func(c *Context) {
			c.Request.Header.Set("X-Forwarded-Proto", "https")
			c.trustProxy = trust
		}
	}
	if runSecure(t, SecureHeadersOptions{}, fwd(false)).Header().Get("Strict-Transport-Security") != "" {
		t.Error("untrusted X-Forwarded-Proto must not enable HSTS")
	}
	if runSecure(t, SecureHeadersOptions{}, fwd(true)).Header().Get("Strict-Transport-Security") == "" {
		t.Error("trusted X-Forwarded-Proto https should enable HSTS")
	}
	rec = runSecure(t, SecureHeadersOptions{HSTSMaxAge: -1}, func(c *Context) { c.Request.TLS = &tls.ConnectionState{} })
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("negative HSTSMaxAge must disable HSTS")
	}
}

func TestSecureHeadersCustomAndOptOut(t *testing.T) {
	rec := runSecure(t, SecureHeadersOptions{
		FrameOptions:          "-",
		ReferrerPolicy:        "same-origin",
		ContentSecurityPolicy: ApiContentSecurityPolicy,
		PermissionsPolicy:     "geolocation=()",
	}, nil)
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Error("X-Frame-Options should be omitted with \"-\"")
	}
	if rec.Header().Get("Referrer-Policy") != "same-origin" ||
		rec.Header().Get("Content-Security-Policy") != ApiContentSecurityPolicy ||
		rec.Header().Get("Permissions-Policy") != "geolocation=()" {
		t.Errorf("custom headers not applied: %v", rec.Header())
	}
}

func TestSecureHeadersOnErrorResponses(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(SecureHeaders())
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/missing", nil))
	if rec.Code != 404 || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("expected nosniff on 404, got %d %v", rec.Code, rec.Header())
	}
}
