package handler

import "testing"

func TestResolveLarkSecretStateAllowsSavingWhenCurrentSecretExists(t *testing.T) {
	configured, required := resolveLarkSecretState("cli_existing", "", false, true)

	if required {
		t.Fatal("secret should not be required when an existing configured secret is present")
	}
	if !configured {
		t.Fatal("configured should be true when the current settings already have a secret")
	}
}

func TestResolveLarkSecretStateRequiresSecretForFirstTimeConfiguration(t *testing.T) {
	configured, required := resolveLarkSecretState("cli_new", "", false, false)

	if !required {
		t.Fatal("secret should be required when configuring Lark for the first time")
	}
	if configured {
		t.Fatal("configured should be false without a request or current secret")
	}
}

func TestResolveLarkSecretStateMarksConfiguredWhenSecretProvided(t *testing.T) {
	configured, required := resolveLarkSecretState("cli_new", "secret", false, false)

	if required {
		t.Fatal("secret should not be required when the request provides one")
	}
	if !configured {
		t.Fatal("configured should be true when the request provides a secret")
	}
}

func TestResolveLarkSecretStateDoesNotRequireSecretWhenAppIDIsEmpty(t *testing.T) {
	configured, required := resolveLarkSecretState("", "", false, true)

	if required {
		t.Fatal("secret should not be required when Lark App ID is empty")
	}
	if !configured {
		t.Fatal("configured should preserve the current configured state")
	}
}
