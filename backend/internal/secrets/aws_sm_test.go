package secrets

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/dbre-maestro/maestro/internal/config"
	"github.com/dbre-maestro/maestro/internal/pool"
)

type fakeSecretGetter struct {
	secret string
	err    error
}

func (getter fakeSecretGetter) GetSecretValue(context.Context, string) (string, error) {
	return getter.secret, getter.err
}

func awsSMConfig() *config.Config {
	return &config.Config{
		AWSSecretsManagerEnabled:  true,
		AWSSecretsManagerRegion:   "ap-northeast-1",
		AWSSecretsManagerSecretID: "/staging/maestro",
		PoolProfiles: map[pool.Profile]pool.ProfileConfig{
			pool.ProfileQuery: pool.DefaultConfigForProfile(pool.ProfileQuery),
		},
	}
}

func TestResolveApplicationSecretsAppliesRequiredValues(t *testing.T) {
	cfg := awsSMConfig()
	secret := `{
		"DB_DSN": "maestro_app:secret@tcp(db.example.com:3306)/maestro?parseTime=true&charset=utf8mb4&loc=UTC",
		"MIGRATION_DSN": "root:secret@tcp(db.example.com:3306)/maestro?parseTime=true&charset=utf8mb4&loc=UTC",
		"DBRE_ENCRYPTION_KEY": "` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `",
		"JWT_SECRET": "jwt-secret"
	}`

	if err := ResolveApplicationSecrets(context.Background(), cfg, fakeSecretGetter{secret: secret}); err != nil {
		t.Fatalf("ResolveApplicationSecrets() error = %v", err)
	}

	if cfg.DBDSN == "" {
		t.Fatal("DBDSN was not applied")
	}
	if !strings.HasPrefix(cfg.MigrationDSN, "root:secret@tcp") {
		t.Fatalf("MigrationDSN = %q, want migration dsn from secret", cfg.MigrationDSN)
	}
	if string(cfg.JWTSecret) != "jwt-secret" {
		t.Fatalf("JWTSecret = %q, want jwt-secret", string(cfg.JWTSecret))
	}
	if len(cfg.EncryptionKey) != 32 {
		t.Fatalf("EncryptionKey length = %d, want 32", len(cfg.EncryptionKey))
	}
}

func TestResolveApplicationSecretsFallsBackMigrationDSNToDBDSN(t *testing.T) {
	cfg := awsSMConfig()
	secret := `{
		"DB_DSN": "maestro_app:secret@tcp(db.example.com:3306)/maestro?parseTime=true&charset=utf8mb4&loc=UTC",
		"DBRE_ENCRYPTION_KEY": "` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `",
		"JWT_SECRET": "jwt-secret"
	}`

	if err := ResolveApplicationSecrets(context.Background(), cfg, fakeSecretGetter{secret: secret}); err != nil {
		t.Fatalf("ResolveApplicationSecrets() error = %v", err)
	}
	if cfg.MigrationDSN != cfg.DBDSN {
		t.Fatalf("MigrationDSN = %q, want DBDSN fallback %q", cfg.MigrationDSN, cfg.DBDSN)
	}
}

func TestResolveApplicationSecretsRequiresRegionAndSecretID(t *testing.T) {
	cfg := &config.Config{AWSSecretsManagerEnabled: true}

	err := ResolveApplicationSecrets(context.Background(), cfg, fakeSecretGetter{})
	if err == nil {
		t.Fatal("ResolveApplicationSecrets() error = nil, want config error")
	}
	if strings.Contains(err.Error(), "secret@tcp") {
		t.Fatalf("error leaks secret-looking text: %v", err)
	}
}

func TestResolveApplicationSecretsRequiresDBDSN(t *testing.T) {
	cfg := awsSMConfig()
	secret := `{
		"DBRE_ENCRYPTION_KEY": "` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `",
		"JWT_SECRET": "jwt-secret"
	}`

	err := ResolveApplicationSecrets(context.Background(), cfg, fakeSecretGetter{secret: secret})
	if err == nil {
		t.Fatal("ResolveApplicationSecrets() error = nil, want DB_DSN required")
	}
	if !strings.Contains(err.Error(), "DB_DSN") {
		t.Fatalf("error = %v, want DB_DSN", err)
	}
}

func TestResolveApplicationSecretsRejectsInvalidEncryptionKey(t *testing.T) {
	cfg := awsSMConfig()
	secret := `{
		"DB_DSN": "maestro_app:secret@tcp(db.example.com:3306)/maestro?parseTime=true&charset=utf8mb4&loc=UTC",
		"DBRE_ENCRYPTION_KEY": "not-base64",
		"JWT_SECRET": "jwt-secret"
	}`

	err := ResolveApplicationSecrets(context.Background(), cfg, fakeSecretGetter{secret: secret})
	if err == nil {
		t.Fatal("ResolveApplicationSecrets() error = nil, want invalid key error")
	}
	if !strings.Contains(err.Error(), "DBRE_ENCRYPTION_KEY") {
		t.Fatalf("error = %v, want DBRE_ENCRYPTION_KEY", err)
	}
}

func TestResolveApplicationSecretsDoesNotFallbackWhenSecretReadFails(t *testing.T) {
	cfg := awsSMConfig()
	cfg.DBDSN = "maestro_app:local@tcp(localhost:3306)/maestro"

	err := ResolveApplicationSecrets(context.Background(), cfg, fakeSecretGetter{err: errors.New("denied")})
	if err == nil {
		t.Fatal("ResolveApplicationSecrets() error = nil, want read error")
	}
	if strings.Contains(err.Error(), cfg.DBDSN) {
		t.Fatalf("error should not mention fallback DSN: %v", err)
	}
}
