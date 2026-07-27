package config

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/netguard"
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

func TestLoadDefaultsMFAEnforcementByEnvironment(t *testing.T) {
	setRequiredEnv(t)

	devCfg, err := Load()
	if err != nil {
		t.Fatalf("Load() development error = %v", err)
	}
	if devCfg.MFAEnforcement != "disabled" {
		t.Fatalf("development MFAEnforcement = %q, want disabled", devCfg.MFAEnforcement)
	}

	setRequiredEnv(t)
	t.Setenv("APP_ENV", "production")
	prodCfg, err := Load()
	if err != nil {
		t.Fatalf("Load() production error = %v", err)
	}
	if prodCfg.MFAEnforcement != "required_for_admins" {
		t.Fatalf("production MFAEnforcement = %q, want required_for_admins", prodCfg.MFAEnforcement)
	}
}

func TestLoadAllowsMFAEnforcementOverride(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("MFA_ENFORCEMENT", "disabled")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MFAEnforcement != "disabled" {
		t.Fatalf("MFAEnforcement = %q, want disabled", cfg.MFAEnforcement)
	}
}

func TestLoadRejectsInvalidMFAEnforcement(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MFA_ENFORCEMENT", "optional")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid MFA_ENFORCEMENT error")
	}
}

func TestLoadReadsStaticDir(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("STATIC_DIR", " /app/public ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StaticDir != "/app/public" {
		t.Fatalf("StaticDir = %q, want /app/public", cfg.StaticDir)
	}
}

func TestLoadReadsDBConnectionHostPolicy(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DB_CONNECTION_HOST_POLICY_ENFORCEMENT", "warn")
	t.Setenv("DB_CONNECTION_HOST_ALLOWLIST", "*.rds.amazonaws.com,*.cache.amazonaws.com")
	t.Setenv("DB_CONNECTION_CIDR_ALLOWLIST", "10.183.0.0/16,10.222.38.0/24")
	t.Setenv("DB_CONNECTION_CIDR_DENYLIST", "127.0.0.0/8,169.254.0.0/16,::1/128")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DBConnectionHostPolicy.Enforcement != "warn" {
		t.Fatalf("Enforcement = %q, want warn", cfg.DBConnectionHostPolicy.Enforcement)
	}
	if got := len(cfg.DBConnectionHostPolicy.HostAllowlist); got != 2 {
		t.Fatalf("HostAllowlist length = %d, want 2", got)
	}
	if _, err := netguard.NewPolicy(cfg.DBConnectionHostPolicy); err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
}

func TestLoadDefaultsLarkOAuthEnterpriseEmailScope(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.LarkOAuth.Scopes) != 1 {
		t.Fatalf("LarkOAuth.Scopes length = %d, want 1", len(cfg.LarkOAuth.Scopes))
	}
	if cfg.LarkOAuth.Scopes[0] != "directory:employee.base.enterprise_email:read" {
		t.Fatalf("LarkOAuth.Scopes[0] = %q, want enterprise email scope", cfg.LarkOAuth.Scopes[0])
	}
}

func TestLoadOverridesLarkOAuthScopes(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("LARK_OAUTH_SCOPES", "directory:employee.base.enterprise_email:read,contact:user.email:readonly")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.LarkOAuth.Scopes) != 2 {
		t.Fatalf("LarkOAuth.Scopes length = %d, want 2", len(cfg.LarkOAuth.Scopes))
	}
	if cfg.LarkOAuth.Scopes[1] != "contact:user.email:readonly" {
		t.Fatalf("LarkOAuth.Scopes[1] = %q, want contact:user.email:readonly", cfg.LarkOAuth.Scopes[1])
	}
}

func TestLoadDefaultsOIDCSSOEnterpriseEmailPolicy(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.OIDCSSO.RequireEnterpriseEmail {
		t.Fatal("OIDCSSO.RequireEnterpriseEmail = false, want true")
	}
	if len(cfg.OIDCSSO.EnterpriseEmailDomains) != 1 || cfg.OIDCSSO.EnterpriseEmailDomains[0] != "edgex.exchange" {
		t.Fatalf("OIDCSSO.EnterpriseEmailDomains = %#v, want edgex.exchange", cfg.OIDCSSO.EnterpriseEmailDomains)
	}
}

func TestLoadOverridesOIDCSSOEnterpriseEmailPolicy(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SSO_OIDC_REQUIRE_ENTERPRISE_EMAIL", "false")
	t.Setenv("SSO_OIDC_ENTERPRISE_EMAIL_DOMAINS", "example.com,edgex.exchange")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.OIDCSSO.RequireEnterpriseEmail {
		t.Fatal("OIDCSSO.RequireEnterpriseEmail = true, want false")
	}
	if len(cfg.OIDCSSO.EnterpriseEmailDomains) != 2 || cfg.OIDCSSO.EnterpriseEmailDomains[0] != "example.com" || cfg.OIDCSSO.EnterpriseEmailDomains[1] != "edgex.exchange" {
		t.Fatalf("OIDCSSO.EnterpriseEmailDomains = %#v, want example.com, edgex.exchange", cfg.OIDCSSO.EnterpriseEmailDomains)
	}
}

func TestLoadRejectsInvalidDBConnectionHostPolicy(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DB_CONNECTION_HOST_POLICY_ENFORCEMENT", "enforce")
	t.Setenv("DB_CONNECTION_CIDR_DENYLIST", "not-a-cidr")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid CIDR error")
	}
}

func TestLoadDefaultsRunMigrationsOnStartup(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.RunMigrationsOnStartup {
		t.Fatal("RunMigrationsOnStartup = false, want true")
	}
}

func TestLoadAllowsDisablingStartupMigrations(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("RUN_MIGRATIONS_ON_STARTUP", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RunMigrationsOnStartup {
		t.Fatal("RunMigrationsOnStartup = true, want false")
	}
}

func TestLoadRejectsInvalidRunMigrationsOnStartup(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("RUN_MIGRATIONS_ON_STARTUP", "sometimes")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid RUN_MIGRATIONS_ON_STARTUP error")
	}
}

func TestLoadRejectsInvalidAWSSecretsManagerEnable(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("AWS_SM_ENABLE", "sometimes")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid AWS_SM_ENABLE error")
	}
}

func TestLoadDefersRequiredSecretsWhenAWSSecretsManagerEnabled(t *testing.T) {
	t.Setenv("AWS_SM_ENABLE", "true")
	t.Setenv("AWS_SM_REGION", "ap-northeast-1")
	t.Setenv("AWS_SM_SECRET_ID", "/sre-test/dbre-maestro")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.AWSSecretsManagerEnabled {
		t.Fatal("AWSSecretsManagerEnabled = false, want true")
	}
	if cfg.AWSSecretsManagerRegion != "ap-northeast-1" {
		t.Fatalf("AWSSecretsManagerRegion = %q, want ap-northeast-1", cfg.AWSSecretsManagerRegion)
	}
	if cfg.AWSSecretsManagerSecretID != "/sre-test/dbre-maestro" {
		t.Fatalf("AWSSecretsManagerSecretID = %q, want /sre-test/dbre-maestro", cfg.AWSSecretsManagerSecretID)
	}
}

func TestValidateRequiredSecretsRequiresDBDSNOutsideAWSSecretsManager(t *testing.T) {
	t.Setenv("JWT_SECRET", "jwt-secret")
	t.Setenv("DBRE_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want DB_DSN required")
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
