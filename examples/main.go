package main

import (
	"log"
	"os"
	"time"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/todos"
)

func main() {
	isProd := os.Getenv("ENV") == "production"

	// Disable runtime TypeScript HTTP routes in production (use app.ExportTypeScriptSDK in CI/CD instead)
	tsURL, tsSDKURL := "/types.d.ts", "/sdk.ts"
	if isProd {
		tsURL, tsSDKURL = "-", "-"
	}

	app := gota.New(gota.Config{
		Title:                 "Gota REPR API",
		Version:               "1.0.0",
		Description:           "Minimal Request-Endpoint-Response (REPR) API with Scalar OpenAPI 3.1 and TypeScript SDK.",
		TypeScriptURL:         tsURL,
		TypeScriptSDKURL:      tsSDKURL,
		DisallowUnknownFields: true,
		DocsCSPEnforce:        true,
	})

	app.Use(gota.RequestID())
	app.Use(gota.Logger())
	app.Use(gota.Recover())
	app.Use(gota.SecureHeaders())
	app.Use(gota.Gzip())
	app.Use(gota.CORS())

	// Protect /docs, /openapi.json, /types.d.ts, and /sdk.ts with Basic Auth and rate limiting
	docsPassword := os.Getenv("DOCS_PASSWORD")
	if docsPassword == "" {
		if isProd {
			log.Fatal("DOCS_PASSWORD environment variable must be set in production")
		}
		docsPassword = "admin123"
	}
	app.UseDocs(gota.BasicAuth("admin", docsPassword, gota.BasicAuthOptions{
		Realm:                "Gota API Documentation",
		RateLimit:            &gota.RateLimiterOptions{Max: 10, Window: time.Minute},
		AllowWeakCredentials: !isProd,
		RequireTLS:           isProd,
	}))

	store := todos.NewTodoStore()

	// Redirect root to Scalar UI and hide this route from OpenAPI/SDK
	app.Get("/", func(ctx *gota.Context) error {
		return ctx.Redirect("/docs")
	}).Hidden()

	todos.RegisterRoutes(app, store)

	if err := app.ListenGraceful(":3000"); err != nil {
		log.Printf("Server error: %v", err)
	}
}
