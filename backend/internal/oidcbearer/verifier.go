// Package oidcbearer verifies tokens issued by the SSO IdP (Authentik) and
// presented as `Authorization: Bearer`, so CLI tools can call the API from a
// loopback OIDC login instead of a browser session. Signing keys come from the
// issuer's discovery document and are cached by go-oidc. The issuer string must
// be copied from that document verbatim (Authentik's ends with a slash).
package oidcbearer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/sync/singleflight"
)

var (
	// ErrForeignIssuer marks a token that names another issuer. It is decided
	// from the unverified payload, so such tokens cost no network round-trip.
	ErrForeignIssuer = errors.New("oidc bearer: token issuer is not ours")
	// ErrDiscovery wraps a failed OIDC discovery (IdP unreachable, issuer typo).
	ErrDiscovery = errors.New("oidc bearer: discovery failed")
)

type Identity struct {
	Subject           string
	Email             string
	EmailVerified     bool
	PreferredUsername string
}

type Verifier interface {
	Verify(ctx context.Context, rawToken string) (Identity, error)
}

type HTTPVerifier struct {
	issuerURL string
	audiences []string
	client    *http.Client

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
	discover singleflight.Group
}

func New(issuerURL string, audiences []string) *HTTPVerifier {
	return &HTTPVerifier{
		issuerURL: issuerURL,
		audiences: audiences,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (v *HTTPVerifier) Verify(ctx context.Context, rawToken string) (Identity, error) {
	// Cheap gate on the unverified payload: wrong issuer, wrong audience or
	// expired tokens never reach the signature check, which may fetch keys.
	unverified, err := peek(rawToken)
	if err != nil {
		return Identity{}, err
	}
	if unverified.Issuer != v.issuerURL {
		return Identity{}, ErrForeignIssuer
	}
	if unverified.Expiry == 0 || time.Unix(unverified.Expiry, 0).Before(time.Now()) {
		return Identity{}, errors.New("oidc bearer: token expired or has no exp")
	}
	if !audienceAllowed(unverified.Audience, v.audiences) {
		return Identity{}, fmt.Errorf("oidc bearer: audience %v not allowed", unverified.Audience)
	}
	verifier, err := v.idTokenVerifier()
	if err != nil {
		return Identity{}, err
	}
	token, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return Identity{}, fmt.Errorf("oidc bearer: %w", err)
	}
	// Same check again on the verified claims; the peek above was unsigned input.
	if !audienceAllowed(token.Audience, v.audiences) {
		return Identity{}, fmt.Errorf("oidc bearer: audience %v not allowed", token.Audience)
	}
	var claims struct {
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := token.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("oidc bearer: claims: %w", err)
	}
	return Identity{
		Subject:           token.Subject,
		Email:             strings.TrimSpace(claims.Email),
		EmailVerified:     claims.EmailVerified,
		PreferredUsername: strings.TrimSpace(claims.PreferredUsername),
	}, nil
}

// idTokenVerifier runs discovery on first use, once for all concurrent callers,
// so an IdP that is unreachable at boot delays only bearer logins. A failed
// discovery is retried on the next call.
func (v *HTTPVerifier) idTokenVerifier() (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	cached := v.verifier
	v.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	result, err, _ := v.discover.Do("discover", func() (any, error) {
		// Background, not a request context: the key set outlives any request and
		// refetches the JWKS on an unknown kid.
		ctx := oidc.ClientContext(context.Background(), v.client)
		provider, err := oidc.NewProvider(ctx, v.issuerURL)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDiscovery, err)
		}
		// Audiences are a list here, so they are checked in Verify.
		verifier := provider.VerifierContext(ctx, &oidc.Config{SkipClientIDCheck: true})
		v.mu.Lock()
		v.verifier = verifier
		v.mu.Unlock()
		return verifier, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*oidc.IDTokenVerifier), nil
}

// audience accepts the JWT `aud` claim as a string or an array of strings.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		*a = audience{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	*a = audience(list)
	return nil
}

type unverifiedClaims struct {
	Issuer   string   `json:"iss"`
	Audience audience `json:"aud"`
	Expiry   int64    `json:"exp"`
}

func peek(rawToken string) (unverifiedClaims, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return unverifiedClaims{}, errors.New("oidc bearer: not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return unverifiedClaims{}, fmt.Errorf("oidc bearer: decode payload: %w", err)
	}
	var claims unverifiedClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return unverifiedClaims{}, fmt.Errorf("oidc bearer: decode claims: %w", err)
	}
	return claims, nil
}

func audienceAllowed(got, allowed []string) bool {
	for _, a := range got {
		for _, b := range allowed {
			if a == b {
				return true
			}
		}
	}
	return false
}
