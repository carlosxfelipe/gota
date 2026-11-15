<div align="center">
	<img src="logo.svg" width="90px" alt="Gota Framework">
</div>

<h1 align="center">Gota Framework 💧</h1>

<div align="center">

[![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Pattern](https://img.shields.io/badge/pattern-REPR-blue)](https://deviq.com/design-patterns/repr-design-pattern/)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-3.1-6BA539)](#examples)
[![Scalar Docs](https://img.shields.io/badge/Scalar-Classic%20Layout-purple)](#examples)

</div>

**Gota** is a pragmatic Go web framework built around the **REPR (Request-Endpoint-Response)** pattern with automatic **OpenAPI 3.1**, **Scalar** docs, and **TypeScript SDK** generation — built on top of `net/http`, [go-chi/chi/v5](https://github.com/go-chi/chi), and [go-playground/validator/v10](https://github.com/go-playground/validator).

## Features

- **Typed REPR Endpoints**: `Endpoint[Req, Res]` / `EndpointFunc[Req, Res]` with `param`, `query`, `header`, `form`, and JSON binding, validation, typed responses, and SSE streams.
- **OpenAPI 3.1 & Scalar**: Automatic spec at `/openapi.json`, interactive UI at `/docs` (with SRI, themes, CSP, and isolated `Config.DocsMiddlewares`), plus `app.ExportOpenAPI()`.
- **TypeScript SDK**: Generates a modular, zero-dependency, tree-shakeable `fetch` client (`createGotaClient`, `parseGotaError`, `isAbortError`, `queryOptions`) split by tag (`group.Tags()` / `route.Tags()`, with path-segment fallback), with `--ts-import-ext=auto` and CI drift verification (`--check`), exported via `app.ExportTypeScriptSDK()` / `app.RunCodegen()`.
- **`net/http` & `chi` Native**: Standard `func(http.Handler) http.Handler` middlewares via `app.Use(...)` and `group.Use(...)`, with safe body/header limits and startup tag checks.

## Installation

Initialize a new Go module (if starting a new project):

```bash
go mod init my-app
```

Install Gota:

```bash
go get github.com/carlosxfelipe/gota
```

Requires Go 1.26 or newer.

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
