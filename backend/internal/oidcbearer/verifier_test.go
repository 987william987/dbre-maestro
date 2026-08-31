package oidcbearer

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeIssuer mimics Authentik: discovery + JWKS, issuer with a trailing slash.
type fakeIssuer struct {
	srv  *httptest.Server
	key  *rsa.PrivateKey
	url  string
	hits atomic.Int32
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.url,
			"jwks_uri":                              f.srv.URL + "/jwks",
			"authorization_endpoint":                f.srv.URL + "/authorize",
			"token_endpoint":                        f.srv.URL + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		pub := &key.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	})
	f.srv = httptest.NewServer(mux)
	f.url = f.srv.URL + "/"
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIssuer) token(t *testing.T, override jwt.MapClaims) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": f.url, "sub": "uuid-1", "aud": "edgex-cli",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"email": "brian@example.com", "email_verified": true, "preferred_username": "brian",
	}
	for k, v := range override {
		claims[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	signed, err := tok.SignedString(f.key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestVerifyAcceptsTokenForAnAllowedAudience(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"other-cli", "edgex-cli"})

	id, err := v.Verify(context.Background(), f.token(t, nil))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if id.Subject != "uuid-1" || id.Email != "brian@example.com" || !id.EmailVerified || id.PreferredUsername != "brian" {
		t.Fatalf("identity = %+v", id)
	}
	// second call reuses discovery and the cached key set
	before := f.hits.Load()
	if _, err := v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("second Verify() error = %v", err)
	}
	if f.hits.Load() != before {
		t.Fatalf("issuer hit again on a cached key: %d -> %d", before, f.hits.Load())
	}
}

func TestVerifyRejectsWrongAudience(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	if _, err := v.Verify(context.Background(), f.token(t, jwt.MapClaims{"aud": "grafana"})); err == nil {
		t.Fatal("Verify() accepted a token for another audience")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	if _, err := v.Verify(context.Background(), f.token(t, jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()})); err == nil {
		t.Fatal("Verify() accepted an expired token")
	}
}

func TestVerifyRejectsForeignIssuerWithoutNetwork(t *testing.T) {
	f := newFakeIssuer(t)
	v := New("https://idp.example.com/application/o/edgex-cli/", []string{"edgex-cli"})
	_, err := v.Verify(context.Background(), f.token(t, nil))
	if !errors.Is(err, ErrForeignIssuer) {
		t.Fatalf("Verify() error = %v, want ErrForeignIssuer", err)
	}
	if f.hits.Load() != 0 {
		t.Fatalf("foreign issuer caused %d network hits", f.hits.Load())
	}
}

func TestVerifyRejectsHMACToken(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": f.url, "sub": "uuid-1", "aud": "edgex-cli", "exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString([]byte("session-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), signed); err == nil {
		t.Fatal("Verify() accepted an HS256 token")
	}
}

func TestVerifyRejectsNonJWT(t *testing.T) {
	v := New("https://idp.example.com/", []string{"edgex-cli"})
	if _, err := v.Verify(context.Background(), "not-a-jwt"); err == nil {
		t.Fatal("Verify() accepted a non-JWT string")
	}
}
