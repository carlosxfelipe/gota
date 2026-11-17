package todos

import (
	"cmp"
	"net/http"
	"slices"
	"sync"

	"github.com/carlosxfelipe/gota/repr"
)

type TodoStore struct {
	mu     sync.RWMutex
	nextID int
	items  []TodoResponse
}

func NewTodoStore() *TodoStore {
	return &TodoStore{
		nextID: 3,
		items: []TodoResponse{
			{ID: 1, Title: "Update README badges", Priority: "low", Done: true},
			{ID: 2, Title: "Review pull requests", Priority: "normal", Done: false},
			{ID: 3, Title: "Deploy release v2.0", Priority: "high", Done: false},
		},
	}
}

type ListTodosRequest struct {
	Done     *bool  `query:"done" doc:"Filter by completion status"`
	Priority string `query:"priority" doc:"Filter by task priority" validate:"omitempty,oneof=low normal high" msg:"Priority must be low, normal, or high"`
}

type GetTodoRequest struct {
	ID int `param:"id" doc:"Todo identifier" example:"1" validate:"required,min=1" msg:"required:Todo ID is required|min:Todo ID must be a positive integer"`
}

type CreateTodoRequest struct {
	Title    string `json:"title" doc:"Task title" example:"Review pull requests" validate:"required,min=3" msg:"required:Title is required|min:Title must be at least 3 characters"`
	Priority string `json:"priority" doc:"Task priority" example:"high" default:"normal" validate:"omitempty,oneof=low normal high" msg:"Priority must be low, normal, or high"`
}

type UpdateTodoRequest struct {
	ID       int    `param:"id" doc:"Todo identifier" example:"1" validate:"required,min=1" msg:"required:Todo ID is required|min:Todo ID must be a positive integer"`
	Title    string `json:"title" doc:"Task title" example:"Review pull requests" validate:"required,min=3" msg:"required:Title is required|min:Title must be at least 3 characters"`
	Priority string `json:"priority" doc:"Task priority" example:"high" validate:"required,oneof=low normal high" msg:"required:Priority is required|oneof:Priority must be low, normal, or high"`
	Done     bool   `json:"done" doc:"Completion status" example:"true"`
}

type PatchTodoRequest struct {
	ID       int     `param:"id" doc:"Todo identifier" example:"1" validate:"required,min=1" msg:"required:Todo ID is required|min:Todo ID must be a positive integer"`
	Title    *string `json:"title,omitempty" doc:"Updated task title" example:"Deploy release v2.1" validate:"omitempty,min=3" msg:"min:Title must be at least 3 characters"`
	Priority *string `json:"priority,omitempty" doc:"Updated task priority" example:"high" validate:"omitempty,oneof=low normal high" msg:"Priority must be low, normal, or high"`
	Done     *bool   `json:"done,omitempty" doc:"Updated completion status" example:"true"`
}

type DeleteTodoRequest struct {
	ID int `param:"id" doc:"Todo identifier" example:"1" validate:"required,min=1" msg:"required:Todo ID is required|min:Todo ID must be a positive integer"`
}

type TodoResponse struct {
	ID       int    `json:"id" doc:"Todo ID" example:"1"`
	Title    string `json:"title" doc:"Task title" example:"Review pull requests"`
	Priority string `json:"priority" doc:"Task priority" example:"normal" validate:"oneof=low normal high"`
	Done     bool   `json:"done" doc:"Completion status" example:"false"`
}

type ListTodosEndpoint struct {
	Store *TodoStore
}

func (e *ListTodosEndpoint) Handle(_ *repr.Context, req ListTodosRequest) ([]TodoResponse, error) {
	e.Store.mu.RLock()
	defer e.Store.mu.RUnlock()

	out := make([]TodoResponse, 0, len(e.Store.items))
	for _, item := range e.Store.items {
		if req.Done != nil && item.Done != *req.Done {
			continue
		}
		if req.Priority != "" && item.Priority != req.Priority {
			continue
		}
		out = append(out, item)
	}
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
	priority := cmp.Or(req.Priority, "normal")

	e.Store.mu.Lock()
	defer e.Store.mu.Unlock()

	e.Store.nextID++
	todo := TodoResponse{
		ID:       e.Store.nextID,
		Title:    req.Title,
		Priority: priority,
		Done:     false,
	}
	e.Store.items = append(e.Store.items, todo)
	return todo, nil
}

type UpdateTodoEndpoint struct {
	Store *TodoStore
}

func (e *UpdateTodoEndpoint) Handle(_ *repr.Context, req UpdateTodoRequest) (TodoResponse, error) {
	e.Store.mu.Lock()
	defer e.Store.mu.Unlock()

	for i := range e.Store.items {
		if e.Store.items[i].ID == req.ID {
			e.Store.items[i].Title = req.Title
			e.Store.items[i].Priority = req.Priority
			e.Store.items[i].Done = req.Done
			return e.Store.items[i], nil
		}
	}
	return TodoResponse{}, repr.NotFound("Todo not found")
}

type PatchTodoEndpoint struct {
	Store *TodoStore
}

func (e *PatchTodoEndpoint) Handle(_ *repr.Context, req PatchTodoRequest) (TodoResponse, error) {
	e.Store.mu.Lock()
	defer e.Store.mu.Unlock()

	for i := range e.Store.items {
		if e.Store.items[i].ID == req.ID {
			if req.Title != nil {
				e.Store.items[i].Title = *req.Title
			}
			if req.Priority != nil {
				e.Store.items[i].Priority = *req.Priority
			}
			if req.Done != nil {
				e.Store.items[i].Done = *req.Done
			}
			return e.Store.items[i], nil
		}
	}
	return TodoResponse{}, repr.NotFound("Todo not found")
}

type DeleteTodoEndpoint struct {
	Store *TodoStore
}

func (e *DeleteTodoEndpoint) Handle(_ *repr.Context, req DeleteTodoRequest) (repr.Empty, error) {
	e.Store.mu.Lock()
	defer e.Store.mu.Unlock()

	for i := range e.Store.items {
		if e.Store.items[i].ID == req.ID {
			e.Store.items = slices.Delete(e.Store.items, i, i+1)
			return repr.Empty{}, nil
		}
	}
	return repr.Empty{}, repr.NotFound("Todo not found")
}

// RegisterRoutes registers all Todo REPR endpoints on the given gota app.
func RegisterRoutes(app *repr.App, store *TodoStore) {
	repr.HandleEndpoint(app, http.MethodGet, "/todos", &ListTodosEndpoint{Store: store}).
		Summary("List all todos").
		Description("Returns all todo items in the store, with optional filtering by completion status and priority.").
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

	repr.HandleEndpoint(app, http.MethodPut, "/todos/{id}", &UpdateTodoEndpoint{Store: store}).
		Summary("Replace a todo").
		Description("Validates the full payload and replaces an existing todo item.").
		Tags("Todos").
		ErrorResponse(http.StatusNotFound, "Todo not found")

	repr.HandleEndpoint(app, http.MethodPatch, "/todos/{id}", &PatchTodoEndpoint{Store: store}).
		Summary("Partially update a todo").
		Description("Updates only the provided fields of an existing todo item.").
		Tags("Todos").
		ErrorResponse(http.StatusNotFound, "Todo not found")

	repr.HandleEndpoint(app, http.MethodDelete, "/todos/{id}", &DeleteTodoEndpoint{Store: store}).
		Summary("Delete a todo").
		Description("Deletes an existing todo item by its unique identifier.").
		Tags("Todos").
		ErrorResponse(http.StatusNotFound, "Todo not found")
}
