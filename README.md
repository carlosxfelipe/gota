<div align="center">
	<img src="logo.svg" width="90px" alt="Gota Framework">
</div>

<h1 align="center">Gota Framework 💧</h1>

<div align="center">

[![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Pattern](https://img.shields.io/badge/pattern-REPR-blue)](https://deviq.com/design-patterns/repr-design-pattern/)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-3.1-6BA539)](http://localhost:3000/openapi.json)
[![Scalar Docs](https://img.shields.io/badge/Scalar-Classic%20Layout-purple)](http://localhost:3000/docs)

</div>

**Gota** is a minimalist, high-performance web micro-framework for Go with **zero external dependencies**, built around the [REPR Design Pattern (Request-Endpoint-Response)](https://deviq.com/design-patterns/repr-design-pattern/) and featuring native interactive API documentation powered by **Scalar** (configured with the `classic` layout by default).

Inspired by the ergonomic fluency of [Kite](https://github.com/claudiovictors/kite) and the cohesive endpoint architecture popularized by [FastEndpoints](https://fast-endpoints.com/).

---

## ✨ Key Features

- **REPR Design Pattern**: Request-Endpoint-Response architecture with automatic model binding, declarative validation, and typed JSON / streaming responses.
- **Zero External Dependencies**: Built 100% on the Go standard library.
- **Native OpenAPI 3.1 & Scalar Docs**: Automatic spec generation from Go types, serving interactive Scalar UI (`classic` layout) at `/docs`.
- **Auto-Generated TypeScript Definitions & Client SDK**: Optional zero-dependency TypeScript declarations (`/types.d.ts`) and standalone client SDK (`/sdk.ts`), or exported programmatically via `app.GenerateTypeScript()` / `app.GenerateTypeScriptSDK()`, providing end-to-end type safety from Go to frontend without npm or build tools.
- **Fluent Route Ergonomics**: Chainable metadata (`.Summary()`, `.Tags()`, `.Middleware()`), route groups, and parameter matching (`:id`, `*wildcard`).
- **Production-Ready Middlewares**: Built-in JWT (HS256, RS256, ES256, EdDSA), CORS, Security Headers, Rate Limiter, Gzip, Panic Recovery, Request ID, Slog logger, Timeouts, and Graceful Shutdown.

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
		return created, nil // Automatically serialized and returned as 200 OK (use .Status(201) when preferred)
	}).
		Summary("Register user").
		Tags("Users")

	log.Fatal(app.Listen(":3000"))
}
```

Run your application:

```bash
go run main.go
```

---

## 🏛️ The REPR Pattern in Gota

In traditional MVC or generic `(w, r)` handlers, controllers accumulate multiple actions and require repetitive manual parsing and validation for each route.

Following the [REPR Design Pattern](https://deviq.com/design-patterns/repr-design-pattern/) and the philosophy of [FastEndpoints](https://fast-endpoints.com/), Gota organizes every route into a cohesive unit of three components:

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

Gota automatically renders the interactive **Scalar** interface at `/docs` using the **`classic` layout** and **`fastify` theme** by default:

```go
app := gota.New(gota.Config{
    Title:        "Store API",
    Version:      "1.0.0",
    DocsURL:      "/docs",         // default
    OpenAPIURL:   "/openapi.json", // default
    ScalarLayout: "classic",       // default
    ScalarTheme:  gota.ThemeFastify, // default
    MaxBodyBytes: 10 << 20,       // default: 10MB (negative value to disable)
    MaxMultipartMemory: 32 << 20, // default: 32MB kept in memory (negative value to disable limit)
})
```

The Scalar script is loaded from a **pinned version with Subresource Integrity (SRI)** (`gota.DefaultScalarCDN` / `gota.DefaultScalarSRI`), so a compromised or updated CDN package cannot change what runs in your docs page. To self-host or use another CDN (e.g. air-gapped environments), set `ScalarScriptURL` and, optionally, `ScalarSRI`:

```go
app := gota.New(gota.Config{
    ScalarScriptURL: "/static/scalar.js",
    ScalarSRI:       "sha384-...", // leave empty to skip the integrity check
})
```

### 🔒 Securing or Disabling Documentation in Production

Documentation routes (`/docs` and `/openapi.json`) are public by default. For production environments, Gota provides two recommended approaches:

#### 1. Protect with Credentials (`app.UseDocs`)

Keep documentation accessible to your team while preventing public access using HTTP Basic Auth (or any custom middleware):

```go
// Restricts access to both /docs and /openapi.json without affecting regular API routes
app.UseDocs(gota.BasicAuth("admin", os.Getenv("DOCS_PASSWORD")))
```

_(You can also configure this declaratively via `DocsMiddlewares: []gota.MiddlewareFunc{...}` inside `gota.Config`.)_

#### 2. Disable Completely in Production (`DisableDocs`)

Completely turn off `/docs` and `/openapi.json` so they are not registered at all (returns 404):

```go
app := gota.New(gota.Config{
    DisableDocs: os.Getenv("ENV") == "production",
})
```

---

## 🔷 Auto-Generated TypeScript Definitions & Client SDK

Gota bridges your Go backend and frontend with **zero-dependency, end-to-end type safety** (similar to tRPC, but natively across Go and TypeScript). At runtime, Gota inspects your REPR routes and models to serve both type declarations and a ready-to-run client SDK:

- **Type Definitions (`/types.d.ts`)**: Pure TypeScript contracts (`.d.ts`). Contains all model interfaces, route signatures, and helper types. Zero runtime footprint (erased at build time).
- **Client SDK (`/sdk.ts`)**: A complete, zero-dependency client script (`.ts`) that includes all type definitions **plus** the native `createGotaClient` fetch implementation.

### 🚀 Frontend Quickstart

You can pull the client SDK directly into your frontend project (React, Next.js, Vue, Svelte, Node, Bun, etc.):

```bash
# Save the generated SDK into your frontend
curl -o src/api.ts http://localhost:3000/sdk.ts
```

Then make fully typed requests with autocomplete and compile-time validation:

```typescript
import { createGotaClient } from "./api";

const api = createGotaClient("http://localhost:3000");

// 1. The editor autocompletes valid routes as soon as you type quotes:
// "GET /users" | "GET /users/:id" | "POST /users" | ...
const user = await api.fetch("GET /users/:id", {
  params: { id: "usr_1" }, // TypeScript enforces required route params!
});

console.log(user.name); // 100% typed: inferred directly from Go struct UserResponse

// 2. Type-safe POST/PUT requests with autocompleted bodies
const newUser = await api.fetch("POST /users", {
  body: {
    name: "Jane Doe",
    email: "jane.doe@example.com",
    role: "admin",
  },
});
```

### 💡 Why `createGotaClient`?

The built-in client wrapper solves classic frontend `fetch` pitfalls with zero external npm dependencies:

- **End-to-End Type Safety**: If a Go struct field or route changes, your frontend fails at compile/build time, preventing runtime bugs before deployment.
- **Safe Route Interpolation**: Automatically encodes and replaces `:param` and `*wildcard` values using `encodeURIComponent`.
- **Automatic Query String Serialization**: Pass `{ query: { page: 1, search: "go" } }` without manual URL string concatenation.
- **Body & FormData Auto-Detection**: Automatically serializes objects to JSON with `Content-Type: application/json`, or preserves multipart boundaries for `FormData` file uploads.
- **True Error Handling**: Native `fetch` does not reject promises on HTTP 4xx/5xx; `createGotaClient` automatically parses error payloads and throws catchable errors with `status` and `data`.
- **204 No Content Protection**: Avoids the common `SyntaxError: Unexpected end of JSON input` by safely returning `undefined` for 204 responses.

### ⚙️ Enabling TypeScript Routes

By default, TypeScript routes are disabled (opt-in) to keep API endpoints clean and minimal. You can enable them by setting their URLs in `gota.Config`:

```go
app := gota.New(gota.Config{
    TypeScriptURL:    "/types.d.ts", // Enable TypeScript declarations
    TypeScriptSDKURL: "/sdk.ts",     // Enable standalone client SDK
})
```

You can also export them programmatically (e.g. in a code generation script or CI pipeline) without registering HTTP routes:

```go
typesDefs := app.GenerateTypeScript()    // Returns .d.ts content as string
clientSDK := app.GenerateTypeScriptSDK() // Returns .ts SDK content as string
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
- **TypeScript Defs**: [http://localhost:3000/types.d.ts](http://localhost:3000/types.d.ts)
- **TypeScript Client SDK**: [http://localhost:3000/sdk.ts](http://localhost:3000/sdk.ts)

> 💡 Looking for a full-featured showcase with JWT authentication, role-based access control, multipart file uploads, Server-Sent Events (SSE), and a Bruno collection? Check out [order-flow-gota](https://github.com/carlosxfelipe/order-flow-gota).

---

## 📄 License

MIT © [Carlos Felipe](https://github.com/carlosxfelipe)
