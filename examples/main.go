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
	items []TodoResponse
}

func NewTodoStore() *TodoStore {
	return &TodoStore{
		items: []TodoResponse{
			{ID: "todo_1", Title: "Update README badges", Priority: "low", Done: true},
			{ID: "todo_2", Title: "Review pull requests", Priority: "normal", Done: false},
			{ID: "todo_3", Title: "Deploy release v1.0", Priority: "high", Done: false},
		},
	}
}

type GetTodoRequest struct {
	ID string `param:"id" doc:"Todo identifier" example:"todo_1" validate:"required" msg:"Todo ID is required"`
}

type CreateTodoRequest struct {
	Title    string `json:"title" doc:"Task title" example:"Review pull requests" validate:"required,min=3" msg:"required:Title is required|min:Title must be at least 3 characters"`
	Priority string `json:"priority" doc:"Task priority" example:"high" default:"normal" validate:"oneof=low,normal,high" msg:"Priority must be low, normal, or high"`
}

type TodoResponse struct {
	ID       string `json:"id" doc:"Todo ID" example:"todo_1"`
	Title    string `json:"title" doc:"Task title" example:"Review pull requests"`
	Priority string `json:"priority" doc:"Task priority" example:"normal"`
	Done     bool   `json:"done" doc:"Completion status" example:"false"`
}

type ListTodosEndpoint struct {
	Store *TodoStore
}

func (e *ListTodosEndpoint) Handle(ctx *gota.Context, _ gota.Empty) ([]TodoResponse, error) {
	e.Store.mu.RLock()
	defer e.Store.mu.RUnlock()

	out := make([]TodoResponse, len(e.Store.items))
	copy(out, e.Store.items)
	return out, nil
}

type GetTodoEndpoint struct {
	Store *TodoStore
}

func (e *GetTodoEndpoint) Handle(ctx *gota.Context, req GetTodoRequest) (TodoResponse, error) {
	e.Store.mu.RLock()
	defer e.Store.mu.RUnlock()

	for _, t := range e.Store.items {
		if t.ID == req.ID {
			return t, nil
		}
	}
	return TodoResponse{}, gota.NotFound("Todo not found")
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

	todo := TodoResponse{
		ID:       fmt.Sprintf("todo_%d", len(e.Store.items)+1),
		Title:    req.Title,
		Priority: priority,
		Done:     false,
	}
	e.Store.items = append(e.Store.items, todo)
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

	gota.HandleEndpoint(app, http.MethodGet, "/todos", &ListTodosEndpoint{Store: store}).
		Summary("List all todos").
		Tags("Todos")

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
