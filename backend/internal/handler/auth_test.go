package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/auth"
	"github.com/dbre-maestro/maestro/internal/config"
	"github.com/dbre-maestro/maestro/internal/crypto"
	"github.com/dbre-maestro/maestro/internal/larkoauth"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/oidcsso"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/pquerna/otp/totp"
)

type auditDetailsReason string

func (m auditDetailsReason) Match(v driver.Value) bool {
	var raw []byte
	switch value := v.(type) {
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	default:
		return false
	}
	var details struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &details); err != nil {
		return false
	}
	return details.Reason == string(m)
}

type mockOIDCSSOClient struct {
	authorizeURL string
	identity     oidcsso.Identity
	err          error
}

func (m mockOIDCSSOClient) AuthorizeURL(ctx context.Context, cfg config.OIDCSSOConfig, state string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.authorizeURL, nil
}

func (m mockOIDCSSOClient) ExchangeCode(ctx context.Context, cfg config.OIDCSSOConfig, code string) (oidcsso.Identity, error) {
	if m.err != nil {
		return oidcsso.Identity{}, m.err
	}
	return m.identity, nil
}

func authUserRows(isActive bool) *sqlmock.Rows {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
		AddRow(7, "alice", "alice@example.com", "$2a$10$0nQylfz.2fD0vExsU1Jd0OHj3W8tLi8fL4v9MXM71j8x9prVf1viy", 0, 0, isActive, now, now)
}

func mfaUserRows(t *testing.T, encKey []byte, secret string, enabled bool) *sqlmock.Rows {
	t.Helper()
	encrypted, err := crypto.Encrypt(encKey, []byte(secret))
	if err != nil {
		t.Fatalf("encrypt mfa secret: %v", err)
	}
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "mfa_enabled", "mfa_secret_encrypted", "mfa_enabled_at", "created_at", "updated_at"}).
		AddRow(7, "alice", "alice@example.com", "hash", 0, 1, 1, enabled, encrypted, now, now, now)
}

func mfaChallengeRows(tokenID string, setup bool, usedAt any, revokedAt any, attempts int) *sqlmock.Rows {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{"id", "token_id", "user_id", "setup", "expires_at", "attempt_count", "used_at", "revoked_at", "created_ip", "created_at"}).
		AddRow(3, tokenID, 7, setup, time.Now().Add(time.Minute), attempts, usedAt, revokedAt, "10.0.0.1", now)
}

func expectSetupCompleted(mock sqlmock.Sqlmock, count int) {
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

func sessionRows(id uint64, userID uint64) *sqlmock.Rows {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{"id", "user_id", "token_hash", "user_agent", "ip_address", "auth_method", "auth_provider", "mfa_satisfied", "mfa_source", "expires_at", "revoked_at", "created_at"}).
		AddRow(id, userID, "hash", nil, nil, "password", "", false, "", now.Add(time.Hour), nil, now)
}

func TestFindOrCreateSSOUserAllowsUnverifiedCompanyEmail(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), nil, nil, []byte("secret"), ssoEnterpriseEmailConfig())

	mock.ExpectQuery(`SELECT \* FROM users WHERE external_identity_source = \? AND external_identity_id = \?`).
		WithArgs("oidc", "authentik-user-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT \* FROM users WHERE email = \?`).
		WithArgs("alice@edgex.exchange").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(7, "alice", "alice@edgex.exchange", "hash", 0, 0, 1, time.Now(), time.Now()))
	mock.ExpectExec(`UPDATE users`).
		WithArgs("oidc", "authentik-user-1", sqlmock.AnyArg(), uint64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(7, "alice", "alice@edgex.exchange", "hash", 0, 0, 1, time.Now(), time.Now()))

	user, err := handler.findOrCreateSSOUser(context.Background(), oidcsso.Identity{
		Subject:                   "authentik-user-1",
		Email:                     "alice@edgex.exchange",
		EmailVerified:             false,
		EmailVerifiedClaimPresent: true,
	})
	if err != nil {
		t.Fatalf("findOrCreateSSOUser error: %v", err)
	}
	if user == nil || user.ID != 7 {
		t.Fatalf("user = %#v, want id 7", user)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFindOrCreateSSOUserAllowsMissingEmailVerifiedForCompanyEmail(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), nil, nil, []byte("secret"), ssoEnterpriseEmailConfig())

	mock.ExpectQuery(`SELECT \* FROM users WHERE external_identity_source = \? AND external_identity_id = \?`).
		WithArgs("oidc", "authentik-user-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT \* FROM users WHERE email = \?`).
		WithArgs("alice@edgex.exchange").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(7, "alice", "alice@edgex.exchange", "hash", 0, 0, 1, time.Now(), time.Now()))
	mock.ExpectExec(`UPDATE users`).
		WithArgs("oidc", "authentik-user-1", sqlmock.AnyArg(), uint64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(7, "alice", "alice@edgex.exchange", "hash", 0, 0, 1, time.Now(), time.Now()))

	user, err := handler.findOrCreateSSOUser(context.Background(), oidcsso.Identity{
		Subject:                   "authentik-user-1",
		Email:                     "alice@edgex.exchange",
		EmailVerifiedClaimPresent: false,
	})
	if err != nil {
		t.Fatalf("findOrCreateSSOUser error: %v", err)
	}
	if user == nil || user.ID != 7 {
		t.Fatalf("user = %#v, want id 7", user)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFindOrCreateSSOUserRejectsNonCompanyEmailBeforeAutoBinding(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), nil, nil, []byte("secret"), ssoEnterpriseEmailConfig())

	mock.ExpectQuery(`SELECT \* FROM users WHERE external_identity_source = \? AND external_identity_id = \?`).
		WithArgs("oidc", "authentik-user-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, err = handler.findOrCreateSSOUser(context.Background(), oidcsso.Identity{
		Subject:                   "authentik-user-1",
		Email:                     "alice@example.com",
		EmailVerified:             true,
		EmailVerifiedClaimPresent: true,
	})
	if err == nil || !strings.Contains(err.Error(), "email domain is not allowed") {
		t.Fatalf("err = %v, want domain error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func ssoEnterpriseEmailConfig() config.OIDCSSOConfig {
	return config.OIDCSSOConfig{
		RequireEnterpriseEmail: true,
		EnterpriseEmailDomains: []string{"edgex.exchange"},
	}
}

func configuredOIDCSSOConfig() config.OIDCSSOConfig {
	cfg := ssoEnterpriseEmailConfig()
	cfg.Enabled = true
	cfg.DisplayName = "Authentik"
	cfg.IssuerURL = "https://auth.example.com/application/o/dbre"
	cfg.ClientID = "dbre"
	cfg.ClientSecret = "secret"
	cfg.RedirectURL = "https://dbre.example.com/api/auth/sso/callback"
	cfg.Scopes = []string{"openid", "profile", "email", "dbre"}
	return cfg
}

func TestSSOUsernameBasePrefersEmailLocalPart(t *testing.T) {
	got := ssoUsernameBase(oidcsso.Identity{
		Email: "William.Yeh@edgex.exchange",
		Name:  "Ignored Name",
	})
	if got != "william.yeh" {
		t.Fatalf("ssoUsernameBase = %q, want william.yeh", got)
	}
}

func TestResolveOIDCSSOConfigAppliesDefaults(t *testing.T) {
	handler := NewAuthHandler(nil, nil, nil, []byte("secret"), config.OIDCSSOConfig{
		Enabled:                true,
		IssuerURL:              "https://auth.example.com",
		ClientID:               "dbre",
		ClientSecret:           "secret",
		RedirectURL:            "https://dbre.example.com/api/auth/sso/callback",
		RequireEnterpriseEmail: true,
		EnterpriseEmailDomains: []string{"edgex.exchange"},
	})

	cfg, err := handler.resolveOIDCSSOConfig(context.Background())
	if err != nil {
		t.Fatalf("resolveOIDCSSOConfig error: %v", err)
	}
	if cfg.DisplayName != "Authentik" {
		t.Fatalf("DisplayName = %q, want Authentik", cfg.DisplayName)
	}
	if len(cfg.Scopes) != 4 || cfg.Scopes[0] != "openid" {
		t.Fatalf("Scopes = %#v, want default oidc scopes", cfg.Scopes)
	}
	if !cfg.RequireEnterpriseEmail || len(cfg.EnterpriseEmailDomains) != 1 || cfg.EnterpriseEmailDomains[0] != "edgex.exchange" {
		t.Fatalf("enterprise email policy = %#v / %#v", cfg.RequireEnterpriseEmail, cfg.EnterpriseEmailDomains)
	}
}

func TestAuthHandlerMe(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	userRepo := repository.NewUserRepo(sqlxDB)
	handler := NewAuthHandler(userRepo, nil, nil, nil)

	userID := uint64(42)
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(userID, "alice", "alice@example.com", "hash", 0, 0, 1, now, now))
	mock.ExpectQuery(`SELECT DISTINCT ag\.id, ag\.group_key, ag\.name, ag\.is_system, ag\.is_protected`).
		WithArgs(userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_key", "name", "is_system", "is_protected"}).
			AddRow(2, "reviewer", "Reviewer", 1, 0).
			AddRow(3, "dba", "DBA", 1, 0))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(userID, "alice", "alice@example.com", "hash", 0, 0, 1, now, now))
	mock.ExpectQuery(`SELECT EXISTS \(`).
		WithArgs(userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT DISTINCT permission_key FROM`).
		WithArgs(userID, userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"permission_key"}).
			AddRow("tickets.review").
			AddRow("tickets.execute"))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(userID, "alice", "alice@example.com", "hash", 0, 0, 1, now, now))
	mock.ExpectQuery(`SELECT EXISTS \(`).
		WithArgs(userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT DISTINCT db_connection_id FROM`).
		WithArgs(userID, userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"db_connection_id"}).
			AddRow(7).
			AddRow(11))

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxUsername, "alice")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	http.HandlerFunc(handler.Me).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		ID         uint64 `json:"id"`
		Username   string `json:"username"`
		Protected  bool   `json:"protected"`
		IsActive   bool   `json:"is_active"`
		AuthGroups []struct {
			GroupKey string `json:"group_key"`
		} `json:"auth_groups"`
		Permissions     []string `json:"permissions"`
		DBConnectionIDs []uint64 `json:"db_connection_ids"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.ID != userID {
		t.Fatalf("id = %d, want %d", got.ID, userID)
	}
	if got.Username != "alice" {
		t.Fatalf("username = %q, want %q", got.Username, "alice")
	}
	if len(got.AuthGroups) != 2 {
		t.Fatalf("auth_groups len = %d, want 2", len(got.AuthGroups))
	}
	if got.AuthGroups[0].GroupKey != string(model.AuthGroupReviewer) || got.AuthGroups[1].GroupKey != string(model.AuthGroupDBA) {
		t.Fatalf("auth_groups = %#v, want [%q %q]", got.AuthGroups, model.AuthGroupReviewer, model.AuthGroupDBA)
	}
	if len(got.Permissions) != 2 || got.Permissions[0] != "tickets.review" {
		t.Fatalf("permissions = %#v", got.Permissions)
	}
	if len(got.DBConnectionIDs) != 2 || got.DBConnectionIDs[0] != 7 {
		t.Fatalf("db_connection_ids = %#v", got.DBConnectionIDs)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerSetupStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	userRepo := repository.NewUserRepo(sqlxDB)
	handler := NewAuthHandler(userRepo, nil, nil, nil)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	req := httptest.NewRequest(http.MethodGet, "/setup/status", nil)
	rec := httptest.NewRecorder()
	handler.SetupStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"setup_completed":true`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestLarkEnterpriseEmailAllowed(t *testing.T) {
	tests := []struct {
		name           string
		email          string
		allowedDomains []string
		want           bool
	}{
		{
			name:           "allows exact configured enterprise domain",
			email:          "william@edgex.exchange",
			allowedDomains: []string{"edgex.exchange"},
			want:           true,
		},
		{
			name:           "allows subdomain of configured enterprise domain",
			email:          "william@staff.edgex.exchange",
			allowedDomains: []string{"edgex.exchange"},
			want:           true,
		},
		{
			name:           "rejects personal email domain",
			email:          "william@gmail.com",
			allowedDomains: []string{"edgex.exchange"},
			want:           false,
		},
		{
			name:           "rejects suffix spoofing",
			email:          "william@notedgex.exchange",
			allowedDomains: []string{"edgex.exchange"},
			want:           false,
		},
		{
			name:           "rejects invalid email",
			email:          "william",
			allowedDomains: []string{"edgex.exchange"},
			want:           false,
		},
		{
			name:           "empty allowlist only validates email shape",
			email:          "william@example.com",
			allowedDomains: nil,
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := larkEnterpriseEmailAllowed(tt.email, tt.allowedDomains); got != tt.want {
				t.Fatalf("larkEnterpriseEmailAllowed(%q, %#v) = %v, want %v", tt.email, tt.allowedDomains, got, tt.want)
			}
		})
	}
}

func TestLarkUsernameBasePrefersEnterpriseEmail(t *testing.T) {
	identity := larkoauth.Identity{
		Email:           "personal.real.name@gmail.com",
		EnterpriseEmail: "william@edgex.exchange",
		DisplayName:     "William Yeh",
		OpenID:          "ou_test",
	}

	if got := larkUsernameBase(identity); got != "william" {
		t.Fatalf("larkUsernameBase() = %q, want %q", got, "william")
	}
}

func TestAuthHandlerMeReturnsEmptyArrayForNoGroups(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	userRepo := repository.NewUserRepo(sqlxDB)
	handler := NewAuthHandler(userRepo, nil, nil, nil)

	userID := uint64(7)
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(userID, "bob", "bob@example.com", "hash", 0, 0, 1, now, now))
	mock.ExpectQuery(`SELECT DISTINCT ag\.id, ag\.group_key, ag\.name, ag\.is_system, ag\.is_protected`).
		WithArgs(userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_key", "name", "is_system", "is_protected"}))
	mock.ExpectQuery(`SELECT DISTINCT ag\.id, ag\.group_key, ag\.name, ag\.is_system, ag\.is_protected`).
		WithArgs(userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_key", "name", "is_system", "is_protected"}))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(userID, "bob", "bob@example.com", "hash", 0, 0, 1, now, now))
	mock.ExpectQuery(`SELECT EXISTS \(`).
		WithArgs(userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT DISTINCT permission_key FROM`).
		WithArgs(userID, userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"permission_key"}))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(userID, "bob", "bob@example.com", "hash", 0, 0, 1, now, now))
	mock.ExpectQuery(`SELECT EXISTS \(`).
		WithArgs(userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT DISTINCT db_connection_id FROM`).
		WithArgs(userID, userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"db_connection_id"}))

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxUsername, "bob")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	http.HandlerFunc(handler.Me).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"auth_groups":[]`) || !strings.Contains(body, `"permissions":[]`) || !strings.Contains(body, `"db_connection_ids":[]`) {
		t.Fatalf("body = %s, want auth_groups/permissions/db_connection_ids to be [] instead of null", body)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerMeProtectedUserGetsAllDBConnections(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	userRepo := repository.NewUserRepo(sqlxDB)
	handler := NewAuthHandler(userRepo, nil, nil, nil)

	userID := uint64(1)
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(userID, "admin", "admin@example.com", "hash", 1, 1, 1, now, now))
	mock.ExpectQuery(`SELECT DISTINCT ag\.id, ag\.group_key, ag\.name, ag\.is_system, ag\.is_protected`).
		WithArgs(userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_key", "name", "is_system", "is_protected"}))
	mock.ExpectQuery(`SELECT DISTINCT ag\.id, ag\.group_key, ag\.name, ag\.is_system, ag\.is_protected`).
		WithArgs(userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_key", "name", "is_system", "is_protected"}).
			AddRow(4, "admin", "Admin", 1, 1))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(userID, "admin", "admin@example.com", "hash", 1, 1, 1, now, now))
	mock.ExpectQuery(`SELECT permission_key FROM permissions ORDER BY permission_key`).
		WillReturnRows(sqlmock.NewRows([]string{"permission_key"}).AddRow("sql_editor.query"))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
			AddRow(userID, "admin", "admin@example.com", "hash", 1, 1, 1, now, now))
	mock.ExpectQuery(`SELECT id\s+FROM db_connections`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7).AddRow(11))

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxUsername, "admin")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	http.HandlerFunc(handler.Me).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"db_connection_ids":[7,11]`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerLoginDisabledUserReturnsForbidden(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	userRepo := repository.NewUserRepo(sqlxDB)
	handler := NewAuthHandler(userRepo, repository.NewSessionRepo(sqlxDB), nil, []byte("secret"))

	expectSetupCompleted(mock, 1)
	mock.ExpectQuery(`SELECT \* FROM users WHERE username = \?`).
		WithArgs("alice").
		WillReturnRows(authUserRows(false))

	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"alice","password":"Password1"}`))
	rec := httptest.NewRecorder()
	handler.Login(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if !strings.Contains(rec.Body.String(), "user is disabled") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerLoginRequiresSetupCompleted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), nil, []byte("secret"))

	expectSetupCompleted(mock, 0)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"alice","password":"Password1"}`))
	rec := httptest.NewRecorder()
	handler.Login(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if !strings.Contains(rec.Body.String(), "setup is required before login") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerStartLarkLoginRequiresSetupCompleted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), nil, []byte("secret"))

	expectSetupCompleted(mock, 0)

	req := httptest.NewRequest(http.MethodGet, "/auth/lark/login/start", nil)
	rec := httptest.NewRecorder()
	handler.StartLarkLogin(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if !strings.Contains(rec.Body.String(), "setup is required before login") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerStartSSOLoginRequiresSetupCompleted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(
		repository.NewUserRepo(sqlxDB),
		repository.NewSessionRepo(sqlxDB),
		nil,
		[]byte("secret"),
		configuredOIDCSSOConfig(),
		repository.NewSSOLoginRepo(sqlxDB),
		mockOIDCSSOClient{authorizeURL: "https://auth.example.com/authorize"},
	)

	expectSetupCompleted(mock, 0)

	req := httptest.NewRequest(http.MethodGet, "/auth/sso/start", nil)
	rec := httptest.NewRecorder()
	handler.StartSSOLogin(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if !strings.Contains(rec.Body.String(), "setup is required before login") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerStartSSOLoginRedirectsToProvider(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(
		repository.NewUserRepo(sqlxDB),
		repository.NewSessionRepo(sqlxDB),
		nil,
		[]byte("secret"),
		configuredOIDCSSOConfig(),
		repository.NewSSOLoginRepo(sqlxDB),
		mockOIDCSSOClient{authorizeURL: "https://auth.example.com/authorize?state=generated"},
	)

	expectSetupCompleted(mock, 1)
	mock.ExpectExec(`INSERT INTO sso_login_states`).
		WithArgs(sqlmock.AnyArg(), "/tickets", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest(http.MethodGet, "/auth/sso/start?returnTo=/tickets", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.StartSSOLogin(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "https://auth.example.com/authorize?state=generated" {
		t.Fatalf("Location = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerCompleteSSOLoginRejectsUnknownState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(
		repository.NewUserRepo(sqlxDB),
		repository.NewSessionRepo(sqlxDB),
		nil,
		[]byte("secret"),
		configuredOIDCSSOConfig(),
		repository.NewSSOLoginRepo(sqlxDB),
		mockOIDCSSOClient{identity: oidcsso.Identity{Subject: "sub", Email: "alice@edgex.exchange"}},
	)

	expectSetupCompleted(mock, 1)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \*\s+FROM sso_login_states\s+WHERE state = \? AND used_at IS NULL AND expires_at > \?`).
		WithArgs("missing", sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodGet, "/auth/sso/callback?state=missing&code=code", nil)
	rec := httptest.NewRecorder()
	handler.CompleteSSOLogin(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "/login?sso_error=login_failed" {
		t.Fatalf("Location = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerConsumeSSOLoginResultRejectsUnknownTicket(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(
		repository.NewUserRepo(sqlxDB),
		repository.NewSessionRepo(sqlxDB),
		nil,
		[]byte("secret"),
		configuredOIDCSSOConfig(),
		repository.NewSSOLoginRepo(sqlxDB),
	)

	expectSetupCompleted(mock, 1)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \*\s+FROM sso_login_states\s+WHERE ticket = \? AND ticket_used_at IS NULL AND ticket_expires_at > \? AND user_id IS NOT NULL`).
		WithArgs("missing-ticket", sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/auth/sso/login/result/consume", strings.NewReader(`{"ticket":"missing-ticket"}`))
	rec := httptest.NewRecorder()
	handler.ConsumeSSOLoginResult(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid sso login ticket") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerLoginInvalidCredentialsWritesAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), repository.NewAuditRepo(sqlxDB), []byte("secret"))

	expectSetupCompleted(mock, 1)
	mock.ExpectQuery(`SELECT \* FROM users WHERE username = \?`).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`INSERT INTO audit_logs \(actor_id, actor_name, action_type, resource_type, resource_id, details, ip_address, created_at\)`).
		WithArgs(nil, "missing", "login_failed", "auth", nil, auditDetailsReason("invalid_credentials"), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"missing","password":"Password1"}`))
	req.RemoteAddr = "10.0.0.9:12345"
	rec := httptest.NewRecorder()
	handler.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "invalid credentials") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerLoginDisabledUserWritesAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), repository.NewAuditRepo(sqlxDB), []byte("secret"))

	expectSetupCompleted(mock, 1)
	mock.ExpectQuery(`SELECT \* FROM users WHERE username = \?`).
		WithArgs("alice").
		WillReturnRows(authUserRows(false))
	mock.ExpectExec(`INSERT INTO audit_logs \(actor_id, actor_name, action_type, resource_type, resource_id, details, ip_address, created_at\)`).
		WithArgs(sqlmock.AnyArg(), "alice", "login_failed", "auth", nil, auditDetailsReason("disabled_user"), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"alice","password":"Password1"}`))
	req.RemoteAddr = "10.0.0.10:12345"
	rec := httptest.NewRecorder()
	handler.Login(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if !strings.Contains(rec.Body.String(), "user is disabled") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerLoginRateLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), nil, []byte("secret"))
	handler.loginRateLimiter = newRequestRateLimiter(1, time.Minute)

	expectSetupCompleted(mock, 1)
	mock.ExpectQuery(`SELECT \* FROM users WHERE username = \?`).
		WithArgs("alice").
		WillReturnError(sql.ErrNoRows)

	firstReq := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"alice","password":"Password1"}`))
	firstReq.RemoteAddr = "10.0.0.1:12345"
	firstRec := httptest.NewRecorder()
	handler.Login(firstRec, firstReq)
	if firstRec.Code != http.StatusUnauthorized {
		t.Fatalf("first status = %d, want %d", firstRec.Code, http.StatusUnauthorized)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"alice","password":"Password1"}`))
	secondReq.RemoteAddr = "10.0.0.1:12345"
	secondRec := httptest.NewRecorder()
	handler.Login(secondRec, secondReq)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", secondRec.Code, http.StatusTooManyRequests)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerRefreshRateLimit(t *testing.T) {
	handler := NewAuthHandler(nil, nil, nil, []byte("secret"))
	handler.refreshRateLimiter = newRequestRateLimiter(1, time.Minute)

	firstReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	firstReq.RemoteAddr = "10.0.0.2:12345"
	firstRec := httptest.NewRecorder()
	handler.Refresh(firstRec, firstReq)
	if firstRec.Code != http.StatusUnauthorized {
		t.Fatalf("first status = %d, want %d", firstRec.Code, http.StatusUnauthorized)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	secondReq.RemoteAddr = "10.0.0.2:12345"
	secondRec := httptest.NewRecorder()
	handler.Refresh(secondRec, secondReq)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", secondRec.Code, http.StatusTooManyRequests)
	}
}

func TestAuthHandlerVerifyMFAMarksChallengeUsed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	encKey := []byte("01234567890123456789012345678901")
	jwtSecret := []byte("secret")
	tokenID := "challenge-success"
	secret := "JBSWY3DPEHPK3PXP"
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	mfaToken, err := auth.NewMFAChallengeTokenWithID(7, "alice", false, tokenID, jwtSecret)
	if err != nil {
		t.Fatalf("NewMFAChallengeTokenWithID: %v", err)
	}
	handler := NewAuthHandler(
		repository.NewUserRepo(sqlxDB, encKey),
		repository.NewSessionRepo(sqlxDB),
		nil,
		jwtSecret,
		MFAEnforcementRequiredForAdmins,
		repository.NewMFAChallengeRepo(sqlxDB),
	)

	mock.ExpectQuery(`SELECT \* FROM mfa_challenges WHERE token_id = \?`).
		WithArgs(tokenID).
		WillReturnRows(mfaChallengeRows(tokenID, false, nil, nil, 0))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(mfaUserRows(t, encKey, secret, true))
	mock.ExpectExec(`UPDATE mfa_challenges\s+SET used_at = \?`).
		WithArgs(sqlmock.AnyArg(), uint64(3), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO sessions`).
		WithArgs(uint64(7), sqlmock.AnyArg(), nil, sqlmock.AnyArg(), "password", "", true, "platform_totp", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(33, 1))
	mock.ExpectQuery(`SELECT \* FROM sessions WHERE id = \?`).
		WithArgs(int64(33)).
		WillReturnRows(sessionRows(33, 7))

	req := httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", strings.NewReader(`{"mfa_token":"`+mfaToken+`","code":"`+code+`"}`))
	rec := httptest.NewRecorder()
	handler.VerifyMFA(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "access_token") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerVerifyMFARejectsUsedChallenge(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	jwtSecret := []byte("secret")
	tokenID := "challenge-used"
	mfaToken, err := auth.NewMFAChallengeTokenWithID(7, "alice", false, tokenID, jwtSecret)
	if err != nil {
		t.Fatalf("NewMFAChallengeTokenWithID: %v", err)
	}
	handler := NewAuthHandler(
		repository.NewUserRepo(sqlxDB, []byte("01234567890123456789012345678901")),
		repository.NewSessionRepo(sqlxDB),
		nil,
		jwtSecret,
		MFAEnforcementRequiredForAdmins,
		repository.NewMFAChallengeRepo(sqlxDB),
	)

	usedAt := time.Now()
	mock.ExpectQuery(`SELECT \* FROM mfa_challenges WHERE token_id = \?`).
		WithArgs(tokenID).
		WillReturnRows(mfaChallengeRows(tokenID, false, usedAt, nil, 0))

	req := httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", strings.NewReader(`{"mfa_token":"`+mfaToken+`","code":"123456"}`))
	rec := httptest.NewRecorder()
	handler.VerifyMFA(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid mfa challenge") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerVerifyMFARecordsFailedAttempt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	encKey := []byte("01234567890123456789012345678901")
	jwtSecret := []byte("secret")
	tokenID := "challenge-failed"
	mfaToken, err := auth.NewMFAChallengeTokenWithID(7, "alice", false, tokenID, jwtSecret)
	if err != nil {
		t.Fatalf("NewMFAChallengeTokenWithID: %v", err)
	}
	handler := NewAuthHandler(
		repository.NewUserRepo(sqlxDB, encKey),
		repository.NewSessionRepo(sqlxDB),
		nil,
		jwtSecret,
		MFAEnforcementRequiredForAdmins,
		repository.NewMFAChallengeRepo(sqlxDB),
	)

	mock.ExpectQuery(`SELECT \* FROM mfa_challenges WHERE token_id = \?`).
		WithArgs(tokenID).
		WillReturnRows(mfaChallengeRows(tokenID, false, nil, nil, 4))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(mfaUserRows(t, encKey, "JBSWY3DPEHPK3PXP", true))
	mock.ExpectExec(`UPDATE mfa_challenges\s+SET attempt_count = attempt_count \+ 1`).
		WithArgs(mfaMaxAttempts, sqlmock.AnyArg(), uint64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", strings.NewReader(`{"mfa_token":"`+mfaToken+`","code":"000000"}`))
	rec := httptest.NewRecorder()
	handler.VerifyMFA(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid mfa code") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerVerifyMFARateLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	jwtSecret := []byte("secret")
	tokenID := "challenge-rate-limit"
	mfaToken, err := auth.NewMFAChallengeTokenWithID(7, "alice", false, tokenID, jwtSecret)
	if err != nil {
		t.Fatalf("NewMFAChallengeTokenWithID: %v", err)
	}
	handler := NewAuthHandler(
		repository.NewUserRepo(sqlxDB, []byte("01234567890123456789012345678901")),
		repository.NewSessionRepo(sqlxDB),
		nil,
		jwtSecret,
		MFAEnforcementRequiredForAdmins,
		repository.NewMFAChallengeRepo(sqlxDB),
	)
	handler.mfaVerifyRateLimiter = newRequestRateLimiter(1, time.Minute)

	mock.ExpectQuery(`SELECT \* FROM mfa_challenges WHERE token_id = \?`).
		WithArgs(tokenID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "token_id", "user_id", "setup", "expires_at", "attempt_count", "used_at", "revoked_at", "created_ip", "created_at"}))

	firstReq := httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", strings.NewReader(`{"mfa_token":"`+mfaToken+`","code":"000000"}`))
	firstReq.RemoteAddr = "10.0.0.3:12345"
	firstRec := httptest.NewRecorder()
	handler.VerifyMFA(firstRec, firstReq)
	if firstRec.Code != http.StatusUnauthorized {
		t.Fatalf("first status = %d, want %d", firstRec.Code, http.StatusUnauthorized)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/auth/mfa/verify", strings.NewReader(`{"mfa_token":"`+mfaToken+`","code":"000000"}`))
	secondReq.RemoteAddr = "10.0.0.3:12345"
	secondRec := httptest.NewRecorder()
	handler.VerifyMFA(secondRec, secondReq)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d, body=%s", secondRec.Code, http.StatusTooManyRequests, secondRec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerRefreshTokenReuseRevokesAllSessions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), nil, []byte("secret"))
	rawToken := "stolen-refresh-token"
	tokenHash := auth.HashRefreshToken(rawToken)
	userID := uint64(7)
	sessionID := uint64(99)
	now := time.Now().UTC()
	revokedAt := now.Add(-time.Minute)

	mock.ExpectQuery(`SELECT \* FROM sessions WHERE token_hash = \?`).
		WithArgs(tokenHash).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "token_hash", "user_agent", "ip_address", "expires_at", "revoked_at", "created_at"}).
			AddRow(sessionID, userID, tokenHash, "browser", "10.0.0.3", now.Add(time.Hour), revokedAt, now.Add(-time.Hour)))
	mock.ExpectExec(`UPDATE sessions SET revoked_at = \? WHERE user_id = \? AND revoked_at IS NULL`).
		WithArgs(sqlmock.AnyArg(), userID).
		WillReturnResult(sqlmock.NewResult(0, 3))

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: rawToken})
	rec := httptest.NewRecorder()
	handler.Refresh(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("refresh cookie was not cleared: %#v", cookies)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerRefreshTokenReuseWithinGraceDoesNotRevokeAllSessions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), nil, []byte("secret"))
	rawToken := "recently-rotated-refresh-token"
	tokenHash := auth.HashRefreshToken(rawToken)
	userID := uint64(7)
	sessionID := uint64(100)
	now := time.Now().UTC()
	revokedAt := now.Add(-5 * time.Second)

	mock.ExpectQuery(`SELECT \* FROM sessions WHERE token_hash = \?`).
		WithArgs(tokenHash).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "token_hash", "user_agent", "ip_address", "expires_at", "revoked_at", "created_at"}).
			AddRow(sessionID, userID, tokenHash, "browser", "10.0.0.3", now.Add(time.Hour), revokedAt, now.Add(-time.Hour)))

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: rawToken})
	rec := httptest.NewRecorder()
	handler.Refresh(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "stale refresh token") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	// Regression: a benign multi-tab race (e.g. a second tab opened from a
	// Lark ticket link) must not clear the refresh cookie — another tab may
	// have already rotated it to a valid new value, and clearing it here
	// would destroy that tab's session too.
	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("refresh cookie should not be cleared on a benign grace-window race: %#v", cookies)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerListSessionsDoesNotExposeTokenHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(nil, repository.NewSessionRepo(sqlxDB), nil, []byte("secret"))
	userID := uint64(7)
	now := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT \* FROM sessions WHERE user_id = \? ORDER BY CASE WHEN revoked_at IS NULL AND expires_at > \? THEN 0 ELSE 1 END, created_at DESC LIMIT \?`).
		WithArgs(userID, sqlmock.AnyArg(), 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "token_hash", "user_agent", "ip_address", "expires_at", "revoked_at", "created_at"}).
			AddRow(11, userID, "secret-token-hash", "browser", "10.0.0.8", now.Add(time.Hour), nil, now))

	req := httptest.NewRequest(http.MethodGet, "/auth/sessions", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ListSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-token-hash") || strings.Contains(rec.Body.String(), "token_hash") {
		t.Fatalf("session response exposed token hash: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerRevokeSessionScopesToCurrentUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(nil, repository.NewSessionRepo(sqlxDB), nil, []byte("secret"))
	userID := uint64(7)
	sessionID := uint64(11)

	mock.ExpectExec(`UPDATE sessions SET revoked_at = \? WHERE id = \? AND user_id = \? AND revoked_at IS NULL`).
		WithArgs(sqlmock.AnyArg(), sessionID, userID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	router := chi.NewRouter()
	router.Delete("/auth/sessions/{id}", handler.RevokeSession)
	req := httptest.NewRequest(http.MethodDelete, "/auth/sessions/11", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxUsername, "alice")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthHandlerLogoutClearsRefreshCookieUnderAPINamespace(t *testing.T) {
	handler := NewAuthHandler(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	handler.Logout(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies len = %d, want 1", len(cookies))
	}
	if cookies[0].Path != refreshCookiePath {
		t.Fatalf("cookie path = %q, want %q", cookies[0].Path, refreshCookiePath)
	}
	if cookies[0].MaxAge != -1 {
		t.Fatalf("cookie MaxAge = %d, want -1", cookies[0].MaxAge)
	}
}

func TestAuthHandlerLogoutClearsSecureRefreshCookieWhenConfigured(t *testing.T) {
	handler := NewAuthHandler(nil, nil, nil, nil, true)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	handler.Logout(rec, req)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies len = %d, want 1", len(cookies))
	}
	if !cookies[0].Secure {
		t.Fatal("cookie Secure = false, want true")
	}
	if cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie SameSite = %v, want Strict", cookies[0].SameSite)
	}
}
