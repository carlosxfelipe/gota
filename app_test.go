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

func TestNonREPRRoutes_ExecutionOpenAPIAndTypeScript(t *testing.T) {
	type healthStatus struct {
		Status string `json:"status" example:"ok"`
	}
	type createNoteReq struct {
		Text string `json:"text"`
	}
	type createNoteRes struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}

	app := repr.New(repr.Config{Title: "Mixed API", Version: "1.0.0"})

	// Raw app.Handle stays invisible in OpenAPI/Scalar/TS
	app.Handle(http.MethodGet, "/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs", http.StatusFound)
	}))

	// Imperative non-REPR Context handler documented in OpenAPI/Scalar/TS
	app.Get("/health", func(ctx *repr.Context) error {
		return ctx.JSON(healthStatus{Status: "ok"})
	}).Summary("Health check").Tags("System").Response(http.StatusOK, healthStatus{}, "Healthy")

	// Standard net/http http.Handler documented in OpenAPI/Scalar/TS
	app.HandleHTTP(http.MethodGet, "/ping", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("pong"))
	})).Summary("Ping").Tags("System").ResponseMedia(http.StatusOK, "text/plain", nil, "Pong")

	// Group non-REPR route with middleware, security, request body, query param, and 201 response
	api := app.Group("/api", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Group", "yes")
			next.ServeHTTP(w, r)
		})
	}).Security("BearerAuth")

	api.Post("/notes/{bucket}", func(ctx *repr.Context) error {
		raw, err := ctx.Body()
		if err != nil {
			return repr.BadRequest(err.Error())
		}
		var body createNoteReq
		if err := ctx.DecodeJSON(raw, &body); err != nil {
			return repr.BadRequest(err.Error())
		}
		return ctx.Status(http.StatusCreated).JSON(createNoteRes{
			ID:   ctx.Param("bucket") + "-1",
			Text: body.Text,
		})
	}).
		Summary("Create note").
		Param("query", "tag", "", false, "Optional tag").
		Request(createNoteReq{}, "Note payload").
		Response(http.StatusCreated, createNoteRes{}, "Created")

	api.Get("/secret", func(ctx *repr.Context) error {
		return ctx.NoContent()
	}).Hidden()

	// 1. Runtime execution checks
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected /health response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "pong" {
		t.Fatalf("unexpected /ping response: %d %q", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/notes/inbox", strings.NewReader(`{"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Group") != "yes" || !strings.Contains(rec.Body.String(), `"id":"inbox-1"`) {
		t.Fatalf("unexpected /api/notes/inbox response: %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}

	// 2. OpenAPI / Scalar spec checks
	spec := app.OpenAPISpec()
	if _, ok := spec.Paths["/"]; ok {
		t.Fatalf("expected raw app.Handle / route to stay omitted from OpenAPI")
	}
	if _, ok := spec.Paths["/api/secret"]; ok {
		t.Fatalf("expected Hidden() non-REPR route to be omitted from OpenAPI")
	}
	if _, ok := spec.Paths["/health"]["get"]; !ok {
		t.Fatalf("expected /health in OpenAPI paths")
	}
	pingOp, ok := spec.Paths["/ping"]["get"]
	if !ok {
		t.Fatalf("expected /ping in OpenAPI paths")
	}
	if _, ok := pingOp.Responses["200"].Content["text/plain"]; !ok {
		t.Fatalf("expected text/plain content on /ping OpenAPI 200 response")
	}
	noteOp, ok := spec.Paths["/api/notes/{bucket}"]["post"]
	if !ok {
		t.Fatalf("expected /api/notes/{bucket} in OpenAPI paths")
	}
	if _, has200 := noteOp.Responses["200"]; has200 {
		t.Fatalf("expected placeholder 200 response to be replaced by 201")
	}
	if _, has201 := noteOp.Responses["201"]; !has201 {
		t.Fatalf("expected 201 response on /api/notes/{bucket}")
	}
	if noteOp.RequestBody == nil || len(noteOp.Parameters) != 2 || len(noteOp.Security) != 1 {
		t.Fatalf("unexpected noteOp metadata: reqBody=%v params=%d sec=%d", noteOp.RequestBody, len(noteOp.Parameters), len(noteOp.Security))
	}

	// 3. TypeScript generation checks
	dts := app.GenerateTypeScript()
	for _, want := range []string{
		`"GET /health"`,
		`response: healthStatus;`,
		`"GET /ping"`,
		`response: string;`,
		`"POST /api/notes/{bucket}"`,
		`params: { bucket: string };`,
		`query?: { tag?: string };`,
		`body: createNoteReq;`,
		`response: createNoteRes;`,
	} {
		if !strings.Contains(dts, want) {
			t.Errorf("expected %q in generated .d.ts:\n%s", want, dts)
		}
	}
	if strings.Contains(dts, `"GET /"`) || strings.Contains(dts, `"/api/secret"`) {
		t.Errorf("unexpected hidden/raw routes in .d.ts:\n%s", dts)
	}
}
