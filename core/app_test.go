package core

import (
	"bytes"
	"context"
	"errors"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestApp_ConfigTimeouts(t *testing.T) {
	app := New(Config{
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 20 * time.Second,
		IdleTimeout:  60 * time.Second,
	})

	if app.config.ReadTimeout != 15*time.Second {
		t.Fatalf("expected ReadTimeout to be 15s, got %v", app.config.ReadTimeout)
	}
	if app.config.WriteTimeout != 20*time.Second {
		t.Fatalf("expected WriteTimeout to be 20s, got %v", app.config.WriteTimeout)
	}
	if app.config.IdleTimeout != 60*time.Second {
		t.Fatalf("expected IdleTimeout to be 60s, got %v", app.config.IdleTimeout)
	}
}

func TestApp_DefaultTimeouts(t *testing.T) {
	app := New(Config{})

	if app.config.ReadTimeout != DefaultReadTimeout {
		t.Fatalf("expected ReadTimeout to default to %v, got %v", DefaultReadTimeout, app.config.ReadTimeout)
	}
	if app.config.WriteTimeout != DefaultWriteTimeout {
		t.Fatalf("expected WriteTimeout to default to %v, got %v", DefaultWriteTimeout, app.config.WriteTimeout)
	}
	if app.config.IdleTimeout != DefaultIdleTimeout {
		t.Fatalf("expected IdleTimeout to default to %v, got %v", DefaultIdleTimeout, app.config.IdleTimeout)
	}
}

func TestApp_ListenAndShutdown(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Get("/ping", func(ctx *Context) error {
		return ctx.Send("pong")
	})

	errChan := make(chan error, 1)
	go func() {
		// Use a high port unlikely to collide
		errChan <- app.Listen("127.0.0.1:0")
	}()

	// Wait for server to bind
	var srv *http.Server
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		srv = app.Server()
		if srv != nil {
			break
		}
	}
	if srv == nil {
		t.Fatal("expected server to be initialized")
	}

	// Verify timeouts on active http.Server
	if srv.ReadTimeout != DefaultReadTimeout {
		t.Errorf("expected server ReadTimeout %v, got %v", DefaultReadTimeout, srv.ReadTimeout)
	}

	// Test shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	err := <-errChan
	if err != nil && err != http.ErrServerClosed {
		t.Fatalf("expected ErrServerClosed, got %v", err)
	}
}

func TestApp_ListenGraceful_Signal(t *testing.T) {
	app := New(Config{DisableDocs: true})

	errChan := make(chan error, 1)
	go func() {
		errChan <- app.ListenGraceful("127.0.0.1:0", syscall.SIGUSR1)
	}()

	// Wait for server to start
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		if app.Server() != nil {
			break
		}
	}
	if app.Server() == nil {
		t.Fatal("server failed to start")
	}

	// Send simulated signal
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)

	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("expected clean shutdown, got error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ListenGraceful did not exit within timeout")
	}
}

func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic", name)
		}
	}()
	fn()
}

func TestLateUsePanics(t *testing.T) {
	app := New(Config{})
	app.Use(RequestID()) // fine: docs routes do not count as user routes
	app.Get("/a", func(ctx *Context) error { return ctx.Send("a") })
	mustPanic(t, "App.Use after route", func() { app.Use(RequestID()) })

	g := app.Group("/g")
	g.Use(RequestID())
	g.Get("/x", func(ctx *Context) error { return ctx.Send("x") })
	mustPanic(t, "RouteGroup.Use after route", func() { g.Use(RequestID()) })
}

func TestDocsRoutesGetGlobalMiddleware(t *testing.T) {
	app := New(Config{})
	app.Use(RequestID())

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("expected global middleware to apply to docs routes")
	}
}

func TestApp_MaxMultipartMemory(t *testing.T) {
	// 1. Default config
	appDefault := New(Config{})
	if appDefault.config.MaxMultipartMemory != DefaultMaxMultipartMemory {
		t.Errorf("expected default MaxMultipartMemory %d, got %d", DefaultMaxMultipartMemory, appDefault.config.MaxMultipartMemory)
	}

	// 2. Custom config propagated to Context
	customLimit := int64(4 << 20)
	appCustom := New(Config{MaxMultipartMemory: customLimit, DisableDocs: true})
	var observedLimit int64
	appCustom.Post("/upload", func(ctx *Context) error {
		observedLimit = ctx.MaxMultipartMemory()
		return ctx.Send("ok")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/upload", nil)
	appCustom.ServeHTTP(rec, req)

	if observedLimit != customLimit {
		t.Errorf("expected context MaxMultipartMemory %d, got %d", customLimit, observedLimit)
	}

	// 3. Standalone context default & setter
	ctx := NewContext(rec, req, nil)
	if ctx.MaxMultipartMemory() != DefaultMaxMultipartMemory {
		t.Errorf("expected standalone context default %d, got %d", DefaultMaxMultipartMemory, ctx.MaxMultipartMemory())
	}
	ctx.SetMaxMultipartMemory(8 << 20)
	if ctx.MaxMultipartMemory() != 8<<20 {
		t.Errorf("expected updated context memory %d, got %d", 8<<20, ctx.MaxMultipartMemory())
	}

	// 4. Negative value disables memory limit (returns math.MaxInt64)
	appDisabled := New(Config{MaxMultipartMemory: -1, DisableDocs: true})
	var observedDisabledLimit int64
	appDisabled.Post("/upload", func(ctx *Context) error {
		observedDisabledLimit = ctx.MaxMultipartMemory()
		return ctx.Send("ok")
	})
	appDisabled.ServeHTTP(httptest.NewRecorder(), req)
	if observedDisabledLimit != math.MaxInt64 {
		t.Errorf("expected disabled memory limit math.MaxInt64, got %d", observedDisabledLimit)
	}

	ctx.SetMaxMultipartMemory(-1)
	if ctx.MaxMultipartMemory() != math.MaxInt64 {
		t.Errorf("expected setter with -1 to return math.MaxInt64, got %d", ctx.MaxMultipartMemory())
	}

	ctx.SetMaxMultipartMemory(0)
	if ctx.MaxMultipartMemory() != DefaultMaxMultipartMemory {
		t.Errorf("expected setter with 0 to revert to default %d, got %d", DefaultMaxMultipartMemory, ctx.MaxMultipartMemory())
	}
}

func TestMaxHeaderBytesConfig(t *testing.T) {
	if got := New(Config{DisableDocs: true}).Config().MaxHeaderBytes; got != DefaultMaxHeaderBytes {
		t.Errorf("default MaxHeaderBytes = %d, want %d", got, DefaultMaxHeaderBytes)
	}
	if got := New(Config{DisableDocs: true, MaxHeaderBytes: -5}).Config().MaxHeaderBytes; got != DefaultMaxHeaderBytes {
		t.Errorf("negative MaxHeaderBytes should fall back to default, got %d", got)
	}
	app := New(Config{DisableDocs: true, MaxHeaderBytes: 4096})
	if got := app.Config().MaxHeaderBytes; got != 4096 {
		t.Errorf("MaxHeaderBytes = %d, want 4096", got)
	}
}

func TestErrorHandlerDoesNotCorruptWrittenResponse(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Get("/stream-fail", func(ctx *Context) error {
		_ = ctx.Send("partial stream payload")
		return errors.New("database disconnected mid-stream")
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/stream-fail", nil))

	// The status and body already written must not be modified or suffixed with JSON error
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 from Send, got %d", rec.Code)
	}
	if rec.Body.String() != "partial stream payload" {
		t.Errorf("body was corrupted by error handler: %q", rec.Body.String())
	}
}

func TestIsProductionEnv(t *testing.T) {
	origENV := os.Getenv("ENV")
	origAppENV := os.Getenv("APP_ENV")
	defer func() {
		os.Setenv("ENV", origENV)
		os.Setenv("APP_ENV", origAppENV)
	}()

	os.Setenv("ENV", "")
	os.Setenv("APP_ENV", "")
	if isProductionEnv() {
		t.Errorf("expected isProductionEnv to be false when env is empty")
	}

	os.Setenv("ENV", "production")
	if !isProductionEnv() {
		t.Errorf("expected isProductionEnv to be true when ENV=production")
	}

	os.Setenv("ENV", "")
	os.Setenv("APP_ENV", "prod")
	if !isProductionEnv() {
		t.Errorf("expected isProductionEnv to be true when APP_ENV=prod")
	}
}

func TestRouteGroup_Hidden(t *testing.T) {
	app := New(Config{
		TypeScriptURL:    "/types.d.ts",
		TypeScriptSDKURL: "/sdk.ts",
	})

	// 1. Public group - routes should be documented
	public := app.Group("/public")
	public.Get("/info", func(ctx *Context) error { return ctx.Send("public info") })

	// 2. Hidden group created fluently - all routes should be hidden from docs & TS
	admin := app.Group("/admin").Hidden()
	admin.Get("/users", func(ctx *Context) error { return ctx.Send("users list") })
	admin.Post("/ban", func(ctx *Context) error { return ctx.Send("user banned") })

	// 3. Nested child group under hidden group should automatically inherit hidden state
	internal := admin.Group("/internal")
	internal.Get("/metrics", func(ctx *Context) error { return ctx.Send("internal metrics") })

	// 4. Group that calls .Hidden() after registering some routes
	late := app.Group("/late")
	late.Get("/early_route", func(ctx *Context) error { return ctx.Send("early") })
	late.Hidden()
	late.Get("/after_route", func(ctx *Context) error { return ctx.Send("after") })

	// Verify all routes execute properly via HTTP
	routesToTest := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/public/info", "public info"},
		{"GET", "/admin/users", "users list"},
		{"POST", "/admin/ban", "user banned"},
		{"GET", "/admin/internal/metrics", "internal metrics"},
		{"GET", "/late/early_route", "early"},
		{"GET", "/late/after_route", "after"},
	}

	for _, rt := range routesToTest {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest(rt.method, rt.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s %s expected 200, got %d", rt.method, rt.path, rec.Code)
		}
		if rec.Body.String() != rt.want {
			t.Errorf("%s %s expected %q, got %q", rt.method, rt.path, rt.want, rec.Body.String())
		}
	}

	// Verify OpenAPI spec
	spec := app.openAPISpec()
	if _, ok := spec.Paths["/public/info"]; !ok {
		t.Errorf("expected /public/info in OpenAPI spec")
	}

	hiddenPaths := []string{
		"/admin/users",
		"/admin/ban",
		"/admin/internal/metrics",
		"/late/early_route",
		"/late/after_route",
	}

	for _, p := range hiddenPaths {
		if _, ok := spec.Paths[p]; ok {
			t.Errorf("hidden path %q unexpectedly found in OpenAPI spec", p)
		}
	}

	// Verify TypeScript generation
	ts := app.GenerateTypeScript()
	if !strings.Contains(ts, "/public/info") {
		t.Errorf("expected /public/info in TypeScript definitions")
	}
	for _, p := range hiddenPaths {
		if strings.Contains(ts, p) {
			t.Errorf("hidden path %q unexpectedly found in TypeScript definitions", p)
		}
	}

	// Verify TypeScript SDK generation
	sdk := app.GenerateTypeScriptSDK()
	if !strings.Contains(sdk, "/public/info") {
		t.Errorf("expected /public/info in TypeScript SDK")
	}
	for _, p := range hiddenPaths {
		if strings.Contains(sdk, p) {
			t.Errorf("hidden path %q unexpectedly found in TypeScript SDK", p)
		}
	}
}

func TestSetHeaderSanitizesControlChars(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := NewContext(rec, httptest.NewRequest("GET", "/", nil), nil)
	ctx.SetHeader("X-Custom\r\n-Header", "val1\r\nX-Injected: evil\x00value")

	got := rec.Header().Get("X-Custom-Header")
	if got != "val1X-Injected: evilvalue" {
		t.Fatalf("expected sanitized header value, got %q", got)
	}
	if rec.Header().Get("X-Injected") != "" {
		t.Fatal("header injection must be prevented")
	}
}

func TestSanitizeFilenameAndSaveUploadedFile(t *testing.T) {
	cases := map[string]string{
		"avatar.png":                     "avatar.png",
		"../../etc/passwd":               "passwd",
		`..\..\Windows\System32\cmd.exe`: "cmd.exe",
		".env":                           "",
		"..":                             "",
		".":                              "",
		"":                               "",
		"///":                            "",
		"file\x00name\r\n.txt":           "filename.txt",
	}
	for input, want := range cases {
		if got := SanitizeFilename(input); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", input, got, want)
		}
	}

	tmpDir := t.TempDir()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "../../outside.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("safe-content"))
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	ctx := NewContext(httptest.NewRecorder(), req, nil)

	fh, err := ctx.FormFile("file")
	if err != nil {
		t.Fatal(err)
	}

	savedPath, err := ctx.SaveUploadedFile(fh, tmpDir)
	if err != nil {
		t.Fatalf("SaveUploadedFile failed: %v", err)
	}
	if filepath.Dir(savedPath) != filepath.Clean(tmpDir) || filepath.Base(savedPath) != "outside.txt" {
		t.Fatalf("file escaped destination directory: %q (want inside %q)", savedPath, tmpDir)
	}
	data, err := os.ReadFile(savedPath)
	if err != nil || string(data) != "safe-content" {
		t.Fatalf("unexpected saved file content: %q (%v)", string(data), err)
	}

	// Hidden dotfile override must be rejected
	if _, err := ctx.SaveUploadedFile(fh, tmpDir, ".htaccess"); err == nil {
		t.Fatal("expected error when saving hidden dotfile")
	}
}
