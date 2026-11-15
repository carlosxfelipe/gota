package main

import (
	"crypto/subtle"
	"log"
	"net/http"
	"os"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/todos"
	"github.com/carlosxfelipe/gota/scalar"
	"github.com/go-chi/chi/v5/middleware"
)

func basicAuthMiddleware(username, password, realm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			uMatch := subtle.ConstantTimeCompare([]byte(u), []byte(username)) == 1
			pMatch := subtle.ConstantTimeCompare([]byte(p), []byte(password)) == 1
			if !ok || !uMatch || !pMatch {
				w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

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

	// Disable runtime TypeScript HTTP routes in production (use examples/codegen in CI/CD instead)
	tsURL, tsSDKURL := "/types.d.ts", "/sdk.ts"
	if isProd {
		tsURL, tsSDKURL = "-", "-"
	}

	docsPassword := os.Getenv("DOCS_PASSWORD")
	if docsPassword == "" {
		if isProd {
			log.Fatal("DOCS_PASSWORD environment variable must be set in production")
		}
		docsPassword = "admin123"
	}

	app := repr.New(repr.Config{
		Title:                 "Gota API",
		Version:               "1.0.0",
		Description:           "Simple API for managing todo items.",
		TypeScriptURL:         tsURL,
		TypeScriptSDKURL:      tsSDKURL,
		DisallowUnknownFields: true,
		Scalar: scalar.Config{
			Theme:      scalar.ThemeFastify,
			Layout:     "classic",
			CSPEnforce: true,
		},
		DocsMiddlewares: []func(http.Handler) http.Handler{
			basicAuthMiddleware("admin", docsPassword, "Gota API Documentation"),
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

	if err := app.ListenGraceful(":3000"); err != nil {
		log.Printf("Server error: %v", err)
	}
}
