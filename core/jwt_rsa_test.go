package core

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func generateTestRSAKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	return priv, &priv.PublicKey
}

func TestSignAndVerifyJWTRSA(t *testing.T) {
	privKey, pubKey := generateTestRSAKeys(t)

	claims := testClaims{
		UserID:   100,
		Username: "rsa_user",
		Role:     "Operator",
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	}

	token, err := SignJWTRSA(privKey, claims, "key-2026-1")
	if err != nil {
		t.Fatalf("failed to sign RS256 token: %v", err)
	}

	// Verify Header
	hdr, err := ParseJWTHeader(token)
	if err != nil {
		t.Fatalf("failed to parse JWT header: %v", err)
	}
	if hdr.Alg != "RS256" || hdr.Typ != "JWT" || hdr.Kid != "key-2026-1" {
		t.Errorf("unexpected header: %+v", hdr)
	}

	// Verify Claims
	var verified testClaims
	if err := VerifyJWTRSA(pubKey, token, &verified); err != nil {
		t.Fatalf("failed to verify RS256 token: %v", err)
	}
	if verified.UserID != 100 || verified.Username != "rsa_user" || verified.Role != "Operator" {
		t.Errorf("decoded claims mismatch: %+v", verified)
	}
}

func TestVerifyJWTRSAExpired(t *testing.T) {
	privKey, pubKey := generateTestRSAKeys(t)

	claims := testClaims{
		UserID:   1,
		Username: "expired",
		Exp:      time.Now().Add(-1 * time.Hour).Unix(),
	}

	token, err := SignJWTRSA(privKey, claims)
	if err != nil {
		t.Fatalf("failed to sign RS256 token: %v", err)
	}

	var verified testClaims
	err = VerifyJWTRSA(pubKey, token, &verified)
	if err == nil || err.Error() != "token expired" {
		t.Fatalf("expected 'token expired', got %v", err)
	}
}

func TestVerifyJWTRSAInvalidSignature(t *testing.T) {
	privKey1, _ := generateTestRSAKeys(t)
	_, pubKey2 := generateTestRSAKeys(t)

	claims := testClaims{UserID: 1, Username: "tampered"}
	token, _ := SignJWTRSA(privKey1, claims)

	var verified testClaims
	err := VerifyJWTRSA(pubKey2, token, &verified)
	if err == nil || err.Error() != "invalid token signature" {
		t.Fatalf("expected 'invalid token signature', got %v", err)
	}
}

func TestAlgorithmConfusionPrevention(t *testing.T) {
	privKey, pubKey := generateTestRSAKeys(t)
	hmacSecret := []byte("shared-secret-12345")

	// 1. Token signed with HS256 must NOT be accepted by VerifyJWTRSA
	hsToken, err := SignJWT(hmacSecret, map[string]any{"sub": "attacker"})
	if err != nil {
		t.Fatalf("failed to sign HS256 token: %v", err)
	}
	if err := VerifyJWTRSA(pubKey, hsToken, nil); err == nil {
		t.Fatal("expected VerifyJWTRSA to reject HS256 token")
	}

	// 2. Token signed with RS256 must NOT be accepted by VerifyJWT (HS256)
	rsToken, err := SignJWTRSA(privKey, map[string]any{"sub": "user"})
	if err != nil {
		t.Fatalf("failed to sign RS256 token: %v", err)
	}
	if err := VerifyJWT(hmacSecret, rsToken, nil); err == nil {
		t.Fatal("expected VerifyJWT to reject RS256 token")
	}

	// 3. Token with "alg": "none" must be rejected by both
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"victim"}`))
	noneToken := noneHeader + "." + payload + "."
	if err := VerifyJWT(hmacSecret, noneToken, nil); err == nil {
		t.Fatal("expected VerifyJWT to reject 'none' algorithm")
	}
	if err := VerifyJWTRSA(pubKey, noneToken, nil); err == nil {
		t.Fatal("expected VerifyJWTRSA to reject 'none' algorithm")
	}
}

func TestPEMParsing(t *testing.T) {
	privKey, pubKey := generateTestRSAKeys(t)

	// PKCS#1 Private Key
	pkcs1Bytes := x509.MarshalPKCS1PrivateKey(privKey)
	pkcs1PEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: pkcs1Bytes,
	})
	parsedPriv1, err := ParseRSAPrivateKeyFromPEM(pkcs1PEM)
	if err != nil {
		t.Fatalf("failed to parse PKCS#1 PEM: %v", err)
	}
	if parsedPriv1.N.Cmp(privKey.N) != 0 {
		t.Error("PKCS#1 parsed private key mismatch")
	}

	// PKCS#8 Private Key
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		t.Fatalf("failed to marshal PKCS#8: %v", err)
	}
	pkcs8PEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	})
	parsedPriv8, err := ParseRSAPrivateKeyFromPEM(pkcs8PEM)
	if err != nil {
		t.Fatalf("failed to parse PKCS#8 PEM: %v", err)
	}
	if parsedPriv8.N.Cmp(privKey.N) != 0 {
		t.Error("PKCS#8 parsed private key mismatch")
	}

	// PKIX Public Key
	pkixBytes, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to marshal PKIX: %v", err)
	}
	pkixPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pkixBytes,
	})
	parsedPubIX, err := ParseRSAPublicKeyFromPEM(pkixPEM)
	if err != nil {
		t.Fatalf("failed to parse PKIX public key: %v", err)
	}
	if parsedPubIX.N.Cmp(pubKey.N) != 0 {
		t.Error("PKIX parsed public key mismatch")
	}

	// PKCS#1 Public Key
	pkcs1PubBytes := x509.MarshalPKCS1PublicKey(pubKey)
	pkcs1PubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: pkcs1PubBytes,
	})
	parsedPub1, err := ParseRSAPublicKeyFromPEM(pkcs1PubPEM)
	if err != nil {
		t.Fatalf("failed to parse PKCS#1 public key: %v", err)
	}
	if parsedPub1.N.Cmp(pubKey.N) != 0 {
		t.Error("PKCS#1 parsed public key mismatch")
	}
}

func TestJWTRSAMiddleware(t *testing.T) {
	privKey, pubKey := generateTestRSAKeys(t)
	app := New(Config{DisableDocs: true})

	authGroup := app.Group("/secure")
	authGroup.Use(JWTRSA(pubKey, JWTOptions{
		ClaimsFactory: func() any { return &testClaims{} },
	}))

	authGroup.Get("/user", func(ctx *Context) error {
		claims, ok := JWTClaims[*testClaims](ctx)
		if !ok {
			return ctx.Status(http.StatusUnauthorized).Send("claims missing")
		}
		return ctx.Json(claims)
	})

	// 1. Missing Authorization header
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/secure/user", nil)
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", rec.Code)
	}

	// 2. Valid RS256 Bearer Token
	validToken, _ := SignJWTRSA(privKey, testClaims{
		UserID:   777,
		Username: "bob",
		Role:     "Auditor",
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	})

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/secure/user", nil)
	req.Header.Set("Authorization", "Bearer "+validToken)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var res testClaims
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.UserID != 777 || res.Username != "bob" {
		t.Errorf("unexpected response body: %+v", res)
	}
}

func TestJWTOptionsAlgorithmDefault(t *testing.T) {
	// Verify that calling JWT without algorithm options defaults to HS256
	secret := []byte("secret-key")
	opts := JWTOptions{}
	app := New(Config{DisableDocs: true})
	app.Use(JWT(secret, opts))

	// An RS256 token must be rejected because default is HS256
	privKey, _ := generateTestRSAKeys(t)
	rsToken, _ := SignJWTRSA(privKey, map[string]any{"sub": "test", "exp": time.Now().Add(time.Hour).Unix()})

	app.Get("/test", func(ctx *Context) error {
		return ctx.Send("ok")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+rsToken)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 rejecting RS256 token on default HS256 middleware, got %d", rec.Code)
	}
}
