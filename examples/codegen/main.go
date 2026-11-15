package main

import (
	"log"
	"os"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/system"
	"github.com/carlosxfelipe/gota/examples/todos"
	"github.com/carlosxfelipe/gota/repr"
)

// Example demonstrating offline TypeScript SDK code generation (e.g. for CI/CD or build scripts).
//
// Running this script writes the modular client SDK and type declarations to disk without needing
// to expose HTTP endpoints in your production application.
//
// Usage:
//
//	go run ./examples/codegen/main.go
//	go run ./examples/codegen/main.go --ts-sdk=./dist/sdk
//	go run ./examples/codegen/main.go --ts-sdk=./dist/sdk --check
//	go run ./examples/codegen/main.go --ts-types=./dist/types.d.ts
//	go run ./examples/codegen/main.go --stdout
func main() {
	app := gota.New(repr.Config{
		Title:         "Gota API",
		Version:       "1.0.0",
		Description:   "Simple API for managing todo items.",
		TypeScriptURL: "-",
	})

	store := todos.NewTodoStore()
	todos.RegisterRoutes(app, store)
	system.RegisterRoutes(app)

	if handled, err := app.RunCodegen(os.Args[1:]); handled {
		if err != nil {
			log.Fatalf("Codegen failed: %v", err)
		}
		return
	}

	distDir := "./dist"
	if _, err := os.Stat("./examples/codegen"); err == nil {
		distDir = "./examples/codegen/dist"
	}

	sdkTarget := distDir + "/sdk"
	if err := app.ExportTypeScriptSDK(sdkTarget); err != nil {
		log.Fatalf("Failed to export TypeScript SDK: %v", err)
	}
	log.Printf("✅ Successfully exported TypeScript SDK to %s", sdkTarget)

	dtsTarget := distDir + "/types.d.ts"
	if err := app.ExportTypeScript(dtsTarget); err != nil {
		log.Fatalf("Failed to export TypeScript definitions: %v", err)
	}
	log.Printf("✅ Successfully exported TypeScript definitions to %s", dtsTarget)
}
