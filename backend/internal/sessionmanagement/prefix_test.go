package sessionmanagement

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidatePrefixNormalizesCaseWhitespaceAndEnforcesEffectiveLength(t *testing.T) {
	got, err := ValidatePrefix("  SELECT   * FROM Orders WHERE status =  ")
	if err != nil || got != "select * from orders where status =" {
		t.Fatalf("ValidatePrefix() = (%q, %v)", got, err)
	}
	if _, err := ValidatePrefix("select 1"); err == nil {
		t.Fatal("short prefix must be rejected to prevent broad cancellation")
	}
}

func TestMatchPrefixSessionsAppliesAllSafetyBoundaries(t *testing.T) {
	base := Session{ID: "1", Database: "orders", DurationSeconds: 12, Command: "Query", Query: "SELECT *   FROM orders WHERE status = 'open'"}
	items := []Session{base, withSession(base, "2", func(s *Session) { s.Database = "other" }), withSession(base, "3", func(s *Session) { s.DurationSeconds = 2 }), withSession(base, "4", func(s *Session) { s.Protected = true }), withSession(base, "5", func(s *Session) { s.Command = "Sleep" }), withSession(base, "6", func(s *Session) { s.Query = "UPDATE orders SET status = 'done'" })}
	matches := MatchPrefixSessions(items, "orders", NormalizePrefix("select * from orders"), 10, "mysql")
	if len(matches) != 1 || matches[0].ID != "1" {
		t.Fatalf("matches = %#v, want only eligible session 1", matches)
	}
}

func TestSanitizeSQLShapeRemovesLiteralsAndComments(t *testing.T) {
	raw := "SELECT * FROM orders WHERE email = 'will@example.com' AND total > 123.45 /* ticket 99 */ AND note = $tag$secret$tag$"
	shape := SanitizeSQLShape(raw)
	if strings.Contains(shape, "will@example.com") || strings.Contains(shape, "123.45") || strings.Contains(shape, "secret") || strings.Contains(shape, "ticket") {
		t.Fatalf("SanitizeSQLShape() leaked a literal: %q", shape)
	}
	if shape != "SELECT * FROM orders WHERE email = ? AND total > ? AND note = ?" {
		t.Fatalf("SanitizeSQLShape() = %q", shape)
	}
}

func TestSanitizeSQLShapePreservesNumericOperators(t *testing.T) {
	shape := SanitizeSQLShape("SELECT 123-45, 1e-3, 0xFF")
	if shape != "SELECT ?-?, ?, ?" {
		t.Fatalf("SanitizeSQLShape() = %q", shape)
	}
}

func TestPreviewStoreIsBoundExpiringAndSingleUse(t *testing.T) {
	store := NewPreviewStore()
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	token, preview, err := store.Create(PrefixPreview{ActorID: 7, ConnectionID: 9, TargetKey: "aws|node"})
	if err != nil || preview.ExpiresAt != now.Add(PreviewTTL) {
		t.Fatalf("Create() = (%q, %#v, %v)", token, preview, err)
	}
	if _, err := store.Consume(token, 8, 9, "aws|node"); !errors.Is(err, ErrPreviewMismatch) {
		t.Fatalf("cross-actor Consume() error = %v", err)
	}
	if _, err := store.Consume(token, 7, 9, "aws|node"); err != nil {
		t.Fatalf("first valid Consume() error = %v", err)
	}
	if _, err := store.Consume(token, 7, 9, "aws|node"); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatalf("replayed Consume() error = %v", err)
	}

	token, _, _ = store.Create(PrefixPreview{ActorID: 7, ConnectionID: 9, TargetKey: "aws|node"})
	now = now.Add(PreviewTTL)
	if _, err := store.Consume(token, 7, 9, "aws|node"); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatalf("expired Consume() error = %v", err)
	}
}

func TestPreviewStoreDoesNotRetainRawSQL(t *testing.T) {
	store := NewPreviewStore()
	token, _, err := store.Create(PrefixPreview{ActorID: 7, ConnectionID: 9, TargetKey: "manual|db|3306", Matches: []Session{{ID: "42", Query: "SELECT secret FROM customers", Command: "Query", QueryHash: "hash"}}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.Consume(token, 7, 9, "manual|db|3306")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Matches[0].Query != "" || preview.Matches[0].Command != "" || preview.Matches[0].QueryHash != "hash" {
		t.Fatalf("stored preview leaked SQL or lost identity hash: %#v", preview.Matches[0])
	}
}

func withSession(base Session, id string, mutate func(*Session)) Session {
	base.ID = id
	mutate(&base)
	return base
}
