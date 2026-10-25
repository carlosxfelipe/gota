package core

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDMiddleware(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(RequestID())

	var capturedReqID string
	app.Get("/test-id", func(ctx *Context) error {
		capturedReqID = ctx.RequestID()
		return ctx.Send("ok")
	})

	// 1. When request doesn't provide X-Request-ID, one is generated
	req1 := httptest.NewRequest("GET", "/test-id", nil)
	rec1 := httptest.NewRecorder()
	app.ServeHTTP(rec1, req1)

	respID1 := rec1.Header().Get("X-Request-ID")
	if respID1 == "" {
		t.Fatal("expected X-Request-ID in response header")
	}
	if capturedReqID != respID1 {
		t.Errorf("expected context request_id %q to match response header %q", capturedReqID, respID1)
	}

	// 2. When request provides X-Request-ID, it is preserved
	req2 := httptest.NewRequest("GET", "/test-id", nil)
	req2.Header.Set("X-Request-ID", "custom-trace-12345")
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)

	respID2 := rec2.Header().Get("X-Request-ID")
	if respID2 != "custom-trace-12345" {
		t.Errorf("expected preserved request ID 'custom-trace-12345', got %q", respID2)
	}
	if capturedReqID != "custom-trace-12345" {
		t.Errorf("expected captured request ID to be 'custom-trace-12345', got %q", capturedReqID)
	}
}

func TestSlogLoggerMiddleware(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	app := New(Config{DisableDocs: true})
	app.Use(RequestID())
	app.Use(SlogLogger(SlogLoggerOptions{Logger: logger}))

	app.Get("/ok", func(ctx *Context) error {
		return ctx.Send("success")
	})
	app.Get("/bad", func(ctx *Context) error {
		return ctx.Status(http.StatusBadRequest).Send("bad request")
	})
	app.Get("/err", func(ctx *Context) error {
		return errors.New("db connection failure")
	})

	// Request 200
	req := httptest.NewRequest("GET", "/ok", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte(`"status":200`)) {
		t.Errorf("expected log output to contain status 200, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"path":"/ok"`)) {
		t.Errorf("expected log output to contain path /ok, got: %s", output)
	}

	// Request 400
	buf.Reset()
	req2 := httptest.NewRequest("GET", "/bad", nil)
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)

	if !bytes.Contains(buf.Bytes(), []byte(`"level":"WARN"`)) {
		t.Errorf("expected warn level for 400 status, got: %s", buf.String())
	}

	// Request 500
	buf.Reset()
	req3 := httptest.NewRequest("GET", "/err", nil)
	rec3 := httptest.NewRecorder()
	app.ServeHTTP(rec3, req3)

	if !bytes.Contains(buf.Bytes(), []byte(`"level":"ERROR"`)) {
		t.Errorf("expected error level for 500 status, got: %s", buf.String())
	}
}

func TestContextClientIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.9:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18")
	ctx := NewContext(httptest.NewRecorder(), req, nil)

	// Default: forged proxy headers are ignored.
	if ip := ctx.ClientIP(); ip != "192.0.2.9" {
		t.Errorf("expected RemoteAddr 192.0.2.9 by default, got %q", ip)
	}

	// Trusted proxy mode.
	ctx.trustProxy = true
	if ip := ctx.ClientIP(); ip != "203.0.113.195" {
		t.Errorf("expected 203.0.113.195 from X-Forwarded-For, got %q", ip)
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("X-Real-IP", "198.51.100.1")
	ctx2 := NewContext(httptest.NewRecorder(), req2, nil)
	ctx2.trustProxy = true
	if ip := ctx2.ClientIP(); ip != "198.51.100.1" {
		t.Errorf("expected 198.51.100.1 from X-Real-IP, got %q", ip)
	}
}

func TestErrorResponseRequestID(t *testing.T) {
	// 1. With RequestID middleware
	appWithID := New(Config{DisableDocs: true})
	appWithID.Use(RequestID())
	appWithID.Get("/fail", func(ctx *Context) error {
		return errors.New("raw db failure")
	})

	req := httptest.NewRequest("GET", "/fail", nil)
	req.Header.Set("X-Request-ID", "req-test-abc")
	rec := httptest.NewRecorder()
	appWithID.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"requestId":"req-test-abc"`)) {
		t.Errorf("expected response to contain requestId, got: %s", body)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"internal server error"`)) {
		t.Errorf("expected sanitized error message, got: %s", body)
	}

	// 2. Without RequestID middleware
	appWithoutID := New(Config{DisableDocs: true})
	appWithoutID.Get("/fail", func(ctx *Context) error {
		return errors.New("raw db failure")
	})

	reqNoID := httptest.NewRequest("GET", "/fail", nil)
	recNoID := httptest.NewRecorder()
	appWithoutID.ServeHTTP(recNoID, reqNoID)

	if recNoID.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", recNoID.Code)
	}
	bodyNoID := recNoID.Body.String()
	if bytes.Contains(recNoID.Body.Bytes(), []byte(`requestId`)) {
		t.Errorf("expected response without requestId when not set, got: %s", bodyNoID)
	}
}

func TestCORSCredentialsWithWildcardPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for wildcard origin with credentials")
		}
	}()
	CORS(CORSOptions{AllowOrigins: []string{"*"}, AllowCredentials: true})
}

func TestCORSExplicitOrigins(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(CORS(CORSOptions{AllowOrigins: []string{"https://ok.example"}, AllowCredentials: true}))
	app.Get("/x", func(ctx *Context) error { return ctx.Send("ok") })

	for origin, wantAllowed := range map[string]bool{"https://ok.example": true, "https://evil.example": false} {
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		gotOrigin := rec.Header().Get("Access-Control-Allow-Origin")
		gotCreds := rec.Header().Get("Access-Control-Allow-Credentials")
		if wantAllowed && (gotOrigin != origin || gotCreds != "true") {
			t.Errorf("%s: expected allow origin+credentials, got %q / %q", origin, gotOrigin, gotCreds)
		}
		if !wantAllowed && (gotOrigin != "" || gotCreds != "") {
			t.Errorf("%s: expected no CORS grant, got %q / %q", origin, gotOrigin, gotCreds)
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Errorf("%s: expected Vary: Origin", origin)
		}
	}
}

func TestRequestIDRejectsUnsafeClientValue(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(RequestID())
	app.Get("/x", func(ctx *Context) error { return ctx.Send("ok") })

	for _, bad := range []string{"has space", "<script>", strings.Repeat("a", 129)} {
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("X-Request-ID", bad)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Request-ID"); got == bad || got == "" {
			t.Errorf("expected regenerated ID for %q, got %q", bad, got)
		}
	}

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Request-ID", "trace-123.abc")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); got != "trace-123.abc" {
		t.Errorf("expected valid ID to be kept, got %q", got)
	}
}
