package gota

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
	if !strings.Contains(html, `"layout":"classic"`) {
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
	app := New()
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
	app := New()
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
	app := New()
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

	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			r := recover()
			if r == nil {
				t.Errorf("%s: expected panic at registration", name)
				return
			}
			if msg := fmt.Sprint(r); !strings.Contains(msg, "name") {
				t.Errorf("%s: expected panic message to mention the field, got %q", name, msg)
			}
		}()
		fn()
	}

	app := New(Config{DisableDocs: true})
	mustPanic("unknown rule", func() {
		Post(app, "/a", func(ctx *Context, req typoRequest) (Empty, error) { return Empty{}, nil })
	})
	mustPanic("invalid rule parameter", func() {
		Post(app, "/b", func(ctx *Context, req badParamRequest) (Empty, error) { return Empty{}, nil })
	})
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
