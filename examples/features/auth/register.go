package auth

import (
	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
)

// RegisterRequest defines input for account registration.
type RegisterRequest struct {
	Username string `json:"username" doc:"Username" example:"novousuario" validate:"required,min=3"`
	Password string `json:"password" doc:"User password" example:"senha123" validate:"required,min=6"`
	Role     string `json:"role,omitempty" doc:"User role (Customer or Admin)" example:"Customer" validate:"oneof=Customer,Admin"`
}

// RegisterResponse returns the created user info along with an initial JWT token.
type RegisterResponse struct {
	ID       int    `json:"id" doc:"User ID" example:"5"`
	Username string `json:"username" doc:"Username" example:"novousuario"`
	Role     string `json:"role" doc:"User role" example:"Customer"`
	Token    string `json:"token" doc:"JWT Bearer token" example:"eyJhbGciOi..."`
}

// RegisterEndpoint handles user account registration.
type RegisterEndpoint struct {
	Store *data.Store
}

// Handle creates a new user account and returns a signed JWT token.
func (e *RegisterEndpoint) Handle(ctx *gota.Context, req RegisterRequest) (RegisterResponse, error) {
	role := req.Role
	if role == "" {
		role = "Customer"
	}

	user, ok := e.Store.CreateUser(req.Username, req.Password, role)
	if !ok {
		return RegisterResponse{}, gota.BadRequest("Username is already taken.")
	}

	token, err := GenerateToken(user)
	if err != nil {
		return RegisterResponse{}, err
	}

	return RegisterResponse{
		ID:       user.ID,
		Username: user.Username,
		Role:     user.Role,
		Token:    token,
	}, nil
}
