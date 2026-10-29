package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func generateTestEdKeys(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate Ed25519 key: %v", err)
	}
	return priv, pub
}

func TestSignAndVerifyJWTEdDSA(t *testing.T) {
	privKey, pubKey := generateTestEdKeys(t)

	claims := testClaims{
		UserID:   300,
		Username: "eddsa_user",
		Role:     "ServiceMesh",
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	}

	token, err := SignJWTEdDSA(privKey, claims, "ed-key-1")
	if err != nil {
		t.Fatalf("failed to sign EdDSA token: %v", err)
	}

	hdr, err := ParseJWTHeader(token)
	if err != nil {
		t.Fatalf("failed to parse JWT header: %v", err)
	}
	if hdr.Alg != "EdDSA" || hdr.Typ != "JWT" || hdr.Kid != "ed-key-1" {
		t.Errorf("unexpected header: %+v", hdr)
	}

	var verified testClaims
	if err := VerifyJWTEdDSA(pubKey, token, &verified); err != nil {
		t.Fatalf("failed to verify EdDSA token: %v", err)
	}
	if verified.UserID != 300 || verified.Username != "eddsa_user" || verified.Role != "ServiceMesh" {
		t.Errorf("decoded claims mismatch: %+v", verified)
	}
}

func TestVerifyJWTEdDSAExpired(t *testing.T) {
	privKey, pubKey := generateTestEdKeys(t)

	token, err := SignJWTEdDSA(privKey, testClaims{
		UserID:   1,
		Username: "expired",
		Exp:      time.Now().Add(-1 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("failed to sign EdDSA token: %v", err)
	}

	var verified testClaims
	err = VerifyJWTEdDSA(pubKey, token, &verified)
	if err == nil || err.Error() != "token expired" {
		t.Fatalf("expected 'token expired', got %v", err)
	}
}

func TestVerifyJWTEdDSAInvalidSignature(t *testing.T) {
	privKey1, _ := generateTestEdKeys(t)
	_, pubKey2 := generateTestEdKeys(t)

	token, _ := SignJWTEdDSA(privKey1, testClaims{UserID: 1, Username: "tampered"})

	var verified testClaims
	err := VerifyJWTEdDSA(pubKey2, token, &verified)
	if err == nil || err.Error() != "invalid token signature" {
		t.Fatalf("expected 'invalid token signature', got %v", err)
	}
}

func TestEdDSAPEMParsers(t *testing.T) {
	privKey, pubKey := generateTestEdKeys(t)

	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		t.Fatalf("failed to marshal PKCS#8: %v", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes})

	pkixBytes, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to marshal PKIX: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkixBytes})

	parsedPriv, err := ParseEdPrivateKeyFromPEM(privPEM)
	if err != nil || !parsedPriv.Equal(privKey) {
		t.Fatalf("failed to parse Ed25519 private key PEM: %v", err)
	}

	parsedPub, err := ParseEdPublicKeyFromPEM(pubPEM)
	if err != nil || !parsedPub.Equal(pubKey) {
		t.Fatalf("failed to parse Ed25519 public key PEM: %v", err)
	}
}

func TestJWTEdDSAMiddleware(t *testing.T) {
	privKey, pubKey := generateTestEdKeys(t)
	app := New(Config{DisableDocs: true})
	app.Use(JWTEdDSA(pubKey, JWTOptions{
		RequireExp:    true,
		ClaimsFactory: func() any { return &testClaims{} },
	}))

	app.Get("/mesh", func(ctx *Context) error {
		c, ok := JWTClaims[*testClaims](ctx)
		if !ok {
			return Unauthorized("missing claims")
		}
		return ctx.Send(c.Username)
	})

	token, _ := SignJWTEdDSA(privKey, testClaims{
		UserID:   77,
		Username: "envoy",
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/mesh", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "envoy" {
		t.Fatalf("expected 200 envoy, got %d: %s", rec.Code, rec.Body.String())
	}
}
