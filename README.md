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
- **Fluent Route Ergonomics**: Chainable metadata (`.Summary()`, `.Tags()`, `.Middleware()`), route groups, and parameter matching (`:id`, `*wildcard`).
- **Production-Ready Middlewares**: Built-in JWT (HS256 & RS256), CORS, Panic Recovery, Request ID, Slog logger, timeouts, and graceful shutdown.

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

- **Scalar UI**: [http://localhost:3000/docs](http://localhost:3000/docs) (or open [http://localhost:3000](http://localhost:3000) which redirects to `/docs`)
- **OpenAPI 3.1 Spec**: [http://localhost:3000/openapi.json](http://localhost:3000/openapi.json)

### 👥 Seed Users & Credentials

The showcase is pre-seeded with users for testing role-based workflows:

| Username | Password    | Role       | Permissions                                  |
| -------- | ----------- | ---------- | -------------------------------------------- |
| `admin`  | `admin123`  | `Admin`    | Full access (views all orders in the system) |
| `carlos` | `carlos123` | `Customer` | Scoped to own orders (seed `Created` order)  |
| `maria`  | `maria123`  | `Customer` | Scoped to own orders (seed `Paid` order)     |
| `luis`   | `luis123`   | `Customer` | Scoped to own orders (seed `Shipped` order)  |

### 🛣️ Showcase API Routes (Order Flow)

| Method   | Endpoint                             | Auth   | Description                                                                                                 |
| -------- | ------------------------------------ | ------ | ----------------------------------------------------------------------------------------------------------- |
| `POST`   | `/api/auth/register`                 | Public | Registers a new `Customer` account (role is always server-assigned) and returns an initial JWT Bearer token |
| `POST`   | `/api/auth/login`                    | Public | Authenticates credentials and returns a signed JWT Bearer token                                             |
| `GET`    | `/api/auth/me`                       | Bearer | Retrieves the profile of the currently authenticated user                                                   |
| `DELETE` | `/api/auth/account`                  | Bearer | Permanently deletes the authenticated user's account                                                        |
| `GET`    | `/api/orders`                        | Bearer | Lists orders (scoped by Customer, or full list for Admin)                                                   |
| `POST`   | `/api/orders`                        | Bearer | Creates a new order in `Created` state                                                                      |
| `GET`    | `/api/orders/:orderId`               | Bearer | Retrieves order details, items, and calculated `totalAmount`                                                |
| `POST`   | `/api/orders/:orderId/items`         | Bearer | Adds a line item to an order still in `Created` state                                                       |
| `DELETE` | `/api/orders/:orderId/items/:itemId` | Bearer | Removes a line item from an open `Created` order                                                            |
| `POST`   | `/api/orders/:orderId/pay`           | Bearer | Pays order with items and transitions status to `Paid`                                                      |
| `POST`   | `/api/orders/:orderId/ship`          | Bearer | Ships paid order and transitions status to `Shipped`                                                        |
| `POST`   | `/api/orders/:orderId/cancel`        | Bearer | Cancels order unless it was already `Shipped`                                                               |
| `POST`   | `/api/orders/:orderId/receipt`       | Bearer | Uploads a payment receipt or invoice document (`multipart/form-data`)                                       |
| `GET`    | `/api/orders/:orderId/track`         | Bearer | Streams real-time order updates via Server-Sent Events (`SSEStream`)                                        |

### 🟠 Testing with Bruno

A pre-configured [Bruno](https://www.usebruno.com/) collection is ready to use in [`examples/bruno/`](examples/bruno):

#### Desktop App (GUI):

1. Open the `examples/bruno` folder in the Bruno app.
2. Run **`10. Login (Customer)`**, **`9. Login (Admin)`**, or create a new user with **`11. Register User`** — the response script automatically stores `jwt_token`.
3. Test your profile with **`12. Get Current User (Me)`**, delete your account with **`13. Delete Account`**, or execute any of the order lifecycle requests (**`Create Order`**, **`Add Item`**, **`Pay Order`**, **`Upload Receipt`**, **`Track Order (SSE)`**, **`Ship Order`**, etc.).

#### Command Line (Bruno CLI):

With the Gota server running (`go run examples/main.go`), execute the entire collection automatically via terminal:

```bash
cd examples/bruno
npx -y @usebruno/cli run --env Local
```

---

## 📄 License

MIT © [Carlos Felipe](https://github.com/carlosxfelipe)
