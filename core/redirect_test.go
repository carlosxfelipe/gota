package core

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLocalRedirectTarget(t *testing.T) {
	ok := []string{"/", "/dashboard", "/a/b?x=1#frag", "/%2f/x"}
	bad := []string{"", "dashboard", "//evil.com", "/\\evil.com", "https://evil.com", "http:evil.com",
		"/\t/evil.com", "/\n/evil.com", "/a\\b", "javascript:alert(1)", "/a\r\nSet-Cookie: x=1"}
	for _, s := range ok {
		if !IsLocalRedirectTarget(s) {
			t.Errorf("%q should be allowed", s)
		}
	}
	for _, s := range bad {
		if IsLocalRedirectTarget(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
}

func TestRedirectLocal(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := NewContext(rec, httptest.NewRequest("GET", "/", nil), nil)
	if err := ctx.RedirectLocal("/home"); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/home" {
		t.Errorf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	ctx = NewContext(rec, httptest.NewRequest("GET", "/", nil), nil)
	err := ctx.RedirectLocal("//evil.com")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusBadRequest {
		t.Fatalf("expected 400 HTTPError, got %v", err)
	}
	if ctx.Written() || rec.Header().Get("Location") != "" {
		t.Error("nothing must be written for rejected targets")
	}
}
