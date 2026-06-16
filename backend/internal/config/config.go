package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/pool"
)

type Config struct {
	Port           string
	DBDSN          string
	MigrationDSN   string
	JWTSecret      []byte
	EncryptionKey  []byte
	AppBaseURL     string
	LarkWebhookURL string // optional; empty = Lark notifications disabled
	PoolProfiles   map[pool.Profile]pool.ProfileConfig
}

func Load() (*Config, error) {
	c := &Config{
		Port:         getEnv("PORT", "8080"),
		DBDSN:        os.Getenv("DB_DSN"),
		MigrationDSN: os.Getenv("MIGRATION_DSN"),
		AppBaseURL:   strings.TrimRight(os.Getenv("APP_BASE_URL"), "/"),
		PoolProfiles: map[pool.Profile]pool.ProfileConfig{
			pool.ProfileQuery:         pool.DefaultConfigForProfile(pool.ProfileQuery),
			pool.ProfileExec:          pool.DefaultConfigForProfile(pool.ProfileExec),
			pool.ProfileMetadata:      pool.DefaultConfigForProfile(pool.ProfileMetadata),
			pool.ProfileScopedPGQuery: pool.DefaultConfigForProfile(pool.ProfileScopedPGQuery),
		},
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

	if err := loadPoolProfileConfig(c); err != nil {
		return nil, err
	}

	return c, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadPoolProfileConfig(c *Config) error {
	profiles := []pool.Profile{
		pool.ProfileQuery,
		pool.ProfileExec,
		pool.ProfileMetadata,
		pool.ProfileScopedPGQuery,
	}

	for _, profile := range profiles {
		config := c.PoolProfiles[profile]

		maxOpen, err := getEnvInt(poolEnvKey(profile, "MAX_OPEN"), config.MaxOpenConns)
		if err != nil {
			return err
		}
		maxIdle, err := getEnvInt(poolEnvKey(profile, "MAX_IDLE"), config.MaxIdleConns)
		if err != nil {
			return err
		}
		maxLifetime, err := getEnvDuration(poolEnvKey(profile, "CONN_MAX_LIFETIME"), config.ConnMaxLifetime)
		if err != nil {
			return err
		}
		maxIdleTime, err := getEnvDuration(poolEnvKey(profile, "CONN_MAX_IDLE_TIME"), config.ConnMaxIdleTime)
		if err != nil {
			return err
		}

		if maxOpen <= 0 {
			return fmt.Errorf("%s must be greater than 0", poolEnvKey(profile, "MAX_OPEN"))
		}
		if maxIdle <= 0 {
			return fmt.Errorf("%s must be greater than 0", poolEnvKey(profile, "MAX_IDLE"))
		}
		if maxIdle > maxOpen {
			return fmt.Errorf("%s cannot be greater than %s", poolEnvKey(profile, "MAX_IDLE"), poolEnvKey(profile, "MAX_OPEN"))
		}
		if maxLifetime <= 0 {
			return fmt.Errorf("%s must be greater than 0", poolEnvKey(profile, "CONN_MAX_LIFETIME"))
		}
		if maxIdleTime <= 0 {
			return fmt.Errorf("%s must be greater than 0", poolEnvKey(profile, "CONN_MAX_IDLE_TIME"))
		}

		c.PoolProfiles[profile] = pool.ProfileConfig{
			MaxOpenConns:    maxOpen,
			MaxIdleConns:    maxIdle,
			ConnMaxLifetime: maxLifetime,
			ConnMaxIdleTime: maxIdleTime,
		}
	}

	return nil
}

func poolEnvKey(profile pool.Profile, suffix string) string {
	return "DB_POOL_" + strings.ToUpper(string(profile)) + "_" + suffix
}

func getEnvInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}
	return parsed, nil
}
