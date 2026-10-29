package core

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
)

// SignJWTEdDSA creates an Ed25519 (EdDSA) signed JWT token string using the provided private key.
// An optional keyID can be provided to populate the 'kid' field in the JWT header.
func SignJWTEdDSA(privateKey ed25519.PrivateKey, payload any, keyID ...string) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", errors.New("jwt: invalid Ed25519 private key size")
	}

	var headerJSON []byte
	if len(keyID) > 0 && keyID[0] != "" {
		headerJSON, _ = json.Marshal(JWTHeader{
			Alg: string(JWTAlgorithmEdDSA),
			Typ: "JWT",
			Kid: keyID[0],
		})
	} else {
		headerJSON = []byte(`{"alg":"EdDSA","typ":"JWT"}`)
	}
	header := base64.RawURLEncoding.EncodeToString(headerJSON)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signingInput := header + "." + payloadEncoded
	sigBytes := ed25519.Sign(privateKey, []byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(sigBytes)

	return signingInput + "." + signature, nil
}

// VerifyJWTEdDSA validates an Ed25519 (EdDSA) JWT token against the given public key and decodes its payload into target.
// If target is nil, only the format, signature, and expiration are verified.
func VerifyJWTEdDSA(publicKey ed25519.PublicKey, tokenString string, target any) error {
	return verifyJWTEdDSA(publicKey, tokenString, target, false)
}

func verifyJWTEdDSA(publicKey ed25519.PublicKey, tokenString string, target any, requireExp bool) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("jwt: invalid Ed25519 public key size")
	}

	return verifyToken(tokenString, target, requireExp, string(JWTAlgorithmEdDSA), func(signingInput string, sig []byte) error {
		if len(sig) != ed25519.SignatureSize || !ed25519.Verify(publicKey, []byte(signingInput), sig) {
			return errors.New("invalid token signature")
		}
		return nil
	})
}

// JWTEdDSA returns a built-in middleware for Bearer token validation using Ed25519 (EdDSA).
// On successful authentication, decoded claims are stored in Context under ContextKey (default: "user"),
// and the raw token is stored under "jwt_token".
func JWTEdDSA(publicKey ed25519.PublicKey, opts ...JWTOptions) MiddlewareFunc {
	cfg := JWTOptions{
		Algorithm:      JWTAlgorithmEdDSA,
		EdDSAPublicKey: publicKey,
		ContextKey:     "user",
	}
	if len(opts) > 0 {
		applyJWTOptions(&cfg, opts[0])
	}
	return createJWTMiddleware(cfg)
}

// ParseEdPrivateKeyFromPEM parses a PEM-encoded Ed25519 private key in PKCS#8 (BEGIN PRIVATE KEY) format.
func ParseEdPrivateKeyFromPEM(keyPEM []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("jwt: failed to parse PEM block containing Ed25519 private key")
	}

	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("jwt: failed to parse Ed25519 private key")
	}
	edKey, ok := parsedKey.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("jwt: parsed key is not an Ed25519 private key")
	}
	return edKey, nil
}

// ParseEdPublicKeyFromPEM parses a PEM-encoded Ed25519 public key in PKIX (BEGIN PUBLIC KEY)
// or from an X.509 Certificate (BEGIN CERTIFICATE).
func ParseEdPublicKeyFromPEM(keyPEM []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("jwt: failed to parse PEM block containing Ed25519 public key")
	}

	// 1. Try PKIX public key (BEGIN PUBLIC KEY)
	if parsedKey, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if edPub, ok := parsedKey.(ed25519.PublicKey); ok {
			return edPub, nil
		}
		return nil, errors.New("jwt: parsed key is not an Ed25519 public key")
	}

	// 2. Try X.509 Certificate (BEGIN CERTIFICATE)
	if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
		if edPub, ok := cert.PublicKey.(ed25519.PublicKey); ok {
			return edPub, nil
		}
		return nil, errors.New("jwt: certificate does not contain an Ed25519 public key")
	}

	return nil, errors.New("jwt: failed to parse Ed25519 public key")
}
