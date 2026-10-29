package core

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// JWTAlgorithm represents a supported JWT signing algorithm.
type JWTAlgorithm string

const (
	// JWTAlgorithmHS256 represents HMAC-SHA256 symmetric signing (default).
	JWTAlgorithmHS256 JWTAlgorithm = "HS256"
	// JWTAlgorithmRS256 represents RSA-SHA256 asymmetric signing.
	JWTAlgorithmRS256 JWTAlgorithm = "RS256"
	// JWTAlgorithmES256 represents ECDSA P-256 with SHA-256 asymmetric signing.
	JWTAlgorithmES256 JWTAlgorithm = "ES256"
	// JWTAlgorithmEdDSA represents Ed25519 asymmetric signing.
	JWTAlgorithmEdDSA JWTAlgorithm = "EdDSA"
)

// JWTHeader represents the decoded JOSE header of a JWT.
type JWTHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid,omitempty"`
}

// JWTKeyFunc dynamically resolves the verification key (e.g. []byte for HS256, *rsa.PublicKey for RS256,
// *ecdsa.PublicKey for ES256, or ed25519.PublicKey for EdDSA) from the decoded JOSE header (such as 'kid').
type JWTKeyFunc func(header JWTHeader) (any, error)

// JWTOptions provides configuration for JWT generation, verification, and middleware.
type JWTOptions struct {
	// Algorithm specifies the JWT signing algorithm (default: JWTAlgorithmHS256).
	Algorithm JWTAlgorithm
	// Secret is the HMAC signing secret (required when using HS256).
	Secret []byte
	// RSAPublicKey is the RSA public key used to verify RS256 tokens.
	RSAPublicKey *rsa.PublicKey
	// ECDSAPublicKey is the ECDSA public key used to verify ES256 tokens.
	ECDSAPublicKey *ecdsa.PublicKey
	// EdDSAPublicKey is the Ed25519 public key used to verify EdDSA tokens.
	EdDSAPublicKey ed25519.PublicKey
	// KeyFunc dynamically selects a verification key based on the token header (e.g. Kid).
	KeyFunc JWTKeyFunc
	// ContextKey is the key under which claims will be stored in Context (default: "user").
	ContextKey string
	// ClaimsFactory is an optional factory function returning a pointer to a custom claims struct.
	// If nil, claims are decoded into map[string]any.
	ClaimsFactory func() any
	// RequireExp rejects tokens that do not carry a positive 'exp' claim, so that
	// tokens without an expiration can never be accepted by the middleware.
	RequireExp bool
}

// ParseJWTHeader decodes and returns the header from a JWT string without verifying its signature.
func ParseJWTHeader(tokenString string) (JWTHeader, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return JWTHeader{}, errors.New("invalid token format")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return JWTHeader{}, errors.New("invalid token format")
	}
	var hdr JWTHeader
	if err := json.Unmarshal(headerBytes, &hdr); err != nil {
		return JWTHeader{}, errors.New("invalid token format")
	}
	return hdr, nil
}

func encodeJWTSigningInput(alg JWTAlgorithm, payload any, keyID ...string) (string, error) {
	var headerJSON []byte
	if len(keyID) > 0 && keyID[0] != "" {
		headerJSON, _ = json.Marshal(JWTHeader{
			Alg: string(alg),
			Typ: "JWT",
			Kid: keyID[0],
		})
	} else {
		headerJSON = []byte(`{"alg":"` + string(alg) + `","typ":"JWT"}`)
	}
	header := base64.RawURLEncoding.EncodeToString(headerJSON)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)

	return header + "." + payloadEncoded, nil
}

func verifyToken(tokenString string, target any, requireExp bool, expectedAlg string, verifySig func(signingInput string, sig []byte) error) error {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return errors.New("invalid token format")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errors.New("invalid token format")
	}

	var hdr JWTHeader
	if err := json.Unmarshal(headerBytes, &hdr); err != nil || hdr.Alg != expectedAlg {
		return errors.New("invalid token signature")
	}

	actualSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return errors.New("invalid token signature")
	}

	signingInput := parts[0] + "." + parts[1]
	if err := verifySig(signingInput, actualSig); err != nil {
		return err
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

func applyJWTOptions(dst *JWTOptions, src JWTOptions) {
	if src.Algorithm != "" {
		dst.Algorithm = src.Algorithm
	}
	if len(src.Secret) > 0 {
		dst.Secret = src.Secret
	}
	if src.RSAPublicKey != nil {
		dst.RSAPublicKey = src.RSAPublicKey
		if dst.Algorithm == "" {
			dst.Algorithm = JWTAlgorithmRS256
		}
	}
	if src.ECDSAPublicKey != nil {
		dst.ECDSAPublicKey = src.ECDSAPublicKey
		if dst.Algorithm == "" {
			dst.Algorithm = JWTAlgorithmES256
		}
	}
	if len(src.EdDSAPublicKey) > 0 {
		dst.EdDSAPublicKey = src.EdDSAPublicKey
		if dst.Algorithm == "" {
			dst.Algorithm = JWTAlgorithmEdDSA
		}
	}
	if src.KeyFunc != nil {
		dst.KeyFunc = src.KeyFunc
	}
	if src.ContextKey != "" {
		dst.ContextKey = src.ContextKey
	}
	if src.ClaimsFactory != nil {
		dst.ClaimsFactory = src.ClaimsFactory
	}
	dst.RequireExp = src.RequireExp
}

// VerifyJWTWithKeyFunc verifies a JWT token by resolving the verification key dynamically via keyFunc.
// To prevent algorithm confusion attacks, the key type returned by keyFunc must strictly match the header's 'alg':
// []byte for HS256, *rsa.PublicKey for RS256, *ecdsa.PublicKey for ES256, or ed25519.PublicKey for EdDSA.
func VerifyJWTWithKeyFunc(keyFunc JWTKeyFunc, tokenString string, target any) error {
	return verifyJWTWithKeyFunc(keyFunc, tokenString, target, false)
}

func verifyJWTWithKeyFunc(keyFunc JWTKeyFunc, tokenString string, target any, requireExp bool) error {
	if keyFunc == nil {
		return errors.New("jwt: keyFunc cannot be nil")
	}
	hdr, err := ParseJWTHeader(tokenString)
	if err != nil {
		return err
	}
	key, err := keyFunc(hdr)
	if err != nil {
		return err
	}
	switch JWTAlgorithm(hdr.Alg) {
	case JWTAlgorithmHS256:
		secret, ok := key.([]byte)
		if !ok {
			return errors.New("invalid token signature")
		}
		return verifyJWT(secret, tokenString, target, requireExp)
	case JWTAlgorithmRS256:
		pub, ok := key.(*rsa.PublicKey)
		if !ok {
			return errors.New("invalid token signature")
		}
		return verifyJWTRSA(pub, tokenString, target, requireExp)
	case JWTAlgorithmES256:
		pub, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return errors.New("invalid token signature")
		}
		return verifyJWTES256(pub, tokenString, target, requireExp)
	case JWTAlgorithmEdDSA:
		pub, ok := key.(ed25519.PublicKey)
		if !ok {
			return errors.New("invalid token signature")
		}
		return verifyJWTEdDSA(pub, tokenString, target, requireExp)
	default:
		return errors.New("invalid token signature")
	}
}

// JWTWithKeyFunc returns a built-in middleware for Bearer token validation using a dynamic JWTKeyFunc
// (ideal for key rotation by 'kid' or multi-issuer verification).
func JWTWithKeyFunc(keyFunc JWTKeyFunc, opts ...JWTOptions) MiddlewareFunc {
	cfg := JWTOptions{
		KeyFunc:    keyFunc,
		ContextKey: "user",
	}
	if len(opts) > 0 {
		applyJWTOptions(&cfg, opts[0])
	}
	return createJWTMiddleware(cfg)
}

func createJWTMiddleware(cfg JWTOptions) MiddlewareFunc {
	mw := func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			unauthorized := func(msg string) error {
				return ctx.Status(http.StatusUnauthorized).Json(ErrorResponse{
					Error:     msg,
					Status:    http.StatusUnauthorized,
					RequestID: ctx.RequestID(),
				})
			}

			authHeader := ctx.Header("Authorization")
			if authHeader == "" {
				return unauthorized("Authorization header is required")
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return unauthorized("Invalid authorization format, expected Bearer <token>")
			}

			var claims any
			if cfg.ClaimsFactory != nil {
				claims = cfg.ClaimsFactory()
			} else {
				var m map[string]any
				claims = &m
			}

			var verifyErr error
			switch {
			case cfg.KeyFunc != nil:
				verifyErr = verifyJWTWithKeyFunc(cfg.KeyFunc, parts[1], claims, cfg.RequireExp)
			case cfg.Algorithm == JWTAlgorithmEdDSA || len(cfg.EdDSAPublicKey) > 0:
				if len(cfg.EdDSAPublicKey) == 0 {
					return unauthorized("Invalid or expired token")
				}
				verifyErr = verifyJWTEdDSA(cfg.EdDSAPublicKey, parts[1], claims, cfg.RequireExp)
			case cfg.Algorithm == JWTAlgorithmES256 || cfg.ECDSAPublicKey != nil:
				if cfg.ECDSAPublicKey == nil {
					return unauthorized("Invalid or expired token")
				}
				verifyErr = verifyJWTES256(cfg.ECDSAPublicKey, parts[1], claims, cfg.RequireExp)
			case cfg.Algorithm == JWTAlgorithmRS256 || cfg.RSAPublicKey != nil:
				if cfg.RSAPublicKey == nil {
					return unauthorized("Invalid or expired token")
				}
				verifyErr = verifyJWTRSA(cfg.RSAPublicKey, parts[1], claims, cfg.RequireExp)
			default:
				verifyErr = verifyJWT(cfg.Secret, parts[1], claims, cfg.RequireExp)
			}

			if verifyErr != nil {
				return unauthorized("Invalid or expired token")
			}

			ctx.Set(cfg.ContextKey, claims)
			ctx.Set("jwt_token", parts[1])
			return next(ctx)
		}
	}
	return registerMiddlewareSecurity(mw, "BearerAuth")
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
