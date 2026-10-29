package main

import (
	"log"

	"github.com/carlosxfelipe/gota"
)

// ----------------------------------------------------------------------
// Request and Response Contracts (REPR Pattern)
// ----------------------------------------------------------------------

type CreateUserRequest struct {
	Name  string `json:"name" doc:"Full name of user" example:"Jane Doe" validate:"required,min=3"`
	Email string `json:"email" doc:"Valid corporate or personal email" example:"jane.doe@example.com" validate:"required,email"`
	Role  string `json:"role" doc:"User access role" example:"admin" default:"member"`
}

type GetUserRequest struct {
	ID string `param:"id" doc:"User unique identifier" example:"usr_1"`
}

type UserResponse struct {
	ID    string `json:"id" example:"usr_1"`
	Name  string `json:"name" example:"Jane Doe"`
	Email string `json:"email" example:"jane.doe@example.com"`
	Role  string `json:"role" example:"admin"`
}

type HealthResponse struct {
	Status  string `json:"status" example:"ok"`
	Version string `json:"version" example:"1.0.0"`
}

var mockUsers = []UserResponse{
	{ID: "usr_1", Name: "Jane Doe", Email: "jane.doe@example.com", Role: "admin"},
	{ID: "usr_2", Name: "John Smith", Email: "john.smith@example.com", Role: "member"},
}

// ----------------------------------------------------------------------
// Struct-Based REPR Endpoint Example
// ----------------------------------------------------------------------

type GetUserEndpoint struct{}

func (e *GetUserEndpoint) Handle(ctx *gota.Context, req GetUserRequest) (UserResponse, error) {
	for _, u := range mockUsers {
		if u.ID == req.ID {
			return u, nil
		}
	}
	return UserResponse{}, gota.NotFound("User not found")
}

// ----------------------------------------------------------------------
// Main Application
// ----------------------------------------------------------------------

func main() {
	// Initializes Gota with built-in OpenAPI 3.1 and Scalar (classic layout default)
	app := gota.New(gota.Config{
		Title:       "Gota REPR API",
		Version:     "1.0.0",
		Description: "Go micro-framework oriented around the REPR Pattern with native interactive Scalar UI.",
	})

	// Global Middlewares
	app.Use(gota.RequestID())
	app.Use(gota.Logger())
	app.Use(gota.Recover())
	app.Use(gota.CORS())

	// 1. Healthcheck Endpoint (Empty Request)
	gota.Get(app, "/", func(ctx *gota.Context, _ gota.Empty) (HealthResponse, error) {
		return HealthResponse{Status: "ok", Version: "1.0.0"}, nil
	}).
		Summary("Healthcheck").
		Description("Returns system status and current version").
		Tags("System")

	// 2. List Users (Empty Request)
	gota.Get(app, "/users", func(ctx *gota.Context, _ gota.Empty) ([]UserResponse, error) {
		return mockUsers, nil
	}).
		Summary("List all users").
		Tags("Users")

	// 3. Get User by ID (Struct-based REPR Endpoint)
	userEndpoint := &GetUserEndpoint{}
	gota.Get(app, "/users/:id", userEndpoint.Handle).
		Summary("Get user by ID").
		Tags("Users")

	// 4. Create User (Function-based REPR with automatic binding and validation)
	gota.Post(app, "/users", func(ctx *gota.Context, req CreateUserRequest) (UserResponse, error) {
		newUser := UserResponse{
			ID:    "usr_3",
			Name:  req.Name,
			Email: req.Email,
			Role:  req.Role,
		}
		mockUsers = append(mockUsers, newUser)
		return newUser, nil
	}).
		Status(201).
		Summary("Register user").
		Description("Validates input data automatically and returns the created user (201 Created)").
		Tags("Users")

	// 5. Classic route (without REPR structs, for quick hooks)
	app.Get("/ping", func(ctx *gota.Context) error {
		return ctx.Json(gota.Map{"message": "pong"})
	}).Summary("Ping Pong").Tags("System")

	if err := app.ListenGraceful(":3000"); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}
