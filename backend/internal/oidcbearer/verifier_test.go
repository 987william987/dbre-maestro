package oidcbearer

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeIssuer mimics Authentik: discovery + JWKS, issuer with a trailing slash.
type fakeIssuer struct {
	srv     *httptest.Server
	key     *rsa.PrivateKey
	kid     string
	use     string // JWK "use", default sig
	alg     string // JWK "alg", default RS256
	jwksURL string // advertised jwks_uri, default <srv>/jwks
	url     string
	hits    atomic.Int32
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIssuer{key: key, kid: "k1", use: "sig", alg: "RS256"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.url,
			"jwks_uri":                              f.jwksURL,
			"authorization_endpoint":                f.srv.URL + "/authorize",
			"token_endpoint":                        f.srv.URL + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		pub := &f.key.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": f.kid, "use": f.use, "alg": f.alg,
			"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/jwks-elsewhere", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		http.Redirect(w, r, "http://example.invalid/jwks", http.StatusFound)
	})
	f.srv = httptest.NewServer(mux)
	f.url = f.srv.URL + "/"
	f.jwksURL = f.srv.URL + "/jwks"
	t.Cleanup(f.srv.Close)
	return f
}

func b64(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (f *fakeIssuer) claims(override map[string]any) map[string]any {
	claims := map[string]any{
		"iss": f.url, "sub": "uuid-1", "aud": "edgex-cli",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"email": "brian@example.com", "email_verified": true, "preferred_username": "brian",
	}
	for k, v := range override {
		if v == nil {
			delete(claims, k)
			continue
		}
		claims[k] = v
	}
	return claims
}

// token signs RS256 by hand (RSASSA-PKCS1-v1_5 over SHA-256) so the test needs no JWT library.
func (f *fakeIssuer) token(t *testing.T, override map[string]any) string {
	return f.tokenWith(t, f.key, f.kid, override)
}

func (f *fakeIssuer) tokenWith(t *testing.T, key *rsa.PrivateKey, kid string, override map[string]any) string {
	t.Helper()
	signingInput := b64(map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid}) + "." + b64(f.claims(override))
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *fakeIssuer) hmacToken(secret string) string {
	signingInput := b64(map[string]string{"alg": "HS256", "typ": "JWT", "kid": "k1"}) + "." + b64(f.claims(nil))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
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
	if _, err := v.Verify(context.Background(), f.token(t, map[string]any{"aud": "grafana"})); err == nil {
		t.Fatal("Verify() accepted a token for another audience")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	if _, err := v.Verify(context.Background(), f.token(t, map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})); err == nil {
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
	if _, err := v.Verify(context.Background(), f.hmacToken("session-secret")); err == nil {
		t.Fatal("Verify() accepted an HS256 token")
	}
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	parts := strings.Split(f.token(t, nil), ".")
	parts[1] = b64(f.claims(map[string]any{"email": "someone-else@example.com"}))
	if _, err := v.Verify(context.Background(), strings.Join(parts, ".")); err == nil {
		t.Fatal("Verify() accepted a token whose payload was swapped")
	}
}

func TestVerifyRejectsNonJWT(t *testing.T) {
	v := New("https://idp.example.com/", []string{"edgex-cli"})
	if _, err := v.Verify(context.Background(), "not-a-jwt"); err == nil {
		t.Fatal("Verify() accepted a non-JWT string")
	}
}

func TestVerifyAcceptsAudienceArray(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	if _, err := v.Verify(context.Background(), f.token(t, map[string]any{"aud": []string{"grafana", "edgex-cli"}})); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifyRejectsCheapCasesWithoutNetwork(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	for name, tok := range map[string]string{
		"wrong audience": f.token(t, map[string]any{"aud": "grafana"}),
		"empty audience": f.token(t, map[string]any{"aud": ""}),
		"expired":        f.token(t, map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}),
		"no exp":         f.token(t, map[string]any{"exp": nil}),
		"too large":      f.token(t, map[string]any{"pad": strings.Repeat("x", MaxTokenBytes)}),
	} {
		t.Run(name, func(t *testing.T) {
			before := f.hits.Load()
			if _, err := v.Verify(context.Background(), tok); err == nil {
				t.Fatal("Verify() accepted the token")
			}
			if f.hits.Load() != before {
				t.Fatalf("%s caused %d network hits", name, f.hits.Load()-before)
			}
		})
	}
}

func TestVerifyRejectsNonRS256Header(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	es256 := b64(map[string]string{"alg": "ES256", "kid": "k1"}) + "." + b64(f.claims(nil)) + ".c2ln"
	if _, err := v.Verify(context.Background(), es256); err == nil {
		t.Fatal("Verify() accepted an ES256 header")
	}
}

func TestVerifyBoundsJWKSRefetchesOnForgedSignatures(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	if _, err := v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatal(err)
	}
	after := f.hits.Load() // discovery + first JWKS fetch
	forger, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := v.Verify(context.Background(), f.tokenWith(t, forger, "k1", nil)); err == nil {
			t.Fatal("forged token accepted")
		}
	}
	if extra := f.hits.Load() - after; extra > 1 {
		t.Fatalf("20 forged tokens caused %d JWKS refetches, want at most 1 per cooldown", extra)
	}
	// legitimate tokens keep working meanwhile
	if _, err := v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("legit token rejected during cooldown: %v", err)
	}
}

func TestVerifyFollowsKeyRolloverAfterCooldown(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	v.cooldown = 0
	if _, err := v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatal(err)
	}
	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.key, f.kid = newKey, "k2"
	before := f.hits.Load()
	if _, err := v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("token under the rolled key rejected: %v", err)
	}
	if f.hits.Load() != before+1 {
		t.Fatalf("rollover caused %d fetches, want 1", f.hits.Load()-before)
	}
}

func TestVerifyDiscoveryFailureIsFlaggedAndSharedAcrossCallers(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	tok := f.token(t, nil)
	f.srv.Close()
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { _, err := v.Verify(context.Background(), tok); errs <- err }()
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; !errors.Is(err, ErrDiscovery) {
			t.Fatalf("Verify() error = %v, want ErrDiscovery", err)
		}
	}
}

func TestVerifyWaiterStopsWhenItsContextEnds(t *testing.T) {
	f := newFakeIssuer(t)
	v := New(f.url, []string{"edgex-cli"})
	tok := f.token(t, nil)
	f.srv.Close()
	v.client = &http.Client{Transport: hangingTransport{}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := v.Verify(ctx, tok); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Verify() error = %v, want context deadline", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("waiter did not stop on its own context")
	}
}

type hangingTransport struct{}

func (hangingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	<-r.Context().Done()
	return nil, r.Context().Err()
}
