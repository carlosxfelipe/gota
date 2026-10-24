package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type testClaims struct {
	UserID   int    `json:"userId"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

func TestSignAndVerifyJWT(t *testing.T) {
	secret := []byte("super-secret-key-12345")

	claims := testClaims{
		UserID:   42,
		Username: "gopher",
		Role:     "Admin",
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	}

	token, err := SignJWT(secret, claims)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	var verified testClaims
	if err := VerifyJWT(secret, token, &verified); err != nil {
		t.Fatalf("failed to verify token: %v", err)
	}

	if verified.UserID != 42 || verified.Username != "gopher" || verified.Role != "Admin" {
		t.Errorf("decoded claims mismatch: %+v", verified)
	}
}

func TestVerifyJWTExpired(t *testing.T) {
	secret := []byte("secret")
	claims := testClaims{
		UserID:   1,
		Username: "old",
		Exp:      time.Now().Add(-1 * time.Hour).Unix(), // already expired
	}

	token, err := SignJWT(secret, claims)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	var verified testClaims
	err = VerifyJWT(secret, token, &verified)
	if err == nil || err.Error() != "token expired" {
		t.Fatalf("expected 'token expired', got %v", err)
	}
}

func TestVerifyJWTInvalidSignature(t *testing.T) {
	secret := []byte("correct-secret")
	wrongSecret := []byte("wrong-secret")

	claims := testClaims{UserID: 1, Username: "test"}
	token, _ := SignJWT(secret, claims)

	var verified testClaims
	err := VerifyJWT(wrongSecret, token, &verified)
	if err == nil || err.Error() != "invalid token signature" {
		t.Fatalf("expected 'invalid token signature', got %v", err)
	}
}

func TestJWTMiddleware(t *testing.T) {
	secret := []byte("middleware-secret")
	app := New(Config{DisableDocs: true})

	authGroup := app.Group("/protected")
	authGroup.Use(JWT(secret, JWTOptions{
		ClaimsFactory: func() any { return &testClaims{} },
	}))

	authGroup.Get("/profile", func(ctx *Context) error {
		claims, ok := JWTClaims[*testClaims](ctx)
		if !ok {
			return ctx.Status(http.StatusUnauthorized).Send("claims missing")
		}
		return ctx.Json(claims)
	})

	// 1. Missing Authorization header
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/protected/profile", nil)
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", rec.Code)
	}

	// 2. Malformed Authorization header
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/protected/profile", nil)
	req.Header.Set("Authorization", "Basic 12345")
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for basic auth, got %d", rec.Code)
	}

	// 3. Valid Bearer Token
	validToken, _ := SignJWT(secret, testClaims{
		UserID:   99,
		Username: "alice",
		Role:     "User",
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	})

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/protected/profile", nil)
	req.Header.Set("Authorization", "Bearer "+validToken)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var res testClaims
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.UserID != 99 || res.Username != "alice" {
		t.Errorf("unexpected response body: %+v", res)
	}
}
