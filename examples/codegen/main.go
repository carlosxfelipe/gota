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
// Usage:
//
//	go run ./examples/codegen/main.go
//	go run ./examples/codegen/main.go --ts-sdk=./dist
//	go run ./examples/codegen/main.go --ts-sdk=./dist --ts-import-ext=auto
//	go run ./examples/codegen/main.go --ts-sdk=./dist --check
func main() {
	app := gota.New(repr.Config{
		Title:       "Gota API",
		Version:     "1.0.0",
		Description: "Simple API for managing todo items.",
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

	if err := app.ExportTypeScriptSDK(distDir); err != nil {
		log.Fatalf("Failed to export TypeScript SDK: %v", err)
	}
	log.Printf("✅ Successfully exported TypeScript SDK to %s", distDir)
}
