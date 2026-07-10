package larkoauth

import "testing"

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
