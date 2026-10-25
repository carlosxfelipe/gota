package auth

import (
	"time"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
)

// RegisterRequest defines input for account registration.
type RegisterRequest struct {
	Username string `json:"username" doc:"Username" example:"novousuario" validate:"required,min=3"`
	Password string `json:"password" doc:"User password" example:"senha123" validate:"required,min=6"`
	Role     string `json:"role,omitempty" doc:"User role (defaults to Customer)" example:"Customer"`
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

	claims := UserClaims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		Exp:      time.Now().AddDate(10, 0, 0).Unix(),
	}

	token, err := gota.SignJWT(Secret, claims)
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
