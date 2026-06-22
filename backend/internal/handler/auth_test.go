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
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
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

func authUserRows(isActive bool) *sqlmock.Rows {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
		AddRow(7, "alice", "alice@example.com", "$2a$10$0nQylfz.2fD0vExsU1Jd0OHj3W8tLi8fL4v9MXM71j8x9prVf1viy", 0, 0, isActive, now, now)
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

func TestAuthHandlerLoginInvalidCredentialsWritesAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthHandler(repository.NewUserRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), repository.NewAuditRepo(sqlxDB), []byte("secret"))

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

	mock.ExpectQuery(`SELECT \* FROM sessions WHERE user_id = \? ORDER BY created_at DESC LIMIT \?`).
		WithArgs(userID, 20).
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
