package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

func routeRequest(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "operator")
	return req.WithContext(ctx)
}

func withURLParam(req *http.Request, key, value string) *http.Request {
	routeCtx, _ := req.Context().Value(chi.RouteCtxKey).(*chi.Context)
	if routeCtx == nil {
		routeCtx = chi.NewRouteContext()
	}
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func userRows() *sqlmock.Rows {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{"id", "username", "email", "lark_recipient", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
		AddRow(7, "alice", "alice@example.com", "", "hash", 0, 0, 1, now, now)
}

func protectedUserRows() *sqlmock.Rows {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{"id", "username", "email", "lark_recipient", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
		AddRow(1, "admin", "admin@example.com", "", "hash", 1, 1, 1, now, now)
}

func userRowsWithID(id uint64, username string) *sqlmock.Rows {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{"id", "username", "email", "lark_recipient", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}).
		AddRow(id, username, username+"@example.com", "", "hash", 0, 0, 1, now, now)
}

func authGroupRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "group_key", "name", "description", "is_system", "is_protected", "created_at", "updated_at"}).
		AddRow(1, "developer", "Developer", "", 1, 0, "2026-06-10T00:00:00Z", "2026-06-10T00:00:00Z").
		AddRow(2, "reviewer", "Reviewer", "", 1, 0, "2026-06-10T00:00:00Z", "2026-06-10T00:00:00Z").
		AddRow(3, "dba", "DBA", "", 1, 0, "2026-06-10T00:00:00Z", "2026-06-10T00:00:00Z").
		AddRow(4, "admin", "Admin", "", 1, 1, "2026-06-10T00:00:00Z", "2026-06-10T00:00:00Z")
}

func authGroupRow(id int, groupKey, name, description string, isSystem, isProtected int) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "group_key", "name", "description", "is_system", "is_protected", "created_at", "updated_at"}).
		AddRow(id, groupKey, name, description, isSystem, isProtected, "2026-06-10T00:00:00Z", "2026-06-11T00:00:00Z")
}

func TestUserHandlerDeleteProtectedUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewUserHandler(repository.NewUserRepo(sqlxDB), repository.NewAuthGroupRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), repository.NewAuditRepo(sqlxDB), repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")))

	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(1)).
		WillReturnRows(protectedUserRows())

	req := withURLParam(routeRequest(http.MethodDelete, "/users/1"), "id", "1")
	rec := httptest.NewRecorder()
	handler.Delete(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if !strings.Contains(rec.Body.String(), "protected system user cannot be deleted") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUserHandlerDeleteSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewUserHandler(repository.NewUserRepo(sqlxDB), repository.NewAuthGroupRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), repository.NewAuditRepo(sqlxDB), repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")))

	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(userRows())
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM auth_group_memberships WHERE user_id = \?`).WithArgs(uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM resource_group_users WHERE user_id = \?`).WithArgs(uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM sessions WHERE user_id = \?`).WithArgs(uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM users WHERE id = \?`).WithArgs(uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))

	req := withURLParam(routeRequest(http.MethodDelete, "/users/7"), "id", "7")
	rec := httptest.NewRecorder()
	handler.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUserHandlerPatchProtectedUserRequiresAllPermissionsAdmin(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewUserHandler(repository.NewUserRepo(sqlxDB), repository.NewAuthGroupRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), repository.NewAuditRepo(sqlxDB), repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")))

	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(1)).
		WillReturnRows(protectedUserRows())

	req := withURLParam(httptest.NewRequest(http.MethodPatch, "/users/1", strings.NewReader(`{"email":"new@example.com"}`)), "id", "1")
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "operator")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.Patch(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUserHandlerResetMFAProtectedUserRequiresAllPermissionsAdmin(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewUserHandler(repository.NewUserRepo(sqlxDB), repository.NewAuthGroupRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), nil, repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")))

	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(1)).
		WillReturnRows(protectedUserRows())
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(99)).
		WillReturnRows(userRowsWithID(99, "operator"))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(uint64(99), sqlmock.AnyArg(), uint64(99), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	req := withURLParam(routeRequest(http.MethodPost, "/users/1/mfa/reset"), "id", "1")
	rec := httptest.NewRecorder()
	handler.ResetMFA(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUserHandlerAddAdminMembershipRequiresAllPermissionsAdmin(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewUserHandler(repository.NewUserRepo(sqlxDB), repository.NewAuthGroupRepo(sqlxDB), nil, nil, repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")))

	mock.ExpectQuery(`SELECT id, group_key, name, description, is_system, is_protected`).
		WithArgs("admin").
		WillReturnRows(authGroupRow(4, "admin", "Admin", "", 1, 1))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(userRows())
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(99)).
		WillReturnRows(userRowsWithID(99, "operator"))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(uint64(99), sqlmock.AnyArg(), uint64(99), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	req := withURLParam(httptest.NewRequest(http.MethodPost, "/users/7/memberships", strings.NewReader(`{"auth_group":"admin"}`)), "id", "7")
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "operator")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.AddMembership(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUserHandlerRemoveMembershipProtectedUserRequiresAllPermissionsAdmin(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewUserHandler(repository.NewUserRepo(sqlxDB), repository.NewAuthGroupRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), repository.NewAuditRepo(sqlxDB), repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")))

	mock.ExpectQuery(`SELECT id, group_key, name, description, is_system, is_protected`).
		WithArgs("admin").
		WillReturnRows(authGroupRow(4, "admin", "Admin", "", 1, 1))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(1)).
		WillReturnRows(protectedUserRows())

	req := withURLParam(routeRequest(http.MethodDelete, "/users/1/memberships/admin"), "id", "1")
	req = withURLParam(req, "group", "admin")
	rec := httptest.NewRecorder()
	handler.RemoveMembership(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUserHandlerPatchCannotRemoveAdminMembershipWithoutAllPermissionsAdmin(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewUserHandler(repository.NewUserRepo(sqlxDB), repository.NewAuthGroupRepo(sqlxDB), nil, nil, repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")))
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(userRows())
	mock.ExpectExec(`UPDATE users SET username=\?, email=\?, lark_recipient=\?, lark_recipient_type=\?, lark_union_id=\?, updated_at=\? WHERE id=\?`).
		WithArgs("alice", "alice@example.com", "", "open_id", "", sqlmock.AnyArg(), uint64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT membership_id AS id, user_id, auth_group, granted_by, expires_at, created_at`).
		WithArgs(uint64(7), sqlmock.AnyArg(), uint64(7), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "auth_group", "granted_by", "expires_at", "created_at"}).
			AddRow(10, 7, "admin", nil, nil, now))
	mock.ExpectQuery(`SELECT id, group_key, name, description, is_system, is_protected`).
		WithArgs("admin").
		WillReturnRows(authGroupRow(4, "admin", "Admin", "", 1, 1))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(99)).
		WillReturnRows(userRowsWithID(99, "operator"))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(uint64(99), sqlmock.AnyArg(), uint64(99), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	req := withURLParam(httptest.NewRequest(http.MethodPatch, "/users/7", strings.NewReader(`{"auth_groups":[]}`)), "id", "7")
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "operator")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.Patch(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "protected auth group can only be changed by all-permissions admin") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthGroupHandlerAddPermissionProtectedGroupRequiresAllPermissionsAdmin(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthGroupHandler(
		repository.NewAuthGroupRepo(sqlxDB),
		repository.NewUserRepo(sqlxDB),
		nil,
	)

	mock.ExpectQuery(`SELECT id, group_key, name, description, is_system, is_protected`).
		WithArgs("admin").
		WillReturnRows(authGroupRow(4, "admin", "Admin", "", 1, 1))
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(99)).
		WillReturnRows(userRowsWithID(99, "operator"))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(uint64(99), sqlmock.AnyArg(), uint64(99), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	req := withURLParam(httptest.NewRequest(http.MethodPost, "/auth-groups/admin/permissions", strings.NewReader(`{"permission_key":"users.write"}`)), "group", "admin")
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "operator")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.AddPermission(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUserHandlerPatchDisableUserRevokesSessions(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewUserHandler(repository.NewUserRepo(sqlxDB), repository.NewAuthGroupRepo(sqlxDB), repository.NewSessionRepo(sqlxDB), repository.NewAuditRepo(sqlxDB), repository.NewDBConnectionRepo(sqlxDB, []byte("01234567890123456789012345678901")))

	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(userRows())
	mock.ExpectExec(`UPDATE users SET username=\?, email=\?, lark_recipient=\?, lark_recipient_type=\?, lark_union_id=\?, updated_at=\? WHERE id=\?`).
		WithArgs("alice", "alice@example.com", "", "open_id", "", sqlmock.AnyArg(), uint64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE users SET is_active=\?, updated_at=\? WHERE id=\?`).
		WithArgs(false, sqlmock.AnyArg(), uint64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE sessions SET revoked_at = \? WHERE user_id = \? AND revoked_at IS NULL`).
		WithArgs(sqlmock.AnyArg(), uint64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))

	req := withURLParam(httptest.NewRequest(http.MethodPatch, "/users/7", strings.NewReader(`{"is_active":false}`)), "id", "7")
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "operator")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.Patch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"is_active":false`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthGroupHandlerList(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthGroupHandler(repository.NewAuthGroupRepo(sqlxDB), repository.NewUserRepo(sqlxDB), nil)

	mock.ExpectQuery(`SELECT id, group_key, name, description, is_system, is_protected`).
		WillReturnRows(authGroupRows())

	for _, group := range []model.AuthGroup{
		model.AuthGroupDeveloper,
		model.AuthGroupReviewer,
		model.AuthGroupDBA,
		model.AuthGroupAdmin,
	} {
		mock.ExpectQuery(`SELECT DISTINCT u\.\*`).
			WithArgs(group, sqlmock.AnyArg(), group, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password", "is_setup", "is_protected", "is_active", "created_at", "updated_at"}))
		mock.ExpectQuery(`SELECT p\.permission_key`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"permission_key"}))
		mock.ExpectQuery(`SELECT db_connection_id`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"db_connection_id"}))
	}

	req := routeRequest(http.MethodGet, "/auth-groups")
	rec := httptest.NewRecorder()
	handler.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"name":"developer"`) || !strings.Contains(rec.Body.String(), `"name":"admin"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthGroupHandlerGet(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthGroupHandler(repository.NewAuthGroupRepo(sqlxDB), repository.NewUserRepo(sqlxDB), nil)

	mock.ExpectQuery(`SELECT id, group_key, name, description, is_system, is_protected`).
		WithArgs("dba").
		WillReturnRows(authGroupRow(3, "dba", "DBA", "Can manage database assets", 1, 0))

	mock.ExpectQuery(`SELECT DISTINCT u\.\*`).
		WithArgs(model.AuthGroupDBA, sqlmock.AnyArg(), model.AuthGroupDBA, sqlmock.AnyArg()).
		WillReturnRows(userRows())
	mock.ExpectQuery(`SELECT p\.permission_key`).
		WithArgs(uint64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"permission_key"}).AddRow("tickets.execute"))
	mock.ExpectQuery(`SELECT db_connection_id`).
		WithArgs(uint64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"db_connection_id"}).AddRow(uint64(7)))

	req := withURLParam(routeRequest(http.MethodGet, "/auth-groups/dba"), "group", "dba")
	rec := httptest.NewRecorder()
	handler.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"name":"dba"`) || !strings.Contains(rec.Body.String(), `"permissions":["tickets.execute"]`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthGroupHandlerCreate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthGroupHandler(
		repository.NewAuthGroupRepo(sqlxDB),
		repository.NewUserRepo(sqlxDB),
		repository.NewAuditRepo(sqlxDB),
	)

	mock.ExpectExec(`INSERT INTO auth_groups`).
		WithArgs("ops", "Ops", "operations", false, false, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectQuery(`SELECT id, group_key, name, description, is_system, is_protected`).
		WithArgs(int64(9)).
		WillReturnRows(authGroupRow(9, "ops", "Ops", "operations", 0, 0))
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM auth_group_permissions WHERE auth_group_id = \?`).
		WithArgs(uint64(9)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM auth_group_db_connections WHERE auth_group_id = \?`).
		WithArgs(uint64(9)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT id, group_key, name, description, is_system, is_protected`).
		WithArgs("ops").
		WillReturnRows(authGroupRow(9, "ops", "Ops", "operations", 0, 0))

	req := httptest.NewRequest(http.MethodPost, "/auth-groups", strings.NewReader(`{"name":"Ops","description":"operations","user_ids":[],"permissions":[],"db_connection_ids":[]}`))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, uint64(99))
	ctx = context.WithValue(ctx, middleware.CtxUsername, "operator")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if !strings.Contains(rec.Body.String(), `"GroupKey":"ops"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAuthGroupHandlerDeleteProtectedGroupReturnsConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewAuthGroupHandler(
		repository.NewAuthGroupRepo(sqlxDB),
		repository.NewUserRepo(sqlxDB),
		nil,
	)

	mock.ExpectQuery(`SELECT id, group_key, name, description, is_system, is_protected`).
		WithArgs("admin").
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_key", "name", "description", "is_system", "is_protected"}).
			AddRow(4, "admin", "Admin", "", 1, 1))

	req := withURLParam(routeRequest(http.MethodDelete, "/auth-groups/admin"), "group", "admin")
	rec := httptest.NewRecorder()
	handler.Delete(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if !strings.Contains(rec.Body.String(), "protected auth group cannot be deleted") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
