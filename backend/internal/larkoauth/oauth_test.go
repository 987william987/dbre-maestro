package larkoauth

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/dbre-maestro/maestro/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestMergeIdentityKeepsBaseEnterpriseEmailWhenOverrideMissing(t *testing.T) {
	base := Identity{
		OpenID:          "ou_base",
		UnionID:         "on_base",
		Email:           "personal@example.com",
		EnterpriseEmail: "user@edgex.exchange",
	}
	override := Identity{
		OpenID: "ou_override",
		Email:  "personal-updated@example.com",
	}

	got := mergeIdentity(base, override)

	if got.OpenID != override.OpenID {
		t.Fatalf("OpenID = %q, want %q", got.OpenID, override.OpenID)
	}
	if got.Email != override.Email {
		t.Fatalf("Email = %q, want %q", got.Email, override.Email)
	}
	if got.EnterpriseEmail != base.EnterpriseEmail {
		t.Fatalf("EnterpriseEmail = %q, want %q", got.EnterpriseEmail, base.EnterpriseEmail)
	}
}

func TestMergeIdentityOverridesEnterpriseEmailWhenProvided(t *testing.T) {
	base := Identity{EnterpriseEmail: "old@edgex.exchange"}
	override := Identity{EnterpriseEmail: "new@edgex.exchange"}

	got := mergeIdentity(base, override)

	if got.EnterpriseEmail != override.EnterpriseEmail {
		t.Fatalf("EnterpriseEmail = %q, want %q", got.EnterpriseEmail, override.EnterpriseEmail)
	}
}

func TestAuthorizeURLIncludesScopes(t *testing.T) {
	got := AuthorizeURL(config.LarkOAuthConfig{
		Site:        "lark",
		AppID:       "cli_test",
		RedirectURL: "https://example.test/api/auth/lark/login/callback",
		Scopes:      []string{"directory:employee.base.enterprise_email:read", "contact:user.email:readonly"},
	}, "state-token")

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", got, err)
	}
	scope := parsed.Query().Get("scope")
	if !strings.Contains(scope, "directory:employee.base.enterprise_email:read") {
		t.Fatalf("scope = %q, want enterprise email scope", scope)
	}
	if !strings.Contains(scope, "contact:user.email:readonly") {
		t.Fatalf("scope = %q, want user email scope", scope)
	}
}

func TestExchangeCodeFetchesContactEnterpriseEmailWhenUserInfoDoesNotReturnIt(t *testing.T) {
	client := HTTPClient{Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.String() == "https://open.larksuite.com/open-apis/authen/v2/oauth/token":
			return jsonResponse(http.StatusOK, `{"code":0,"data":{"access_token":"user-token","open_id":"ou_test","union_id":"on_test"}}`), nil
		case req.URL.String() == "https://open.larksuite.com/open-apis/authen/v1/user_info":
			return jsonResponse(http.StatusOK, `{"code":0,"data":{"open_id":"ou_test","union_id":"on_test","name":"William"}}`), nil
		case req.URL.String() == "https://open.larksuite.com/open-apis/auth/v3/tenant_access_token/internal":
			return jsonResponse(http.StatusOK, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`), nil
		case strings.HasPrefix(req.URL.String(), "https://open.larksuite.com/open-apis/contact/v3/users/ou_test"):
			if got := req.URL.Query().Get("user_id_type"); got != "open_id" {
				t.Fatalf("user_id_type = %q, want open_id", got)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("Authorization = %q, want tenant token", got)
			}
			return jsonResponse(http.StatusOK, `{"code":0,"data":{"user":{"open_id":"ou_test","union_id":"on_test","name":"William","enterprise_email":"william@edgex.exchange"}}}`), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.String())
			return nil, nil
		}
	})}}

	identity, err := client.ExchangeCode(context.Background(), config.LarkOAuthConfig{
		Site:                   "lark",
		AppID:                  "cli_test",
		AppSecret:              "secret_test",
		RedirectURL:            "https://example.test/api/auth/lark/login/callback",
		RequireEnterpriseEmail: true,
	}, "oauth-code")
	if err != nil {
		t.Fatalf("ExchangeCode() error = %v", err)
	}
	if identity.EnterpriseEmail != "william@edgex.exchange" {
		t.Fatalf("EnterpriseEmail = %q, want contact enterprise email", identity.EnterpriseEmail)
	}
}

func TestExchangeCodeRequiresContactEnterpriseEmailWhenConfigured(t *testing.T) {
	client := HTTPClient{Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.String() == "https://open.larksuite.com/open-apis/authen/v2/oauth/token":
			return jsonResponse(http.StatusOK, `{"code":0,"data":{"access_token":"user-token","open_id":"ou_test","union_id":"on_test"}}`), nil
		case req.URL.String() == "https://open.larksuite.com/open-apis/authen/v1/user_info":
			return jsonResponse(http.StatusOK, `{"code":0,"data":{"open_id":"ou_test","union_id":"on_test","name":"William"}}`), nil
		case req.URL.String() == "https://open.larksuite.com/open-apis/auth/v3/tenant_access_token/internal":
			return jsonResponse(http.StatusOK, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`), nil
		case strings.HasPrefix(req.URL.String(), "https://open.larksuite.com/open-apis/contact/v3/users/ou_test"):
			return jsonResponse(http.StatusOK, `{"code":0,"data":{"user":{"open_id":"ou_test","union_id":"on_test","name":"William"}}}`), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.String())
			return nil, nil
		}
	})}}

	_, err := client.ExchangeCode(context.Background(), config.LarkOAuthConfig{
		Site:                   "lark",
		AppID:                  "cli_test",
		AppSecret:              "secret_test",
		RedirectURL:            "https://example.test/api/auth/lark/login/callback",
		RequireEnterpriseEmail: true,
	}, "oauth-code")
	if err == nil || !strings.Contains(err.Error(), "missing enterprise_email") {
		t.Fatalf("ExchangeCode() error = %v, want missing enterprise_email", err)
	}
}

func TestExchangeCodeIgnoresContactFailureWhenEnterpriseEmailNotRequired(t *testing.T) {
	client := HTTPClient{Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.String() == "https://open.larksuite.com/open-apis/authen/v2/oauth/token":
			return jsonResponse(http.StatusOK, `{"code":0,"data":{"access_token":"user-token","open_id":"ou_test","union_id":"on_test"}}`), nil
		case req.URL.String() == "https://open.larksuite.com/open-apis/authen/v1/user_info":
			return jsonResponse(http.StatusOK, `{"code":0,"data":{"open_id":"ou_test","union_id":"on_test","name":"William"}}`), nil
		case req.URL.String() == "https://open.larksuite.com/open-apis/auth/v3/tenant_access_token/internal":
			return jsonResponse(http.StatusForbidden, `{"code":99991663,"msg":"permission denied"}`), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.String())
			return nil, nil
		}
	})}}

	identity, err := client.ExchangeCode(context.Background(), config.LarkOAuthConfig{
		Site:                   "lark",
		AppID:                  "cli_test",
		AppSecret:              "secret_test",
		RedirectURL:            "https://example.test/api/auth/lark/login/callback",
		RequireEnterpriseEmail: false,
	}, "oauth-code")
	if err != nil {
		t.Fatalf("ExchangeCode() error = %v", err)
	}
	if identity.OpenID != "ou_test" {
		t.Fatalf("OpenID = %q, want ou_test", identity.OpenID)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
