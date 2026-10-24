package auth

import "github.com/carlosxfelipe/gota"

// Secret is the HMAC signing key used for JWT tokens in this showcase.
var Secret = []byte("GotaOrderFlowSecretKey2026!@#")

// UserClaims represents the authenticated user payload inside the JWT.
type UserClaims struct {
	UserID   int    `json:"UserId"`
	Username string `json:"Username"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

// CurrentUser retrieves the authenticated user claims from the Gota Context.
func CurrentUser(ctx *gota.Context) (*UserClaims, bool) {
	return gota.JWTClaims[*UserClaims](ctx)
}
