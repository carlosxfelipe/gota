package core

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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
	// The rightmost entry is the one appended by the trusted proxy; the left is client-forged.
	if ip := ctx.ClientIP(); ip != "70.41.3.18" {
		t.Errorf("expected 70.41.3.18 from X-Forwarded-For, got %q", ip)
	}
	ctx.trustedProxies = 2
	if ip := ctx.ClientIP(); ip != "203.0.113.195" {
		t.Errorf("expected 203.0.113.195 with 2 trusted proxies, got %q", ip)
	}
	ctx.trustedProxies = 5
	if ip := ctx.ClientIP(); ip != "192.0.2.9" {
		t.Errorf("expected RemoteAddr fallback when XFF is shorter than proxy count, got %q", ip)
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

func TestBasicAuthMiddleware(t *testing.T) {
	app := New(Config{})
	admin := app.Group("/admin")
	admin.Use(BasicAuth("admin", "s3cr3t", BasicAuthOptions{Realm: "AdminPanel"}))
	admin.Get("/dashboard", func(ctx *Context) error {
		u, _ := ctx.Get("user")
		return ctx.Send("welcome " + u.(string))
	})

	// 1. Missing credentials -> 401 + WWW-Authenticate
	rec1 := httptest.NewRecorder()
	app.ServeHTTP(rec1, httptest.NewRequest("GET", "/admin/dashboard", nil))
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec1.Code)
	}
	if got := rec1.Header().Get("WWW-Authenticate"); got != `Basic realm="AdminPanel"` {
		t.Errorf("expected WWW-Authenticate header, got %q", got)
	}

	// 2. Wrong password -> 401
	req2 := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req2.SetBasicAuth("admin", "wrong")
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", rec2.Code)
	}

	// 3. Valid credentials -> 200
	req3 := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req3.SetBasicAuth("admin", "s3cr3t")
	rec3 := httptest.NewRecorder()
	app.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK || rec3.Body.String() != "welcome admin" {
		t.Fatalf("expected 200 welcome admin, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// 4. Verify BasicAuth security scheme documented in OpenAPI
	spec := app.openAPISpec()
	if _, ok := spec.Components.SecuritySchemes["BasicAuth"]; !ok {
		t.Errorf("expected BasicAuth in components.securitySchemes, got %+v", spec.Components.SecuritySchemes)
	}
}

func TestBasicAuthWithRateLimit(t *testing.T) {
	app := New(Config{})
	app.Get("/secret", func(ctx *Context) error {
		return ctx.Send("ok")
	}).Middleware(BasicAuth("admin", "pass", BasicAuthOptions{
		AllowWeakCredentials: true,
		RateLimit: &RateLimiterOptions{
			Max:    2,
			Window: time.Minute,
		},
	}))

	// Attempt 1: Wrong password -> 401
	req1 := httptest.NewRequest("GET", "/secret", nil)
	req1.SetBasicAuth("admin", "wrong1")
	rec1 := httptest.NewRecorder()
	app.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec1.Code)
	}

	// Attempt 2: Wrong password -> 401
	req2 := httptest.NewRequest("GET", "/secret", nil)
	req2.SetBasicAuth("admin", "wrong2")
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec2.Code)
	}

	// Attempt 3: Limit reached -> 429 Too Many Requests (Brute-force blocked!)
	req3 := httptest.NewRequest("GET", "/secret", nil)
	req3.SetBasicAuth("admin", "pass")
	rec3 := httptest.NewRecorder()
	app.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", rec3.Code)
	}
	if rec3.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header on 429 response")
	}
}

func TestBasicAuth_WeakPasswordWarning(t *testing.T) {
	var logged []string
	BasicAuthWarningLogger = func(w string) {
		logged = append(logged, w)
	}
	defer func() { BasicAuthWarningLogger = nil }()

	// 1. Weak password emits warning
	_ = BasicAuth("admin", "admin123")
	if len(logged) == 0 || !strings.Contains(logged[0], "admin123") {
		t.Fatalf("expected warning for weak password, got %v", logged)
	}

	// 2. AllowWeakCredentials silences warning
	logged = nil
	_ = BasicAuth("admin", "admin123", BasicAuthOptions{AllowWeakCredentials: true})
	if len(logged) != 0 {
		t.Fatalf("expected no warning when AllowWeakCredentials is true, got %v", logged)
	}

	// 3. Empty password emits warning
	logged = nil
	_ = BasicAuth("admin", "")
	if len(logged) == 0 || !strings.Contains(logged[0], "empty") {
		t.Fatalf("expected warning for empty password, got %v", logged)
	}

	// 4. Same username and password emits warning
	logged = nil
	_ = BasicAuth("carlos", "carlos")
	if len(logged) == 0 {
		t.Fatalf("expected warning when password equals username")
	}

	// 5. Strong password emits no warning
	logged = nil
	_ = BasicAuth("admin", "Super_Str0ng!P@ssw0rd#2026")
	if len(logged) != 0 {
		t.Fatalf("expected no warning for strong password, got %v", logged)
	}
}

func TestBasicAuth_RequireTLS(t *testing.T) {
	app := New(Config{})
	app.Get("/secure", func(ctx *Context) error {
		return ctx.Send("ok")
	}).Middleware(BasicAuth("admin", "secret123", BasicAuthOptions{
		RequireTLS: true,
	}))

	// 1. Remote client over plain HTTP -> 403 Forbidden
	reqRemoteHTTP := httptest.NewRequest("GET", "/secure", nil)
	reqRemoteHTTP.RemoteAddr = "203.0.113.50:45678"
	reqRemoteHTTP.SetBasicAuth("admin", "secret123")
	recRemoteHTTP := httptest.NewRecorder()
	app.ServeHTTP(recRemoteHTTP, reqRemoteHTTP)
	if recRemoteHTTP.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for remote non-TLS request, got %d", recRemoteHTTP.Code)
	}
	if !strings.Contains(recRemoteHTTP.Body.String(), "HTTPS/TLS is required") {
		t.Errorf("expected TLS required error message, got %s", recRemoteHTTP.Body.String())
	}

	// 2. Localhost client over plain HTTP -> Allowed
	reqLocalHTTP := httptest.NewRequest("GET", "/secure", nil)
	reqLocalHTTP.RemoteAddr = "127.0.0.1:45678"
	reqLocalHTTP.SetBasicAuth("admin", "secret123")
	recLocalHTTP := httptest.NewRecorder()
	app.ServeHTTP(recLocalHTTP, reqLocalHTTP)
	if recLocalHTTP.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for local request, got %d", recLocalHTTP.Code)
	}

	// 3. Localhost IPv6 over plain HTTP -> Allowed
	reqLocalIPv6 := httptest.NewRequest("GET", "/secure", nil)
	reqLocalIPv6.RemoteAddr = "[::1]:45678"
	reqLocalIPv6.SetBasicAuth("admin", "secret123")
	recLocalIPv6 := httptest.NewRecorder()
	app.ServeHTTP(recLocalIPv6, reqLocalIPv6)
	if recLocalIPv6.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for local IPv6 request, got %d", recLocalIPv6.Code)
	}

	// 4. Remote client behind reverse proxy terminating HTTPS (X-Forwarded-Proto: https) -> Allowed
	reqProxyHTTPS := httptest.NewRequest("GET", "/secure", nil)
	reqProxyHTTPS.RemoteAddr = "203.0.113.50:45678"
	reqProxyHTTPS.Header.Set("X-Forwarded-Proto", "https")
	reqProxyHTTPS.SetBasicAuth("admin", "secret123")
	recProxyHTTPS := httptest.NewRecorder()
	app.ServeHTTP(recProxyHTTPS, reqProxyHTTPS)
	if recProxyHTTPS.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for proxy HTTPS request, got %d", recProxyHTTPS.Code)
	}

	// 5. Remote client with direct TLS -> Allowed
	reqDirectTLS := httptest.NewRequest("GET", "/secure", nil)
	reqDirectTLS.RemoteAddr = "203.0.113.50:45678"
	reqDirectTLS.TLS = &tls.ConnectionState{}
	reqDirectTLS.SetBasicAuth("admin", "secret123")
	recDirectTLS := httptest.NewRecorder()
	app.ServeHTTP(recDirectTLS, reqDirectTLS)
	if recDirectTLS.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for direct TLS request, got %d", recDirectTLS.Code)
	}
}

func TestBasicAuth_InsecureHTTPWarningInProduction(t *testing.T) {
	origENV := os.Getenv("ENV")
	defer os.Setenv("ENV", origENV)
	os.Setenv("ENV", "production")

	var warned string
	origLogger := BasicAuthWarningLogger
	BasicAuthWarningLogger = func(msg string) {
		warned = msg
	}
	defer func() {
		BasicAuthWarningLogger = origLogger
	}()

	app := New(Config{})
	app.Get("/data", func(ctx *Context) error { return ctx.Send("ok") }).
		Middleware(BasicAuth("admin", "strong_secret_password_123", BasicAuthOptions{
			RequireTLS: false,
		}))

	req := httptest.NewRequest("GET", "/data", nil)
	req.RemoteAddr = "198.51.100.22:12345"
	req.SetBasicAuth("admin", "strong_secret_password_123")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(warned, "Insecure BasicAuth attempt over HTTP without TLS") {
		t.Errorf("expected warning in production for insecure BasicAuth, got: %q", warned)
	}
}

func TestRateLimiterMiddleware(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(RateLimiter(RateLimiterOptions{
		Max:    2,
		Window: time.Minute,
	}))
	app.Get("/limited", func(ctx *Context) error {
		return ctx.Send("ok")
	})

	for i := 1; i <= 2; i++ {
		req := httptest.NewRequest("GET", "/limited", nil)
		req.RemoteAddr = "192.0.2.55:1234"
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
		if rec.Header().Get("X-RateLimit-Limit") != "2" {
			t.Errorf("expected X-RateLimit-Limit=2, got %q", rec.Header().Get("X-RateLimit-Limit"))
		}
	}

	// 3rd request from same IP exceeds Max=2 -> 429 Too Many Requests
	req3 := httptest.NewRequest("GET", "/limited", nil)
	req3.RemoteAddr = "192.0.2.55:1234"
	rec3 := httptest.NewRecorder()
	app.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", rec3.Code)
	}
	if rec3.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header on 429 response")
	}
}

func TestNormalizeIPKey(t *testing.T) {
	if got := normalizeIPKey("192.0.2.1"); got != "192.0.2.1" {
		t.Errorf("ipv4 should be unchanged, got %q", got)
	}
	a := normalizeIPKey("2001:db8:1:2:aaaa::1")
	b := normalizeIPKey("2001:db8:1:2:bbbb::9")
	if a != b || a != "2001:db8:1:2::/64" {
		t.Errorf("expected same /64 key, got %q and %q", a, b)
	}
}

func TestClientIPIgnoresInvalidForwarded(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.9:1"
	req.Header.Set("X-Forwarded-For", "not-an-ip")
	ctx := NewContext(httptest.NewRecorder(), req, nil)
	ctx.trustProxy = true
	if ip := ctx.ClientIP(); ip != "192.0.2.9" {
		t.Errorf("got %q", ip)
	}
}

func TestRateLimiterMaxKeysBoundsMemory(t *testing.T) {
	now := time.Now()
	buckets := map[string]*rateBucket{}
	for i := 0; i < 5; i++ {
		buckets[fmt.Sprintf("k%d", i)] = &rateBucket{tokens: 1, lastRefill: now.Add(-time.Duration(i) * time.Second)}
	}
	evictRateBuckets(buckets, now, time.Minute, 5)
	if len(buckets) != 4 {
		t.Fatalf("expected 4 buckets after evicting LRU, got %d", len(buckets))
	}
	if _, ok := buckets["k4"]; ok {
		t.Error("expected least recently used key k4 to be evicted")
	}
	// Idle buckets (older than window) are all dropped.
	evictRateBuckets(buckets, now.Add(2*time.Minute), time.Minute, 4)
	if len(buckets) != 0 {
		t.Errorf("expected idle buckets to be dropped, got %d", len(buckets))
	}
}

func TestRateLimiterRotatingKeysStayBounded(t *testing.T) {
	var i int
	mw := RateLimiter(RateLimiterOptions{Max: 1, Window: time.Hour, MaxKeys: 50, KeyFunc: func(*Context) string {
		i++
		return fmt.Sprintf("key-%d", i)
	}})
	h := mw(func(ctx *Context) error { return nil })
	for n := 0; n < 500; n++ {
		ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), nil)
		if err := h(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func captureStdLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	return &buf
}

func TestLoggerPreventsLogInjection(t *testing.T) {
	buf := captureStdLog(t)
	app := New(Config{DisableDocs: true})
	app.Use(Logger())
	app.Get("/*rest", func(ctx *Context) error { return ctx.Send("ok") })

	req := httptest.NewRequest("GET", "/a%0Agota:%20forged%20line%20%5B200%5D%1b%5B2J", nil)
	app.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	if strings.Count(out, "\n") != 1 {
		t.Errorf("expected exactly one log line, got %q", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("escape sequences must be neutralized, got %q", out)
	}
	if !strings.Contains(out, `\n`) {
		t.Errorf("newline should be rendered escaped, got %q", out)
	}
}

func TestHandlerErrorLogPreventsInjection(t *testing.T) {
	buf := captureStdLog(t)
	app := New(Config{DisableDocs: true})
	app.Get("/fail", func(ctx *Context) error { return errors.New("bad input\ngota: forged") })
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/fail", nil))

	if strings.Count(buf.String(), "\n") != 1 {
		t.Errorf("expected exactly one log line, got %q", buf.String())
	}
}

func TestQuoteLog(t *testing.T) {
	if got := quoteLog("a\r\nb"); got != `"a\r\nb"` {
		t.Errorf("got %s", got)
	}
}

func corsRequest(app *App, method, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/x", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func TestCORSWildcardSetsVaryOrigin(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(CORS())
	app.Get("/x", func(ctx *Context) error { return ctx.Send("ok") })

	for _, origin := range []string{"https://a.example", ""} {
		rec := corsRequest(app, "GET", origin)
		if rec.Header().Get("Vary") != "Origin" {
			t.Errorf("origin %q: expected Vary: Origin, got %q", origin, rec.Header().Get("Vary"))
		}
	}
	if corsRequest(app, "GET", "https://a.example").Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected wildcard allow-origin")
	}
}

func TestCORSDisallowedOriginGetsNoCORSHeaders(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(CORS(CORSOptions{
		AllowOrigins:  []string{"https://ok.example"},
		AllowMethods:  []string{"GET"},
		AllowHeaders:  []string{"Content-Type"},
		ExposeHeaders: []string{"X-Request-ID"},
		MaxAge:        60,
	}))
	app.Get("/x", func(ctx *Context) error { return ctx.Send("ok") })

	for _, method := range []string{"GET", "OPTIONS"} {
		rec := corsRequest(app, method, "https://evil.example")
		for h := range rec.Header() {
			if strings.HasPrefix(h, "Access-Control-") {
				t.Errorf("%s: disallowed origin must not receive %s", method, h)
			}
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Errorf("%s: expected Vary: Origin", method)
		}
	}

	rec := corsRequest(app, "OPTIONS", "https://ok.example")
	if rec.Header().Get("Access-Control-Allow-Methods") != "GET" ||
		rec.Header().Get("Access-Control-Allow-Origin") != "https://ok.example" ||
		rec.Header().Get("Access-Control-Expose-Headers") != "X-Request-ID" {
		t.Errorf("allowed origin missing CORS headers: %v", rec.Header())
	}
}

func TestCORSNullOriginNeverMatchesWildcardEntry(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(CORS(CORSOptions{AllowOrigins: []string{"*", "https://ok.example"}}))
	app.Get("/x", func(ctx *Context) error { return ctx.Send("ok") })

	if got := corsRequest(app, "GET", "null").Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("null origin must not be granted via wildcard entry, got %q", got)
	}
	if got := corsRequest(app, "GET", "https://any.example").Header().Get("Access-Control-Allow-Origin"); got != "https://any.example" {
		t.Errorf("expected echoed origin, got %q", got)
	}

	app2 := New(Config{DisableDocs: true})
	app2.Use(CORS(CORSOptions{AllowOrigins: []string{"null"}}))
	app2.Get("/x", func(ctx *Context) error { return ctx.Send("ok") })
	if got := corsRequest(app2, "GET", "null").Header().Get("Access-Control-Allow-Origin"); got != "null" {
		t.Errorf("explicitly listed null origin should be allowed, got %q", got)
	}
}

func TestIPWhitelistMiddleware(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(IPWhitelist("192.168.1.0/24", "10.0.0.1", "2001:db8::/32"))
	app.Get("/internal", func(ctx *Context) error { return ctx.Send("ok") })

	// 1. Matching CIDR subnet -> 200 OK
	req1 := httptest.NewRequest("GET", "/internal", nil)
	req1.RemoteAddr = "192.168.1.55:12345"
	rec1 := httptest.NewRecorder()
	app.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 for 192.168.1.55, got %d", rec1.Code)
	}

	// 2. Exact IP match -> 200 OK
	req2 := httptest.NewRequest("GET", "/internal", nil)
	req2.RemoteAddr = "10.0.0.1:54321"
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 for 10.0.0.1, got %d", rec2.Code)
	}

	// 3. IPv6 CIDR match -> 200 OK
	reqIPv6 := httptest.NewRequest("GET", "/internal", nil)
	reqIPv6.RemoteAddr = "[2001:db8:ffff::1]:54321"
	recIPv6 := httptest.NewRecorder()
	app.ServeHTTP(recIPv6, reqIPv6)
	if recIPv6.Code != http.StatusOK {
		t.Fatalf("expected 200 for IPv6 in 2001:db8::/32, got %d", recIPv6.Code)
	}

	// 4. Disallowed IP -> 403 Forbidden
	reqBad := httptest.NewRequest("GET", "/internal", nil)
	reqBad.RemoteAddr = "203.0.113.195:12345"
	recBad := httptest.NewRecorder()
	app.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for 203.0.113.195, got %d", recBad.Code)
	}

	// 5. Localhost loopback allowed by default
	reqLocal := httptest.NewRequest("GET", "/internal", nil)
	reqLocal.RemoteAddr = "127.0.0.1:12345"
	recLocal := httptest.NewRecorder()
	app.ServeHTTP(recLocal, reqLocal)
	if recLocal.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for localhost loopback, got %d", recLocal.Code)
	}

	// 6. When AllowLocalhost is explicitly false, loopback not in whitelist is rejected
	appStrict := New(Config{DisableDocs: true})
	appStrict.Use(IPWhitelistWithOptions(IPWhitelistOptions{AllowLocalhost: false}, "10.0.0.0/8"))
	appStrict.Get("/strict", func(ctx *Context) error { return ctx.Send("ok") })

	recStrict := httptest.NewRecorder()
	reqStrict := httptest.NewRequest("GET", "/strict", nil)
	reqStrict.RemoteAddr = "127.0.0.1:12345"
	appStrict.ServeHTTP(recStrict, reqStrict)
	if recStrict.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when AllowLocalhost is false, got %d", recStrict.Code)
	}
}

func TestBearerTokenMiddleware(t *testing.T) {
	app := New(Config{})
	app.Get("/api/secret", func(ctx *Context) error {
		tok, _ := ctx.Get("token")
		return ctx.Send("token=" + tok.(string))
	}).Middleware(BearerToken("super-secret-dev-token", BearerTokenOptions{
		Realm:      "DeveloperArea",
		RequireTLS: true,
		RateLimit: &RateLimiterOptions{
			Max:    2,
			Window: time.Minute,
		},
	}))

	// 1. Missing Authorization header -> 401
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/api/secret", nil)
	req1.RemoteAddr = "127.0.0.1:1234"
	app.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec1.Code)
	}
	if rec1.Header().Get("WWW-Authenticate") != `Bearer realm="DeveloperArea"` {
		t.Errorf("expected WWW-Authenticate header, got %q", rec1.Header().Get("WWW-Authenticate"))
	}

	// 2. Wrong token -> 401
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/secret", nil)
	req2.RemoteAddr = "127.0.0.1:1234"
	req2.Header.Set("Authorization", "Bearer invalid-token")
	app.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong token, got %d", rec2.Code)
	}

	// 3. Attempt 3 with wrong token exceeds rate limit (Max=2) -> 429
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("GET", "/api/secret", nil)
	req3.RemoteAddr = "127.0.0.1:1234"
	req3.Header.Set("Authorization", "Bearer wrong-token-again")
	app.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", rec3.Code)
	}

	// 4. Remote non-TLS request with RequireTLS -> 403 Forbidden
	app2 := New(Config{})
	app2.Get("/tls-check", func(ctx *Context) error { return ctx.Send("ok") }).
		Middleware(BearerToken("valid-token", BearerTokenOptions{RequireTLS: true}))

	reqRemote := httptest.NewRequest("GET", "/tls-check", nil)
	reqRemote.RemoteAddr = "203.0.113.10:45678"
	reqRemote.Header.Set("Authorization", "Bearer valid-token")
	recRemote := httptest.NewRecorder()
	app2.ServeHTTP(recRemote, reqRemote)
	if recRemote.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for remote non-TLS, got %d", recRemote.Code)
	}

	// 5. Valid token over HTTPS -> 200 OK
	reqValid := httptest.NewRequest("GET", "/tls-check", nil)
	reqValid.RemoteAddr = "203.0.113.10:45678"
	reqValid.Header.Set("X-Forwarded-Proto", "https")
	reqValid.Header.Set("Authorization", "Bearer valid-token")
	recValid := httptest.NewRecorder()
	app2.ServeHTTP(recValid, reqValid)
	if recValid.Code != http.StatusOK || recValid.Body.String() != "ok" {
		t.Fatalf("expected 200 OK, got %d: %s", recValid.Code, recValid.Body.String())
	}

	// 6. Documents BearerAuth in OpenAPI
	spec := app.openAPISpec()
	if _, ok := spec.Components.SecuritySchemes["BearerAuth"]; !ok {
		t.Errorf("expected BearerAuth in components.securitySchemes, got %+v", spec.Components.SecuritySchemes)
	}
}
