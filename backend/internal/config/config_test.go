package config

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/pool"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DSN", "root:secret@tcp(localhost:3306)/maestro")
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("DBRE_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
}

func TestLoadUsesDefaultPoolProfiles(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.PoolProfiles[pool.ProfileQuery]; got != pool.DefaultConfigForProfile(pool.ProfileQuery) {
		t.Fatalf("query pool config = %#v, want %#v", got, pool.DefaultConfigForProfile(pool.ProfileQuery))
	}
	if got := cfg.PoolProfiles[pool.ProfileScopedPGQuery]; got != pool.DefaultConfigForProfile(pool.ProfileScopedPGQuery) {
		t.Fatalf("scoped pg query config = %#v, want %#v", got, pool.DefaultConfigForProfile(pool.ProfileScopedPGQuery))
	}
	if got := cfg.PoolProfiles[pool.ProfileShadowValidation]; got != pool.DefaultConfigForProfile(pool.ProfileShadowValidation) {
		t.Fatalf("shadow validation pool config = %#v, want %#v", got, pool.DefaultConfigForProfile(pool.ProfileShadowValidation))
	}
}

func TestLoadForcesSecureRefreshCookieInProduction(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "production")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.RefreshCookieSecure {
		t.Fatal("RefreshCookieSecure = false, want true for production")
	}
}

func TestLoadDoesNotAllowDisablingSecureRefreshCookieInProduction(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("REFRESH_COOKIE_SECURE", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.RefreshCookieSecure {
		t.Fatal("RefreshCookieSecure = false, want true because production forces secure cookies")
	}
}

func TestLoadAllowsForcingSecureRefreshCookieOutsideProduction(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("REFRESH_COOKIE_SECURE", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.RefreshCookieSecure {
		t.Fatal("RefreshCookieSecure = false, want true when explicitly enabled")
	}
}

func TestLoadOverridesPoolProfilesFromEnv(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DB_POOL_QUERY_MAX_OPEN", "20")
	t.Setenv("DB_POOL_QUERY_MAX_IDLE", "8")
	t.Setenv("DB_POOL_QUERY_CONN_MAX_LIFETIME", "7m")
	t.Setenv("DB_POOL_QUERY_CONN_MAX_IDLE_TIME", "3m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.PoolProfiles[pool.ProfileQuery]
	want := pool.ProfileConfig{
		MaxOpenConns:    20,
		MaxIdleConns:    8,
		ConnMaxLifetime: 7 * time.Minute,
		ConnMaxIdleTime: 3 * time.Minute,
	}
	if got != want {
		t.Fatalf("query pool config = %#v, want %#v", got, want)
	}
}
