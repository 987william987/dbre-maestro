package larkoauth

import (
	"net/url"
	"strings"
	"testing"

	"github.com/dbre-maestro/maestro/internal/config"
)

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
