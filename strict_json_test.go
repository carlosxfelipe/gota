package gota

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type strictReq struct {
	Name string `json:"name" validate:"required"`
	ID   string `param:"id"`
}

func strictPost(cfg Config, body string) *httptest.ResponseRecorder {
	cfg.DisableDocs = true
	cfg.SilenceTagWarnings = true
	app := New(cfg)
	Post(app, "/u/:id", func(ctx *Context, req strictReq) (Map, error) {
		return Map{"name": req.Name, "id": req.ID}, nil
	})
	req := httptest.NewRequest("POST", "/u/7", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func TestDisallowUnknownFields(t *testing.T) {
	// Default: unknown keys are ignored.
	if rec := strictPost(Config{}, `{"name":"a","isAdmin":true}`); rec.Code != http.StatusOK {
		t.Errorf("default should ignore unknown fields, got %d: %s", rec.Code, rec.Body.String())
	}

	strict := Config{DisallowUnknownFields: true}
	rec := strictPost(strict, `{"name":"a","isAdmin":true}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `unknown field \"isAdmin\"`) {
		t.Errorf("strict should reject unknown field with 400, got %d: %s", rec.Code, rec.Body.String())
	}

	if rec := strictPost(strict, `{"name":"a"}`); rec.Code != http.StatusOK {
		t.Errorf("known fields must pass in strict mode, got %d: %s", rec.Code, rec.Body.String())
	}
	// A body key matching a param-tagged field is a known field (still zeroed, never spoofs the path).
	rec = strictPost(strict, `{"name":"a","ID":"hacked"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"7"`) {
		t.Errorf("param field must not be spoofable: %d %s", rec.Code, rec.Body.String())
	}
	// Trailing data is still rejected in strict mode.
	if rec := strictPost(strict, `{"name":"a"}{"name":"b"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("trailing JSON value must be rejected, got %d", rec.Code)
	}
	if rec := strictPost(strict, `{"name":"a"} garbage`); rec.Code != http.StatusBadRequest {
		t.Errorf("trailing garbage must be rejected, got %d", rec.Code)
	}
}
