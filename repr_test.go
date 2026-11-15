package repr_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/scalar"
)

type getItemReq struct {
	ID     string `param:"id" doc:"Item ID" example:"item_1" validate:"required"`
	Filter string `query:"filter" doc:"Optional filter" example:"active"`
	Trace  string `header:"X-Trace-ID"`
}

type getItemRes struct {
	ID      string `json:"id"`
	Filter  string `json:"filter,omitempty"`
	Trace   string `json:"trace,omitempty"`
	Version string `header:"X-API-Version" doc:"API version"`
}

type createItemReq struct {
	Title    string `json:"title" doc:"Item title" example:"Ship v2" validate:"required,min=3" msg:"required:Title is required|min:Title too short"`
	Priority string `json:"priority" default:"normal" validate:"omitempty,oneof=low normal high"`
}

func (r createItemReq) Validate() error {
	if r.Title == "forbidden" {
		return repr.BadRequest("forbidden title")
	}
	return nil
}

type createItemRes struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Priority string `json:"priority"`
}

type structEndpoint struct{}

func (e *structEndpoint) Handle(_ *repr.Context, _ repr.Empty) (repr.Empty, error) {
	return repr.Empty{}, nil
}

func TestREPREndpoints_BindValidateAndRespond(t *testing.T) {
	app := repr.New(repr.Config{
		Title:                 "Test API",
		Version:               "2.0.0",
		DisallowUnknownFields: true,
		Scalar: scalar.Config{
			Theme:  scalar.ThemeFastify,
			Layout: "classic",
		},
	})

	repr.Get(app, "/items/{id}", func(_ *repr.Context, req getItemReq) (getItemRes, error) {
		return getItemRes{
			ID:      req.ID,
			Filter:  req.Filter,
			Trace:   req.Trace,
			Version: "2.0",
		}, nil
	}).Summary("Get item").Tags("Items")

	repr.Post(app, "/items", func(_ *repr.Context, req createItemReq) (createItemRes, error) {
		p := req.Priority
		if p == "" {
			p = "normal"
		}
		return createItemRes{ID: "1", Title: req.Title, Priority: p}, nil
	}).Status(http.StatusCreated).Summary("Create item").Tags("Items")

	repr.HandleEndpoint(app, http.MethodDelete, "/items/{id}", &structEndpoint{}).
		Summary("Delete item").Tags("Items")

	// 1. GET with param, query, header, and response header
	req := httptest.NewRequest(http.MethodGet, "/items/abc?filter=open", nil)
	req.Header.Set("X-Trace-ID", "tr-99")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-API-Version"); got != "2.0" {
		t.Errorf("expected response header X-API-Version=2.0, got %q", got)
	}
	var gotItem getItemRes
	if err := json.Unmarshal(rec.Body.Bytes(), &gotItem); err != nil {
		t.Fatal(err)
	}
	if gotItem.ID != "abc" || gotItem.Filter != "open" || gotItem.Trace != "tr-99" {
		t.Errorf("unexpected response: %+v", gotItem)
	}

	// 2. POST valid -> 201 Created
	postReq := httptest.NewRequest(http.MethodPost, "/items", bytes.NewBufferString(`{"title":"Ship v2","priority":"high"}`))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	app.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", postRec.Code, postRec.Body.String())
	}

	// 3. POST validation error -> 422 with custom msg
	badReq := httptest.NewRequest(http.MethodPost, "/items", bytes.NewBufferString(`{"title":"hi"}`))
	badReq.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	app.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", badRec.Code, badRec.Body.String())
	}
	var valErr repr.ValidationError
	if err := json.Unmarshal(badRec.Body.Bytes(), &valErr); err != nil {
		t.Fatal(err)
	}
	if msgs := valErr.Errors["title"]; len(msgs) == 0 || msgs[0] != "Title too short" {
		t.Errorf("expected custom min message, got %+v", valErr.Errors)
	}

	// 4. POST unknown field rejected -> 400
	unkReq := httptest.NewRequest(http.MethodPost, "/items", bytes.NewBufferString(`{"title":"valid","extra":1}`))
	unkReq.Header.Set("Content-Type", "application/json")
	unkRec := httptest.NewRecorder()
	app.ServeHTTP(unkRec, unkReq)
	if unkRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on unknown field, got %d", unkRec.Code)
	}

	// 5. DELETE Empty -> 204 No Content
	delReq := httptest.NewRequest(http.MethodDelete, "/items/abc", nil)
	delRec := httptest.NewRecorder()
	app.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delRec.Code)
	}
}

func TestJSONCannotSpoofParamOrQuery(t *testing.T) {
	type reqStruct struct {
		ID   string `param:"id"`
		Role string `query:"role"`
		Name string `json:"name"`
	}
	app := repr.New(repr.Config{})
	repr.Post(app, "/users/{id}", func(_ *repr.Context, req reqStruct) (reqStruct, error) {
		return req, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/users/real-id", bytes.NewBufferString(`{"ID":"fake","Role":"admin","name":"Alice"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out reqStruct
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "real-id" {
		t.Errorf("expected ID=real-id, got %q", out.ID)
	}
	if out.Role != "" {
		t.Errorf("expected Role cleared when not in query, got %q", out.Role)
	}
	if out.Name != "Alice" {
		t.Errorf("expected Name=Alice, got %q", out.Name)
	}
}

func TestGroupSecurityOpenAPIAndDocsIsolation(t *testing.T) {
	app := repr.New(repr.Config{
		Title:   "Grouped API",
		Version: "2.0.0",
		DocsMiddlewares: []func(http.Handler) http.Handler{
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Docs-Protected", "true")
					next.ServeHTTP(w, r)
				})
			},
		},
	})

	// Global middleware should NOT leak into /docs or /openapi.json
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			next.ServeHTTP(w, r)
		})
	})

	v1 := app.Group("/api/v1").Security("BearerAuth")
	repr.Get(v1, "/ping", func(_ *repr.Context, _ repr.Empty) (map[string]string, error) {
		return map[string]string{"ok": "true"}, nil
	}).Summary("Ping").ErrorResponse(http.StatusUnauthorized, "Unauthorized")

	// Docs route isolated from global CORS, protected by DocsMiddlewares
	docsRec := httptest.NewRecorder()
	app.ServeHTTP(docsRec, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if docsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /docs, got %d", docsRec.Code)
	}
	if docsRec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("global CORS leaked onto /docs")
	}
	if docsRec.Header().Get("X-Docs-Protected") != "true" {
		t.Errorf("expected DocsMiddlewares to run on /docs")
	}

	// OpenAPI JSON check
	specRec := httptest.NewRecorder()
	app.ServeHTTP(specRec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if specRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /openapi.json, got %d", specRec.Code)
	}
	var spec repr.OpenAPISpec
	if err := json.Unmarshal(specRec.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAPI != "3.1.0" {
		t.Errorf("expected OpenAPI 3.1.0, got %q", spec.OpenAPI)
	}
	if _, ok := spec.Paths["/api/v1/ping"]["get"]; !ok {
		t.Fatalf("expected /api/v1/ping GET in OpenAPI paths: %+v", spec.Paths)
	}
	if _, ok := spec.Components.SecuritySchemes["BearerAuth"]; !ok {
		t.Errorf("expected BearerAuth in securitySchemes")
	}

	// ExportOpenAPI to file
	tmpFile := filepath.Join(t.TempDir(), "openapi.json")
	if err := app.ExportOpenAPI(tmpFile); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(tmpFile)
	if err != nil || !strings.Contains(string(raw), `"openapi": "3.1.0"`) {
		t.Errorf("unexpected exported openapi.json: %v", err)
	}
}

func TestHTTPErrorHandlingAndSSE(t *testing.T) {
	app := repr.New(repr.Config{DisableDocs: true})

	repr.Get(app, "/err", func(_ *repr.Context, _ repr.Empty) (repr.Empty, error) {
		return repr.Empty{}, repr.NotFound("missing item")
	})
	repr.Get(app, "/boom", func(_ *repr.Context, _ repr.Empty) (repr.Empty, error) {
		return repr.Empty{}, errors.New("secret db failure")
	})
	repr.Get(app, "/events", func(_ *repr.Context, _ repr.Empty) (repr.SSEStream, error) {
		ch := make(chan repr.SSEvent, 1)
		ch <- repr.SSEvent{Event: "tick", Data: map[string]int{"n": 1}}
		close(ch)
		return repr.SSEStream{Events: ch}, nil
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/err", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	boomRec := httptest.NewRecorder()
	app.ServeHTTP(boomRec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if boomRec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", boomRec.Code)
	}
	if strings.Contains(boomRec.Body.String(), "secret db failure") {
		t.Errorf("internal error leaked to response body: %s", boomRec.Body.String())
	}

	sseRec := httptest.NewRecorder()
	app.ServeHTTP(sseRec, httptest.NewRequest(http.MethodGet, "/events", nil))
	if sseRec.Code != http.StatusOK || !strings.Contains(sseRec.Body.String(), "event: tick") {
		t.Errorf("unexpected SSE response (%d): %s", sseRec.Code, sseRec.Body.String())
	}
}

func TestCheckStructTagsPanicsOnMistakes(t *testing.T) {
	type badReq struct {
		ID string `path:"id"`
	}
	app := repr.New(repr.Config{DisableDocs: true})

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for path:\"id\" tag")
		}
	}()
	repr.Get(app, "/items/{id}", func(_ *repr.Context, _ badReq) (repr.Empty, error) {
		return repr.Empty{}, nil
	})
}

func TestCheckRoutePathParamsRejectsColonSyntax(t *testing.T) {
	type req struct {
		ID string `param:"id"`
	}
	app := repr.New(repr.Config{DisableDocs: true})

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic when route uses :id instead of chi's {id}")
		}
	}()
	repr.Get(app, "/items/:id", func(_ *repr.Context, _ req) (repr.Empty, error) {
		return repr.Empty{}, nil
	})
}
