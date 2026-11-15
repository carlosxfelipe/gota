package repr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const (
	DefaultMaxBodyBytes       int64         = 10 << 20 // 10 MiB
	DefaultMaxMultipartMemory int64         = 32 << 20 // 32 MiB
	DefaultSSEWriteTimeout    time.Duration = 10 * time.Second
)

// Empty represents an empty Request or Response in REPR endpoints.
type Empty struct{}

// Context provides HTTP request/response helpers for REPR handlers when needed
// beyond the typed Req and Res structs.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request

	statusCode            int
	statusSet             bool
	written               bool
	bodyBytes             []byte
	bodyRead              bool
	disallowUnknownFields bool
	maxBodyBytes          int64
	maxMultipartMemory    int64
}

// NewContext constructs a Context wrapping w and r with safe default limits.
func NewContext(w http.ResponseWriter, r *http.Request) *Context {
	return &Context{
		Writer:             w,
		Request:            r,
		statusCode:         http.StatusOK,
		maxBodyBytes:       DefaultMaxBodyBytes,
		maxMultipartMemory: DefaultMaxMultipartMemory,
	}
}

// Context returns the underlying request context.Context.
func (c *Context) Context() context.Context {
	return c.Request.Context()
}

// Param returns a URL route parameter by name (extracted via chi).
func (c *Context) Param(name string) string {
	return chi.URLParam(c.Request, name)
}

// Header returns the request header value for key.
func (c *Context) Header(key string) string {
	return c.Request.Header.Get(key)
}

// RequestID returns the request ID from chi/middleware or the X-Request-ID header.
func (c *Context) RequestID() string {
	if id := middleware.GetReqID(c.Request.Context()); id != "" {
		return id
	}
	if id := c.Header("X-Request-ID"); isValidRequestID(id) {
		return id
	}
	return ""
}

func isValidRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x20 || id[i] == 0x7f {
			return false
		}
	}
	return true
}

// Cookie returns the named cookie from the request.
func (c *Context) Cookie(name string) (*http.Cookie, error) {
	return c.Request.Cookie(name)
}

// SetCookie adds a Set-Cookie header to the response.
func (c *Context) SetCookie(cookie *http.Cookie) *Context {
	http.SetCookie(c.Writer, cookie)
	return c
}

// Status sets the HTTP status code for the response. Chainable.
func (c *Context) Status(code int) *Context {
	c.statusCode = code
	c.statusSet = true
	return c
}

// StatusExplicit reports whether Status was explicitly called by the handler.
func (c *Context) StatusExplicit() bool {
	return c.statusSet
}

// StatusCode returns the currently configured HTTP status code.
func (c *Context) StatusCode() int {
	return c.statusCode
}

// Written reports whether the response headers have already been written.
func (c *Context) Written() bool {
	return c.written
}

func sanitizeHeaderValue(s string) string {
	if !strings.ContainsAny(s, "\r\n\x00") {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if ch := s[i]; ch != '\r' && ch != '\n' && ch != 0 {
			sb.WriteByte(ch)
		}
	}
	return sb.String()
}

// SetHeader sets a response header after stripping CR, LF, and NUL bytes.
func (c *Context) SetHeader(key, val string) *Context {
	c.Writer.Header().Set(sanitizeHeaderValue(key), sanitizeHeaderValue(val))
	return c
}

func (c *Context) writeHeaderOnce() {
	if !c.written {
		c.Writer.WriteHeader(c.statusCode)
		c.written = true
	}
}

// Body reads and caches the request body up to maxBodyBytes.
func (c *Context) Body() ([]byte, error) {
	if c.bodyRead {
		return c.bodyBytes, nil
	}
	if c.Request.Body == nil {
		c.bodyRead = true
		return nil, nil
	}
	reader := io.Reader(c.Request.Body)
	if c.maxBodyBytes > 0 {
		reader = http.MaxBytesReader(c.Writer, c.Request.Body, c.maxBodyBytes)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	c.bodyBytes = data
	c.bodyRead = true
	return data, nil
}

// DecodeJSON unmarshals data into target, honoring DisallowUnknownFields when enabled.
func (c *Context) DecodeJSON(data []byte, target any) error {
	if !c.disallowUnknownFields {
		return json.Unmarshal(data, target)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("invalid character after top-level value")
	}
	return nil
}

// JSON serializes data as JSON and writes the response.
func (c *Context) JSON(data any) error {
	c.SetHeader("Content-Type", "application/json; charset=utf-8")
	c.writeHeaderOnce()
	return json.NewEncoder(c.Writer).Encode(data)
}

// NoContent writes a 204 No Content response.
func (c *Context) NoContent() error {
	c.statusCode = http.StatusNoContent
	c.writeHeaderOnce()
	return nil
}

// Redirect sends an HTTP redirect to targetURL.
func (c *Context) Redirect(targetURL string, code ...int) error {
	status := http.StatusFound
	if len(code) > 0 {
		status = code[0]
	}
	http.Redirect(c.Writer, c.Request, targetURL, status)
	c.written = true
	return nil
}

// IsLocalRedirectTarget reports whether target is a safe same-site path starting with a single "/".
func IsLocalRedirectTarget(target string) bool {
	if len(target) == 0 || target[0] != '/' {
		return false
	}
	if len(target) > 1 && (target[1] == '/' || target[1] == '\\') {
		return false
	}
	for i := 0; i < len(target); i++ {
		if ch := target[i]; ch < 0x20 || ch == 0x7f || ch == '\\' {
			return false
		}
	}
	return true
}

// RedirectLocal redirects to a same-site path only, rejecting open redirect targets with 400.
func (c *Context) RedirectLocal(target string, code ...int) error {
	if !IsLocalRedirectTarget(target) {
		return BadRequest("invalid redirect target")
	}
	return c.Redirect(target, code...)
}

// Stream sends a chunked streaming response using http.Flusher.
func (c *Context) Stream(step func(w io.Writer) bool) error {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("repr: streaming unsupported by response writer")
	}
	c.writeHeaderOnce()
	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if !step(c.Writer) {
				flusher.Flush()
				return nil
			}
			flusher.Flush()
		}
	}
}

// SSEvent sends a Server-Sent Event frame and flushes it immediately.
func (c *Context) SSEvent(event string, data any) error {
	if !c.written {
		c.SetHeader("Content-Type", "text/event-stream")
		c.SetHeader("Cache-Control", "no-cache")
		c.SetHeader("Connection", "keep-alive")
		c.writeHeaderOnce()
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("repr: streaming unsupported by response writer")
	}

	var sb strings.Builder
	event = strings.NewReplacer("\r", "", "\n", "").Replace(event)
	if event != "" {
		sb.WriteString("event: ")
		sb.WriteString(event)
		sb.WriteByte('\n')
	}

	var payload string
	switch d := data.(type) {
	case string:
		payload = d
	case []byte:
		payload = string(d)
	default:
		b, err := json.Marshal(d)
		if err != nil {
			return err
		}
		payload = string(b)
	}

	payload = strings.ReplaceAll(payload, "\r\n", "\n")
	payload = strings.ReplaceAll(payload, "\r", "\n")
	for _, line := range strings.Split(payload, "\n") {
		sb.WriteString("data: ")
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')

	rc := http.NewResponseController(c.Writer)
	_ = rc.SetWriteDeadline(time.Now().Add(DefaultSSEWriteTimeout))
	defer func() {
		_ = rc.SetWriteDeadline(time.Time{})
	}()

	if _, err := c.Writer.Write([]byte(sb.String())); err != nil {
		return fmt.Errorf("repr: failed to write SSE frame: %w", err)
	}
	flusher.Flush()
	return nil
}
