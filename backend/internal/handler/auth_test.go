package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jmoiron/sqlx"
)

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
	mock.ExpectQuery(`SELECT DISTINCT permission_key FROM`).
		WithArgs(userID, userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"permission_key"}).
			AddRow("tickets.review").
			AddRow("tickets.execute"))
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
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
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
	mock.ExpectQuery(`SELECT DISTINCT permission_key FROM`).
		WithArgs(userID, userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"permission_key"}))
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
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"auth_groups":[]`) || !strings.Contains(body, `"permissions":[]`) || !strings.Contains(body, `"db_connection_ids":[]`) {
		t.Fatalf("body = %s, want auth_groups/permissions/db_connection_ids to be [] instead of null", body)
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
