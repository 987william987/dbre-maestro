package larkoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/config"
)

const (
	feishuAuthorizeURL = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"
	feishuTokenURL     = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	feishuUserInfoURL  = "https://open.feishu.cn/open-apis/authen/v1/user_info"
	larkAuthorizeURL   = "https://accounts.larksuite.com/open-apis/authen/v1/authorize"
	larkTokenURL       = "https://open.larksuite.com/open-apis/authen/v2/oauth/token"
	larkUserInfoURL    = "https://open.larksuite.com/open-apis/authen/v1/user_info"
)

type Identity struct {
	OpenID          string
	UnionID         string
	Email           string
	EnterpriseEmail string
	DisplayName     string
	AvatarURL       string
}

type Client interface {
	ExchangeCode(ctx context.Context, cfg config.LarkOAuthConfig, code string) (Identity, error)
}

type HTTPClient struct {
	Client *http.Client
}

func NewHTTPClient() HTTPClient {
	return HTTPClient{Client: &http.Client{Timeout: 10 * time.Second}}
}

func AuthorizeURL(cfg config.LarkOAuthConfig, state string) string {
	values := url.Values{}
	values.Set("app_id", cfg.AppID)
	values.Set("redirect_uri", cfg.RedirectURL)
	values.Set("state", state)
	if len(cfg.Scopes) > 0 {
		values.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	return authorizeURL(cfg) + "?" + values.Encode()
}

func (c HTTPClient) ExchangeCode(ctx context.Context, cfg config.LarkOAuthConfig, code string) (Identity, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     cfg.AppID,
		"client_secret": cfg.AppSecret,
		"code":          strings.TrimSpace(code),
		"redirect_uri":  cfg.RedirectURL,
	})
	if err != nil {
		return Identity{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL(cfg), bytes.NewReader(body))
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Identity{}, fmt.Errorf("lark oauth token status %d", resp.StatusCode)
	}

	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AccessToken     string `json:"access_token"`
			OpenID          string `json:"open_id"`
			UnionID         string `json:"union_id"`
			Email           string `json:"email"`
			EnterpriseEmail string `json:"enterprise_email"`
		} `json:"data"`
		AccessToken     string `json:"access_token"`
		OpenID          string `json:"open_id"`
		UnionID         string `json:"union_id"`
		Email           string `json:"email"`
		EnterpriseEmail string `json:"enterprise_email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Identity{}, err
	}
	if payload.Code != 0 {
		return Identity{}, fmt.Errorf("lark oauth token failed: %s", payload.Msg)
	}

	identity := Identity{
		OpenID:          payload.Data.OpenID,
		UnionID:         payload.Data.UnionID,
		Email:           payload.Data.Email,
		EnterpriseEmail: payload.Data.EnterpriseEmail,
	}
	if identity.OpenID == "" {
		identity.OpenID = payload.OpenID
	}
	if identity.UnionID == "" {
		identity.UnionID = payload.UnionID
	}
	if identity.Email == "" {
		identity.Email = payload.Email
	}
	if identity.EnterpriseEmail == "" {
		identity.EnterpriseEmail = payload.EnterpriseEmail
	}

	accessToken := strings.TrimSpace(payload.Data.AccessToken)
	if accessToken == "" {
		accessToken = strings.TrimSpace(payload.AccessToken)
	}
	if accessToken != "" {
		userInfo, err := c.fetchUserInfo(ctx, cfg, accessToken)
		if err == nil {
			identity = mergeIdentity(identity, userInfo)
		} else if strings.TrimSpace(identity.OpenID) == "" {
			return Identity{}, err
		}
	}
	if strings.TrimSpace(identity.OpenID) == "" {
		return Identity{}, fmt.Errorf("lark oauth response missing open_id")
	}
	return identity, nil
}

func (c HTTPClient) fetchUserInfo(ctx context.Context, cfg config.LarkOAuthConfig, accessToken string) (Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL(cfg), nil)
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
		return Identity{}, fmt.Errorf("lark user info status %d", resp.StatusCode)
	}

	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OpenID          string `json:"open_id"`
			UnionID         string `json:"union_id"`
			Name            string `json:"name"`
			Email           string `json:"email"`
			EnterpriseEmail string `json:"enterprise_email"`
			AvatarURL       string `json:"avatar_url"`
		} `json:"data"`
		OpenID          string `json:"open_id"`
		UnionID         string `json:"union_id"`
		Name            string `json:"name"`
		Email           string `json:"email"`
		EnterpriseEmail string `json:"enterprise_email"`
		AvatarURL       string `json:"avatar_url"`
		AvatarMiddle    string `json:"avatar_middle"`
		AvatarThumb     string `json:"avatar_thumb"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Identity{}, err
	}
	if payload.Code != 0 {
		return Identity{}, fmt.Errorf("lark user info failed: %s", payload.Msg)
	}
	identity := Identity{
		OpenID:          firstNonEmpty(payload.Data.OpenID, payload.OpenID),
		UnionID:         firstNonEmpty(payload.Data.UnionID, payload.UnionID),
		Email:           firstNonEmpty(payload.Data.Email, payload.Email),
		EnterpriseEmail: firstNonEmpty(payload.Data.EnterpriseEmail, payload.EnterpriseEmail),
		DisplayName:     firstNonEmpty(payload.Data.Name, payload.Name),
		AvatarURL:       firstNonEmpty(payload.Data.AvatarURL, payload.AvatarURL, payload.AvatarMiddle, payload.AvatarThumb),
	}
	if strings.TrimSpace(identity.OpenID) == "" {
		return Identity{}, fmt.Errorf("lark user info response missing open_id")
	}
	return identity, nil
}

func mergeIdentity(base Identity, override Identity) Identity {
	if strings.TrimSpace(override.OpenID) != "" {
		base.OpenID = strings.TrimSpace(override.OpenID)
	}
	if strings.TrimSpace(override.UnionID) != "" {
		base.UnionID = strings.TrimSpace(override.UnionID)
	}
	if strings.TrimSpace(override.Email) != "" {
		base.Email = strings.TrimSpace(override.Email)
	}
	if strings.TrimSpace(override.EnterpriseEmail) != "" {
		base.EnterpriseEmail = strings.TrimSpace(override.EnterpriseEmail)
	}
	if strings.TrimSpace(override.DisplayName) != "" {
		base.DisplayName = strings.TrimSpace(override.DisplayName)
	}
	if strings.TrimSpace(override.AvatarURL) != "" {
		base.AvatarURL = strings.TrimSpace(override.AvatarURL)
	}
	return base
}

func authorizeURL(cfg config.LarkOAuthConfig) string {
	if cfg.Site == "feishu" {
		return feishuAuthorizeURL
	}
	return larkAuthorizeURL
}

func tokenURL(cfg config.LarkOAuthConfig) string {
	if cfg.Site == "feishu" {
		return feishuTokenURL
	}
	return larkTokenURL
}

func userInfoURL(cfg config.LarkOAuthConfig) string {
	if cfg.Site == "feishu" {
		return feishuUserInfoURL
	}
	return larkUserInfoURL
}

func (c HTTPClient) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
