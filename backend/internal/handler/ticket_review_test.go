package handler

import "testing"

func TestSanitizeMySQLShadowValidationError(t *testing.T) {
	t.Run("sanitizes shadow database privilege failure", func(t *testing.T) {
		got := sanitizeMySQLShadowValidationError(assertErr("create shadow database failed: Error 1044 (42000): Access denied for user 'maestro_app'@'%' to database 'shadow_demo'"))
		want := "shadow validation is not available because the platform validation database privilege is not configured"
		if got != want {
			t.Fatalf("expected sanitized message %q, got %q", want, got)
		}
	})

	t.Run("keeps business validation errors readable", func(t *testing.T) {
		got := sanitizeMySQLShadowValidationError(assertErr("database \"foo\" already exists"))
		if got != "database \"foo\" already exists" {
			t.Fatalf("unexpected sanitized message: %q", got)
		}
	})
}

func assertErr(message string) error {
	return testError(message)
}

type testError string

func (e testError) Error() string {
	return string(e)
}
