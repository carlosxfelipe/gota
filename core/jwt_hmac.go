package core

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
)

func hmacSHA256(secret []byte, data string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// SignJWT creates an HMAC-SHA256 (HS256) signed JWT token string from the given payload.
// An optional keyID can be provided to populate the 'kid' field in the JWT header.
func SignJWT(secret []byte, payload any, keyID ...string) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("jwt: secret key cannot be empty")
	}

	var headerJSON []byte
	if len(keyID) > 0 && keyID[0] != "" {
		headerJSON, _ = json.Marshal(JWTHeader{
			Alg: string(JWTAlgorithmHS256),
			Typ: "JWT",
			Kid: keyID[0],
		})
	} else {
		headerJSON = []byte(`{"alg":"HS256","typ":"JWT"}`)
	}
	header := base64.RawURLEncoding.EncodeToString(headerJSON)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signingInput := header + "." + payloadEncoded
	signature := base64.RawURLEncoding.EncodeToString(hmacSHA256(secret, signingInput))

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
	return verifyToken(tokenString, target, requireExp, string(JWTAlgorithmHS256), func(signingInput string, sig []byte) error {
		expectedSig := hmacSHA256(secret, signingInput)
		if !hmac.Equal(expectedSig, sig) {
			return errors.New("invalid token signature")
		}
		return nil
	})
}

// JWT returns a built-in middleware for Bearer token validation using HMAC-SHA256 (HS256) by default.
// If opts specifies RSAPublicKey (or Algorithm == JWTAlgorithmRS256), RS256 validation is used.
// On successful authentication, decoded claims are stored in Context under ContextKey (default: "user"),
// and the raw token is stored under "jwt_token".
func JWT(secret []byte, opts ...JWTOptions) MiddlewareFunc {
	cfg := JWTOptions{
		Algorithm:  JWTAlgorithmHS256,
		Secret:     secret,
		ContextKey: "user",
	}
	if len(opts) > 0 {
		applyJWTOptions(&cfg, opts[0])
	}
	return createJWTMiddleware(cfg)
}
