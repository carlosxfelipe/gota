<div align="center">
	<img src="logo.svg" width="90px" alt="Gota Framework">
</div>

<h1 align="center">Gota Framework 💧</h1>

<div align="center">

[![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Pattern](https://img.shields.io/badge/pattern-REPR-blue)](https://deviq.com/design-patterns/repr-design-pattern/)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-3.1-6BA539)](#-interactive-docs--openapi-31)
[![Scalar Docs](https://img.shields.io/badge/Scalar-Classic%20Layout-purple)](#-interactive-docs--openapi-31)

</div>

**Gota** is a minimalist, high-performance web framework for Go with **zero external dependencies**, built around the [REPR Design Pattern (Request-Endpoint-Response)](https://deviq.com/design-patterns/repr-design-pattern/) and featuring native interactive API documentation powered by **Scalar** (configured with the `classic` layout by default).

Inspired by the ergonomic fluency of [Kite](https://github.com/claudiovictors/kite) and the cohesive endpoint architecture popularized by [FastEndpoints](https://fast-endpoints.com/).

---

## ✨ Key Features

- **REPR Design Pattern**: Request-Endpoint-Response architecture with automatic model binding, declarative validation, and typed JSON / streaming responses.
- **Zero External Dependencies**: Built 100% on the Go standard library.
- **Native OpenAPI 3.1 & Scalar Docs**: Automatic spec generation from Go types, serving interactive Scalar UI (`classic` layout) at `/docs`.
- **Auto-Generated TypeScript Definitions & Client SDK**: Optional zero-dependency TypeScript declarations (`/types.d.ts`) and standalone client SDK (`/sdk.ts`), or exported offline directly to files via `app.ExportTypeScript()` / `app.ExportTypeScriptSDK()`, providing end-to-end type safety from Go to frontend without npm or build tools.
- **Fluent Route Ergonomics**: Chainable metadata (`.Summary()`, `.Tags()`, `.Middleware()`), route groups, and parameter matching (`:id`, `*wildcard`).
- **Production-Ready Middlewares**: Built-in BasicAuth, BearerToken, IPWhitelist (CIDR support), JWT (HS256, RS256, ES256, EdDSA), CORS, Security Headers, Rate Limiter, Gzip, Panic Recovery, Request ID, Slog logger, Timeouts, and Graceful Shutdown.

> 📖 Looking for in-depth patterns, validation recipes, security best practices, and AI guidance? See [llms.txt](llms.txt).

---

## 📦 Installation

Initialize your Go module (if starting a new project):

```bash
go mod init myapp
```

Install Gota:

```bash
go get github.com/carlosxfelipe/gota
```

Requires Go 1.26 or newer.

---

## 🚀 Quickstart (REPR Pattern)

Create a `main.go` file:

```go
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
	Priority string `json:"priority" doc:"Task priority" example:"normal" validate:"oneof=low,normal,high"`
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

	// Disable runtime TypeScript HTTP routes in production (use app.ExportTypeScriptSDK in CI/CD instead)
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
	app.Use(gota.Gzip())
	app.Use(gota.CORS())

	// Protect /docs, /openapi.json, /types.d.ts, and /sdk.ts with Basic Auth and rate limiting
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

	// Redirect root to Scalar UI and hide this route from OpenAPI/SDK
	app.Get("/", func(ctx *gota.Context) error {
		return ctx.Redirect("/docs")
	}).Hidden()

	gota.HandleEndpoint(app, http.MethodGet, "/todos", &ListTodosEndpoint{Store: store}).
		Summary("List all todos").
		Description("Returns all todo items in the store.").
		Tags("Todos")

	gota.HandleEndpoint(app, http.MethodGet, "/todos/:id", &GetTodoEndpoint{Store: store}).
		Summary("Get todo by ID").
		Description("Retrieves a single todo item by its unique identifier.").
		Tags("Todos")

	gota.HandleEndpoint(app, http.MethodPost, "/todos", &CreateTodoEndpoint{Store: store}).
		Status(http.StatusCreated).
		Summary("Create a new todo").
		Description("Validates the input payload and creates a new todo item.").
		Tags("Todos")

	if err := app.ListenGraceful(":3000"); err != nil {
		log.Printf("Server error: %v", err)
	}
}
```

Run your application:

```bash
go run main.go
```

---

## 📖 Interactive Docs & OpenAPI 3.1

Gota serves an interactive [Scalar](https://scalar.com/) documentation UI at `/docs` (configured with the `classic` layout) and the OpenAPI 3.1 spec at `/openapi.json` out of the box.

To secure documentation routes in production:

```go
app.UseDocs(gota.BasicAuth("admin", os.Getenv("DOCS_PASSWORD"), gota.BasicAuthOptions{
    RateLimit:  &gota.RateLimiterOptions{Max: 10, Window: time.Minute},
    RequireTLS: os.Getenv("ENV") == "production",
}))
```

---

## 🔷 TypeScript Client SDK

Gota can generate a zero-dependency, end-to-end type-safe TypeScript client SDK directly from your Go models and routes:

```bash
# Fetch from your running local server:
curl -u admin:admin123 -o src/api.ts http://localhost:3000/sdk.ts
```

Or export offline during build/CI without starting an HTTP server:

```go
_ = app.ExportTypeScriptSDK("./frontend/src/api.ts")
```

Then consume it in your frontend with full route, payload, and error autocomplete:

```typescript
import { createGotaClient, GotaHttpError, type ResponseError } from "./api";

const api = createGotaClient("http://localhost:3000");

// 1. Autocompletes "GET /todos/:id" and types the response
const todo = await api.fetch("GET /todos/:id", {
  params: { id: "todo_1" },
  next: { revalidate: 60 }, // Native Next.js App Router cache/revalidation support
  timeoutMs: 5000, // Native request timeout
  retry: 2, // Auto-retry with exponential backoff on network/5xx
});
console.log(todo.title);

// 2. Strict typed error handling (derived from Go route annotations)
try {
  await api.fetch("POST /todos", {
    body: { title: "New Task", priority: "high" },
  });
} catch (err) {
  if (err instanceof GotaHttpError) {
    const errorData = err.data as ResponseError<"POST /todos">;
    console.error(err.status, errorData);
  }
}
```

---

## 🧪 Running Tests

```bash
go test ./... -v
```

## 🏃 Running the Example

```bash
go run examples/main.go
```

Then explore:

- **Scalar UI**: [http://localhost:3000/docs](http://localhost:3000/docs) (`admin` / `admin123`)
- **OpenAPI 3.1**: [http://localhost:3000/openapi.json](http://localhost:3000/openapi.json)
- **TypeScript SDK**: [http://localhost:3000/sdk.ts](http://localhost:3000/sdk.ts)

> 💡 Looking for a full-featured real-world showcase with JWT auth, role-based access control, file uploads, SSE streaming, and Bruno collections? Check out [order-flow](https://github.com/carlosxfelipe/order-flow).

---

## 📚 Complete Specification & AI Agent Guide

For exhaustive documentation on all middlewares (JWT, CORS, RateLimiter, SecureHeaders), model validation tags, file uploads, SSE streaming, and agent prompts, refer to:

👉 [llms.txt](llms.txt)

---

## 📄 License

MIT © [Carlos Felipe](https://github.com/carlosxfelipe)
