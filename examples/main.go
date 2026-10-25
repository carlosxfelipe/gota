package main

import (
	"log"
	"time"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/features/auth"
	"github.com/carlosxfelipe/gota/examples/features/orders"
)

func main() {
	// Initializes Gota with built-in OpenAPI 3.1, Scalar UI, and production timeouts
	app := gota.New(gota.Config{
		Title:        "Order Flow API",
		Version:      "1.0.0",
		Description:  "Order management API with REPR pattern and zero external dependencies.",
		ScalarTheme:  gota.ThemeFastify,
		ScalarLayout: "classic",
		MaxBodyBytes: 10 << 20, // 10MB request body protection
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	})

	// Global Middlewares (Observability, Resilience, Security)
	app.Use(gota.RequestID())
	app.Use(gota.Logger())
	app.Use(gota.Recover())
	app.Use(gota.CORS())

	// Data store pre-seeded with order-flow demo data
	store := data.NewStore()

	// Redirect root "/" directly to Scalar documentation (matches order-flow Program.cs)
	app.Get("/", func(ctx *gota.Context) error {
		return ctx.Redirect("/docs")
	}).Hidden()

	// 1. Public Auth Endpoints (Anonymous: Login, Register)
	auth.RegisterPublicRoutes(app, store)

	// 2. Protected Endpoints (Secured with built-in gota.JWT middleware)
	protectedGroup := app.Group("")
	protectedGroup.Use(gota.JWT(auth.Secret, gota.JWTOptions{
		ClaimsFactory: func() any { return &auth.UserClaims{} },
	}))
	auth.RegisterProtectedRoutes(protectedGroup, store)
	orders.RegisterRoutes(protectedGroup, store)

	// Starts server with automatic graceful shutdown on SIGINT / SIGTERM
	if err := app.ListenGraceful(":3000"); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}
