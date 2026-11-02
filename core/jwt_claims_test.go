package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func jwtStatus(t *testing.T, opts JWTOptions, secret []byte, claims map[string]any) int {
	t.Helper()
	app := New(Config{DisableDocs: true})
	g := app.Group("/p")
	g.Use(JWT(secret, opts))
	g.Get("/x", func(ctx *Context) error { return ctx.Send("ok") })
	token, err := SignJWT(secret, claims)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	app.ServeHTTP(rec, req)
	return rec.Code
}

func TestJWTIssuerValidation(t *testing.T) {
	secret := []byte("issuer-secret-0123456789-0123456789-abcdef")
	opts := JWTOptions{Issuer: "https://auth.example.com"}
	cases := []struct {
		name   string
		claims map[string]any
		want   int
	}{
		{"match", map[string]any{"iss": "https://auth.example.com"}, http.StatusOK},
		{"mismatch", map[string]any{"iss": "https://evil.example.com"}, http.StatusUnauthorized},
		{"missing", map[string]any{"sub": "1"}, http.StatusUnauthorized},
		{"wrong type", map[string]any{"iss": 123}, http.StatusUnauthorized},
	}
	for _, c := range cases {
		if got := jwtStatus(t, opts, secret, c.claims); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	// Optional by default.
	if got := jwtStatus(t, JWTOptions{}, secret, map[string]any{"iss": 123}); got != http.StatusOK {
		t.Errorf("iss must not be checked when Issuer is unset, got %d", got)
	}
}

func TestJWTAudienceValidation(t *testing.T) {
	secret := []byte("audience-secret-0123456789-0123456789-abcdef")
	opts := JWTOptions{Audiences: []string{"my-api"}}
	cases := []struct {
		name   string
		claims map[string]any
		want   int
	}{
		{"string match", map[string]any{"aud": "my-api"}, http.StatusOK},
		{"array contains", map[string]any{"aud": []string{"other", "my-api"}}, http.StatusOK},
		{"array without", map[string]any{"aud": []string{"other"}}, http.StatusUnauthorized},
		{"string mismatch", map[string]any{"aud": "other"}, http.StatusUnauthorized},
		{"missing", map[string]any{"sub": "1"}, http.StatusUnauthorized},
		{"wrong type", map[string]any{"aud": 5}, http.StatusUnauthorized},
	}
	for _, c := range cases {
		if got := jwtStatus(t, opts, secret, c.claims); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestJWTAudiencesSliceValidation(t *testing.T) {
	secret := []byte("audience-secret-0123456789-0123456789-abcdef")
	opts := JWTOptions{Audiences: []string{"api-v1", "api-v2"}}
	cases := []struct {
		name   string
		claims map[string]any
		want   int
	}{
		{"matches first", map[string]any{"aud": "api-v1"}, http.StatusOK},
		{"matches second", map[string]any{"aud": "api-v2"}, http.StatusOK},
		{"matches one in array", map[string]any{"aud": []string{"other", "api-v2"}}, http.StatusOK},
		{"matches none in array", map[string]any{"aud": []string{"other", "api-v3"}}, http.StatusUnauthorized},
		{"mismatch string", map[string]any{"aud": "other"}, http.StatusUnauthorized},
	}
	for _, c := range cases {
		if got := jwtStatus(t, opts, secret, c.claims); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestJWTLeeway(t *testing.T) {
	secret := []byte("leeway-secret-0123456789-0123456789-abcdef")
	expired30s := map[string]any{"exp": time.Now().Add(-30 * time.Second).Unix()}
	if got := jwtStatus(t, JWTOptions{}, secret, expired30s); got != http.StatusUnauthorized {
		t.Errorf("no leeway: got %d", got)
	}
	if got := jwtStatus(t, JWTOptions{Leeway: time.Minute}, secret, expired30s); got != http.StatusOK {
		t.Errorf("exp within leeway: got %d", got)
	}
	nbfSoon := map[string]any{"nbf": time.Now().Add(30 * time.Second).Unix()}
	if got := jwtStatus(t, JWTOptions{Leeway: time.Minute}, secret, nbfSoon); got != http.StatusOK {
		t.Errorf("nbf within leeway: got %d", got)
	}
	// Leeway is capped at 5 minutes.
	expired1h := map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}
	if got := jwtStatus(t, JWTOptions{Leeway: 24 * time.Hour}, secret, expired1h); got != http.StatusUnauthorized {
		t.Errorf("leeway cap: got %d", got)
	}
	// Negative leeway is treated as zero.
	if got := jwtStatus(t, JWTOptions{Leeway: -time.Hour}, secret, map[string]any{"exp": time.Now().Add(time.Minute).Unix()}); got != http.StatusOK {
		t.Errorf("negative leeway: got %d", got)
	}
}
