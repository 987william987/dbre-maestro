// Package oidcbearer verifies tokens issued by the SSO IdP (Authentik) and
// presented as `Authorization: Bearer`, so CLI tools can call the API from a
// loopback OIDC login instead of a browser session. Signing keys come from the
// issuer's discovery document. The issuer string must be copied from that
// document verbatim (Authentik's ends with a slash).
package oidcbearer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
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

const (
	// MaxTokenBytes bounds the work spent on a token before any check; real
	// Authentik tokens are around 1-2 KiB. Callers that parse other token
	// kinds first should apply the same bound before parsing.
	MaxTokenBytes = 16 << 10
	// jwksCooldown caps JWKS refetches caused by tokens no cached key verifies.
	jwksCooldown = 30 * time.Second
)

type Identity struct {
	Subject           string
	Email             string
	EmailVerified     bool
	PreferredUsername string
	// Lark ids from Authentik's Lark source, when the provider's scope mapping
	// emits them (claims lark_open_id / lark_union_id). Empty otherwise.
	LarkOpenID  string
	LarkUnionID string
}

type Verifier interface {
	Verify(ctx context.Context, rawToken string) (Identity, error)
}

type HTTPVerifier struct {
	issuerURL string
	audiences []string
	client    *http.Client
	cooldown  time.Duration

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
	discover singleflight.Group
}

func New(issuerURL string, audiences []string) *HTTPVerifier {
	return &HTTPVerifier{
		issuerURL: issuerURL,
		audiences: audiences,
		client:    &http.Client{Timeout: 10 * time.Second, CheckRedirect: secureRedirectsOnly},
		cooldown:  jwksCooldown,
	}
}

// secureURL is https, or plain http on a loopback host (tests, local IdPs).
// Discovery and JWKS fetches over anything else would let an on-path attacker
// hand us signing keys.
func secureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	}
	return false
}

func secureRedirectsOnly(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("oidc bearer: too many redirects")
	}
	if !secureURL(req.URL.String()) {
		return errors.New("oidc bearer: redirect to a non-https URL refused")
	}
	return nil
}

func (v *HTTPVerifier) Verify(ctx context.Context, rawToken string) (Identity, error) {
	if len(rawToken) > MaxTokenBytes {
		return Identity{}, errors.New("oidc bearer: token too large")
	}
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
	verifier, err := v.idTokenVerifier(ctx)
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
		LarkOpenID        string `json:"lark_open_id"`
		LarkUnionID       string `json:"lark_union_id"`
	}
	if err := token.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("oidc bearer: claims: %w", err)
	}
	return Identity{
		Subject:           token.Subject,
		Email:             strings.TrimSpace(claims.Email),
		EmailVerified:     claims.EmailVerified,
		PreferredUsername: strings.TrimSpace(claims.PreferredUsername),
		LarkOpenID:        strings.TrimSpace(claims.LarkOpenID),
		LarkUnionID:       strings.TrimSpace(claims.LarkUnionID),
	}, nil
}

// idTokenVerifier runs discovery on first use, once for all concurrent callers,
// so an IdP that is unreachable at boot delays only bearer logins. A failed
// discovery is retried on the next call; a caller whose ctx ends stops waiting.
func (v *HTTPVerifier) idTokenVerifier(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	cached := v.verifier
	v.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	ch := v.discover.DoChan("discover", func() (any, error) {
		v.mu.Lock()
		done := v.verifier
		v.mu.Unlock()
		if done != nil {
			return done, nil
		}
		if !secureURL(v.issuerURL) {
			return nil, fmt.Errorf("%w: issuer must be an https URL", ErrDiscovery)
		}
		// Background, not a request context: the fetch is shared by every waiter.
		bg := oidc.ClientContext(context.Background(), v.client)
		provider, err := oidc.NewProvider(bg, v.issuerURL)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDiscovery, err)
		}
		var endpoints struct {
			JWKSURL string `json:"jwks_uri"`
		}
		if err := provider.Claims(&endpoints); err != nil || endpoints.JWKSURL == "" {
			return nil, fmt.Errorf("%w: discovery document has no jwks_uri", ErrDiscovery)
		}
		if !secureURL(endpoints.JWKSURL) {
			return nil, fmt.Errorf("%w: jwks_uri must be an https URL", ErrDiscovery)
		}
		keys := &cachedKeySet{jwksURL: endpoints.JWKSURL, client: v.client, cooldown: v.cooldown, now: time.Now}
		// Audiences are a list here, so they are checked in Verify.
		verifier := oidc.NewVerifier(v.issuerURL, keys, &oidc.Config{
			SkipClientIDCheck:    true,
			SupportedSigningAlgs: []string{oidc.RS256},
		})
		v.mu.Lock()
		v.verifier = verifier
		v.mu.Unlock()
		return verifier, nil
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*oidc.IDTokenVerifier), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
		if a == "" {
			continue
		}
		for _, b := range allowed {
			if a == b {
				return true
			}
		}
	}
	return false
}
