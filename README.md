<div align="center">
	<img src="logo.svg" width="90px" alt="Gota Framework">
</div>

<h1 align="center">Gota Framework 💧</h1>

<div align="center">

[![Go Version](https://img.shields.io/badge/go-1.27%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Pattern](https://img.shields.io/badge/pattern-REPR-blue)](https://deviq.com/design-patterns/repr-design-pattern/)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-3.1-6BA539)](#examples)
[![Scalar Docs](https://img.shields.io/badge/Docs-Scalar-purple)](#examples)

</div>

**Gota** is a pragmatic Go web framework built around the **REPR (Request-Endpoint-Response)** pattern with automatic **OpenAPI 3.1**, **Scalar** docs, and **TypeScript SDK** generation — built on top of `net/http`, [go-chi/chi/v5](https://github.com/go-chi/chi), and [go-playground/validator/v10](https://github.com/go-playground/validator).

## Features

- **Typed REPR Endpoints**: Define endpoints with Go request and response structs, automatic binding (`param`, `query`, `header`, `form`, and JSON), validation, structured errors, and SSE streaming.
- **OpenAPI 3.1 & Scalar Docs**: Automatic OpenAPI spec at `/openapi.json` and interactive Scalar documentation at `/docs` with built-in security headers (CSP, SRI), themes, and isolated docs middlewares.
- **TypeScript SDK Codegen**: Generates a modular, zero-dependency `fetch` client directly from your Go routes, with automatic import-extension detection and CI drift checks (`--check`).
- **Standard Go Ecosystem**: Built on `net/http` and `chi`, supporting standard middlewares, route groups, safe server defaults, and fail-fast startup checks.

## Installation

Initialize a new Go module (if starting a new project):

```bash
go mod init my-app
```

Install Gota:

```bash
go get github.com/carlosxfelipe/gota
```

Requires Go 1.27 or newer.

## Examples

A Todos API example using `package repr` is available in [examples/](examples):

- Server & routes setup: [examples/main.go](examples/main.go), [examples/todos/todos.go](examples/todos/todos.go) (REPR), and [examples/system/system.go](examples/system/system.go) (non-REPR)
- Offline TypeScript SDK generator: [examples/codegen/main.go](examples/codegen/main.go)

Start the example API (Scalar UI at `/docs` with `admin` / `admin123`):

```bash
go run ./examples/main.go
```

Export the modular TypeScript SDK to `examples/codegen/dist/`:

```bash
go run ./examples/codegen/main.go
```

## Documentation for LLMs

A complete framework usage reference for AI coding assistants is available in [llms.txt](llms.txt).

To keep AI coding assistants aligned with modern Go idioms matching [go.mod](go.mod), you can install the [go-modern-guidelines](https://github.com/JetBrains/go-modern-guidelines) skill globally:

```bash
npx skills add JetBrains/go-modern-guidelines -g
```

## Development

Format Go, JavaScript, and shell files ([fmt.sh](fmt.sh)):

```bash
./fmt.sh
```

Run tests:

```bash
go test ./...
```

## License

Licensed under the [MIT License](LICENSE).
