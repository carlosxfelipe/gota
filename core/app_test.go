package core

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
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
