package core

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Map is a convenient alias for map[string]any.
type Map map[string]any

// Context encapsulates both the incoming HTTP request and response writer.
type Context struct {
	ResponseWriter http.ResponseWriter
	Request        *http.Request
	Params         map[string]string

	statusCode int
	statusSet  bool
	written    bool
	bodyBytes  []byte
	bodyRead   bool

	// trustProxy enables reading the client IP from X-Forwarded-For / X-Real-IP.
	trustProxy bool

	// maxMultipartMemory limits memory in bytes used to parse multipart forms.
	maxMultipartMemory int64

	mu    sync.RWMutex
	store map[string]any
}

// NewContext creates a fresh Context wrapping http.ResponseWriter and *http.Request.
func NewContext(w http.ResponseWriter, r *http.Request, params map[string]string) *Context {
	if params == nil {
		params = make(map[string]string)
	}
	return &Context{
		ResponseWriter:     w,
		Request:            r,
		Params:             params,
		statusCode:         http.StatusOK,
		maxMultipartMemory: DefaultMaxMultipartMemory,
	}
}

// Param returns a route path parameter by name, or an empty string if not found.
func (c *Context) Param(name string) string {
	return c.Params[name]
}

// HasParam checks if a route path parameter exists.
func (c *Context) HasParam(name string) bool {
	_, ok := c.Params[name]
	return ok
}

// ParamInt returns a route parameter parsed as integer, or def on failure.
func (c *Context) ParamInt(name string, def int) int {
	val, err := strconv.Atoi(c.Params[name])
	if err != nil {
		return def
	}
	return val
}

// ParamInt64 returns a route parameter parsed as int64, or def on failure.
func (c *Context) ParamInt64(name string, def int64) int64 {
	val, err := strconv.ParseInt(c.Params[name], 10, 64)
	if err != nil {
		return def
	}
	return val
}

// Query returns the first query parameter matching name, or empty string.
func (c *Context) Query(name string) string {
	return c.Request.URL.Query().Get(name)
}

// QueryDefault returns the query parameter or def if empty.
func (c *Context) QueryDefault(name, def string) string {
	if v := c.Query(name); v != "" {
		return v
	}
	return def
}

// QueryInt returns the query parameter parsed as int, or def on failure.
func (c *Context) QueryInt(name string, def int) int {
	v := c.Query(name)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

// QueryBool returns true if the query parameter is "1", "true", "yes", etc.
func (c *Context) QueryBool(name string, def bool) bool {
	v := strings.ToLower(c.Query(name))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "t" || v == "yes" || v == "y"
}

// Header returns the request header value for the given key.
func (c *Context) Header(name string) string {
	return c.Request.Header.Get(name)
}

// RequestID returns the request ID set by the RequestID middleware, or from X-Request-ID header.
func (c *Context) RequestID() string {
	if id, ok := c.Get("request_id"); ok {
		if s, ok := id.(string); ok && s != "" {
			return s
		}
	}
	if id := c.Header("X-Request-ID"); validRequestID(id) {
		return id
	}
	return ""
}

// ClientIP returns the client IP address.
// X-Forwarded-For and X-Real-IP are only honored when Config.TrustProxyHeaders is enabled
// (i.e. the app runs behind a trusted reverse proxy); otherwise RemoteAddr is used,
// since those headers can be freely forged by any client.
func (c *Context) ClientIP() string {
	if c.trustProxy {
		if xff := c.Header("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0])
			}
		}
		if xrip := c.Header("X-Real-IP"); xrip != "" {
			return strings.TrimSpace(xrip)
		}
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err == nil {
		return host
	}
	return c.Request.RemoteAddr
}

// Form returns the form value from POST/PUT body or query string.
func (c *Context) Form(name string) string {
	return c.Request.FormValue(name)
}

// MaxMultipartMemory returns the maximum memory in bytes used to parse multipart forms.
func (c *Context) MaxMultipartMemory() int64 {
	if c.maxMultipartMemory <= 0 {
		return DefaultMaxMultipartMemory
	}
	return c.maxMultipartMemory
}

// SetMaxMultipartMemory configures the maximum memory in bytes used by ParseMultipartForm for this context.
func (c *Context) SetMaxMultipartMemory(limit int64) {
	c.maxMultipartMemory = limit
}

// FormFile returns the first file for the provided form key.
func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	if c.Request.MultipartForm == nil {
		if err := c.Request.ParseMultipartForm(c.MaxMultipartMemory()); err != nil {
			return nil, err
		}
	}
	file, header, err := c.Request.FormFile(name)
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	return header, nil
}

// MultipartForm returns the parsed multipart form, including values and file headers.
func (c *Context) MultipartForm() (*multipart.Form, error) {
	if c.Request.MultipartForm == nil {
		if err := c.Request.ParseMultipartForm(c.MaxMultipartMemory()); err != nil {
			return nil, err
		}
	}
	return c.Request.MultipartForm, nil
}

// Body reads and caches the raw HTTP request body bytes.
func (c *Context) Body() ([]byte, error) {
	if c.bodyRead {
		return c.bodyBytes, nil
	}
	if c.Request.Body == nil {
		c.bodyBytes = []byte{}
		c.bodyRead = true
		return c.bodyBytes, nil
	}
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.bodyBytes = data
	c.bodyRead = true
	return c.bodyBytes, nil
}

// BindJson decodes the JSON request body into the target pointer.
func (c *Context) BindJson(target any) error {
	data, err := c.Body()
	if err != nil {
		return fmt.Errorf("gota: failed to read body: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("gota: request body is empty")
	}
	return json.Unmarshal(data, target)
}

// Set stores a value scoped to this request context.
func (c *Context) Set(key string, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.store == nil {
		c.store = make(map[string]any)
	}
	c.store[key] = val
}

// Get retrieves a value scoped to this request context.
func (c *Context) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.store == nil {
		return nil, false
	}
	val, ok := c.store[key]
	return val, ok
}

// Cookie returns the named cookie from the request.
func (c *Context) Cookie(name string) (*http.Cookie, error) {
	return c.Request.Cookie(name)
}

// SetCookie adds a Set-Cookie header to the response.
func (c *Context) SetCookie(cookie *http.Cookie) *Context {
	http.SetCookie(c.ResponseWriter, cookie)
	return c
}

// Status sets the HTTP status code for the response. Chainable.
func (c *Context) Status(code int) *Context {
	c.statusCode = code
	c.statusSet = true
	return c
}

// StatusExplicit reports whether a status code was explicitly set via Status.
func (c *Context) StatusExplicit() bool {
	return c.statusSet
}

// StatusCode returns the currently configured HTTP status code.
func (c *Context) StatusCode() int {
	return c.statusCode
}

// SetHeader sets an HTTP header on the response. Chainable.
func (c *Context) SetHeader(key, val string) *Context {
	c.ResponseWriter.Header().Set(key, val)
	return c
}

// Written reports whether the response header has already been written.
func (c *Context) Written() bool {
	return c.written
}

func (c *Context) writeHeaderOnce() {
	if !c.written {
		c.ResponseWriter.WriteHeader(c.statusCode)
		c.written = true
	}
}

// Json serializes the provided data as JSON and writes it with status.
func (c *Context) Json(data any) error {
	c.SetHeader("Content-Type", "application/json; charset=utf-8")
	c.writeHeaderOnce()
	return json.NewEncoder(c.ResponseWriter).Encode(data)
}

// Send writes a plain text string response.
func (c *Context) Send(text string) error {
	if c.ResponseWriter.Header().Get("Content-Type") == "" {
		c.SetHeader("Content-Type", "text/plain; charset=utf-8")
	}
	c.writeHeaderOnce()
	_, err := c.ResponseWriter.Write([]byte(text))
	return err
}

// SendBytes writes raw bytes to the response.
func (c *Context) SendBytes(data []byte) error {
	c.writeHeaderOnce()
	_, err := c.ResponseWriter.Write(data)
	return err
}

// NoContent writes a 204 No Content response.
func (c *Context) NoContent() error {
	c.statusCode = http.StatusNoContent
	c.writeHeaderOnce()
	return nil
}

// Redirect sends an HTTP redirect to the specified URL.
func (c *Context) Redirect(targetURL string, code ...int) error {
	status := http.StatusFound
	if len(code) > 0 {
		status = code[0]
	}
	http.Redirect(c.ResponseWriter, c.Request, targetURL, status)
	c.written = true
	return nil
}

// Stream sends a streaming chunked response using http.Flusher.
// The callback `step` is invoked repeatedly until it returns false or the client disconnects.
func (c *Context) Stream(step func(w io.Writer) bool) error {
	flusher, ok := c.ResponseWriter.(http.Flusher)
	if !ok {
		return fmt.Errorf("gota: streaming unsupported by response writer")
	}

	c.writeHeaderOnce()
	ctx := c.Request.Context()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if !step(c.ResponseWriter) {
				flusher.Flush()
				return nil
			}
			flusher.Flush()
		}
	}
}

// SSEvent sends a Server-Sent Event formatted frame and flushes it immediately.
func (c *Context) SSEvent(event string, data any) error {
	if !c.written {
		c.SetHeader("Content-Type", "text/event-stream")
		c.SetHeader("Cache-Control", "no-cache")
		c.SetHeader("Connection", "keep-alive")
		c.writeHeaderOnce()
	}

	flusher, ok := c.ResponseWriter.(http.Flusher)
	if !ok {
		return fmt.Errorf("gota: streaming unsupported by response writer")
	}

	var sb strings.Builder
	if event != "" {
		sb.WriteString("event: ")
		sb.WriteString(event)
		sb.WriteByte('\n')
	}

	sb.WriteString("data: ")
	switch d := data.(type) {
	case string:
		sb.WriteString(d)
	case []byte:
		sb.Write(d)
	default:
		bytes, err := json.Marshal(d)
		if err != nil {
			return err
		}
		sb.Write(bytes)
	}
	sb.WriteString("\n\n")

	if _, err := c.ResponseWriter.Write([]byte(sb.String())); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// Empty represents an empty Request or Response in REPR pattern.
type Empty struct{}
