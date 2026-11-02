package core

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBodyLimitOutsideServeHTTP(t *testing.T) {
	// A Context built directly (no MaxBytesReader from App.ServeHTTP) still enforces a limit.
	req := httptest.NewRequest("POST", "/", strings.NewReader("0123456789"))
	ctx := NewContext(httptest.NewRecorder(), req, nil)
	ctx.maxBodyBytes = 5

	_, err := ctx.Body()
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) || tooLarge.Limit != 5 {
		t.Fatalf("expected MaxBytesError with limit 5, got %v", err)
	}
}

func TestBodyLimitAllowsExactSize(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("12345"))
	ctx := NewContext(httptest.NewRecorder(), req, nil)
	ctx.maxBodyBytes = 5
	data, err := ctx.Body()
	if err != nil || string(data) != "12345" {
		t.Fatalf("got %q, %v", data, err)
	}
	// Cached on second call.
	if data2, err := ctx.Body(); err != nil || string(data2) != "12345" {
		t.Fatalf("cached read failed: %q, %v", data2, err)
	}
}

func TestBodyLimitDefaultAndDisabled(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), nil)
	if ctx.maxBodyBytes != DefaultMaxBodyBytes {
		t.Errorf("NewContext default = %d, want %d", ctx.maxBodyBytes, DefaultMaxBodyBytes)
	}

	// A negative Config.MaxBodyBytes is an explicit opt-out: no limit is applied.
	app := New(Config{DisableDocs: true, MaxBodyBytes: -1})
	app.Post("/x", func(ctx *Context) error {
		b, err := ctx.Body()
		if err != nil {
			return err
		}
		return ctx.Send(string(b))
	})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 100))))
	if rec.Code != http.StatusOK || rec.Body.Len() != 100 {
		t.Errorf("unlimited body failed: %d len=%d", rec.Code, rec.Body.Len())
	}
}

func TestBodyLimitFromConfigYields413(t *testing.T) {
	app := New(Config{DisableDocs: true, MaxBodyBytes: 8})
	app.Post("/x", func(ctx *Context) error {
		if _, err := ctx.Body(); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return ctx.Status(http.StatusRequestEntityTooLarge).Send("too large")
			}
			return err
		}
		return ctx.Send("ok")
	})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("POST", "/x", strings.NewReader("this is longer than eight bytes")))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rec.Code)
	}
}
