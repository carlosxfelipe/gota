package core

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
)

// SignJWTES256 creates an ECDSA P-256 SHA-256 (ES256) signed JWT token string using the provided private key.
// The resulting signature adheres to RFC 7515/7518 (raw concatenated 64-byte R || S integer pair).
// An optional keyID can be provided to populate the 'kid' field in the JWT header.
func SignJWTES256(privateKey *ecdsa.PrivateKey, payload any, keyID ...string) (string, error) {
	if privateKey == nil {
		return "", errors.New("jwt: ECDSA private key cannot be nil")
	}
	if privateKey.Curve == nil || privateKey.Curve.Params().Name != "P-256" {
		return "", errors.New("jwt: private key curve must be P-256 for ES256")
	}

	var headerJSON []byte
	if len(keyID) > 0 && keyID[0] != "" {
		headerJSON, _ = json.Marshal(JWTHeader{
			Alg: string(JWTAlgorithmES256),
			Typ: "JWT",
			Kid: keyID[0],
		})
	} else {
		headerJSON = []byte(`{"alg":"ES256","typ":"JWT"}`)
	}
	header := base64.RawURLEncoding.EncodeToString(headerJSON)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signingInput := header + "." + payloadEncoded
	hashed := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, privateKey, hashed[:])
	if err != nil {
		return "", err
	}

	// RFC 7518 Section 3.4: ES256 signature is R || S with 32 octets each (64 bytes total)
	const keyBytes = 32
	sigBytes := make([]byte, 2*keyBytes)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sigBytes[keyBytes-len(rBytes):keyBytes], rBytes)
	copy(sigBytes[2*keyBytes-len(sBytes):], sBytes)

	signature := base64.RawURLEncoding.EncodeToString(sigBytes)

	return signingInput + "." + signature, nil
}

// VerifyJWTES256 validates an ECDSA P-256 SHA-256 (ES256) JWT token against the given public key and decodes its payload into target.
// If target is nil, only the format, signature, and expiration are verified.
func VerifyJWTES256(publicKey *ecdsa.PublicKey, tokenString string, target any) error {
	return verifyJWTES256(publicKey, tokenString, target, false)
}

func verifyJWTES256(publicKey *ecdsa.PublicKey, tokenString string, target any, requireExp bool) error {
	if publicKey == nil {
		return errors.New("jwt: ECDSA public key cannot be nil")
	}
	if publicKey.Curve == nil || publicKey.Curve.Params().Name != "P-256" {
		return errors.New("jwt: public key curve must be P-256 for ES256")
	}

	return verifyToken(tokenString, target, requireExp, string(JWTAlgorithmES256), func(signingInput string, sig []byte) error {
		if len(sig) != 64 {
			return errors.New("invalid token signature")
		}
		hashed := sha256.Sum256([]byte(signingInput))
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])

		if !ecdsa.Verify(publicKey, hashed[:], r, s) {
			return errors.New("invalid token signature")
		}
		return nil
	})
}

// JWTES256 returns a built-in middleware for Bearer token validation using ECDSA P-256 SHA-256 (ES256).
// On successful authentication, decoded claims are stored in Context under ContextKey (default: "user"),
// and the raw token is stored under "jwt_token".
func JWTES256(publicKey *ecdsa.PublicKey, opts ...JWTOptions) MiddlewareFunc {
	cfg := JWTOptions{
		Algorithm:      JWTAlgorithmES256,
		ECDSAPublicKey: publicKey,
		ContextKey:     "user",
	}
	if len(opts) > 0 {
		applyJWTOptions(&cfg, opts[0])
	}
	return createJWTMiddleware(cfg)
}

// ParseECPrivateKeyFromPEM parses a PEM-encoded ECDSA private key in SEC 1 (BEGIN EC PRIVATE KEY)
// or PKCS#8 (BEGIN PRIVATE KEY) format.
func ParseECPrivateKeyFromPEM(keyPEM []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("jwt: failed to parse PEM block containing EC private key")
	}

	// 1. Try SEC 1 EC private key (BEGIN EC PRIVATE KEY)
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	// 2. Try PKCS#8 private key (BEGIN PRIVATE KEY)
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		if ecKey, ok := parsedKey.(*ecdsa.PrivateKey); ok {
			return ecKey, nil
		}
		return nil, errors.New("jwt: parsed key is not an ECDSA private key")
	}

	return nil, errors.New("jwt: failed to parse EC private key")
}

// ParseECPublicKeyFromPEM parses a PEM-encoded ECDSA public key in PKIX (BEGIN PUBLIC KEY)
// or from an X.509 Certificate (BEGIN CERTIFICATE).
func ParseECPublicKeyFromPEM(keyPEM []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("jwt: failed to parse PEM block containing EC public key")
	}

	// 1. Try PKIX public key (BEGIN PUBLIC KEY)
	if parsedKey, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if ecPub, ok := parsedKey.(*ecdsa.PublicKey); ok {
			return ecPub, nil
		}
		return nil, errors.New("jwt: parsed key is not an ECDSA public key")
	}

	// 2. Try X.509 Certificate (BEGIN CERTIFICATE)
	if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
		if ecPub, ok := cert.PublicKey.(*ecdsa.PublicKey); ok {
			return ecPub, nil
		}
		return nil, errors.New("jwt: certificate does not contain an ECDSA public key")
	}

	return nil, errors.New("jwt: failed to parse EC public key")
}
