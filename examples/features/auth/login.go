package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/carlosxfelipe/gota"
	"github.com/carlosxfelipe/gota/examples/data"
)

// Claims represents the claims embedded inside the JWT bearer token.
type Claims struct {
	UserID   int    `json:"UserId"`
	Username string `json:"Username"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

var jwtSecret = []byte("GotaOrderFlowSecretKey2026!@#")

// GenerateToken creates an HMAC-SHA256 signed JWT token for the user.
func GenerateToken(userID int, username, role string) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		// Exp:      time.Now().Add(24 * time.Hour).Unix(),
		Exp: time.Now().AddDate(10, 0, 0).Unix(), // 10 years for showcase convenience
	}
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(claimsBytes)

	data := header + "." + payload
	h := hmac.New(sha256.New, jwtSecret)
	h.Write([]byte(data))
	sig := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	return data + "." + sig, nil
}

// VerifyToken validates the token and returns the decoded claims.
func VerifyToken(tokenString string) (*Claims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	data := parts[0] + "." + parts[1]
	h := hmac.New(sha256.New, jwtSecret)
	h.Write([]byte(data))
	expectedSig := h.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(expectedSig, actualSig) {
		return nil, errors.New("invalid token signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid token payload")
	}

	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, err
	}

	if claims.Exp < time.Now().Unix() {
		return nil, errors.New("token expired")
	}

	return &claims, nil
}

// Middleware enforces Bearer token authentication on protected endpoints.
func Middleware() gota.MiddlewareFunc {
	return func(next gota.HandlerFunc) gota.HandlerFunc {
		return func(ctx *gota.Context) error {
			authHeader := ctx.Header("Authorization")
			if authHeader == "" {
				return ctx.Status(http.StatusUnauthorized).Json(gota.ErrorResponse{
					Error:  "Authorization header is required",
					Status: http.StatusUnauthorized,
				})
			}
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return ctx.Status(http.StatusUnauthorized).Json(gota.ErrorResponse{
					Error:  "Invalid authorization format, expected Bearer <token>",
					Status: http.StatusUnauthorized,
				})
			}
			claims, err := VerifyToken(parts[1])
			if err != nil {
				return ctx.Status(http.StatusUnauthorized).Json(gota.ErrorResponse{
					Error:  "Invalid or expired token: " + err.Error(),
					Status: http.StatusUnauthorized,
				})
			}
			ctx.Set("user", claims)
			return next(ctx)
		}
	}
}

// CurrentUser retrieves the authenticated user claims from Context.
func CurrentUser(ctx *gota.Context) (*Claims, bool) {
	val, ok := ctx.Get("user")
	if !ok {
		return nil, false
	}
	claims, ok := val.(*Claims)
	return claims, ok
}

// ----------------------------------------------------------------------
// Login REPR Endpoint (matches order-flow Features/Auth/Login)
// ----------------------------------------------------------------------

// LoginRequest defines credentials for authentication.
type LoginRequest struct {
	Username string `json:"username" doc:"Nome de usuário" example:"carlos" validate:"required"`
	Password string `json:"password" doc:"Senha do usuário" example:"carlos123" validate:"required"`
}

// LoginResponse returns the generated JWT Bearer token.
type LoginResponse struct {
	Token string `json:"token" doc:"Token JWT Bearer" example:"eyJhbGciOi..."`
}

// LoginEndpoint handles authentication.
type LoginEndpoint struct {
	Store *data.Store
}

// Handle processes the login request.
func (e *LoginEndpoint) Handle(ctx *gota.Context, req LoginRequest) (LoginResponse, error) {
	user, ok := e.Store.GetUserByUsername(req.Username)
	if !ok || user.Password != req.Password {
		return LoginResponse{}, gota.BadRequest("Invalid username or password.")
	}

	token, err := GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		return LoginResponse{}, err
	}

	return LoginResponse{
		Token: token,
	}, nil
}
