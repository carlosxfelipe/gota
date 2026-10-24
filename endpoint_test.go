package gota

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	// Test Successful POST (returns 201 Created by default for Post)
	body := []byte(`{"title":"Keyboard","price":5000}`)
	req := httptest.NewRequest("POST", "/items", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d. Body: %s", w.Code, w.Body.String())
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
