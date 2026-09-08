package oidcbearer

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestKeySetFetchesOncePerCooldownUnderConcurrency(t *testing.T) {
	f := newFakeIssuer(t)
	forger, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	k := &cachedKeySet{jwksURL: f.jwksURL, client: &http.Client{Timeout: 5 * time.Second}, cooldown: time.Minute, now: time.Now}
	forged := f.tokenWith(t, forger, "k1", nil)

	const callers = 16
	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer done.Done()
			start.Wait()
			if _, err := k.VerifySignature(context.Background(), forged); err == nil {
				t.Error("forged token accepted")
			}
		}()
	}
	start.Done()
	done.Wait()
	if f.hits.Load() != 1 {
		t.Fatalf("%d concurrent misses caused %d JWKS fetches, want 1", callers, f.hits.Load())
	}
	// and a legitimate token verifies against what that one fetch brought in
	if _, err := k.VerifySignature(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("legit token rejected: %v", err)
	}
}

func TestKeySetIgnoresKeysNotMeantForRS256Signing(t *testing.T) {
	for name, meta := range map[string][2]string{
		"use=enc":   {"enc", "RS256"},
		"alg=RS512": {"sig", "RS512"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeIssuer(t)
			f.use, f.alg = meta[0], meta[1]
			v := New(f.url, []string{"edgex-cli"})
			if _, err := v.Verify(context.Background(), f.token(t, nil)); err == nil {
				t.Fatalf("token verified with a JWK marked %s", name)
			}
		})
	}
	f := newFakeIssuer(t)
	f.use, f.alg = "", "" // metadata absent: allowed
	v := New(f.url, []string{"edgex-cli"})
	if _, err := v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("token rejected with metadata-less JWK: %v", err)
	}
}

func TestVerifyRefusesInsecureJWKSURLAndRedirects(t *testing.T) {
	f := newFakeIssuer(t)
	f.jwksURL = "http://example.invalid/jwks" // non-loopback http advertised by discovery
	v := New(f.url, []string{"edgex-cli"})
	if _, err := v.Verify(context.Background(), f.token(t, nil)); err == nil {
		t.Fatal("accepted a discovery document with an http jwks_uri")
	}

	f2 := newFakeIssuer(t)
	f2.jwksURL = f2.srv.URL + "/jwks-elsewhere" // loopback, but redirects to http://example.invalid
	v2 := New(f2.url, []string{"edgex-cli"})
	if _, err := v2.Verify(context.Background(), f2.token(t, nil)); err == nil {
		t.Fatal("followed a JWKS redirect to a non-https host")
	}
}

func TestSecureURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://authentik.example.com/application/o/x/": true,
		"http://127.0.0.1:8080/jwks":                     true,
		"http://localhost/jwks":                          true,
		"http://[::1]:9/jwks":                            true,
		"http://example.com/jwks":                        false,
		"ftp://example.com/jwks":                         false,
		"https://":                                       false,
		"not a url":                                      false,
	} {
		if got := secureURL(raw); got != want {
			t.Errorf("secureURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
