package core

import (
	"errors"
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

func TestVerifyJWTMalformedExpIsRejected(t *testing.T) {
	secret := []byte("secret")

	// Expired token whose exp is a fractional number must still be rejected.
	token, _ := SignJWT(secret, map[string]any{"exp": float64(time.Now().Add(-time.Hour).Unix()) + 0.5})
	if err := VerifyJWT(secret, token, nil); err == nil || err.Error() != "token expired" {
		t.Fatalf("expected 'token expired' for fractional exp, got %v", err)
	}

	// exp with the wrong type must not disable the check.
	token, _ = SignJWT(secret, map[string]any{"exp": "1"})
	if err := VerifyJWT(secret, token, nil); err == nil {
		t.Fatal("expected error for string exp")
	}
}

func TestVerifyJWTNotBefore(t *testing.T) {
	secret := []byte("secret")
	token, _ := SignJWT(secret, map[string]any{"nbf": time.Now().Add(time.Hour).Unix()})
	if err := VerifyJWT(secret, token, nil); err == nil || err.Error() != "token not yet valid" {
		t.Fatalf("expected 'token not yet valid', got %v", err)
	}
}

func TestJWTMiddlewareRequireExp(t *testing.T) {
	secret := []byte("middleware-secret")
	app := New(Config{DisableDocs: true})
	group := app.Group("/p")
	group.Use(JWT(secret, JWTOptions{RequireExp: true}))
	group.Get("/x", func(ctx *Context) error { return ctx.Send("ok") })

	noExp, _ := SignJWT(secret, map[string]any{"sub": "1"})
	withExp, _ := SignJWT(secret, map[string]any{"sub": "1", "exp": time.Now().Add(time.Hour).Unix()})

	for token, want := range map[string]int{noExp: http.StatusUnauthorized, withExp: http.StatusOK} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/p/x", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		app.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("expected %d, got %d", want, rec.Code)
		}
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
