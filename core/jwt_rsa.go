package core

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
)

// SignJWTRSA creates an RSA-SHA256 (RS256) signed JWT token string using the provided RSA private key.
// An optional keyID can be provided to populate the 'kid' field in the JWT header.
func SignJWTRSA(privateKey *rsa.PrivateKey, payload any, keyID ...string) (string, error) {
	if privateKey == nil {
		return "", errors.New("jwt: RSA private key cannot be nil")
	}

	signingInput, err := encodeJWTSigningInput(JWTAlgorithmRS256, payload, keyID...)
	if err != nil {
		return "", err
	}
	hashed := sha256.Sum256([]byte(signingInput))

	signatureBytes, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", err
	}
	signature := base64.RawURLEncoding.EncodeToString(signatureBytes)

	return signingInput + "." + signature, nil
}

// VerifyJWTRSA validates an RSA-SHA256 (RS256) JWT token against the given RSA public key and decodes its payload into target.
// If target is nil, only the format, signature, and expiration are verified.
func VerifyJWTRSA(publicKey *rsa.PublicKey, tokenString string, target any) error {
	return verifyJWTRSA(publicKey, tokenString, target, false)
}

func verifyJWTRSA(publicKey *rsa.PublicKey, tokenString string, target any, requireExp bool) error {
	if publicKey == nil {
		return errors.New("jwt: RSA public key cannot be nil")
	}
	return verifyToken(tokenString, target, requireExp, string(JWTAlgorithmRS256), func(signingInput string, sig []byte) error {
		hashed := sha256.Sum256([]byte(signingInput))
		if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hashed[:], sig); err != nil {
			return errors.New("invalid token signature")
		}
		return nil
	})
}

// JWTRSA returns a built-in middleware for Bearer token validation using RSA-SHA256 (RS256).
// On successful authentication, decoded claims are stored in Context under ContextKey (default: "user"),
// and the raw token is stored under "jwt_token".
func JWTRSA(publicKey *rsa.PublicKey, opts ...JWTOptions) MiddlewareFunc {
	cfg := JWTOptions{
		Algorithm:    JWTAlgorithmRS256,
		RSAPublicKey: publicKey,
		ContextKey:   "user",
	}
	if len(opts) > 0 {
		applyJWTOptions(&cfg, opts[0])
	}
	return createJWTMiddleware(cfg)
}

// ParseRSAPrivateKeyFromPEM parses a PEM-encoded RSA private key in PKCS#1 (BEGIN RSA PRIVATE KEY)
// or PKCS#8 (BEGIN PRIVATE KEY) format.
func ParseRSAPrivateKeyFromPEM(keyPEM []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("jwt: failed to parse PEM block containing private key")
	}

	// 1. Try PKCS#1 (BEGIN RSA PRIVATE KEY)
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	// 2. Try PKCS#8 (BEGIN PRIVATE KEY)
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		if rsaKey, ok := parsedKey.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("jwt: parsed key is not an RSA private key")
	}

	return nil, errors.New("jwt: failed to parse RSA private key")
}

// ParseRSAPublicKeyFromPEM parses a PEM-encoded RSA public key in PKIX (BEGIN PUBLIC KEY),
// PKCS#1 (BEGIN RSA PUBLIC KEY), or an X.509 Certificate (BEGIN CERTIFICATE).
func ParseRSAPublicKeyFromPEM(keyPEM []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("jwt: failed to parse PEM block containing public key")
	}

	// 1. Try PKIX public key (BEGIN PUBLIC KEY)
	if parsedKey, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaPub, ok := parsedKey.(*rsa.PublicKey); ok {
			return rsaPub, nil
		}
		return nil, errors.New("jwt: parsed key is not an RSA public key")
	}

	// 2. Try PKCS#1 RSA public key (BEGIN RSA PUBLIC KEY)
	if rsaPub, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return rsaPub, nil
	}

	// 3. Try X.509 Certificate (BEGIN CERTIFICATE)
	if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
		if rsaPub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return rsaPub, nil
		}
		return nil, errors.New("jwt: certificate does not contain an RSA public key")
	}

	return nil, errors.New("jwt: failed to parse RSA public key")
}
