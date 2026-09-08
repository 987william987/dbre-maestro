package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestParseAccessTokenRoundTrip(t *testing.T) {
	secret := []byte("s")
	tok, err := NewAccessToken(42, "admin", 7, secret)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ParseAccessToken(tok, secret)
	if err != nil || claims.UserID != 42 || claims.Username != "admin" || claims.SessionID != 7 {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}
}

func TestParseAccessTokenRejectsOtherTokenKindsSignedWithTheSameSecret(t *testing.T) {
	secret := []byte("s")
	mfa, err := NewMFAChallengeToken(42, "admin", false, secret)
	if err != nil {
		t.Fatal(err)
	}
	queryCtx, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": 42, "sub": "admin", "aud": "query-context",
		"exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}
	for name, tok := range map[string]string{"mfa challenge": mfa, "query context": queryCtx} {
		if claims, err := ParseAccessToken(tok, secret); err == nil {
			t.Fatalf("%s token accepted as access token: %+v", name, claims)
		}
	}
}
