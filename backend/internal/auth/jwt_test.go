package auth_test

import (
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/auth"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	secret := []byte("testsecret32byteslong_padding_xx")
	token, err := auth.NewAccessToken(42, "alice", secret)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := auth.ParseAccessToken(token, secret)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != 42 {
		t.Errorf("UserID: got %d, want 42", claims.UserID)
	}
	if claims.Username != "alice" {
		t.Errorf("Username: got %s, want alice", claims.Username)
	}
	if time.Until(claims.ExpiresAt.Time) > auth.AccessTokenTTL {
		t.Error("expiry exceeds expected TTL")
	}
}

func TestAccessTokenWrongSecret(t *testing.T) {
	token, _ := auth.NewAccessToken(1, "bob", []byte("secret-a"))
	_, err := auth.ParseAccessToken(token, []byte("secret-b"))
	if err == nil {
		t.Fatal("expected error with wrong secret, got nil")
	}
}

func TestRefreshTokenUnique(t *testing.T) {
	raw1, hash1, _ := auth.NewRefreshToken()
	raw2, hash2, _ := auth.NewRefreshToken()
	if raw1 == raw2 {
		t.Error("refresh tokens should be unique")
	}
	if hash1 == hash2 {
		t.Error("refresh token hashes should be unique")
	}
}

func TestRefreshTokenHashConsistent(t *testing.T) {
	raw, hash, _ := auth.NewRefreshToken()
	got := auth.HashRefreshToken(raw)
	if got != hash {
		t.Errorf("hash mismatch: got %s, want %s", got, hash)
	}
}
