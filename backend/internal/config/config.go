package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/netguard"
	"github.com/dbre-maestro/maestro/internal/pool"
)

type Config struct {
	Port                      string
	AppEnv                    string
	MFAEnforcement            string
	DBDSN                     string
	MigrationDSN              string
	JWTSecret                 []byte
	EncryptionKey             []byte
	AppBaseURL                string
	StaticDir                 string
	RunMigrationsOnStartup    bool
	AWSSecretsManagerEnabled  bool
	AWSSecretsManagerRegion   string
	AWSSecretsManagerSecretID string
	RefreshCookieSecure       bool
	LarkWebhookURL            string // optional; empty = Lark notifications disabled
	LarkOAuth                 LarkOAuthConfig
	OIDCSSO                   OIDCSSOConfig
	PoolProfiles              map[pool.Profile]pool.ProfileConfig
	DBConnectionHostPolicy    netguard.Config
}

type LarkOAuthConfig struct {
	Enabled                bool
	Site                   string
	AppID                  string
	AppSecret              string
	RedirectURL            string
	Scopes                 []string
	RequireEnterpriseEmail bool
	EnterpriseEmailDomains []string
}

func (c LarkOAuthConfig) Configured() bool {
	return c.Enabled && c.AppID != "" && c.AppSecret != "" && c.RedirectURL != ""
}

type OIDCSSOConfig struct {
	Enabled                bool
	DisplayName            string
	IssuerURL              string
	ClientID               string
	ClientSecret           string
	RedirectURL            string
	Scopes                 []string
	TrustMFA               bool
	RequireEnterpriseEmail bool
	EnterpriseEmailDomains []string
}

func (c OIDCSSOConfig) Configured() bool {
	return c.Enabled && c.IssuerURL != "" && c.ClientID != "" && c.ClientSecret != "" && c.RedirectURL != ""
}

func (c OIDCSSOConfig) ScopesOrDefault() []string {
	if len(c.Scopes) > 0 {
		return c.Scopes
	}
	return []string{"openid", "profile", "email", "dbre"}
}

func Load() (*Config, error) {
	c := &Config{
		Port:                      getEnv("PORT", "8080"),
		AppEnv:                    normalizeAppEnv(getEnv("APP_ENV", "development")),
		DBDSN:                     os.Getenv("DB_DSN"),
		MigrationDSN:              os.Getenv("MIGRATION_DSN"),
		AppBaseURL:                strings.TrimRight(os.Getenv("APP_BASE_URL"), "/"),
		StaticDir:                 strings.TrimSpace(os.Getenv("STATIC_DIR")),
		RunMigrationsOnStartup:    true,
		AWSSecretsManagerRegion:   strings.TrimSpace(os.Getenv("AWS_SM_REGION")),
		AWSSecretsManagerSecretID: strings.TrimSpace(os.Getenv("AWS_SM_SECRET_ID")),
		DBConnectionHostPolicy: netguard.Config{
			Enforcement:   os.Getenv("DB_CONNECTION_HOST_POLICY_ENFORCEMENT"),
			HostAllowlist: netguard.SplitCSV(os.Getenv("DB_CONNECTION_HOST_ALLOWLIST")),
			CIDRAllowlist: netguard.SplitCSV(os.Getenv("DB_CONNECTION_CIDR_ALLOWLIST")),
			CIDRDenylist:  netguard.SplitCSV(os.Getenv("DB_CONNECTION_CIDR_DENYLIST")),
		},
		PoolProfiles: map[pool.Profile]pool.ProfileConfig{
			pool.ProfileQuery:            pool.DefaultConfigForProfile(pool.ProfileQuery),
			pool.ProfileExec:             pool.DefaultConfigForProfile(pool.ProfileExec),
			pool.ProfileMetadata:         pool.DefaultConfigForProfile(pool.ProfileMetadata),
			pool.ProfileScopedPGQuery:    pool.DefaultConfigForProfile(pool.ProfileScopedPGQuery),
			pool.ProfileShadowValidation: pool.DefaultConfigForProfile(pool.ProfileShadowValidation),
		},
	}
	c.MFAEnforcement = defaultMFAEnforcement(c.AppEnv)

	if raw := os.Getenv("AWS_SM_ENABLE"); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("AWS_SM_ENABLE must be a boolean: %w", err)
		}
		c.AWSSecretsManagerEnabled = enabled
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret != "" {
		c.JWTSecret = []byte(jwtSecret)
	}

	encKey := os.Getenv("DBRE_ENCRYPTION_KEY")
	if encKey != "" {
		if err := c.SetEncryptionKey(encKey); err != nil {
			return nil, err
		}
	}

	if c.MigrationDSN == "" && !c.AWSSecretsManagerEnabled {
		c.MigrationDSN = c.DBDSN
	}

	c.LarkWebhookURL = os.Getenv("LARK_WEBHOOK_URL")
	c.LarkOAuth = LarkOAuthConfig{
		Enabled:                truthyEnv(os.Getenv("LARK_OAUTH_ENABLE")),
		Site:                   normalizeLarkSite(getEnv("LARK_SITE", "lark")),
		AppID:                  strings.TrimSpace(os.Getenv("LARK_APP_ID")),
		AppSecret:              strings.TrimSpace(os.Getenv("LARK_APP_SECRET")),
		RedirectURL:            strings.TrimSpace(os.Getenv("LARK_OAUTH_REDIRECT_URL")),
		Scopes:                 splitCSV(getEnv("LARK_OAUTH_SCOPES", "directory:employee.base.enterprise_email:read")),
		RequireEnterpriseEmail: truthyEnv(getEnv("LARK_OAUTH_REQUIRE_ENTERPRISE_EMAIL", "true")),
		EnterpriseEmailDomains: splitCSV(getEnv("LARK_OAUTH_ENTERPRISE_EMAIL_DOMAINS", "")),
	}
	c.OIDCSSO = OIDCSSOConfig{
		Enabled:                truthyEnv(os.Getenv("SSO_OIDC_ENABLED")),
		DisplayName:            strings.TrimSpace(getEnv("SSO_OIDC_DISPLAY_NAME", "Authentik")),
		IssuerURL:              strings.TrimRight(strings.TrimSpace(os.Getenv("SSO_OIDC_ISSUER_URL")), "/"),
		ClientID:               strings.TrimSpace(os.Getenv("SSO_OIDC_CLIENT_ID")),
		ClientSecret:           strings.TrimSpace(os.Getenv("SSO_OIDC_CLIENT_SECRET")),
		RedirectURL:            strings.TrimSpace(os.Getenv("SSO_OIDC_REDIRECT_URL")),
		Scopes:                 splitCSV(getEnv("SSO_OIDC_SCOPES", "openid,profile,email,dbre")),
		TrustMFA:               truthyEnv(os.Getenv("SSO_OIDC_TRUST_MFA")),
		RequireEnterpriseEmail: truthyEnv(getEnv("SSO_OIDC_REQUIRE_ENTERPRISE_EMAIL", "true")),
		EnterpriseEmailDomains: splitCSV(getEnv("SSO_OIDC_ENTERPRISE_EMAIL_DOMAINS", "")),
	}
	if raw := os.Getenv("RUN_MIGRATIONS_ON_STARTUP"); raw != "" {
		runMigrations, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("RUN_MIGRATIONS_ON_STARTUP must be a boolean: %w", err)
		}
		c.RunMigrationsOnStartup = runMigrations
	}
	if raw := os.Getenv("MFA_ENFORCEMENT"); raw != "" {
		c.MFAEnforcement = normalizeMFAEnforcement(raw)
		if c.MFAEnforcement == "" {
			return nil, errors.New("MFA_ENFORCEMENT must be disabled or required_for_admins")
		}
	}
	c.RefreshCookieSecure = c.AppEnv == "production"
	if raw := os.Getenv("REFRESH_COOKIE_SECURE"); raw != "" {
		secure, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("REFRESH_COOKIE_SECURE must be a boolean: %w", err)
		}
		c.RefreshCookieSecure = c.RefreshCookieSecure || secure
	}

	if err := loadPoolProfileConfig(c); err != nil {
		return nil, err
	}
	if _, err := netguard.NewPolicy(c.DBConnectionHostPolicy); err != nil {
		return nil, err
	}
	if !c.AWSSecretsManagerEnabled {
		if err := c.ValidateRequiredSecrets(); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func (c *Config) SetEncryptionKey(value string) error {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return errors.New("DBRE_ENCRYPTION_KEY must be base64-encoded 32 bytes")
	}
	if len(key) != 32 {
		return errors.New("DBRE_ENCRYPTION_KEY must decode to exactly 32 bytes")
	}
	c.EncryptionKey = key
	return nil
}

func (c *Config) ValidateRequiredSecrets() error {
	if c.DBDSN == "" {
		return errors.New("DB_DSN is required")
	}
	if len(c.JWTSecret) == 0 {
		return errors.New("JWT_SECRET is required")
	}
	if len(c.EncryptionKey) == 0 {
		return errors.New("DBRE_ENCRYPTION_KEY is required")
	}
	if len(c.EncryptionKey) != 32 {
		return errors.New("DBRE_ENCRYPTION_KEY must decode to exactly 32 bytes")
	}
	if c.MigrationDSN == "" {
		c.MigrationDSN = c.DBDSN
	}
	return nil
}

func defaultMFAEnforcement(appEnv string) string {
	if appEnv == "production" {
		return "required_for_admins"
	}
	return "disabled"
}

func normalizeMFAEnforcement(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "disabled", "required_for_admins":
		return normalized
	default:
		return ""
	}
}

func truthyEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y":
		return true
	default:
		return false
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func normalizeLarkSite(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "feishu", "cn", "china":
		return "feishu"
	default:
		return "lark"
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func normalizeAppEnv(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "prod" {
		return "production"
	}
	if normalized == "" {
		return "development"
	}
	return normalized
}

func loadPoolProfileConfig(c *Config) error {
	profiles := []pool.Profile{
		pool.ProfileQuery,
		pool.ProfileExec,
		pool.ProfileMetadata,
		pool.ProfileScopedPGQuery,
		pool.ProfileShadowValidation,
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
