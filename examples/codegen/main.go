package main

import (
	"log"

	"github.com/carlosxfelipe/gota"
)

// Example demonstrating offline TypeScript SDK code generation (e.g. for CI/CD or build scripts).
//
// Running this script writes the full client SDK to disk without needing to expose
// HTTP endpoints (/sdk.ts or /types.d.ts) in your production application.
//
// Usage:
//
//	go run ./examples/codegen/main.go
func main() {
	// 1. Initialize your app (or import your app router configuration)
	app := gota.New(gota.Config{
		Title:       "Production API",
		Version:     "1.0.0",
		Description: "Offline-generated client SDK example",
		// In production, keep HTTP endpoints disabled
		TypeScriptURL:    "-",
		TypeScriptSDKURL: "-",
	})

	// 2. Register your models and endpoints
	gota.Get(app, "/health", func(ctx *gota.Context, _ gota.Empty) (gota.Map, error) {
		return gota.Map{"status": "ok"}, nil
	}).Summary("Healthcheck")

	// 3. Export TypeScript SDK and Definitions directly to disk
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
