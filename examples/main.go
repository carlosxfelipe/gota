package main

import (
	"log"
	"net/http"
	"os"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/system"
	"github.com/carlosxfelipe/gota/examples/todos"
	"github.com/carlosxfelipe/gota/repr"
	"github.com/carlosxfelipe/gota/scalar"
	"github.com/go-chi/chi/v5/middleware"
)

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	isProd := os.Getenv("ENV") == "production"

	// Disable runtime TypeScript HTTP route in production (use examples/codegen in CI/CD instead)
	tsURL := "/types.d.ts"
	if isProd {
		tsURL = "-"
	}

	docsPassword := os.Getenv("DOCS_PASSWORD")
	if docsPassword == "" {
		if isProd {
			log.Fatal("DOCS_PASSWORD environment variable must be set in production")
		}
		docsPassword = "admin123"
	}

	app := gota.New(repr.Config{
		Title:                 "Gota API",
		Version:               "1.0.0",
		Description:           "Simple API for managing todo items.",
		TypeScriptURL:         tsURL,
		DisallowUnknownFields: true,
		Scalar: scalar.Config{
			Theme:      scalar.ThemeFastify,
			Layout:     "classic",
			CSPEnforce: true,
		},
		DocsMiddlewares: []func(http.Handler) http.Handler{
			middleware.BasicAuth("Gota API Documentation", map[string]string{"admin": docsPassword}),
		},
	})

	app.Use(
		middleware.RequestID,
		middleware.Logger,
		middleware.Recoverer,
		middleware.Compress(5),
		corsMiddleware,
	)

	store := todos.NewTodoStore()

	// Redirect root to Scalar UI
	app.Handle(http.MethodGet, "/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs", http.StatusFound)
	}))

	todos.RegisterRoutes(app, store)
	system.RegisterRoutes(app)

	if err := app.ListenGraceful(":3000"); err != nil {
		log.Printf("Server error: %v", err)
	}
}
