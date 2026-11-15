<div align="center">
	<img src="logo.svg" width="90px" alt="Gota Framework">
</div>

<h1 align="center">Gota Framework 💧</h1>

<div align="center">

[![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Pattern](https://img.shields.io/badge/pattern-REPR-blue)](https://deviq.com/design-patterns/repr-design-pattern/)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-3.1-6BA539)](#interactive-docs--sdk)
[![Scalar Docs](https://img.shields.io/badge/Scalar-Classic%20Layout-purple)](#interactive-docs--sdk)

</div>

**Gota** is a pragmatic Go web framework built around the **REPR (Request-Endpoint-Response)** pattern, with automatic **OpenAPI 3.1**, **Scalar** documentation, and **TypeScript SDK** generation — built directly on top of `net/http`, [`go-chi/chi/v5`](https://github.com/go-chi/chi), and [`go-playground/validator/v10`](https://github.com/go-playground/validator).

## Key Features

- **Typed REPR Endpoints**: `Endpoint[Req, Res]` structs or `EndpointFunc[Req, Res]` functions with automatic binding (`param`, `query`, `header`, `form`, JSON), validation, typed JSON / `Empty` / `SSEStream` responses, and response `header` tags.
- **Native OpenAPI 3.1 & Scalar**: Automatic spec generation at `/openapi.json` and interactive Scalar UI at `/docs` (with SRI pinning, themes, and CSP), plus offline export via `app.ExportOpenAPI()`.
- **TypeScript Types & SDK**: Built-in `.d.ts` and zero-dependency `fetch` client SDK generation (`createGotaClient`), exportable offline via `app.ExportTypeScriptSDK()` / `app.RunCodegen()`.
- **`net/http` & `chi` Interoperability**: Plug in any standard `func(http.Handler) http.Handler` middleware via `app.Use(...)` and `group.Use(...)`.
- **Safe Defaults**: Body/header/multipart limits, `DisallowUnknownFields`, anti-spoofing for `param`/`query`/`header` fields, startup tag checks, `AuditModel`/`AuditRoutes`, and isolated docs routing.

## Installation

```bash
go get github.com/carlosxfelipe/gota
```

Requires Go 1.26 or newer.

## Quickstart

```go
package main

import (
	"log"
	"net/http"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/scalar"
	"github.com/go-chi/chi/v5/middleware"
)

type GetTodoRequest struct {
	ID string `param:"id" doc:"Todo identifier" example:"todo_1" validate:"required"`
}

type CreateTodoRequest struct {
	Title    string `json:"title" doc:"Task title" example:"Review pull requests" validate:"required,min=3" msg:"required:Title is required|min:Title must be at least 3 characters"`
	Priority string `json:"priority" doc:"Task priority" example:"high" default:"normal" validate:"omitempty,oneof=low normal high"`
}

type TodoResponse struct {
	ID       string `json:"id" example:"todo_1"`
	Title    string `json:"title" example:"Review pull requests"`
	Priority string `json:"priority" example:"normal"`
	Done     bool   `json:"done" example:"false"`
}

func main() {
	app := repr.New(repr.Config{
		Title:                 "Gota API",
		Version:               "1.0.0",
		DisallowUnknownFields: true,
		Scalar: scalar.Config{
			Theme:      scalar.ThemeFastify,
			Layout:     "classic",
			CSPEnforce: true,
		},
	})

	app.Use(middleware.RequestID, middleware.Logger, middleware.Recoverer)

	repr.Get(app, "/todos/{id}", func(_ *repr.Context, req GetTodoRequest) (TodoResponse, error) {
		return TodoResponse{ID: req.ID, Title: "Review pull requests", Priority: "normal"}, nil
	}).Summary("Get todo by ID").Tags("Todos")

	repr.Post(app, "/todos", func(_ *repr.Context, req CreateTodoRequest) (TodoResponse, error) {
		p := req.Priority
		if p == "" {
			p = "normal"
		}
		return TodoResponse{ID: "todo_2", Title: req.Title, Priority: p}, nil
	}).Status(http.StatusCreated).Summary("Create a new todo").Tags("Todos")

	log.Fatal(app.ListenGraceful(":3000"))
}
```

## Interactive Docs & SDK

- **Scalar UI**: `/docs` | **OpenAPI 3.1**: `/openapi.json` (isolated from `app.Use(...)` and protectable via `Config.DocsMiddlewares`).
- **TypeScript SDK**: Enable runtime routes via `Config.TypeScriptURL` / `Config.TypeScriptSDKURL`, or export offline:

```bash
go run ./examples/codegen/main.go
```

## Running the Example & Tests

```bash
go run ./examples/main.go
go test ./... -v
```

- Scalar UI: `http://localhost:3000/docs` (`admin` / `admin123`)
- OpenAPI 3.1: `http://localhost:3000/openapi.json`

## License

MIT © [Carlos Felipe](https://github.com/carlosxfelipe)
