package core

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gzipApp(opts GzipOptions, h HandlerFunc) *App {
	app := New(Config{DisableDocs: true})
	app.Use(Gzip(opts))
	app.Get("/x", h)
	return app
}

func gzipDo(app *App, mod func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	if mod != nil {
		mod(req)
	}
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func gunzip(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGzipCompressesLargeJSON(t *testing.T) {
	payload := strings.Repeat("hello ", 500)
	app := gzipApp(GzipOptions{}, func(ctx *Context) error { return ctx.Json(Map{"v": payload}) })
	rec := gzipDo(app, nil)
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Content-Length") != "" {
		t.Fatalf("expected gzip, got %v", rec.Header())
	}
	if !strings.Contains(gunzip(t, rec), payload) {
		t.Error("decompressed body mismatch")
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Error("expected Vary: Accept-Encoding")
	}
	if rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("content type changed: %q", rec.Header().Get("Content-Type"))
	}
}

func TestGzipSkips(t *testing.T) {
	big := strings.Repeat("a", 5000)
	cases := map[string]struct {
		h   HandlerFunc
		mod func(*http.Request)
	}{
		"small body":      {func(c *Context) error { return c.Send("tiny") }, nil},
		"no accept":       {func(c *Context) error { return c.Send(big) }, func(r *http.Request) { r.Header.Del("Accept-Encoding") }},
		"q=0":             {func(c *Context) error { return c.Send(big) }, func(r *http.Request) { r.Header.Set("Accept-Encoding", "gzip;q=0") }},
		"binary type":     {func(c *Context) error { c.SetHeader("Content-Type", "image/png"); return c.Send(big) }, nil},
		"already encoded": {func(c *Context) error { c.SetHeader("Content-Encoding", "br"); return c.Send(big) }, nil},
		"range":           {func(c *Context) error { return c.Send(big) }, func(r *http.Request) { r.Header.Set("Range", "bytes=0-10") }},
		"sse":             {func(c *Context) error { c.SetHeader("Content-Type", "text/event-stream"); return c.Send(big) }, nil},
	}
	for name, c := range cases {
		rec := gzipDo(gzipApp(GzipOptions{}, c.h), c.mod)
		if enc := rec.Header().Get("Content-Encoding"); enc == "gzip" {
			t.Errorf("%s: must not compress", name)
		}
		if name != "small body" && name != "no accept" && name != "q=0" && name != "range" && rec.Body.Len() == 0 {
			t.Errorf("%s: empty body", name)
		}
	}
	rec := gzipDo(gzipApp(GzipOptions{}, func(c *Context) error { return c.Send("tiny") }), nil)
	if rec.Body.String() != "tiny" {
		t.Errorf("small body must be passed through untouched, got %q", rec.Body.String())
	}
}

func TestGzipNoContentAndErrors(t *testing.T) {
	app := gzipApp(GzipOptions{}, func(c *Context) error { return c.NoContent() })
	rec := gzipDo(app, nil)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("204 must be untouched: %d %v", rec.Code, rec.Header())
	}

	app = gzipApp(GzipOptions{}, func(c *Context) error { return NotFound("missing") })
	rec = gzipDo(app, nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "missing") {
		t.Errorf("error response broken: %d %q", rec.Code, rec.Body.String())
	}
}

func TestGzipStreamingFlush(t *testing.T) {
	app := gzipApp(GzipOptions{}, func(c *Context) error {
		c.SetHeader("Content-Type", "application/x-ndjson")
		n := 0
		return c.Stream(func(w io.Writer) bool {
			_, _ = io.WriteString(w, "{\"row\":1}\n")
			n++
			return n < 3
		})
	})
	rec := gzipDo(app, nil)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected streamed gzip, got %v", rec.Header())
	}
	if got := gunzip(t, rec); got != strings.Repeat("{\"row\":1}\n", 3) {
		t.Errorf("stream body mismatch: %q", got)
	}
}

func TestGzipSniffsMissingContentType(t *testing.T) {
	app := gzipApp(GzipOptions{MinLength: 10}, func(c *Context) error {
		return c.SendBytes([]byte("<html><body>" + strings.Repeat("x", 100) + "</body></html>"))
	})
	rec := gzipDo(app, nil)
	if rec.Header().Get("Content-Encoding") != "gzip" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("expected sniffed html + gzip, got %v", rec.Header())
	}
}

func TestGzipInvalidLevelPanicsAndAcceptParsing(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic")
		}
	}()
	for in, want := range map[string]bool{"gzip": true, "br, GZIP": true, "gzip;q=0.5": true, "gzip;q=0": false, "deflate": false, "": false, "*": false} {
		if got := acceptsGzip(in); got != want {
			t.Errorf("acceptsGzip(%q)=%v want %v", in, got, want)
		}
	}
	Gzip(GzipOptions{Level: 42})
}
