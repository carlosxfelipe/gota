package core

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// JWTOptions provides configuration for JWT generation, verification, and middleware.
type JWTOptions struct {
	// Secret is the HMAC signing secret (required).
	Secret []byte
	// ContextKey is the key under which claims will be stored in Context (default: "user").
	ContextKey string
	// ClaimsFactory is an optional factory function returning a pointer to a custom claims struct.
	// If nil, claims are decoded into map[string]any.
	ClaimsFactory func() any
	// RequireExp rejects tokens that do not carry a positive 'exp' claim, so that
	// tokens without an expiration can never be accepted by the middleware.
	RequireExp bool
}

// SignJWT creates an HMAC-SHA256 (HS256) signed JWT token string from the given payload.
func SignJWT(secret []byte, payload any) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("jwt: secret key cannot be empty")
	}

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signingInput := header + "." + payloadEncoded
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	return signingInput + "." + signature, nil
}

// VerifyJWT validates an HMAC-SHA256 JWT token against the given secret and decodes its payload into target.
// If target is nil, only the format, signature, and expiration are verified.
// Numeric 'exp' and 'nbf' claims (integer or fractional) are enforced when present;
// a present but malformed 'exp' or 'nbf' rejects the token.
func VerifyJWT(secret []byte, tokenString string, target any) error {
	return verifyJWT(secret, tokenString, target, false)
}

func verifyJWT(secret []byte, tokenString string, target any, requireExp bool) error {
	if len(secret) == 0 {
		return errors.New("jwt: secret key cannot be empty")
	}

	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return errors.New("invalid token format")
	}

	signingInput := parts[0] + "." + parts[1]
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(signingInput))
	expectedSig := h.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(expectedSig, actualSig) {
		return errors.New("invalid token signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("invalid token payload")
	}

	// Verify time claims. A malformed exp/nbf must never silently disable the check.
	var timeCheck struct {
		Exp *float64 `json:"exp"`
		Nbf *float64 `json:"nbf"`
	}
	if err := json.Unmarshal(payloadBytes, &timeCheck); err != nil {
		return errors.New("invalid token claims")
	}
	now := float64(time.Now().Unix())
	if timeCheck.Exp != nil && *timeCheck.Exp > 0 {
		if now > *timeCheck.Exp {
			return errors.New("token expired")
		}
	} else if requireExp {
		return errors.New("token missing expiration")
	}
	if timeCheck.Nbf != nil && now < *timeCheck.Nbf {
		return errors.New("token not yet valid")
	}

	if target != nil {
		if err := json.Unmarshal(payloadBytes, target); err != nil {
			return errors.New("failed to decode claims: " + err.Error())
		}
	}

	return nil
}

// JWT returns a built-in middleware for Bearer token validation using HMAC-SHA256 (HS256).
// On successful authentication, decoded claims are stored in Context under ContextKey (default: "user"),
// and the raw token is stored under "jwt_token".
func JWT(secret []byte, opts ...JWTOptions) MiddlewareFunc {
	cfg := JWTOptions{
		Secret:     secret,
		ContextKey: "user",
	}
	if len(opts) > 0 {
		if len(opts[0].Secret) > 0 {
			cfg.Secret = opts[0].Secret
		}
		if opts[0].ContextKey != "" {
			cfg.ContextKey = opts[0].ContextKey
		}
		cfg.ClaimsFactory = opts[0].ClaimsFactory
		cfg.RequireExp = opts[0].RequireExp
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			authHeader := ctx.Header("Authorization")
			if authHeader == "" {
				return ctx.Status(http.StatusUnauthorized).Json(ErrorResponse{
					Error:     "Authorization header is required",
					Status:    http.StatusUnauthorized,
					RequestID: ctx.RequestID(),
				})
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return ctx.Status(http.StatusUnauthorized).Json(ErrorResponse{
					Error:     "Invalid authorization format, expected Bearer <token>",
					Status:    http.StatusUnauthorized,
					RequestID: ctx.RequestID(),
				})
			}

			var claims any
			if cfg.ClaimsFactory != nil {
				claims = cfg.ClaimsFactory()
			} else {
				var m map[string]any
				claims = &m
			}

			if err := verifyJWT(cfg.Secret, parts[1], claims, cfg.RequireExp); err != nil {
				return ctx.Status(http.StatusUnauthorized).Json(ErrorResponse{
					Error:     "Invalid or expired token",
					Status:    http.StatusUnauthorized,
					RequestID: ctx.RequestID(),
				})
			}

			ctx.Set(cfg.ContextKey, claims)
			ctx.Set("jwt_token", parts[1])
			return next(ctx)
		}
	}
}

// JWTClaims retrieves the authenticated claims from Context into type T.
func JWTClaims[T any](ctx *Context, key ...string) (T, bool) {
	k := "user"
	if len(key) > 0 && key[0] != "" {
		k = key[0]
	}

	val, ok := ctx.Get(k)
	if !ok {
		var zero T
		return zero, false
	}

	if typed, ok := val.(T); ok {
		return typed, true
	}

	if ptr, ok := val.(*T); ok && ptr != nil {
		return *ptr, true
	}

	var zero T
	return zero, false
}
