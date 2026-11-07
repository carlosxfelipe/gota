package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/carlosxfelipe/gota"
)

// ----------------------------------------------------------------------
// Shared Dependency (Injected into Endpoint structs)
// ----------------------------------------------------------------------

type TodoStore struct {
	mu    sync.RWMutex
	items map[string]TodoResponse
}

func NewTodoStore() *TodoStore {
	return &TodoStore{
		items: map[string]TodoResponse{
			"todo_1": {ID: "todo_1", Title: "Learn Gota REPR Pattern", Done: true},
		},
	}
}

// ----------------------------------------------------------------------
// Feature 1: Get Todo by ID (Request -> Endpoint -> Response)
// ----------------------------------------------------------------------

type GetTodoRequest struct {
	ID string `param:"id" doc:"Todo identifier" example:"todo_1" validate:"required"`
}

type TodoResponse struct {
	ID    string `json:"id" doc:"Todo ID" example:"todo_1"`
	Title string `json:"title" doc:"Task title" example:"Learn Gota REPR Pattern"`
	Done  bool   `json:"done" doc:"Completion status" example:"false"`
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

// ----------------------------------------------------------------------
// Feature 2: Create Todo (Request -> Endpoint -> Response)
// ----------------------------------------------------------------------

type CreateTodoRequest struct {
	Title string `json:"title" doc:"Task title" example:"Ship production API" validate:"required,min=3"`
}

type CreateTodoEndpoint struct {
	Store *TodoStore
}

func (e *CreateTodoEndpoint) Handle(ctx *gota.Context, req CreateTodoRequest) (TodoResponse, error) {
	e.Store.mu.Lock()
	defer e.Store.mu.Unlock()

	id := fmt.Sprintf("todo_%d", len(e.Store.items)+1)
	todo := TodoResponse{
		ID:    id,
		Title: req.Title,
		Done:  false,
	}
	e.Store.items[id] = todo
	return todo, nil
}

// ----------------------------------------------------------------------
// Main Application
// ----------------------------------------------------------------------

func main() {
	app := gota.New(gota.Config{
		Title:                 "Gota Minimal REPR Example",
		Version:               "1.0.0",
		Description:           "Minimal Request-Endpoint-Response (REPR) example using gota.HandleEndpoint.",
		DisallowUnknownFields: true,
		DocsCSPEnforce:        true,
	})

	app.Use(gota.RequestID())
	app.Use(gota.Logger())
	app.Use(gota.Recover())
	app.Use(gota.SecureHeaders())

	store := NewTodoStore()

	// Register REPR Endpoints with gota.HandleEndpoint
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
