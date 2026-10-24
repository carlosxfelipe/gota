package auth

import (
	"time"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
)

// LoginRequest defines credentials for authentication.
type LoginRequest struct {
	Username string `json:"username" doc:"Username" example:"carlos" validate:"required"`
	Password string `json:"password" doc:"User password" example:"carlos123" validate:"required"`
}

// LoginResponse returns the generated JWT Bearer token.
type LoginResponse struct {
	Token string `json:"token" doc:"JWT Bearer token" example:"eyJhbGciOi..."`
}

// LoginEndpoint handles authentication (matches FastEndpoints Features/Auth/Login).
type LoginEndpoint struct {
	Store *data.Store
}

// Handle processes the login request and returns a signed JWT token.
func (e *LoginEndpoint) Handle(ctx *gota.Context, req LoginRequest) (LoginResponse, error) {
	user, ok := e.Store.GetUserByUsername(req.Username)
	if !ok || user.Password != req.Password {
		return LoginResponse{}, gota.BadRequest("Invalid username or password.")
	}

	claims := UserClaims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		// Exp:      time.Now().Add(24 * time.Hour).Unix(),
		Exp: time.Now().AddDate(10, 0, 0).Unix(), // 10 years for showcase convenience
	}

	token, err := gota.SignJWT(Secret, claims)
	if err != nil {
		return LoginResponse{}, err
	}

	return LoginResponse{
		Token: token,
	}, nil
}
