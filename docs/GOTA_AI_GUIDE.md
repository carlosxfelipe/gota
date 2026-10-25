# Gota Framework — AI Assistant & Developer Guide 💧

> **Instructions for AI Assistants (LLMs):**
> This document contains the definitive specification, patterns, and rules for the **Gota** web framework in Go. Because Gota is a modern, specialized micro-framework, use the patterns, signatures, and constraints defined here rather than guessing or applying patterns from other frameworks (like Gin or Fiber).

---

## 1. Overview & Core Philosophy

**Gota** is a minimalist, zero-dependency Go web micro-framework built around the **REPR Design Pattern (Request-Endpoint-Response)** with native interactive API documentation powered by **Scalar** and automatic **OpenAPI 3.1** generation.

- **Package Import**: `github.com/carlosxfelipe/gota`
- **Zero External Dependencies**: Built entirely on the Go standard library (`net/http`, `reflect`, `encoding/json`, `sync`, `crypto`).
- **Implements `net/http.Handler`**: `app` can be passed directly to `http.ListenAndServe`, `httptest.NewServer`, or standard Go middlewares.
- **Go Version**: Requires Go 1.26+.

---

## 2. Application Bootstrap

`gota.New` accepts **exactly one `gota.Config` struct** (it is **not** variadic):

```go
package main

import (
	"log"

	"github.com/carlosxfelipe/gota"
)

func main() {
	// All fields have sensible defaults; pass gota.Config{} if defaults are sufficient
	app := gota.New(gota.Config{
		Title:        "My API",
		Version:      "1.0.0",
		Description:  "REST API built with Gota REPR pattern",
		DocsURL:      "/docs",         // Default: /docs
		OpenAPIURL:   "/openapi.json", // Default: /openapi.json
		ScalarTheme:  gota.ThemeDefault, // Themes: ThemeDefault, ThemeMoon, ThemeLaserwave, etc. (default: ThemeLaserwave)
		ScalarLayout: "classic",      // "classic" (Swagger-style) or "modern" (default: "classic")
		MaxBodyBytes: 10 << 20,       // 10MB limit (default: 10MB, negative to disable)
	})

	// Global Middlewares
	app.Use(gota.Logger())
	app.Use(gota.Recover())
	app.Use(gota.CORS())

	// Start server (logs http://localhost:3000, /docs, and /openapi.json)
	log.Fatal(app.Listen(":3000"))
}
```

---

## 3. The REPR Pattern in Gota

Instead of monolithic controllers with manual parsing, Gota organizes every route into a cohesive unit:

1. **Request DTO**: Strongly typed input contract with binding and validation tags.
2. **Endpoint**: Single responsibility handler logic.
3. **Response DTO**: Strongly typed output contract serialized automatically into JSON.

### 3.1 Request Struct Tags

Gota automatically binds fields from four sources using struct tags:

| Tag                 | Source                                  | Example                                   |
| :------------------ | :-------------------------------------- | :---------------------------------------- |
| `param:"name"`      | Path parameter (`:name` or `*wildcard`) | `ID string \`param:"id"\``                |
| `query:"name"`      | Query string parameter (`?name=...`)    | `Page int \`query:"page"\``               |
| `header:"Name"`     | HTTP Request Header                     | `Token string \`header:"Authorization"\`` |
| `json:"name"`       | JSON Request Body                       | `Title string \`json:"title"\``           |
| `validate:"rules"`  | Validation rules (comma-separated)      | `validate:"required,min=3,email"`         |
| `doc:"description"` | OpenAPI parameter/field description     | `doc:"User email address"`                |
| `example:"val"`     | OpenAPI example value                   | `example:"jane@example.com"`              |
| `format:"fmt"`      | OpenAPI schema format hint              | `format:"date-time"`                      |
| `default:"val"`     | OpenAPI default value                   | `default:"10"`                            |

#### Supported `validate` Rules:

- `required`: Field must not be zero-value or empty string.
- `min=N`: Minimum string length, slice length, or numeric value (e.g. `min=3`, `min=10.5`).
- `max=N`: Maximum string length, slice length, or numeric value (e.g. `max=100`).
- `email`: Valid email format via `mail.ParseAddress`.
- `url`: Valid URL format via `url.ParseRequestURI`.
- `uuid`: Valid UUID format (`xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`).

> **Fail-Fast Tag Verification**: Gota verifies all `validate` tags at startup during route registration via `validation.CheckTags`. If a rule name is misspelled (e.g. `validate:"requried"`) or a numeric parameter is missing or invalid (e.g. `min=abc`), Gota **panics immediately** upon registration to prevent silent security regressions.

#### Custom Struct Validation (`Validator` & `ContextValidator`):

Beyond declarative struct tags, request structs can implement custom business validation via either of two interfaces:

```go
// 1. Standard Validator
func (r *CreateOrderRequest) Validate() error {
	if r.Quantity <= 0 {
		return errors.New("quantity must be greater than zero")
	}
	return nil
}

// 2. ContextValidator (for checks that need HTTP Context, auth, headers, etc.)
func (r *CreateOrderRequest) Validate(ctx *core.Context) error {
	if ctx.Header("X-Tenant-ID") == "" {
		return errors.New("missing tenant header")
	}
	return nil
}
```

> **Validation Failure**: Automatically returns `HTTP 422 Unprocessable Entity` with a structured `ValidationError` JSON response. Tag validation failures are keyed by field name (or JSON property name), and custom validator errors are placed under `_general`.

### 3.2 Response Struct & Special Types

- Any Go struct with `json` tags is automatically serialized into JSON with `HTTP 200 OK`.
- **No Content**: Use `gota.Empty{}` (or `struct{}{}`) as the Response type to automatically return `HTTP 204 No Content`.
- **Explicit Status**: Chain `.Status(201)` on the route registration, or call `ctx.Status(http.StatusCreated)` inside the handler.

---

## 4. Route Registration

### 4.1 Function-Based Endpoints (Generics)

Use package-level functions `gota.Get`, `gota.Post`, `gota.Put`, `gota.Patch`, `gota.Delete`.
Signature: `func(ctx *gota.Context, req Req) (Res, error)`

```go
type CreateUserRequest struct {
	Name  string `json:"name" doc:"User full name" validate:"required,min=3"`
	Email string `json:"email" doc:"User email" validate:"required,email"`
}

type UserResponse struct {
	ID    int    `json:"id" example:"1"`
	Name  string `json:"name" example:"Jane Doe"`
	Email string `json:"email" example:"jane@example.com"`
}

gota.Post(app, "/api/users", func(ctx *gota.Context, req CreateUserRequest) (UserResponse, error) {
	// req is already bound and validated!
	return UserResponse{ID: 1, Name: req.Name, Email: req.Email}, nil
}).
	Summary("Create new user").
	Description("Registers a user and returns their profile.").
	Tags("Users").
	Status(http.StatusCreated) // HTTP 201 Created
```

### 4.2 Struct-Based Endpoints (`Endpoint[Req, Res]`)

Ideal for vertical slice architecture, dependency injection, and keeping handlers isolated in separate files:

```go
type GetUserEndpoint struct {
	DB *sql.DB // Inject dependencies
}

func (e *GetUserEndpoint) Handle(ctx *gota.Context, req GetUserRequest) (UserResponse, error) {
	user, err := e.findUser(ctx.Request.Context(), req.ID)
	if err != nil {
		return UserResponse{}, gota.NotFound("User not found")
	}
	return user, nil
}

// Registration:
gota.HandleEndpoint(app, http.MethodGet, "/api/users/:id", &GetUserEndpoint{DB: db}).
	Summary("Get user by ID").
	Tags("Users")
```

### 4.3 Route Groups & Prefixes

Route groups share common URL prefixes and middlewares:

```go
api := app.Group("/api")
v1 := api.Group("/v1")

gota.Get(v1, "/ping", func(ctx *gota.Context, _ gota.Empty) (gota.Map, error) {
	return gota.Map{"message": "pong"}, nil
})
```

---

## 5. Error Handling

Always return `(Res{}, error)` from REPR handlers. Gota automatically renders standard error responses:

### 5.1 Built-in HTTP Errors

Use the built-in constructors to return specific HTTP status codes:

```go
return Res{}, gota.BadRequest("Invalid input parameters.")       // 400
return Res{}, gota.Unauthorized("Bearer token required.")       // 401
return Res{}, gota.Forbidden("Insufficient permissions.")        // 403
return Res{}, gota.NotFound("Resource not found.")              // 404
return Res{}, gota.UnprocessableEntity("Semantic error.")       // 422
return Res{}, gota.InternalServerError("Server failure.")       // 500
```

All of these produce standard JSON responses:

```json
{
  "error": "Resource not found.",
  "status": 404
}
```

### 5.2 Unexpected Errors (500 Protection)

If a raw Go `error` (e.g., database connection failure) is returned, Gota logs the actual error and stacktrace internally and returns a sanitized `500 Internal Server Error` to prevent information leakage:

```json
{
  "error": "internal server error",
  "status": 500
}
```

---

## 6. Authentication & JWT

Gota includes zero-dependency HS256 JWT utilities:

### 6.1 Signing and Verifying Tokens

```go
var Secret = []byte("my-super-secret-key")

type CustomClaims struct {
	UserID   int    `json:"userId"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

// Sign JWT:
token, err := gota.SignJWT(Secret, CustomClaims{
	UserID:   1,
	Username: "carlos",
	Role:     "Admin",
	Exp:      time.Now().Add(24 * time.Hour).Unix(),
})

// Verify raw JWT:
var claims CustomClaims
err := gota.VerifyJWT(Secret, tokenString, &claims)
```

### 6.2 Securing Routes with JWT Middleware

```go
// Protected group
protected := app.Group("/api/protected")
protected.Use(gota.JWT(Secret, gota.JWTOptions{
	ClaimsFactory: func() any { return &CustomClaims{} },
}))

// In any protected endpoint, extract claims from ctx:
gota.Get(protected, "/profile", func(ctx *gota.Context, _ gota.Empty) (UserResponse, error) {
	claims, ok := gota.JWTClaims[*CustomClaims](ctx)
	if !ok {
		return UserResponse{}, gota.Unauthorized("Unauthorized")
	}
	return UserResponse{ID: claims.UserID, Name: claims.Username}, nil
})
```

---

## 7. Fluent Route Metadata Chaining

Methods returned by `gota.Get`, `gota.Post`, `gota.HandleEndpoint`, etc.:

```go
route.Summary("Short summary for OpenAPI")
route.Description("Detailed description of what the endpoint does")
route.Tags("Auth", "Orders")           // Group in Scalar UI
route.Status(http.StatusAccepted)      // Change default success status
route.Deprecated()                     // Mark as deprecated in OpenAPI
route.Hidden()                         // Omit from OpenAPI and Scalar UI
route.Middleware(authMiddleware)       // Add route-specific middleware
route.Name("getUserById")              // Name route for reverse lookup via app.URLFor
```

Reverse URL lookup example:

```go
url, err := app.URLFor("getUserById", map[string]string{"id": "42"})
// Returns: "/api/users/42"
```

---

## 8. Non-REPR Low-Level Routes (Escape Hatch)

For infrastructure routes that do not need DTO contracts or OpenAPI generation (e.g. redirects, static file serving, simple healthchecks):

```go
// Standard Context handler: func(ctx *gota.Context) error
app.Get("/", func(ctx *gota.Context) error {
	return ctx.Redirect("/docs")
}).Hidden()

app.Get("/healthz", func(ctx *gota.Context) error {
	return ctx.Send("OK")
})
```

---

## 9. Critical Rules & Anti-Patterns for AI Agents

> [!CAUTION]
> **DO NOT DO THE FOLLOWING:**

1. ❌ **Do NOT use variadic config**: `gota.New()` does **NOT** accept `...Config`. Always pass a single struct: `app := gota.New(gota.Config{...})` or `app := gota.New(gota.Config{})`.
2. ❌ **Do NOT attempt generic methods on `app`**: `app.Get[Req, Res]` is invalid Go syntax. Always call `gota.Get(app, path, handler)` or `gota.Post(app, path, handler)`.
3. ❌ **Do NOT use `path:"id"` tag**: Path parameters in Gota use the tag `param:"id"`, NOT `path:"id"`. (Supports `:param` and `*wildcard`).
4. ❌ **Do NOT manually parse params/query/body inside REPR handlers**: Define them in the `Req` struct using tags; Gota automatically parses, converts, and validates them before invoking the handler.
5. ❌ **Do NOT manually encode JSON responses in REPR**: Return the response struct directly: `return MyResponse{...}, nil`.
6. ❌ **Do NOT omit error from handler returns**: Handlers must always return `(Res, error)`.
7. ❌ **Do NOT import third-party packages for Gota features**: Gota has built-in CORS, JWT, Logger, Recover, Validator, Scalar UI, and OpenAPI generator without requiring any external module dependencies.
8. ❌ **Do NOT mistype validation tags**: Gota validates all `validate` rules at startup; unknown rules or invalid arguments trigger an intentional `panic` during route registration.
