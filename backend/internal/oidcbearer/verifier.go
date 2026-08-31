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
)

// ErrForeignIssuer marks a token that names another issuer. It is decided from
// the unverified payload, so such tokens cost no network round-trip.
var ErrForeignIssuer = errors.New("oidc bearer: token issuer is not ours")

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
}

func New(issuerURL string, audiences []string) *HTTPVerifier {
	return &HTTPVerifier{
		issuerURL: issuerURL,
		audiences: audiences,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (v *HTTPVerifier) Verify(ctx context.Context, rawToken string) (Identity, error) {
	issuer, err := peekIssuer(rawToken)
	if err != nil {
		return Identity{}, err
	}
	if issuer != v.issuerURL {
		return Identity{}, ErrForeignIssuer
	}
	verifier, err := v.idTokenVerifier()
	if err != nil {
		return Identity{}, err
	}
	token, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return Identity{}, fmt.Errorf("oidc bearer: %w", err)
	}
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

// idTokenVerifier runs discovery on first use, so an IdP that is unreachable at
// boot delays only bearer logins. A failed discovery is retried on the next call.
func (v *HTTPVerifier) idTokenVerifier() (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return v.verifier, nil
	}
	// Background, not a request context: the key set outlives any request and
	// refetches the JWKS on an unknown kid.
	ctx := oidc.ClientContext(context.Background(), v.client)
	provider, err := oidc.NewProvider(ctx, v.issuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc bearer: discovery: %w", err)
	}
	// Audiences are a list here, so they are checked in Verify.
	v.verifier = provider.VerifierContext(ctx, &oidc.Config{SkipClientIDCheck: true})
	return v.verifier, nil
}

func peekIssuer(rawToken string) (string, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return "", errors.New("oidc bearer: not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("oidc bearer: decode payload: %w", err)
	}
	var claims struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("oidc bearer: decode claims: %w", err)
	}
	return claims.Issuer, nil
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
