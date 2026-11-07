<div align="center">
	<img src="logo.svg" width="90px" alt="Gota Framework">
</div>

<h1 align="center">Gota Framework 💧</h1>

<div align="center">

[![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Pattern](https://img.shields.io/badge/pattern-REPR-blue)](https://deviq.com/design-patterns/repr-design-pattern/)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-3.1-6BA539)](#-interactive-documentation-with-scalar)
[![Scalar Docs](https://img.shields.io/badge/Scalar-Classic%20Layout-purple)](#-interactive-documentation-with-scalar)

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

Create a `main.go` file (also available in [`examples/main.go`](examples/main.go)):

```go
package main

import (
	"log"
	"os"
	"time"

	"github.com/carlosxfelipe/gota"
)

// ----------------------------------------------------------------------
// Request and Response Contracts (REPR Pattern)
// ----------------------------------------------------------------------

type CreateUserRequest struct {
	Name  string `json:"name" doc:"Full name of user" example:"Jane Doe" validate:"required,min=3"`
	Email string `json:"email" doc:"Valid corporate or personal email" example:"jane.doe@example.com" validate:"required,email"`
	Role  string `json:"role" doc:"User access role" example:"admin" default:"member"`
}

type GetUserRequest struct {
	ID string `param:"id" doc:"User unique identifier" example:"usr_1"`
}

type UserResponse struct {
	ID    string `json:"id" example:"usr_1"`
	Name  string `json:"name" example:"Jane Doe"`
	Email string `json:"email" example:"jane.doe@example.com"`
	Role  string `json:"role" example:"admin"`
}

type HealthResponse struct {
	Status  string `json:"status" example:"ok"`
	Version string `json:"version" example:"1.0.0"`
}

var mockUsers = []UserResponse{
	{ID: "usr_1", Name: "Jane Doe", Email: "jane.doe@example.com", Role: "admin"},
	{ID: "usr_2", Name: "John Smith", Email: "john.smith@example.com", Role: "member"},
}

// ----------------------------------------------------------------------
// Struct-Based REPR Endpoint Example
// ----------------------------------------------------------------------

type GetUserEndpoint struct{}

func (e *GetUserEndpoint) Handle(ctx *gota.Context, req GetUserRequest) (UserResponse, error) {
	for _, u := range mockUsers {
		if u.ID == req.ID {
			return u, nil
		}
	}
	return UserResponse{}, gota.NotFound("User not found")
}

// ----------------------------------------------------------------------
// Main Application
// ----------------------------------------------------------------------

func main() {
	isProd := os.Getenv("ENV") == "production"

	// In production, disable runtime HTTP TypeScript endpoints (use offline export instead)
	tsURL := "/types.d.ts"
	tsSDKURL := "/sdk.ts"
	if isProd {
		tsURL = "-"
		tsSDKURL = "-"
	}

	// Initializes Gota with built-in OpenAPI 3.1, Scalar docs, and TypeScript SDK routes
	app := gota.New(gota.Config{
		Title:            "Gota REPR API",
		Version:          "1.0.0",
		Description:      "Go web framework oriented around the REPR Pattern with native interactive Scalar UI.",
		TypeScriptURL:    tsURL,
		TypeScriptSDKURL: tsSDKURL,
	})

	// Global Middlewares
	app.Use(gota.RequestID())
	app.Use(gota.Logger())
	app.Use(gota.Recover())
	app.Use(gota.CORS())

	// Protect documentation, schemas, and SDK routes (/docs, /openapi.json, /types.d.ts, /sdk.ts)
	// with HTTP Basic Auth and built-in Rate Limiting to prevent brute-force attacks:
	docsPassword := os.Getenv("DOCS_PASSWORD")
	if docsPassword == "" {
		if isProd {
			log.Fatal("DOCS_PASSWORD environment variable must be set in production")
		}
		docsPassword = "admin123" // Fallback password for local development
	}
	app.UseDocs(gota.BasicAuth("admin", docsPassword, gota.BasicAuthOptions{
		Realm: "Gota API Documentation",
		RateLimit: &gota.RateLimiterOptions{
			Max:    10,          // Max 10 attempts
			Window: time.Minute, // per minute per client IP
		},
		AllowWeakCredentials: true,   // silences the weak password warning during local development
		RequireTLS:           isProd, // enforces HTTPS for remote clients in production
	}))

	// 1. Healthcheck Endpoint (Empty Request)
	gota.Get(app, "/", func(ctx *gota.Context, _ gota.Empty) (HealthResponse, error) {
		return HealthResponse{Status: "ok", Version: "1.0.0"}, nil
	}).
		Summary("Healthcheck").
		Description("Returns system status and current version").
		Tags("System")

	// 2. List Users (Empty Request)
	gota.Get(app, "/users", func(ctx *gota.Context, _ gota.Empty) ([]UserResponse, error) {
		return mockUsers, nil
	}).
		Summary("List all users").
		Tags("Users")

	// 3. Get User by ID (Struct-based REPR Endpoint)
	userEndpoint := &GetUserEndpoint{}
	gota.Get(app, "/users/:id", userEndpoint.Handle).
		Summary("Get user by ID").
		Tags("Users")

	// 4. Create User (Function-based REPR with automatic binding and validation)
	gota.Post(app, "/users", func(ctx *gota.Context, req CreateUserRequest) (UserResponse, error) {
		newUser := UserResponse{
			ID:    "usr_3",
			Name:  req.Name,
			Email: req.Email,
			Role:  req.Role,
		}
		mockUsers = append(mockUsers, newUser)
		return newUser, nil
	}).
		Status(201).
		Summary("Register user").
		Description("Validates input data automatically and returns the created user (201 Created)").
		Tags("Users")

	// 5. Classic route (without REPR structs, for quick hooks)
	app.Get("/ping", func(ctx *gota.Context) error {
		return ctx.Json(gota.Map{"message": "pong"})
	}).Summary("Ping Pong").Tags("System")

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

3. **Response**: Defines the output contract and generates documentation schemas automatically:
   ```go
   type UserResponse struct {
       ID   string `json:"id" example:"usr_1"`
       Name string `json:"name" example:"Jane Doe"`
   }
   ```

Finally, register the endpoint on the router with automatic type inference and documentation:

```go
gota.Get(app, "/users/:id", (&GetUserEndpoint{}).Handle).
    Summary("Get user profile").
    Tags("Users")
```

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

Documentation routes (`/docs`, `/openapi.json`, `/types.d.ts`, `/sdk.ts`) are public by default with automatic anti-caching and CORS isolation. For production environments, Gota provides flexible protection options:

#### 1. Protect with Credentials (`app.UseDocs`)

```go
// Protect with BasicAuth, rate limiting, and HTTPS enforcement:
app.UseDocs(gota.BasicAuth("admin", os.Getenv("DOCS_PASSWORD"), gota.BasicAuthOptions{
    RateLimit:  &gota.RateLimiterOptions{Max: 5, Window: time.Minute},
    RequireTLS: os.Getenv("ENV") == "production", // Rejects remote plain-HTTP credentials
}))

// Or restrict to private VPN / CIDR subnets:
app.UseDocs(gota.IPWhitelist("10.0.0.0/8", "192.168.1.0/24"))

// Or protect with a developer token (Authorization: Bearer <token>):
app.UseDocs(gota.BearerToken(os.Getenv("DOCS_TOKEN")))
```

#### 2. Disable Completely in Production (`DisableDocs`)

```go
app := gota.New(gota.Config{
    DisableDocs: os.Getenv("ENV") == "production",
})
```

#### 3. Batch Route Hiding (`Group.Hidden()`)

Hide entire internal, administrative, or diagnostic subtrees from both OpenAPI documentation and TypeScript generation in a single call:

```go
admin := app.Group("/admin").Hidden()
admin.Get("/metrics", metricsHandler) // Executable via HTTP, but absent from /openapi.json and /types.d.ts
```

---

## 🔷 Auto-Generated TypeScript Definitions & Client SDK

Gota bridges your Go backend and frontend with **zero-dependency, end-to-end type safety** (similar to tRPC, but natively across Go and TypeScript). At runtime, Gota inspects your REPR routes and models to serve both type declarations and a ready-to-run client SDK:

- **Type Definitions (`/types.d.ts`)**: Pure TypeScript contracts (`.d.ts`). Contains all model interfaces, route signatures, and helper types. Zero runtime footprint (erased at build time).
- **Client SDK (`/sdk.ts`)**: A complete, zero-dependency client script (`.ts`) that includes all type definitions **plus** the native `createGotaClient` fetch implementation.

### 🚀 Generating the SDK for your Frontend

The client SDK is generated at **build/development time** (committed to your frontend repo or produced during CI/CD), so the `/sdk.ts` route never needs to be exposed in production:

#### Option A: During Local Development (via curl)

While your local Go development server is running:

```bash
# If docs/SDK routes are protected by BasicAuth (as configured in the Quickstart):
curl -u admin:admin123 -o src/api.ts http://localhost:3000/sdk.ts

# Or without credentials if app.UseDocs() is not configured:
curl -o src/api.ts http://localhost:3000/sdk.ts
```

#### Option B: Offline / CI/CD (without running an HTTP server)

Export directly to your frontend project in a build script or CI/CD pipeline (see [`examples/codegen/main.go`](examples/codegen/main.go)):

```go
_ = app.ExportTypeScriptSDK("./frontend/src/api.ts")
```

---

### 💻 Using the Generated Client

Once `src/api.ts` is in your frontend, use it with full compile-time type safety and route autocomplete:

```typescript
import { createGotaClient } from "./api";

// Configure with your API base URL and optional global client settings (auth headers, cookies, custom fetch)
const api = createGotaClient(
  process.env.NEXT_PUBLIC_API_URL || "http://localhost:3000",
  {
    credentials: "include",
    headers: async () => ({
      Authorization: `Bearer ${await getAccessToken()}`,
    }),
  },
);

// 1. The editor autocompletes valid routes as soon as you type quotes:
// "GET /users" | "GET /users/:id" | "POST /users" | ...
const user = await api.fetch("GET /users/:id", {
  params: { id: "usr_1" }, // TypeScript enforces required route params!
});

console.log(user.name); // 100% typed (non-optional string): inferred directly from Go struct UserResponse

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
- **Smart Request vs. Response Contracts**: Request fields are required when marked with `validate:"required"`, while Response fields are automatically non-optional (`T`) unless they are pointers (`*T`) or tagged with `json:",omitempty"` / `omitzero`.
- **Global Client Options (`GotaClientOptions`)**: Configure static or async dynamic `headers` (e.g. injecting fresh JWT tokens), `credentials` (`"include"`), and custom `fetch` implementations once at client creation, with per-request overrides.
- **Safe Route Interpolation**: Automatically sorts parameter keys longest-first (preventing `:id` vs `:id_item` prefix collisions) and encodes `:param` and `*wildcard` values using `encodeURIComponent`.
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

You can also export them directly to disk (e.g. in a CI/CD pipeline, `go:generate`, or build script) without exposing HTTP routes in production:

```go
// Export directly to disk (automatically creates parent directories)
_ = app.ExportTypeScriptSDK("./frontend/src/api/sdk.ts")
_ = app.ExportTypeScript("./frontend/src/api/types.d.ts")

// Or retrieve raw strings:
typesDefs := app.GenerateTypeScript()
clientSDK := app.GenerateTypeScriptSDK()
```

> 🔒 **Security Best Practice for Production**: In production, do not serve `/types.d.ts` or `/sdk.ts` over HTTP. Generate the SDK offline via `app.ExportTypeScriptSDK(...)` in your CI/CD build step (see `examples/codegen/main.go`), and set `TypeScriptURL: "-"` and `TypeScriptSDKURL: "-"` in your production environment. Gota automatically issues a startup warning if TypeScript routes are exposed publicly in production environments.
>
> 🛡️ **Model Schema Auditing**: Use `gota.AuditModel(MyModel{})` in your test suite to assert that sensitive entity fields (e.g. `PasswordHash`, `ClientSecret`, `ResetToken`) are hidden with `json:"-"` and never leaked into API contracts.

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

> 🔐 **Authentication**: Documentation and SDK routes in `examples/main.go` are protected by HTTP Basic Auth.
>
> - **Username**: `admin`
> - **Password**: `admin123` (or the value of `$DOCS_PASSWORD` if set)

> 💡 Looking for a full-featured showcase with JWT authentication, role-based access control, multipart file uploads, Server-Sent Events (SSE), and a Bruno collection? Check out [order-flow-gota](https://github.com/carlosxfelipe/order-flow-gota).

---

## 📄 License

MIT © [Carlos Felipe](https://github.com/carlosxfelipe)
