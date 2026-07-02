package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	AccessTokenTTL  = 15 * time.Minute
	MFAChallengeTTL = 5 * time.Minute
	RefreshTokenTTL = 7 * 24 * time.Hour
)

type Claims struct {
	UserID    uint64 `json:"uid"`
	Username  string `json:"sub"`
	SessionID uint64 `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

type MFAChallengeClaims struct {
	UserID   uint64 `json:"uid"`
	Username string `json:"sub"`
	Setup    bool   `json:"setup"`
	jwt.RegisteredClaims
}

func NewAccessToken(userID uint64, username string, sessionID uint64, secret []byte) (string, error) {
	claims := Claims{
		UserID:    userID,
		Username:  username,
		SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AccessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func ParseAccessToken(tokenStr string, secret []byte) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func NewMFAChallengeToken(userID uint64, username string, setup bool, secret []byte) (string, error) {
	tokenID, err := NewTokenID()
	if err != nil {
		return "", err
	}
	return NewMFAChallengeTokenWithID(userID, username, setup, tokenID, secret)
}

func NewMFAChallengeTokenWithID(userID uint64, username string, setup bool, tokenID string, secret []byte) (string, error) {
	claims := MFAChallengeClaims{
		UserID:   userID,
		Username: username,
		Setup:    setup,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(MFAChallengeTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   username,
			Audience:  []string{"mfa"},
			ID:        tokenID,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func ParseMFAChallengeToken(tokenStr string, secret []byte) (*MFAChallengeClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &MFAChallengeClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	}, jwt.WithAudience("mfa"))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*MFAChallengeClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func NewRefreshToken() (raw string, hash string, err error) {
	raw, err = NewTokenID()
	if err != nil {
		return "", "", err
	}
	hash = HashRefreshToken(raw)
	return raw, hash, nil
}

func NewTokenID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func HashRefreshToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
