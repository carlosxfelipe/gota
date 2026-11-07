package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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

	// trustedProxies is the number of trusted proxies in front of the app (X-Forwarded-For depth).
	trustedProxies int

	// disallowUnknownFields makes DecodeJSON / BindJson reject unknown JSON object keys.
	disallowUnknownFields bool

	// maxBodyBytes caps what Body() will read (<= 0 disables). Defaults to DefaultMaxBodyBytes so a
	// Context created outside App.ServeHTTP is still protected against unbounded request bodies.
	maxBodyBytes int64

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
		maxBodyBytes:       DefaultMaxBodyBytes,
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
//
// X-Forwarded-For is read from the right: each trusted proxy appends the address it
// received the request from, so the leftmost entries are client-controlled. With
// Config.TrustedProxyCount = N, the Nth entry from the right is used (default 1).
// Malformed addresses fall back to RemoteAddr.
func (c *Context) ClientIP() string {
	if c.trustProxy {
		if xff := c.Header("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			n := c.trustedProxies
			if n < 1 {
				n = 1
			}
			if idx := len(parts) - n; idx >= 0 {
				if ip := net.ParseIP(strings.TrimSpace(parts[idx])); ip != nil {
					return ip.String()
				}
			}
		} else if xrip := strings.TrimSpace(c.Header("X-Real-IP")); xrip != "" {
			if ip := net.ParseIP(xrip); ip != nil {
				return ip.String()
			}
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
// Returns DefaultMaxMultipartMemory when unset (0). A negative value disables the memory
// limit (returns math.MaxInt64), allowing the form to be kept in memory up to MaxBodyBytes.
func (c *Context) MaxMultipartMemory() int64 {
	if c.maxMultipartMemory == 0 {
		return DefaultMaxMultipartMemory
	}
	if c.maxMultipartMemory < 0 {
		return math.MaxInt64
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

// SanitizeFilename strips directory paths (both Unix '/' and Windows '\\'), control characters,
// and null bytes from a user-supplied filename. It returns an empty string if the resulting
// name is empty, ".", "..", or starts with a dot (hidden file).
func SanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if idx := strings.LastIndexByte(name, '/'); idx != -1 {
		name = name[idx+1:]
	}
	var sb strings.Builder
	sb.Grow(len(name))
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if ch < 0x20 || ch == 0x7f || ch == '/' || ch == '\\' || ch == ':' {
			continue
		}
		sb.WriteByte(ch)
	}
	clean := strings.TrimSpace(sb.String())
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, ".") {
		return ""
	}
	return clean
}

// SaveUploadedFile safely saves a multipart FileHeader inside dstDir, neutralizing path
// traversal attempts ("../", backslashes, control characters, or hidden dotfiles).
// An optional overrideName can be passed to specify the target filename instead of fh.Filename.
// Returns the full cleaned destination path written on disk.
func (c *Context) SaveUploadedFile(fh *multipart.FileHeader, dstDir string, overrideName ...string) (string, error) {
	if fh == nil {
		return "", errors.New("gota: nil multipart FileHeader")
	}
	if strings.TrimSpace(dstDir) == "" {
		return "", errors.New("gota: destination directory cannot be empty")
	}

	rawName := fh.Filename
	if len(overrideName) > 0 && overrideName[0] != "" {
		rawName = overrideName[0]
	}
	safeName := SanitizeFilename(rawName)
	if safeName == "" {
		return "", errors.New("gota: invalid or unsafe upload filename")
	}

	cleanDir := filepath.Clean(dstDir)
	targetPath := filepath.Join(cleanDir, safeName)
	rel, err := filepath.Rel(cleanDir, targetPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Base(targetPath) != safeName {
		return "", errors.New("gota: upload path traversal detected")
	}

	if err := os.MkdirAll(cleanDir, 0750); err != nil {
		return "", err
	}

	src, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	dst, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return targetPath, nil
}

// Body reads and caches the raw HTTP request body bytes.
// At most Config.MaxBodyBytes are read (DefaultMaxBodyBytes outside App.ServeHTTP); larger bodies
// fail with *http.MaxBytesError, which Gota maps to 413 Request Entity Too Large.
func (c *Context) Body() ([]byte, error) {
	if c.bodyRead {
		return c.bodyBytes, nil
	}
	if c.Request.Body == nil {
		c.bodyBytes = []byte{}
		c.bodyRead = true
		return c.bodyBytes, nil
	}
	var data []byte
	var err error
	if limit := c.maxBodyBytes; limit > 0 {
		data, err = io.ReadAll(io.LimitReader(c.Request.Body, limit+1))
		if err == nil && int64(len(data)) > limit {
			return nil, &http.MaxBytesError{Limit: limit}
		}
	} else {
		data, err = io.ReadAll(c.Request.Body)
	}
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
	return c.DecodeJSON(data, target)
}

// DecodeJSON decodes JSON data into target like json.Unmarshal. When Config.DisallowUnknownFields
// is enabled, unknown object keys are rejected (json: unknown field "x").
func (c *Context) DecodeJSON(data []byte, target any) error {
	if !c.disallowUnknownFields {
		return json.Unmarshal(data, target)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	// Like json.Unmarshal, reject anything after the first JSON value.
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("invalid character after top-level value")
	}
	return nil
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

func sanitizeHeaderValue(s string) string {
	if !strings.ContainsAny(s, "\r\n\x00") {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != '\r' && c != '\n' && c != 0 {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// SetHeader sets an HTTP header on the response after stripping CR, LF, and NUL bytes. Chainable.
func (c *Context) SetHeader(key, val string) *Context {
	c.ResponseWriter.Header().Set(sanitizeHeaderValue(key), sanitizeHeaderValue(val))
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
// Never pass user-controlled input to it: use RedirectLocal to avoid open redirects.
func (c *Context) Redirect(targetURL string, code ...int) error {
	status := http.StatusFound
	if len(code) > 0 {
		status = code[0]
	}
	http.Redirect(c.ResponseWriter, c.Request, targetURL, status)
	c.written = true
	return nil
}

// IsLocalRedirectTarget reports whether target is a same-site absolute path that is safe to
// redirect to: it must start with a single "/" and contain no backslashes or control characters
// (browsers treat "//host", "/\\host" and "/\t/host" as protocol-relative URLs to another host).
func IsLocalRedirectTarget(target string) bool {
	if len(target) == 0 || target[0] != '/' {
		return false
	}
	if len(target) > 1 && (target[1] == '/' || target[1] == '\\') {
		return false
	}
	for i := 0; i < len(target); i++ {
		if c := target[i]; c < 0x20 || c == 0x7f || c == '\\' {
			return false
		}
	}
	return true
}

// RedirectLocal redirects to a same-site path only, rejecting anything else with 400 Bad Request.
// Use it instead of Redirect whenever the target comes from user input (e.g. a "next" or
// "returnUrl" parameter) to prevent open redirect attacks.
func (c *Context) RedirectLocal(target string, code ...int) error {
	if !IsLocalRedirectTarget(target) {
		return BadRequest("invalid redirect target")
	}
	return c.Redirect(target, code...)
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

// sseEventName strips CR and LF so an event name can never terminate its own field.
func sseEventName(event string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(event)
}

// DefaultSSEWriteTimeout bounds the time spent writing and flushing a single SSE frame
// so a slow-reading or stalled client cannot block a streaming goroutine indefinitely.
const DefaultSSEWriteTimeout = 10 * time.Second

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
	// Newlines terminate SSE fields, so an unsanitized event name or data line containing
	// them would let user-controlled content inject extra fields or forged events.
	if event = sseEventName(event); event != "" {
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
		bytes, err := json.Marshal(d)
		if err != nil {
			return err
		}
		payload = string(bytes)
	}
	// Multi-line data is split into one "data:" field per line (the client rejoins them with "\n").
	payload = strings.ReplaceAll(payload, "\r\n", "\n")
	payload = strings.ReplaceAll(payload, "\r", "\n")
	for _, line := range strings.Split(payload, "\n") {
		sb.WriteString("data: ")
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')

	rc := http.NewResponseController(c.ResponseWriter)
	_ = rc.SetWriteDeadline(time.Now().Add(DefaultSSEWriteTimeout))
	defer func() {
		_ = rc.SetWriteDeadline(time.Time{})
	}()

	if _, err := c.ResponseWriter.Write([]byte(sb.String())); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// Empty represents an empty Request or Response in REPR pattern.
type Empty struct{}
