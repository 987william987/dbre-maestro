package oidcsso

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/config"
)

type Identity struct {
	Subject                   string
	Email                     string
	EmailVerified             bool
	EmailVerifiedClaimPresent bool
	Name                      string
	LarkOpenID                string
	LarkUnionID               string
	LarkTenantKey             string
	RawClaims                 map[string]any
}

type Client interface {
	AuthorizeURL(ctx context.Context, cfg config.OIDCSSOConfig, state string) (string, error)
	ExchangeCode(ctx context.Context, cfg config.OIDCSSOConfig, code string) (Identity, error)
}

type HTTPClient struct {
	Client *http.Client
}

type discoveryDocument struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

func NewHTTPClient() HTTPClient {
	return HTTPClient{Client: &http.Client{Timeout: 10 * time.Second}}
}

func (c HTTPClient) AuthorizeURL(ctx context.Context, cfg config.OIDCSSOConfig, state string) (string, error) {
	doc, err := c.discover(ctx, cfg)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", cfg.ClientID)
	values.Set("redirect_uri", cfg.RedirectURL)
	values.Set("state", state)
	values.Set("scope", strings.Join(cfg.ScopesOrDefault(), " "))
	return doc.AuthorizationEndpoint + "?" + values.Encode(), nil
}

func (c HTTPClient) ExchangeCode(ctx context.Context, cfg config.OIDCSSOConfig, code string) (Identity, error) {
	doc, err := c.discover(ctx, cfg)
	if err != nil {
		return Identity{}, err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", cfg.RedirectURL)
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, doc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Identity{}, fmt.Errorf("oidc token status %d", resp.StatusCode)
	}

	var tokenPayload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenPayload); err != nil {
		return Identity{}, err
	}
	if strings.TrimSpace(tokenPayload.AccessToken) == "" {
		return Identity{}, fmt.Errorf("oidc token response missing access_token")
	}
	return c.fetchUserinfo(ctx, doc.UserinfoEndpoint, tokenPayload.AccessToken)
}

func (c HTTPClient) discover(ctx context.Context, cfg config.OIDCSSOConfig) (discoveryDocument, error) {
	issuer := strings.TrimRight(strings.TrimSpace(cfg.IssuerURL), "/")
	if issuer == "" {
		return discoveryDocument{}, fmt.Errorf("oidc issuer url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return discoveryDocument{}, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return discoveryDocument{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return discoveryDocument{}, fmt.Errorf("oidc discovery status %d", resp.StatusCode)
	}
	var doc discoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return discoveryDocument{}, err
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" || doc.UserinfoEndpoint == "" {
		return discoveryDocument{}, fmt.Errorf("oidc discovery document missing required endpoints")
	}
	return doc, nil
}

func (c HTTPClient) fetchUserinfo(ctx context.Context, endpoint string, accessToken string) (Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Identity{}, fmt.Errorf("oidc userinfo status %d", resp.StatusCode)
	}
	var claims map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return Identity{}, err
	}
	emailVerified, emailVerifiedPresent := claimBool(claims, "email_verified")
	identity := Identity{
		Subject:                   claimString(claims, "sub"),
		Email:                     claimString(claims, "email"),
		EmailVerified:             emailVerified,
		EmailVerifiedClaimPresent: emailVerifiedPresent,
		Name:                      claimString(claims, "name"),
		LarkOpenID:                claimString(claims, "lark_open_id"),
		LarkUnionID:               claimString(claims, "lark_union_id"),
		LarkTenantKey:             claimString(claims, "lark_tenant_key"),
		RawClaims:                 claims,
	}
	if identity.Subject == "" {
		return Identity{}, fmt.Errorf("oidc userinfo missing sub")
	}
	if identity.Email == "" {
		return Identity{}, fmt.Errorf("oidc userinfo missing email")
	}
	return identity, nil
}

func (c HTTPClient) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func claimString(claims map[string]any, key string) string {
	if value, ok := claims[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func claimBool(claims map[string]any, key string) (bool, bool) {
	value, ok := claims[key]
	if !ok {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		if normalized == "true" {
			return true, true
		}
		if normalized == "false" {
			return false, true
		}
	}
	return false, true
}
