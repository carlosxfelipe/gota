package core

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// GzipOptions configures the Gzip middleware.
type GzipOptions struct {
	// Level is the gzip compression level (default: gzip.DefaultCompression).
	// Valid values are gzip.HuffmanOnly (-2) to gzip.BestCompression (9); anything else panics.
	Level int
	// MinLength is the minimum body size in bytes worth compressing (default: 1024).
	// Smaller responses are sent as is. Streaming responses (flushed early) skip this check.
	MinLength int
}

// DefaultGzipMinLength is the default GzipOptions.MinLength.
const DefaultGzipMinLength = 1024

// Gzip returns a middleware that gzip-compresses responses for clients that send
// "Accept-Encoding: gzip". Only compressible content types (text/*, JSON, XML, JavaScript, SVG)
// are compressed; already-encoded responses, bodiless statuses, HEAD requests, Range requests and
// text/event-stream are left untouched. "Vary: Accept-Encoding" is always set.
//
// Security note (BREACH): compressing HTTPS responses that mix secrets (CSRF tokens, session data)
// with attacker-reflected input can leak the secret. Do not enable it on such endpoints.
func Gzip(opts ...GzipOptions) MiddlewareFunc {
	level := gzip.DefaultCompression
	minLength := DefaultGzipMinLength
	if len(opts) > 0 {
		if opts[0].Level != 0 {
			level = opts[0].Level
		}
		if opts[0].MinLength > 0 {
			minLength = opts[0].MinLength
		}
	}
	if level < gzip.HuffmanOnly || level > gzip.BestCompression {
		panic("gota: Gzip level must be between -2 and 9")
	}
	pool := &sync.Pool{New: func() any {
		w, _ := gzip.NewWriterLevel(nil, level)
		return w
	}}

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			h := ctx.ResponseWriter.Header()
			h.Add("Vary", "Accept-Encoding")

			if !acceptsGzip(ctx.Request.Header.Get("Accept-Encoding")) ||
				ctx.Request.Method == http.MethodHead ||
				ctx.Request.Header.Get("Range") != "" {
				return next(ctx)
			}

			orig := ctx.ResponseWriter
			gw := &gzipResponseWriter{ResponseWriter: orig, pool: pool, minLength: minLength}
			ctx.ResponseWriter = gw
			defer func() { ctx.ResponseWriter = orig }()

			err := next(ctx)
			gw.finish()
			return err
		}
	}
}

// acceptsGzip parses Accept-Encoding and reports whether gzip is acceptable (q > 0).
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") {
			continue
		}
		for _, p := range fields[1:] {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "q=") {
				if q, err := strconv.ParseFloat(p[2:], 64); err == nil && q <= 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}

func isCompressibleType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch {
	case ct == "text/event-stream":
		return false
	case strings.HasPrefix(ct, "text/"),
		strings.HasSuffix(ct, "+json"), strings.HasSuffix(ct, "+xml"),
		ct == "application/json", ct == "application/xml", ct == "application/javascript",
		ct == "application/x-ndjson", ct == "image/svg+xml":
		return true
	}
	return false
}

// gzipResponseWriter buffers the first minLength bytes to decide whether compressing is worthwhile.
type gzipResponseWriter struct {
	http.ResponseWriter
	pool      *sync.Pool
	minLength int

	status      int
	wroteHeader bool // WriteHeader was requested by the handler
	decided     bool
	gz          *gzip.Writer
	buf         []byte
}

func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.decided || w.wroteHeader {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code) // informational responses pass through
		return
	}
	w.status = code
	w.wroteHeader = true
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	if !w.decided {
		if !w.wroteHeader {
			w.status = http.StatusOK
			w.wroteHeader = true
		}
		w.buf = append(w.buf, p...)
		if len(w.buf) < w.minLength {
			return len(p), nil
		}
		w.decide(true)
		return len(p), nil
	}
	if w.gz != nil {
		return w.gz.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

// decide commits to compressing (or not), sends the header and flushes the buffer.
// force skips the MinLength check (used once the buffer reached it, or when flushing a stream).
func (w *gzipResponseWriter) decide(force bool) {
	w.decided = true
	h := w.ResponseWriter.Header()

	eligible := (force || len(w.buf) >= w.minLength) &&
		h.Get("Content-Encoding") == "" &&
		w.status != http.StatusNoContent && w.status != http.StatusNotModified &&
		w.status != http.StatusSwitchingProtocols && w.status >= 200
	if eligible {
		ct := h.Get("Content-Type")
		if ct == "" && len(w.buf) > 0 {
			ct = http.DetectContentType(w.buf)
			h.Set("Content-Type", ct)
		}
		eligible = isCompressibleType(ct)
	}

	if eligible {
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")
		w.gz = w.pool.Get().(*gzip.Writer)
		w.gz.Reset(w.ResponseWriter)
	}
	w.ResponseWriter.WriteHeader(w.status)
	if len(w.buf) > 0 {
		if w.gz != nil {
			_, _ = w.gz.Write(w.buf)
		} else {
			_, _ = w.ResponseWriter.Write(w.buf)
		}
		w.buf = nil
	}
}

// Flush implements http.Flusher so streaming handlers keep working.
func (w *gzipResponseWriter) Flush() {
	if !w.decided {
		if !w.wroteHeader {
			return
		}
		w.decide(true)
	}
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// finish sends any buffered response and finalizes the gzip stream.
func (w *gzipResponseWriter) finish() {
	if !w.decided && w.wroteHeader {
		w.decide(false)
	}
	if w.gz != nil {
		_ = w.gz.Close()
		w.pool.Put(w.gz)
		w.gz = nil
	}
}
