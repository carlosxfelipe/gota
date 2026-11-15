package todos

import (
	"fmt"
	"net/http"
	"sync"

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
			{ID: "todo_3", Title: "Deploy release v2.0", Priority: "high", Done: false},
		},
	}
}

type GetTodoRequest struct {
	ID string `param:"id" doc:"Todo identifier" example:"todo_1" validate:"required" msg:"Todo ID is required"`
}

type CreateTodoRequest struct {
	Title    string `json:"title" doc:"Task title" example:"Review pull requests" validate:"required,min=3" msg:"required:Title is required|min:Title must be at least 3 characters"`
	Priority string `json:"priority" doc:"Task priority" example:"high" default:"normal" validate:"omitempty,oneof=low normal high" msg:"Priority must be low, normal, or high"`
}

type TodoResponse struct {
	ID       string `json:"id" doc:"Todo ID" example:"todo_1"`
	Title    string `json:"title" doc:"Task title" example:"Review pull requests"`
	Priority string `json:"priority" doc:"Task priority" example:"normal" validate:"oneof=low normal high"`
	Done     bool   `json:"done" doc:"Completion status" example:"false"`
}

type ListTodosEndpoint struct {
	Store *TodoStore
}

func (e *ListTodosEndpoint) Handle(_ *repr.Context, _ repr.Empty) ([]TodoResponse, error) {
	e.Store.mu.RLock()
	defer e.Store.mu.RUnlock()

	out := make([]TodoResponse, len(e.Store.items))
	copy(out, e.Store.items)
	return out, nil
}

type GetTodoEndpoint struct {
	Store *TodoStore
}

func (e *GetTodoEndpoint) Handle(_ *repr.Context, req GetTodoRequest) (TodoResponse, error) {
	e.Store.mu.RLock()
	defer e.Store.mu.RUnlock()

	for _, t := range e.Store.items {
		if t.ID == req.ID {
			return t, nil
		}
	}
	return TodoResponse{}, repr.NotFound("Todo not found")
}

type CreateTodoEndpoint struct {
	Store *TodoStore
}

func (e *CreateTodoEndpoint) Handle(_ *repr.Context, req CreateTodoRequest) (TodoResponse, error) {
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

// RegisterRoutes registers all Todo REPR endpoints on the given gota app.
func RegisterRoutes(app *repr.App, store *TodoStore) {
	repr.HandleEndpoint(app, http.MethodGet, "/todos", &ListTodosEndpoint{Store: store}).
		Summary("List all todos").
		Description("Returns all todo items in the store.").
		Tags("Todos")

	repr.HandleEndpoint(app, http.MethodGet, "/todos/{id}", &GetTodoEndpoint{Store: store}).
		Summary("Get todo by ID").
		Description("Retrieves a single todo item by its unique identifier.").
		Tags("Todos").
		ErrorResponse(http.StatusNotFound, "Todo not found")

	repr.HandleEndpoint(app, http.MethodPost, "/todos", &CreateTodoEndpoint{Store: store}).
		Status(http.StatusCreated).
		Summary("Create a new todo").
		Description("Validates the input payload and creates a new todo item.").
		Tags("Todos")
}
