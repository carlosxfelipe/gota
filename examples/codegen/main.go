package main

import (
	"log"
	"os"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/todos"
)

// Example demonstrating offline TypeScript SDK code generation (e.g. for CI/CD or build scripts).
//
// Running this script writes the full client SDK to disk without needing to expose
// HTTP endpoints (/sdk.ts or /types.d.ts) in your production application.
//
// Usage:
//
//	go run ./examples/codegen/main.go
//	go run ./examples/codegen/main.go --ts-sdk=./dist/sdk.ts --ts-types=./dist/types.d.ts
//	go run ./examples/codegen/main.go --stdout
func main() {
	// 1. Initialize your app (or import your app router configuration)
	app := gota.New(gota.Config{
		Title:       "Gota REPR API",
		Version:     "1.0.0",
		Description: "Minimal Request-Endpoint-Response (REPR) API with Scalar OpenAPI 3.1 and TypeScript SDK.",
		// In production, keep HTTP endpoints disabled
		TypeScriptURL:    "-",
		TypeScriptSDKURL: "-",
	})

	// 2. Register your models and endpoints from the example package
	store := todos.NewTodoStore()
	todos.RegisterRoutes(app, store)

	// 3. Process CLI flags if passed (e.g. for go generate or CI)
	if handled, err := app.RunCodegen(os.Args[1:]); handled {
		if err != nil {
			log.Fatalf("Codegen failed: %v", err)
		}
		return
	}

	// 4. Default behavior: export to dist/
	sdkTarget := "./dist/sdk.ts"
	if err := app.ExportTypeScriptSDK(sdkTarget); err != nil {
		log.Fatalf("Failed to export TypeScript SDK: %v", err)
	}
	log.Printf("✅ Successfully exported TypeScript SDK to %s", sdkTarget)

	dtsTarget := "./dist/types.d.ts"
	if err := app.ExportTypeScript(dtsTarget); err != nil {
		log.Fatalf("Failed to export TypeScript definitions: %v", err)
	}
	log.Printf("✅ Successfully exported TypeScript definitions to %s", dtsTarget)
}
