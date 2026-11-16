package repr_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/carlosxfelipe/gota/repr"
	"github.com/carlosxfelipe/gota/scalar"
)

func TestBodyLimitReturns413(t *testing.T) {
	type payloadReq struct {
		Name string `json:"name"`
	}

	app := repr.New(repr.Config{
		DisableDocs:  true,
		MaxBodyBytes: 16,
	})
	repr.Post(app, "/limited", func(_ *repr.Context, req payloadReq) (payloadReq, error) {
		return req, nil
	})

	bigBody := bytes.NewBufferString(`{"name":"this payload is larger than 16 bytes"}`)
	req := httptest.NewRequest(http.MethodPost, "/limited", bigBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 Request Entity Too Large, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRedirectLocalBlocksOpenRedirects(t *testing.T) {
	app := repr.New(repr.Config{DisableDocs: true})
	repr.Get(app, "/go", func(ctx *repr.Context, _ repr.Empty) (repr.Empty, error) {
		target := ctx.Request.URL.Query().Get("next")
		return repr.Empty{}, ctx.RedirectLocal(target)
	})

	badTargets := []string{
		"https://evil.com",
		"//evil.com",
		`/\evil.com`,
		"/\tevil",
		"",
	}
	for _, target := range badTargets {
		if repr.IsLocalRedirectTarget(target) {
			t.Errorf("IsLocalRedirectTarget(%q) = true, want false", target)
		}
		req := httptest.NewRequest(http.MethodGet, "/go?next="+url.QueryEscape(target), nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("target %q: expected 400 Bad Request, got %d", target, rec.Code)
		}
	}

	okReq := httptest.NewRequest(http.MethodGet, "/go?next=/todos/1", nil)
	okRec := httptest.NewRecorder()
	app.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusFound || okRec.Header().Get("Location") != "/todos/1" {
		t.Errorf("expected 302 to /todos/1, got %d (%q)", okRec.Code, okRec.Header().Get("Location"))
	}
}

func TestHeaderAndSSESanitization(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := repr.NewContext(rec, req)

	ctx.SetHeader("X-Custom", "ok\r\nEvil-Header: injected\x00")
	if got := rec.Header().Get("X-Custom"); strings.ContainsAny(got, "\r\n\x00") {
		t.Fatalf("header injection not stripped: %q", got)
	}

	sseRec := httptest.NewRecorder()
	sseCtx := repr.NewContext(sseRec, req)
	if err := sseCtx.SSEvent("msg\r\nevent: forged", "line1\nline2"); err != nil {
		t.Fatal(err)
	}
	body := sseRec.Body.String()
	if strings.Contains(body, "\nevent: forged") || strings.Contains(body, "\r") {
		t.Errorf("SSE event name injection created a forged line: %q", body)
	}
	if !strings.Contains(body, "data: line1\ndata: line2\n") {
		t.Errorf("expected multi-line data split safely, got: %q", body)
	}
}

func TestFailFastInvalidValidateTagAndMsgWithoutValidate(t *testing.T) {
	t.Run("unknown validate rule", func(t *testing.T) {
		type badValidateReq struct {
			Title string `json:"title" validate:"requried"`
		}
		app := repr.New(repr.Config{DisableDocs: true})
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("expected panic for invalid validate tag")
			}
		}()
		repr.Post(app, "/bad-validate", func(_ *repr.Context, _ badValidateReq) (repr.Empty, error) {
			return repr.Empty{}, nil
		})
	})

	t.Run("msg without validate", func(t *testing.T) {
		type msgOnlyReq struct {
			Title string `json:"title" msg:"required"`
		}
		app := repr.New(repr.Config{DisableDocs: true})
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("expected panic for msg without validate")
			}
		}()
		repr.Post(app, "/msg-only", func(_ *repr.Context, _ msgOnlyReq) (repr.Empty, error) {
			return repr.Empty{}, nil
		})
	})
}

func TestAuditModelAndSanitizeFilename(t *testing.T) {
	type unsafeUser struct {
		ID           string `json:"id"`
		Password     string `json:"password"`
		PasswordHash string `json:"passwordHash"`
		InternalKey  string `json:"-"`
		CVV          string `json:"cvv"`
	}
	warnings := repr.AuditModel(unsafeUser{})
	if len(warnings) != 3 {
		t.Fatalf("expected 3 audit warnings, got %d: %v", len(warnings), warnings)
	}

	type safeUser struct {
		ID           string `json:"id"`
		PasswordHash string `json:"-"`
		PublicSalt   string `json:"publicSalt" validate:"nowarn"`
	}
	if safeWarnings := repr.AuditModel(safeUser{}); len(safeWarnings) != 0 {
		t.Errorf("expected 0 warnings for safeUser, got %v", safeWarnings)
	}

	app := repr.New(repr.Config{DisableDocs: true})
	repr.Get(app, "/safe", func(_ *repr.Context, _ repr.Empty) (safeUser, error) {
		return safeUser{}, nil
	})
	if routeWarnings := app.AuditRoutes(); len(routeWarnings) != 0 {
		t.Errorf("expected 0 AuditRoutes warnings when nowarn is set, got %v", routeWarnings)
	}

	cases := map[string]string{
		"../../etc/passwd":  "passwd",
		`C:\evil\photo.png`: "photo.png",
		".env":              "",
		"..":                "",
		"report\x00.pdf":    "report.pdf",
	}
	for in, want := range cases {
		if got := repr.SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDocsSecurityHeadersAndCSPEnforce(t *testing.T) {
	app := repr.New(repr.Config{
		Scalar: scalar.Config{
			CSPEnforce: true,
		},
	})

	docsRec := httptest.NewRecorder()
	app.ServeHTTP(docsRec, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if docsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /docs, got %d", docsRec.Code)
	}
	if docsRec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("expected X-Frame-Options DENY")
	}
	if docsRec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("expected X-Content-Type-Options nosniff")
	}
	if docsRec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("expected Referrer-Policy no-referrer")
	}
	if docsRec.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("expected enforcing Content-Security-Policy header")
	}

	specRec := httptest.NewRecorder()
	app.ServeHTTP(specRec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if specRec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("expected nosniff on /openapi.json")
	}
}
