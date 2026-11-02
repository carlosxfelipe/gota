package core

import (
	"strings"
	"testing"
)

func mustPanicContaining(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		if msg, _ := r.(string); !strings.Contains(msg, want) {
			t.Fatalf("panic %q does not contain %q", r, want)
		}
	}()
	fn()
}

func TestJWTRejectsShortHS256Secret(t *testing.T) {
	mustPanicContaining(t, "at least 32 bytes", func() { JWT([]byte("short")) })
	mustPanicContaining(t, "at least 32 bytes", func() { JWT(nil) })
	mustPanicContaining(t, "got 31", func() { JWT([]byte(strings.Repeat("a", 31))) })
}

func TestJWTAcceptsStrongOrExplicitlyAllowedSecret(t *testing.T) {
	JWT([]byte(strings.Repeat("a", MinHS256SecretLength)))
	JWT([]byte("short"), JWTOptions{AllowShortSecret: true})
}

func TestJWTSecretCheckSkippedForAsymmetricAndKeyFunc(t *testing.T) {
	_, rsaPub := generateTestRSAKeys(t)
	_, ecPub := generateTestECKeys(t)
	_, edPub := generateTestEdKeys(t)

	// No HMAC secret is involved in these modes, so none is required.
	JWT(nil, JWTOptions{RSAPublicKey: rsaPub})
	JWT(nil, JWTOptions{ECDSAPublicKey: ecPub})
	JWT(nil, JWTOptions{EdDSAPublicKey: edPub})
	JWT(nil, JWTOptions{Algorithm: JWTAlgorithmRS256, RSAPublicKey: rsaPub})
	JWTWithKeyFunc(func(JWTHeader) (any, error) { return nil, nil })
}
