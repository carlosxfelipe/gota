package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func timeoutApp(d time.Duration, h HandlerFunc) *App {
	app := New(Config{DisableDocs: true})
	app.Use(Timeout(d))
	app.Get("/x", h)
	return app
}

func serve(app *App) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	return rec
}

func TestTimeoutReturns503WhenHandlerHonorsContext(t *testing.T) {
	app := timeoutApp(20*time.Millisecond, func(ctx *Context) error {
		select {
		case <-ctx.Request.Context().Done():
			return ctx.Request.Context().Err()
		case <-time.After(2 * time.Second):
			return ctx.Send("too late")
		}
	})
	start := time.Now()
	rec := serve(app)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if time.Since(start) > time.Second {
		t.Error("handler was not cancelled by the deadline")
	}
}

func TestTimeoutWrappedDeadlineErrorAnd5xxOnly(t *testing.T) {
	app := timeoutApp(time.Second, func(ctx *Context) error {
		return errors.Join(errors.New("db failed"), context.DeadlineExceeded)
	})
	if rec := serve(app); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("wrapped DeadlineExceeded should map to 503, got %d", rec.Code)
	}

	app = timeoutApp(time.Second, func(ctx *Context) error { return NotFound("nope") })
	if rec := serve(app); rec.Code != http.StatusNotFound {
		t.Errorf("unrelated errors must pass through, got %d", rec.Code)
	}
}

func TestTimeoutFastHandlerAndRestoredRequest(t *testing.T) {
	var deadlineSeen bool
	app := timeoutApp(time.Second, func(ctx *Context) error {
		_, deadlineSeen = ctx.Request.Context().Deadline()
		return ctx.Send("ok")
	})
	rec := serve(app)
	if rec.Code != 200 || rec.Body.String() != "ok" || !deadlineSeen {
		t.Errorf("unexpected: %d %q deadline=%v", rec.Code, rec.Body.String(), deadlineSeen)
	}
}

func TestTimeoutDoesNotOverrideWrittenResponse(t *testing.T) {
	app := timeoutApp(10*time.Millisecond, func(ctx *Context) error {
		_ = ctx.Send("partial")
		<-ctx.Request.Context().Done()
		return ctx.Request.Context().Err()
	})
	// The status already sent is kept (no 503 is produced after the response started).
	if rec := serve(app); rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), "partial") {
		t.Errorf("already written response must be kept, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestTimeoutPanicsOnInvalidDuration(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for non-positive duration")
		}
	}()
	Timeout(0)
}
