package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
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

func TestJWTWithKeyFuncKeyRotationAndAlgConfusion(t *testing.T) {
	rsaPriv1, rsaPub1 := generateTestRSAKeys(t)
	ecPriv2, ecPub2 := generateTestECKeys(t)
	edPriv3, edPub3 := generateTestEdKeys(t)
	hmacSecret := []byte("shared-secret-key")

	keysByKid := map[string]any{
		"rsa-2025": rsaPub1,
		"ec-2026":  ecPub2,
		"ed-2026":  edPub3,
		"hs-2026":  hmacSecret,
	}

	keyFunc := func(hdr JWTHeader) (any, error) {
		k, ok := keysByKid[hdr.Kid]
		if !ok {
			return nil, errors.New("unknown kid")
		}
		return k, nil
	}

	app := New(Config{DisableDocs: true})
	app.Use(JWTWithKeyFunc(keyFunc, JWTOptions{
		RequireExp:    true,
		ClaimsFactory: func() any { return &testClaims{} },
	}))
	app.Get("/whoami", func(ctx *Context) error {
		c, _ := JWTClaims[*testClaims](ctx)
		return ctx.Send(c.Username)
	})

	exp := time.Now().Add(time.Hour).Unix()
	tRSA, _ := SignJWTRSA(rsaPriv1, testClaims{Username: "rsa_ok", Exp: exp}, "rsa-2025")
	tEC, _ := SignJWTES256(ecPriv2, testClaims{Username: "ec_ok", Exp: exp}, "ec-2026")
	tEd, _ := SignJWTEdDSA(edPriv3, testClaims{Username: "ed_ok", Exp: exp}, "ed-2026")
	tHS, _ := SignJWT(hmacSecret, testClaims{Username: "hs_ok", Exp: exp}, "hs-2026")

	for wantUser, tok := range map[string]string{
		"rsa_ok": tRSA,
		"ec_ok":  tEC,
		"ed_ok":  tEd,
		"hs_ok":  tHS,
	} {
		req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != wantUser {
			t.Errorf("expected 200 %q, got %d %q", wantUser, rec.Code, rec.Body.String())
		}
	}

	// Algorithm confusion check: signing an HS256 token claiming kid="rsa-2025" (which maps to *rsa.PublicKey) must fail!
	confusedToken, _ := SignJWT(hmacSecret, testClaims{Username: "hacker", Exp: exp}, "rsa-2025")
	if err := VerifyJWTWithKeyFunc(keyFunc, confusedToken, nil); err == nil {
		t.Fatal("expected VerifyJWTWithKeyFunc to reject token when header alg (HS256) does not match key type (*rsa.PublicKey)")
	}
}
