package gota

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Map is a convenient alias for map[string]any, matching Kite's syntax.
type Map map[string]any

// Context encapsulates both the incoming HTTP request and response writer,
// providing chainable methods for request inspection and response emission.
type Context struct {
	ResponseWriter http.ResponseWriter
	Request        *http.Request
	Params         map[string]string

	statusCode int
	written    bool
	bodyBytes  []byte
	bodyRead   bool

	mu    sync.RWMutex
	store map[string]any
}

// newContext creates a fresh Context wrapping http.ResponseWriter and *http.Request.
func newContext(w http.ResponseWriter, r *http.Request, params map[string]string) *Context {
	if params == nil {
		params = make(map[string]string)
	}
	return &Context{
		ResponseWriter: w,
		Request:        r,
		Params:         params,
		statusCode:     http.StatusOK,
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
	return c
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

// Empty represents an empty Request or Response in REPR pattern.
type Empty struct{}
