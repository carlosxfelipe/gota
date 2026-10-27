package core

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func generateTestECKeys(t *testing.T) (*ecdsa.PrivateKey, *ecdsa.PublicKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate EC key: %v", err)
	}
	return priv, &priv.PublicKey
}

func TestSignAndVerifyJWTES256(t *testing.T) {
	privKey, pubKey := generateTestECKeys(t)

	claims := testClaims{
		UserID:   200,
		Username: "ecdsa_user",
		Role:     "MobileApp",
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	}

	token, err := SignJWTES256(privKey, claims, "apple-key-1")
	if err != nil {
		t.Fatalf("failed to sign ES256 token: %v", err)
	}

	// Verify Header
	hdr, err := ParseJWTHeader(token)
	if err != nil {
		t.Fatalf("failed to parse JWT header: %v", err)
	}
	if hdr.Alg != "ES256" || hdr.Typ != "JWT" || hdr.Kid != "apple-key-1" {
		t.Errorf("unexpected header: %+v", hdr)
	}

	// Verify Claims
	var verified testClaims
	if err := VerifyJWTES256(pubKey, token, &verified); err != nil {
		t.Fatalf("failed to verify ES256 token: %v", err)
	}
	if verified.UserID != 200 || verified.Username != "ecdsa_user" || verified.Role != "MobileApp" {
		t.Errorf("decoded claims mismatch: %+v", verified)
	}
}

func TestVerifyJWTES256Expired(t *testing.T) {
	privKey, pubKey := generateTestECKeys(t)

	claims := testClaims{
		UserID:   1,
		Username: "expired",
		Exp:      time.Now().Add(-1 * time.Hour).Unix(),
	}

	token, err := SignJWTES256(privKey, claims)
	if err != nil {
		t.Fatalf("failed to sign ES256 token: %v", err)
	}

	var verified testClaims
	err = VerifyJWTES256(pubKey, token, &verified)
	if err == nil || err.Error() != "token expired" {
		t.Fatalf("expected 'token expired', got %v", err)
	}
}

func TestVerifyJWTES256InvalidSignature(t *testing.T) {
	privKey1, _ := generateTestECKeys(t)
	_, pubKey2 := generateTestECKeys(t)

	claims := testClaims{UserID: 1, Username: "tampered"}
	token, _ := SignJWTES256(privKey1, claims)

	var verified testClaims
	err := VerifyJWTES256(pubKey2, token, &verified)
	if err == nil || err.Error() != "invalid token signature" {
		t.Fatalf("expected 'invalid token signature', got %v", err)
	}
}

func TestES256AlgorithmConfusionPrevention(t *testing.T) {
	ecPrivKey, ecPubKey := generateTestECKeys(t)
	rsaPrivKey, rsaPubKey := generateTestRSAKeys(t)
	hmacSecret := []byte("hmac-secret-12345")

	// 1. ES256 token rejected by VerifyJWT (HS256)
	esToken, _ := SignJWTES256(ecPrivKey, map[string]any{"sub": "user"})
	if err := VerifyJWT(hmacSecret, esToken, nil); err == nil {
		t.Fatal("expected VerifyJWT (HS256) to reject ES256 token")
	}

	// 2. ES256 token rejected by VerifyJWTRSA (RS256)
	if err := VerifyJWTRSA(rsaPubKey, esToken, nil); err == nil {
		t.Fatal("expected VerifyJWTRSA (RS256) to reject ES256 token")
	}

	// 3. HS256 token rejected by VerifyJWTES256
	hsToken, _ := SignJWT(hmacSecret, map[string]any{"sub": "user"})
	if err := VerifyJWTES256(ecPubKey, hsToken, nil); err == nil {
		t.Fatal("expected VerifyJWTES256 to reject HS256 token")
	}

	// 4. RS256 token rejected by VerifyJWTES256
	rsToken, _ := SignJWTRSA(rsaPrivKey, map[string]any{"sub": "user"})
	if err := VerifyJWTES256(ecPubKey, rsToken, nil); err == nil {
		t.Fatal("expected VerifyJWTES256 to reject RS256 token")
	}

	// 5. 'none' algorithm rejected
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"victim"}`))
	noneToken := noneHeader + "." + payload + "."
	if err := VerifyJWTES256(ecPubKey, noneToken, nil); err == nil {
		t.Fatal("expected VerifyJWTES256 to reject 'none' algorithm")
	}
}

func TestECPEMParsing(t *testing.T) {
	privKey, pubKey := generateTestECKeys(t)

	// SEC 1 EC Private Key
	sec1Bytes, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		t.Fatalf("failed to marshal EC private key: %v", err)
	}
	sec1PEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: sec1Bytes,
	})
	parsedPriv1, err := ParseECPrivateKeyFromPEM(sec1PEM)
	if err != nil {
		t.Fatalf("failed to parse SEC 1 EC PEM: %v", err)
	}
	if parsedPriv1.D.Cmp(privKey.D) != 0 {
		t.Error("SEC 1 parsed private key mismatch")
	}

	// PKCS#8 EC Private Key
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		t.Fatalf("failed to marshal PKCS#8: %v", err)
	}
	pkcs8PEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	})
	parsedPriv8, err := ParseECPrivateKeyFromPEM(pkcs8PEM)
	if err != nil {
		t.Fatalf("failed to parse PKCS#8 EC PEM: %v", err)
	}
	if parsedPriv8.D.Cmp(privKey.D) != 0 {
		t.Error("PKCS#8 parsed private key mismatch")
	}

	// PKIX EC Public Key
	pkixBytes, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to marshal PKIX: %v", err)
	}
	pkixPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pkixBytes,
	})
	parsedPub, err := ParseECPublicKeyFromPEM(pkixPEM)
	if err != nil {
		t.Fatalf("failed to parse PKIX EC public key: %v", err)
	}
	if parsedPub.X.Cmp(pubKey.X) != 0 || parsedPub.Y.Cmp(pubKey.Y) != 0 {
		t.Error("PKIX parsed EC public key mismatch")
	}
}

func TestJWTES256Middleware(t *testing.T) {
	privKey, pubKey := generateTestECKeys(t)
	app := New(Config{DisableDocs: true})

	authGroup := app.Group("/mobile")
	authGroup.Use(JWTES256(pubKey, JWTOptions{
		ClaimsFactory: func() any { return &testClaims{} },
	}))

	authGroup.Get("/dashboard", func(ctx *Context) error {
		claims, ok := JWTClaims[*testClaims](ctx)
		if !ok {
			return ctx.Status(http.StatusUnauthorized).Send("claims missing")
		}
		return ctx.Json(claims)
	})

	// 1. Missing Authorization header
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mobile/dashboard", nil)
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", rec.Code)
	}

	// 2. Valid ES256 Bearer Token
	validToken, _ := SignJWTES256(privKey, testClaims{
		UserID:   888,
		Username: "ios_user",
		Role:     "Driver",
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	})

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/mobile/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+validToken)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var res testClaims
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.UserID != 888 || res.Username != "ios_user" {
		t.Errorf("unexpected response body: %+v", res)
	}
}

func TestES256CurveValidation(t *testing.T) {
	// Attempting to sign with P-384 key must fail for ES256
	p384Priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate P-384 key: %v", err)
	}

	_, err = SignJWTES256(p384Priv, map[string]any{"sub": "invalid"})
	if err == nil {
		t.Fatal("expected error when signing ES256 with P-384 key")
	}

	err = VerifyJWTES256(&p384Priv.PublicKey, "a.b.c", nil)
	if err == nil {
		t.Fatal("expected error when verifying ES256 with P-384 key")
	}
}
