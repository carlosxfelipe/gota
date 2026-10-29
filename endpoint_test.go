package gota

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/carlosxfelipe/gota/validation"
)

type CreateItemRequest struct {
	Title string `json:"title" doc:"Item title" validate:"required,min=2"`
	Price int    `json:"price" doc:"Item price in cents" validate:"required,min=1"`
}

type ItemResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Price int    `json:"price"`
}

// Struct-based REPR Endpoint
type GetItemRequest struct {
	ID string `param:"id" doc:"Item identifier"`
}

type GetItemEndpoint struct{}

func (e *GetItemEndpoint) Handle(ctx *Context, req GetItemRequest) (ItemResponse, error) {
	if req.ID != "item_123" {
		return ItemResponse{}, NotFound("item not found")
	}
	return ItemResponse{ID: req.ID, Title: "Gota Item", Price: 100}, nil
}

func TestREPREndpoints(t *testing.T) {
	app := New(Config{
		Title:       "Test Store API",
		Version:     "1.0.0",
		ScalarTheme: ThemeLaserwave,
	})

	// 1. Register functional REPR endpoint
	Post(app, "/items", func(ctx *Context, req CreateItemRequest) (ItemResponse, error) {
		return ItemResponse{
			ID:    "item_123",
			Title: req.Title,
			Price: req.Price,
		}, nil
	}).Summary("Create Item").Tags("Items")

	// 2. Register struct-based REPR endpoint
	HandleEndpoint[GetItemRequest, ItemResponse](app, http.MethodGet, "/items/:id", &GetItemEndpoint{}).
		Summary("Get Item by ID").
		Tags("Items")

	// Test Successful POST (returns 200 OK by default)
	body := []byte(`{"title":"Keyboard","price":5000}`)
	req := httptest.NewRequest("POST", "/items", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var res ItemResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if res.ID != "item_123" || res.Title != "Keyboard" || res.Price != 5000 {
		t.Errorf("unexpected response content: %+v", res)
	}

	// Test Failed Validation on POST (returns 422 Unprocessable Entity)
	invalidBody := []byte(`{"title":"","price":0}`)
	req = httptest.NewRequest("POST", "/items", bytes.NewReader(invalidBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Test Struct-based GET found
	req = httptest.NewRequest("GET", "/items/item_123", nil)
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Test Struct-based GET not found (404)
	req = httptest.NewRequest("GET", "/items/item_999", nil)
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestScalarAndOpenAPI(t *testing.T) {
	app := New(Config{
		Title:       "Test Gota Docs",
		Version:     "2.0.0",
		ScalarTheme: ThemeLaserwave,
	})

	Post(app, "/items", func(ctx *Context, req CreateItemRequest) (ItemResponse, error) {
		return ItemResponse{}, nil
	}).Summary("Create Item")

	// Test /openapi.json
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for openapi.json, got %d", w.Code)
	}

	var spec OpenAPISpec
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to parse OpenAPI spec: %v", err)
	}
	if spec.OpenAPI != "3.1.0" {
		t.Errorf("expected OpenAPI 3.1.0, got %s", spec.OpenAPI)
	}
	if _, ok := spec.Paths["/items"]["post"]; !ok {
		t.Errorf("expected POST /items in OpenAPI paths")
	}

	// Test /docs (Scalar UI)
	req = httptest.NewRequest("GET", "/docs", nil)
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /docs, got %d", w.Code)
	}

	html := w.Body.String()
	if !strings.Contains(html, `@scalar/api-reference`) {
		t.Errorf("expected Scalar script tag in docs HTML")
	}
	if !strings.Contains(stdhtml.UnescapeString(html), `"layout":"classic"`) {
		t.Errorf("expected default classic layout in Scalar configuration")
	}
}

func TestCORSPreflightMaxAgeInSeconds(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(CORS())
	Get(app, "/ping", func(ctx *Context, req Empty) (Empty, error) { return Empty{}, nil })

	req := httptest.NewRequest("OPTIONS", "/ping", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("expected Access-Control-Max-Age 86400, got %q", got)
	}
}

func TestWrappedHTTPErrorKeepsStatus(t *testing.T) {
	app := New(Config{DisableDocs: true})
	Get(app, "/wrapped", func(ctx *Context, req Empty) (Empty, error) {
		return Empty{}, fmt.Errorf("loading user: %w", NotFound("user missing"))
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/wrapped", nil))

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for wrapped HTTPError, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestLoggerReportsErrorStatus(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	app := New(Config{DisableDocs: true})
	app.Use(Logger())
	Get(app, "/bad", func(ctx *Context, req Empty) (Empty, error) {
		return Empty{}, BadRequest("nope")
	})

	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/bad", nil))

	if !strings.Contains(buf.String(), "[400]") {
		t.Errorf("expected log to contain [400], got %q", buf.String())
	}
}

func TestGlobalMiddlewareRunsOnNotFound(t *testing.T) {
	app := New(Config{DisableDocs: true})
	app.Use(CORS())

	req := httptest.NewRequest("OPTIONS", "/does-not-exist", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Errorf("expected CORS headers on unknown route, got none (status %d)", w.Code)
	}

	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/does-not-exist", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown route, got %d", w.Code)
	}
}

type SearchRequest struct {
	Q string `query:"q" validate:"required,min=2"`
}

func TestRequiredQueryParamDocumentedWithMultipleRules(t *testing.T) {
	app := New(Config{})
	Get(app, "/search", func(ctx *Context, req SearchRequest) (Empty, error) { return Empty{}, nil })

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))

	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to parse OpenAPI spec: %v", err)
	}
	op := spec["paths"].(map[string]any)["/search"].(map[string]any)["get"].(map[string]any)
	params := op["parameters"].([]any)

	found := false
	for _, p := range params {
		pm := p.(map[string]any)
		if pm["name"] == "q" {
			found = true
			if required, _ := pm["required"].(bool); !required {
				t.Errorf("expected query param q to be required in OpenAPI")
			}
		}
	}
	if !found {
		t.Errorf("query param q not documented")
	}
}

func TestValidationErrorReturnedFromHandlerIs422(t *testing.T) {
	app := New(Config{DisableDocs: true})
	Get(app, "/invalid", func(ctx *Context, req Empty) (Empty, error) {
		return Empty{}, &ValidationError{
			Message: "validation failed",
			Errors:  map[string][]string{"name": {"field name is required"}},
		}
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/invalid", nil))

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d. Body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"errors"`) {
		t.Errorf("expected structured validation errors in body, got %s", w.Body.String())
	}
}

func TestRouteStatusChangesResponseAndDocs(t *testing.T) {
	app := New(Config{})
	Post(app, "/jobs", func(ctx *Context, req Empty) (ItemResponse, error) {
		return ItemResponse{ID: "job_1"}, nil
	}).Status(http.StatusAccepted)

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("POST", "/jobs", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d. Body: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to parse OpenAPI spec: %v", err)
	}
	responses := spec["paths"].(map[string]any)["/jobs"].(map[string]any)["post"].(map[string]any)["responses"].(map[string]any)
	if _, ok := responses["202"]; !ok {
		t.Errorf("expected 202 documented, got %v", responses)
	}
	if _, ok := responses["200"]; ok {
		t.Errorf("did not expect default 200 to remain documented, got %v", responses)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	app := New(Config{DisableDocs: true})
	Get(app, "/things", func(ctx *Context, req Empty) (Empty, error) { return Empty{}, nil })
	Post(app, "/things", func(ctx *Context, req Empty) (Empty, error) { return Empty{}, nil })

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("DELETE", "/things", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
	allow := w.Header().Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
		t.Errorf("expected Allow header to list GET and POST, got %q", allow)
	}

	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/unknown", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown path, got %d", w.Code)
	}
}

func TestSamePathMultipleMethodsAndOptions(t *testing.T) {
	app := New(Config{})
	Get(app, "/multi", func(ctx *Context, req Empty) (ItemResponse, error) { return ItemResponse{ID: "g"}, nil })
	Post(app, "/multi", func(ctx *Context, req Empty) (ItemResponse, error) { return ItemResponse{ID: "p"}, nil })
	Put(app, "/multi", func(ctx *Context, req Empty) (ItemResponse, error) { return ItemResponse{ID: "u"}, nil })

	for method, want := range map[string]int{"GET": 200, "POST": 200, "PUT": 200, "OPTIONS": 204} {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest(method, "/multi", nil))
		if w.Code != want {
			t.Errorf("%s /multi: expected %d, got %d", method, want, w.Code)
		}
	}

	// Automatic OPTIONS routes must not show up as operations in the docs.
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	if strings.Contains(w.Body.String(), `"options"`) {
		t.Errorf("automatic OPTIONS leaked into OpenAPI: %s", w.Body.String())
	}
}

func TestExplicitOptionsOverridesAutomatic(t *testing.T) {
	app := New(Config{DisableDocs: true})
	Get(app, "/custom", func(ctx *Context, req Empty) (Empty, error) { return Empty{}, nil })
	app.RegisterRoute("OPTIONS", "/custom", func(ctx *Context) error { return ctx.Send("custom") })

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("OPTIONS", "/custom", nil))
	if w.Body.String() != "custom" {
		t.Errorf("expected explicit OPTIONS handler, got %q", w.Body.String())
	}
}

func TestPanicResponseIsJSON(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	app := New(Config{DisableDocs: true})
	Get(app, "/boom", func(ctx *Context, req Empty) (Empty, error) {
		panic("kaboom")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/boom", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected JSON content type, got %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Errorf("expected valid JSON body, got %q (%v)", w.Body.String(), err)
	}
}

func TestRequestBodyTooLargeReturns413(t *testing.T) {
	app := New(Config{DisableDocs: true, MaxBodyBytes: 32})
	Post(app, "/items", func(ctx *Context, req CreateItemRequest) (ItemResponse, error) {
		return ItemResponse{ID: "item_1", Title: req.Title, Price: req.Price}, nil
	})

	small := `{"title":"Pen","price":10}`
	req := httptest.NewRequest("POST", "/items", strings.NewReader(small))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("small body: expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	big := `{"title":"` + strings.Repeat("a", 100) + `","price":10}`
	req = httptest.NewRequest("POST", "/items", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big body: expected 413, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestRequestBodyLimitCanBeDisabled(t *testing.T) {
	app := New(Config{DisableDocs: true, MaxBodyBytes: -1})
	Post(app, "/items", func(ctx *Context, req CreateItemRequest) (ItemResponse, error) {
		return ItemResponse{ID: "item_1"}, nil
	})

	big := `{"title":"` + strings.Repeat("a", 1000) + `","price":10}`
	req := httptest.NewRequest("POST", "/items", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with limit disabled, got %d", w.Code)
	}
}

func TestInvalidValidateTagPanicsAtRegistration(t *testing.T) {
	type typoRequest struct {
		Name string `json:"name" validate:"requird"`
	}
	type badParamRequest struct {
		Name string `json:"name" validate:"min=abc"`
	}
	type pathTagRequest struct {
		ID string `path:"id"`
	}
	type omitemptyRequest struct {
		Name string `json:"name" validate:"omitempty,min=3"`
	}

	mustPanic := func(name string, fieldName string, fn func()) {
		t.Helper()
		defer func() {
			r := recover()
			if r == nil {
				t.Errorf("%s: expected panic at registration", name)
				return
			}
			if msg := fmt.Sprint(r); !strings.Contains(msg, fieldName) {
				t.Errorf("%s: expected panic message to mention the field %q, got %q", name, fieldName, msg)
			}
		}()
		fn()
	}

	app := New(Config{DisableDocs: true})
	mustPanic("unknown rule", "name", func() {
		Post(app, "/a", func(ctx *Context, req typoRequest) (Empty, error) { return Empty{}, nil })
	})
	mustPanic("invalid rule parameter", "name", func() {
		Post(app, "/b", func(ctx *Context, req badParamRequest) (Empty, error) { return Empty{}, nil })
	})
	mustPanic("path tag anti-pattern", "ID", func() {
		Post(app, "/c/:id", func(ctx *Context, req pathTagRequest) (Empty, error) { return Empty{}, nil })
	})

	// omitempty must NOT panic at registration
	Post(app, "/d", func(ctx *Context, req omitemptyRequest) (Empty, error) { return Empty{}, nil })
}

func TestPostCanExplicitlySet201Created(t *testing.T) {
	app := New(Config{DisableDocs: true})
	Post(app, "/users", func(ctx *Context, req Empty) (ItemResponse, error) {
		return ItemResponse{ID: "u_1", Title: "User"}, nil
	}).Status(http.StatusCreated)

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("POST", "/users", nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

type UploadAvatarRequest struct {
	UserID string                `param:"id"`
	File   *multipart.FileHeader `form:"file" validate:"required"`
}

type UploadAvatarResponse struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	UserID   string `json:"userId"`
}

func TestREPRMultipartFileUpload(t *testing.T) {
	app := New(Config{DisableDocs: true})

	Post(app, "/users/:id/avatar", func(ctx *Context, req UploadAvatarRequest) (UploadAvatarResponse, error) {
		return UploadAvatarResponse{
			Filename: req.File.Filename,
			Size:     req.File.Size,
			UserID:   req.UserID,
		}, nil
	})

	// 1. Success case with file
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = part.Write([]byte("fake-png-content"))
	_ = w.Close()

	req := httptest.NewRequest("POST", "/users/usr_42/avatar", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var res UploadAvatarResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Filename != "avatar.png" || res.UserID != "usr_42" || res.Size == 0 {
		t.Errorf("unexpected response: %+v", res)
	}

	// 2. Failure case: missing required file
	var emptyBuf bytes.Buffer
	wEmpty := multipart.NewWriter(&emptyBuf)
	_ = wEmpty.Close()

	reqEmpty := httptest.NewRequest("POST", "/users/usr_42/avatar", &emptyBuf)
	reqEmpty.Header.Set("Content-Type", wEmpty.FormDataContentType())
	recEmpty := httptest.NewRecorder()
	app.ServeHTTP(recEmpty, reqEmpty)

	if recEmpty.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing required file, got %d", recEmpty.Code)
	}
}

func TestREPRStreamingAndSSE(t *testing.T) {
	app := New(Config{DisableDocs: true})

	// 1. Generic StreamFunc endpoint
	Get(app, "/stream", func(ctx *Context, _ Empty) (StreamFunc, error) {
		stream := func(c *Context) error {
			return c.Stream(func(w io.Writer) bool {
				_, _ = w.Write([]byte("chunk1-"))
				_, _ = w.Write([]byte("chunk2"))
				return false // finish stream
			})
		}
		return stream, nil
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/stream", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "chunk1-chunk2" {
		t.Errorf("expected 'chunk1-chunk2', got %q", rec.Body.String())
	}

	// 2. SSEStream endpoint
	Get(app, "/events", func(ctx *Context, _ Empty) (SSEStream, error) {
		events := make(chan SSEvent, 2)
		events <- SSEvent{Event: "greeting", Data: "hello"}
		events <- SSEvent{Event: "status", Data: Map{"ready": true}}
		close(events)
		return SSEStream{Events: events}, nil
	})

	recSSE := httptest.NewRecorder()
	app.ServeHTTP(recSSE, httptest.NewRequest("GET", "/events", nil))

	if recSSE.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recSSE.Code)
	}
	if recSSE.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", recSSE.Header().Get("Content-Type"))
	}
	bodyStr := recSSE.Body.String()
	if !strings.Contains(bodyStr, "event: greeting\ndata: hello\n\n") {
		t.Errorf("expected greeting event in SSE body: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "event: status\ndata: {\"ready\":true}\n\n") {
		t.Errorf("expected status event in SSE body: %s", bodyStr)
	}
}

func TestSSEStreamStopsOnClientDisconnect(t *testing.T) {
	app := New(Config{DisableDocs: true})
	events := make(chan SSEvent) // never closed, never written
	Get(app, "/stream", func(ctx *Context, _ Empty) (SSEStream, error) {
		return SSEStream{Events: events}, nil
	})

	reqCtx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(reqCtx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		app.ServeHTTP(rec, req)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSEStream did not stop after client disconnect")
	}
}

type SuspiciousReq struct {
	Active bool `json:"active" validate:"required"`
}

type SilencedReq struct {
	Active bool `json:"active" validate:"required,nowarn"`
}

func TestWrapEndpoint_PrimitiveRequiredWarning(t *testing.T) {
	var logged []string
	validation.TagWarningLogger = func(w string) {
		logged = append(logged, w)
	}
	defer func() {
		validation.TagWarningLogger = nil
		validation.SilenceTagWarnings = false
	}()

	// 1. SuspiciousReq emits warning
	WrapEndpoint(func(ctx *Context, req SuspiciousReq) (Empty, error) {
		return Empty{}, nil
	}, http.StatusOK)

	if len(logged) == 0 {
		t.Fatal("expected warning for SuspiciousReq with non-pointer bool required")
	}
	if !strings.Contains(logged[0], "non-pointer bool") {
		t.Errorf("expected warning mentioning non-pointer bool, got: %s", logged[0])
	}

	// 2. SilencedReq with nowarn tag emits no warning
	logged = nil
	WrapEndpoint(func(ctx *Context, req SilencedReq) (Empty, error) {
		return Empty{}, nil
	}, http.StatusOK)
	if len(logged) != 0 {
		t.Errorf("expected no warning for SilencedReq with nowarn, got: %v", logged)
	}

	// 3. Global silence suppresses warning
	SilenceTagWarnings(true)
	type AnotherSuspiciousReq struct {
		Count int `json:"count" validate:"required"`
	}
	logged = nil
	WrapEndpoint(func(ctx *Context, req AnotherSuspiciousReq) (Empty, error) {
		return Empty{}, nil
	}, http.StatusOK)
	if len(logged) != 0 {
		t.Errorf("expected no warning when SilenceTagWarnings is true, got: %v", logged)
	}
}

type EmbeddedPagination struct {
	Page  *int `query:"page" doc:"Page number" example:"1"`
	Limit *int `query:"limit" doc:"Items per page" example:"20"`
}

type ListWithEmbeddedQueryReq struct {
	EmbeddedPagination
	Limit *int   `query:"limit" doc:"Overridden limit" example:"50"` // shadows EmbeddedPagination.Limit
	Tag   string `query:"tag" doc:"Filter tag" example:"golang"`
}

func TestIntrospectEndpoint_EmbeddedParamsAndExamples(t *testing.T) {
	app := New(Config{})
	Get(app, "/items", func(ctx *Context, req ListWithEmbeddedQueryReq) (Empty, error) {
		return Empty{}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))

	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to parse OpenAPI spec: %v", err)
	}
	op := spec["paths"].(map[string]any)["/items"].(map[string]any)["get"].(map[string]any)

	// Pure query request with embedded struct must NOT generate a requestBody
	if _, hasBody := op["requestBody"]; hasBody {
		t.Errorf("did not expect requestBody for pure query request with embedded struct, got: %v", op["requestBody"])
	}

	params, ok := op["parameters"].([]any)
	if !ok || len(params) != 3 {
		t.Fatalf("expected 3 query parameters (page, limit, tag), got: %v", op["parameters"])
	}

	byName := make(map[string]map[string]any)
	for _, p := range params {
		pm := p.(map[string]any)
		byName[pm["name"].(string)] = pm
	}

	if byName["page"]["example"] != float64(1) {
		t.Errorf("expected page example 1, got: %v", byName["page"]["example"])
	}
	if byName["limit"]["description"] != "Overridden limit" || byName["limit"]["example"] != float64(50) {
		t.Errorf("expected outer limit to shadow embedded limit, got: %v", byName["limit"])
	}
	if byName["tag"]["example"] != "golang" {
		t.Errorf("expected tag example 'golang', got: %v", byName["tag"]["example"])
	}
}

type ConstrainedQueryReq struct {
	ID     string `param:"id" validate:"uuid"`
	Limit  *int   `query:"limit" default:"20" validate:"min=1,max=100"`
	Status string `query:"status" validate:"oneof=open,closed"`
}

func TestOpenAPISecuritySchemesAndParamConstraints(t *testing.T) {
	app := New(Config{
		Servers: []ServerConfig{{URL: "https://api.example.com", Description: "Prod"}},
	})

	protected := app.Group("/api")
	protected.Use(JWT([]byte("secret")))

	Get(protected, "/orders/:id", func(ctx *Context, req ConstrainedQueryReq) (Empty, error) {
		return Empty{}, nil
	}).Summary("Get Order")

	Get(app, "/custom-auth", func(ctx *Context, req Empty) (Empty, error) {
		return Empty{}, nil
	}).BearerAuth()

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var spec OpenAPISpec
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to unmarshal spec: %v", err)
	}

	// Verify BearerAuth in components.securitySchemes
	bearer, ok := spec.Components.SecuritySchemes["BearerAuth"]
	if !ok || bearer.Type != "http" || bearer.Scheme != "bearer" || bearer.BearerFormat != "JWT" {
		t.Errorf("expected BearerAuth security scheme in components, got: %+v", spec.Components.SecuritySchemes)
	}

	// Verify automatic security on JWT-protected group route
	orderOp := spec.Paths["/api/orders/{id}"]["get"]
	if len(orderOp.Security) != 1 || orderOp.Security[0]["BearerAuth"] == nil {
		t.Errorf("expected automatic BearerAuth security on /api/orders/{id}, got: %+v", orderOp.Security)
	}

	// Verify explicit .BearerAuth() on route
	customOp := spec.Paths["/custom-auth"]["get"]
	if len(customOp.Security) != 1 || customOp.Security[0]["BearerAuth"] == nil {
		t.Errorf("expected explicit BearerAuth security on /custom-auth, got: %+v", customOp.Security)
	}

	// Verify parameter validation constraints in schema
	paramByName := make(map[string]OpenAPIParam)
	for _, p := range orderOp.Parameters {
		paramByName[p.Name] = p
	}
	if paramByName["id"].Schema["format"] != "uuid" {
		t.Errorf("expected id param format=uuid, got: %v", paramByName["id"].Schema)
	}
	if paramByName["limit"].Schema["minimum"] != float64(1) || paramByName["limit"].Schema["maximum"] != float64(100) || paramByName["limit"].Schema["default"] != float64(20) {
		t.Errorf("expected limit param min=1 max=100 default=20, got: %v", paramByName["limit"].Schema)
	}
	if enum, ok := paramByName["status"].Schema["enum"].([]any); !ok || len(enum) != 2 {
		t.Errorf("expected status param enum [open, closed], got: %v", paramByName["status"].Schema)
	}
}

type GetByIDReq struct {
	ID string `param:"id"`
}

type DeleteByUserIDReq struct {
	UserID string `param:"userId"`
}

type CreatedWithHeaderRes struct {
	Location   string `header:"Location" json:"-" doc:"Resource URI"`
	TotalCount int    `header:"X-Total-Count" json:"-" doc:"Total count" example:"42"`
	ID         string `json:"id"`
}

func TestCrossMethodDifferentParamNamesAndResponseHeaders(t *testing.T) {
	app := New(Config{})

	// Different param names at the same URL segment across GET and DELETE must NOT panic on auto OPTIONS!
	Get(app, "/users/:id", func(ctx *Context, req GetByIDReq) (CreatedWithHeaderRes, error) {
		return CreatedWithHeaderRes{
			Location:   "/users/" + req.ID,
			TotalCount: 42,
			ID:         req.ID,
		}, nil
	})
	Delete(app, "/users/:userId", func(ctx *Context, req DeleteByUserIDReq) (Empty, error) {
		return Empty{}, nil
	})

	// Test GET response headers & body
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/users/abc", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Location") != "/users/abc" || w.Header().Get("X-Total-Count") != "42" {
		t.Errorf("expected response headers Location=/users/abc and X-Total-Count=42, got %v", w.Header())
	}

	// Test DELETE with :userId
	wDel := httptest.NewRecorder()
	app.ServeHTTP(wDel, httptest.NewRequest("DELETE", "/users/xyz", nil))
	if wDel.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for DELETE, got %d", wDel.Code)
	}

	// Test OPTIONS preflight on /users/abc
	wOpt := httptest.NewRecorder()
	app.ServeHTTP(wOpt, httptest.NewRequest("OPTIONS", "/users/abc", nil))
	if wOpt.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS, got %d", wOpt.Code)
	}

	// Verify response headers documented in OpenAPI 3.1
	wSpec := httptest.NewRecorder()
	app.ServeHTTP(wSpec, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec OpenAPISpec
	if err := json.Unmarshal(wSpec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to parse spec: %v", err)
	}
	resp200 := spec.Paths["/users/{id}"]["get"].Responses["200"]
	if _, ok := resp200.Headers["Location"]; !ok {
		t.Errorf("expected Location in OpenAPI response headers, got %+v", resp200.Headers)
	}
	if hdr, ok := resp200.Headers["X-Total-Count"]; !ok || hdr.Example != float64(42) {
		t.Errorf("expected X-Total-Count with example 42 in OpenAPI response headers, got %+v", resp200.Headers)
	}
}
