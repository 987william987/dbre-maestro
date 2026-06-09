package config

import (
	"encoding/base64"
	"errors"
	"os"
)

type Config struct {
	Port            string
	DBDSN           string
	MigrationDSN    string
	JWTSecret       []byte
	EncryptionKey   []byte
	LarkWebhookURL  string // optional; empty = Lark notifications disabled
}

func Load() (*Config, error) {
	c := &Config{
		Port:         getEnv("PORT", "8080"),
		DBDSN:        os.Getenv("DB_DSN"),
		MigrationDSN: os.Getenv("MIGRATION_DSN"),
	}

	if c.DBDSN == "" {
		return nil, errors.New("DB_DSN is required")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, errors.New("JWT_SECRET is required")
	}
	c.JWTSecret = []byte(jwtSecret)

	encKey := os.Getenv("DBRE_ENCRYPTION_KEY")
	if encKey == "" {
		return nil, errors.New("DBRE_ENCRYPTION_KEY is required")
	}
	key, err := base64.StdEncoding.DecodeString(encKey)
	if err != nil {
		return nil, errors.New("DBRE_ENCRYPTION_KEY must be base64-encoded 32 bytes")
	}
	if len(key) != 32 {
		return nil, errors.New("DBRE_ENCRYPTION_KEY must decode to exactly 32 bytes")
	}
	c.EncryptionKey = key

	if c.MigrationDSN == "" {
		c.MigrationDSN = c.DBDSN
	}

	c.LarkWebhookURL = os.Getenv("LARK_WEBHOOK_URL")

	return c, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
