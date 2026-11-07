package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/carlosxfelipe/gota"
)

type TodoStore struct {
	mu    sync.RWMutex
	items map[string]TodoResponse
}

func NewTodoStore() *TodoStore {
	return &TodoStore{
		items: map[string]TodoResponse{
			"todo_1": {ID: "todo_1", Title: "Learn Gota REPR Pattern", Priority: "high", Done: true},
		},
	}
}

type GetTodoRequest struct {
	ID string `param:"id" doc:"Todo identifier" example:"todo_1" validate:"required"`
}

type CreateTodoRequest struct {
	Title    string `json:"title" doc:"Task title" example:"Ship production API" validate:"required,min=3"`
	Priority string `json:"priority" doc:"Task priority" example:"high" default:"normal" validate:"oneof=low,normal,high"`
}

type TodoResponse struct {
	ID       string `json:"id" doc:"Todo ID" example:"todo_1"`
	Title    string `json:"title" doc:"Task title" example:"Learn Gota REPR Pattern"`
	Priority string `json:"priority" doc:"Task priority" example:"high"`
	Done     bool   `json:"done" doc:"Completion status" example:"false"`
}

type GetTodoEndpoint struct {
	Store *TodoStore
}

func (e *GetTodoEndpoint) Handle(ctx *gota.Context, req GetTodoRequest) (TodoResponse, error) {
	e.Store.mu.RLock()
	defer e.Store.mu.RUnlock()

	todo, ok := e.Store.items[req.ID]
	if !ok {
		return TodoResponse{}, gota.NotFound("Todo not found")
	}
	return todo, nil
}

type CreateTodoEndpoint struct {
	Store *TodoStore
}

func (e *CreateTodoEndpoint) Handle(ctx *gota.Context, req CreateTodoRequest) (TodoResponse, error) {
	priority := req.Priority
	if priority == "" {
		priority = "normal"
	}

	e.Store.mu.Lock()
	defer e.Store.mu.Unlock()

	id := fmt.Sprintf("todo_%d", len(e.Store.items)+1)
	todo := TodoResponse{
		ID:       id,
		Title:    req.Title,
		Priority: priority,
		Done:     false,
	}
	e.Store.items[id] = todo
	return todo, nil
}

func main() {
	isProd := os.Getenv("ENV") == "production"

	tsURL, tsSDKURL := "/types.d.ts", "/sdk.ts"
	if isProd {
		tsURL, tsSDKURL = "-", "-"
	}

	app := gota.New(gota.Config{
		Title:                 "Gota REPR API",
		Version:               "1.0.0",
		Description:           "Minimal Request-Endpoint-Response (REPR) API with Scalar OpenAPI 3.1 and TypeScript SDK.",
		TypeScriptURL:         tsURL,
		TypeScriptSDKURL:      tsSDKURL,
		DisallowUnknownFields: true,
		DocsCSPEnforce:        true,
	})

	app.Use(gota.RequestID())
	app.Use(gota.Logger())
	app.Use(gota.Recover())
	app.Use(gota.SecureHeaders())
	app.Use(gota.CORS())

	docsPassword := os.Getenv("DOCS_PASSWORD")
	if docsPassword == "" {
		if isProd {
			log.Fatal("DOCS_PASSWORD environment variable must be set in production")
		}
		docsPassword = "admin123"
	}
	app.UseDocs(gota.BasicAuth("admin", docsPassword, gota.BasicAuthOptions{
		Realm:                "Gota API Documentation",
		RateLimit:            &gota.RateLimiterOptions{Max: 10, Window: time.Minute},
		AllowWeakCredentials: !isProd,
		RequireTLS:           isProd,
	}))

	store := NewTodoStore()

	gota.HandleEndpoint(app, http.MethodGet, "/todos/:id", &GetTodoEndpoint{Store: store}).
		Summary("Get todo by ID").
		Tags("Todos")

	gota.HandleEndpoint(app, http.MethodPost, "/todos", &CreateTodoEndpoint{Store: store}).
		Status(http.StatusCreated).
		Summary("Create a new todo").
		Tags("Todos")

	if err := app.ListenGraceful(":3000"); err != nil {
		log.Printf("Server error: %v", err)
	}
}
