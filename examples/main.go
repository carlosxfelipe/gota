package main

import (
	"log"
	"net/http"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
	"github.com/carlosxfelipe/gota/examples/features/auth"
	"github.com/carlosxfelipe/gota/examples/features/orders"
)

func main() {
	// Initializes Gota with built-in OpenAPI 3.1 and Scalar (classic layout by default)
	app := gota.New(gota.Config{
		Title:        "Order Flow API",
		Version:      "1.0.0",
		Description:  "Order management API (REPR pattern).",
		ScalarTheme:  gota.ThemeDefault,
		ScalarLayout: "classic",
		MaxBodyBytes: 10 << 20, // 10MB request body protection
	})

	// Global Middlewares
	app.Use(gota.Logger())
	app.Use(gota.Recover())
	app.Use(gota.CORS())

	// Data store pre-seeded with order-flow demo data
	store := data.NewStore()

	// Redirect root "/" directly to Scalar documentation (matches order-flow Program.cs)
	app.Get("/", func(ctx *gota.Context) error {
		return ctx.Redirect("/docs")
	}).Hidden()

	// 1. Auth Endpoint (Anonymous)
	gota.HandleEndpoint(app, http.MethodPost, "/api/auth/login", &auth.LoginEndpoint{Store: store}).
		Summary("Authenticates a user").
		Description("Validates user credentials and returns a JWT Bearer token.").
		Tags("Auth")

	// 2. Protected Orders Endpoints (Secured with built-in gota.JWT middleware)
	ordersGroup := app.Group("")
	ordersGroup.Use(gota.JWT(auth.Secret, gota.JWTOptions{
		ClaimsFactory: func() any { return &auth.UserClaims{} },
	}))
	orders.RegisterRoutes(ordersGroup, store)

	log.Println("💧 Gota Order Flow server starting...")
	log.Println("📖 Interactive Scalar docs (classic layout): http://localhost:3000/docs")
	log.Println("📄 OpenAPI 3.1 specification: http://localhost:3000/openapi.json")

	log.Fatal(app.Listen(":3000"))
}
