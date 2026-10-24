<div align="center">
	<img src="logo.svg" width="90px" alt="Gota Framework">
</div>

<h1 align="center">Gota Framework 💧</h1>

<div align="center">

[![Go Version](https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Pattern](https://img.shields.io/badge/pattern-REPR-blue)](https://deviq.com/design-patterns/repr-design-pattern/)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-3.1-6BA539)](http://localhost:3000/openapi.json)
[![Scalar Docs](https://img.shields.io/badge/Scalar-Classic%20Layout-purple)](http://localhost:3000/docs)

</div>

**Gota** is a minimalist, high-performance web micro-framework for Go with **zero external dependencies**, built around the [**REPR Design Pattern (Request-Endpoint-Response)**](https://deviq.com/design-patterns/repr-design-pattern/) and featuring native interactive API documentation powered by **Scalar** (configured with the `classic` layout by default).

Inspired by the ergonomic fluency of [Kite](https://github.com/claudiovictors/kite) and the cohesive endpoint architecture popularized by **FastEndpoints**.

---

## ✨ Features

- **First-Class REPR Design Pattern**:
  - **Request**: Automatic binding across route params, query string, headers, and JSON body (`param`, `query`, `header`, `json` tags) with declarative validation (`validate:"required,min=3,email"` or custom `Validator` interface).
  - **Endpoint**: Single Responsibility Principle per operation, implemented via generic functions (`gota.Get`, `gota.Post`, etc.) or struct-based endpoints (`Endpoint[Req, Res]`).
  - **Response**: Automatic JSON serialization with semantic HTTP status codes (200 OK, 201 Created for POST, 204 No Content for `gota.Empty`).
- **Zero External Dependencies**: Built entirely on top of the Go standard library (`net/http`, `reflect`, `encoding/json`, `sync`, etc.).
- **Interactive Documentation with Scalar**:
  - Automatic **OpenAPI 3.1** specification generation derived directly from `Req` and `Res` types.
  - Interactive **Scalar** UI served at `/docs` with **`classic` layout enabled by default** (Swagger-style).
  - OpenAPI 3.1 JSON schema exposed at `/openapi.json` out of the box.
- **Fluent Kite-like Ergonomics**: Chainable metadata methods (`.Summary()`, `.Description()`, `.Tags()`, `.Middleware()`), route groups (`app.Group("/api")`), and path parameter matching (`:id`, `*wildcard`).
- **Built-in Middlewares**: Production-ready CORS with automatic `OPTIONS` preflight handling, panic `Recover` with stacktrace logging, and request `Logger`.

---

## 📦 Installation

```bash
go get github.com/carlosxfelipe/gota
```

Requires Go 1.24 or newer.

---

## 🚀 Quickstart (REPR Pattern)

```go
package main

import (
	"log"

	"github.com/carlosxfelipe/gota"
)

// 1. Request DTO with automated binding and validation
type CreateUserRequest struct {
	Name  string `json:"name" doc:"Full name" example:"Jane Doe" validate:"required,min=3"`
	Email string `json:"email" doc:"Corporate email" example:"jane.doe@example.com" validate:"required,email"`
}

// 2. Response DTO
type UserResponse struct {
	ID    string `json:"id" example:"usr_100"`
	Name  string `json:"name" example:"Jane Doe"`
	Email string `json:"email" example:"jane.doe@example.com"`
}

func main() {
	app := gota.New(gota.Config{
		Title:       "Gota API",
		Version:     "1.0.0",
		Description: "REST API built with the REPR Design Pattern.",
	})

	// 3. REPR Endpoint
	gota.Post(app, "/users", func(ctx *gota.Context, req CreateUserRequest) (UserResponse, error) {
		// Input binding and validation have already executed automatically!
		created := UserResponse{
			ID:    "usr_1",
			Name:  req.Name,
			Email: req.Email,
		}
		return created, nil // Automatically serialized and returned as 201 Created
	}).
		Summary("Register user").
		Tags("Users")

	log.Println("📖 Scalar UI (Classic Layout): http://localhost:3000/docs")
	log.Println("📄 OpenAPI 3.1 Specification: http://localhost:3000/openapi.json")

	log.Fatal(app.Listen(":3000"))
}
```

---

## 🏛️ The REPR Pattern in Gota

In traditional MVC or generic `(w, r)` handlers, controllers accumulate multiple actions and require repetitive manual parsing and validation for each route.

Following the [**REPR Design Pattern**](https://deviq.com/design-patterns/repr-design-pattern/) and the philosophy of **FastEndpoints**, Gota organizes every route into a cohesive unit of three components:

1. **Request**: Defines the input contract (body, route params, query string, headers):
   ```go
   type GetUserRequest struct {
       ID   string `param:"id" doc:"User ID"`
       Tab  string `query:"tab" doc:"Profile tab"`
       Auth string `header:"Authorization" doc:"Bearer token"`
   }
   ```
2. **Endpoint**: Encapsulates business logic for that specific operation:

   ```go
   type GetUserEndpoint struct{}

   func (e *GetUserEndpoint) Handle(ctx *gota.Context, req GetUserRequest) (UserResponse, error) {
       return UserResponse{ID: req.ID, Name: "Jane Doe"}, nil
   }
   ```

3. **Response**: Defines the output contract and generates documentation schemas automatically.

---

## 📖 Interactive Documentation with Scalar

Gota automatically renders the interactive **Scalar** interface at `/docs` using the **`classic` layout** by default:

```go
app := gota.New(gota.Config{
    Title:        "Store API",
    Version:      "1.0.0",
    DocsURL:      "/docs",        // default
    OpenAPIURL:   "/openapi.json", // default
    ScalarLayout: "classic",      // default: "classic" (or "modern")
    ScalarTheme:  gota.ThemeLaserwave,
})
```

---

## 📁 Project Structure

```
gota/
├── binding/         # Automated model binding (param, query, header, json)
│   ├── binder.go
│   └── binder_test.go
├── core/            # Core HTTP engine (app, context, router, middleware, openapi, scalar)
│   ├── app.go
│   ├── context.go
│   ├── errors.go
│   ├── middleware.go
│   ├── openapi.go
│   ├── router.go
│   ├── router_test.go
│   └── scalar.go
├── examples/        # Showcase example application
│   └── main.go
├── validation/      # Input validation engine and declarative rules
│   ├── validator.go
│   └── validator_test.go
├── endpoint.go      # First-class REPR generic endpoint engine
├── endpoint_test.go # End-to-end test suite
├── gota.go          # Clean public entry point (facade)
├── go.mod
├── logo.svg
├── LICENSE
└── README.md
```

---

## 🧹 Code Formatting

To recursively format all Go source files starting from the repository root:

```bash
go fmt ./...
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

Then visit:

- **Scalar UI**: [http://localhost:3000/docs](http://localhost:3000/docs)
- **OpenAPI 3.1 Spec**: [http://localhost:3000/openapi.json](http://localhost:3000/openapi.json)

---

## 📄 License

MIT © [Carlos Felipe](https://github.com/carlosxfelipe)
