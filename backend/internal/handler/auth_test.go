package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jmoiron/sqlx"
)

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
	mock.ExpectQuery(`SELECT auth_group FROM auth_group_memberships`).
		WithArgs(userID, sqlmock.AnyArg()).
		WillReturnRows(
			sqlmock.NewRows([]string{"auth_group"}).
				AddRow(string(model.AuthGroupReviewer)).
				AddRow(string(model.AuthGroupDBA)),
		)

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxUsername, "alice")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	middleware.InjectAuthGroups(userRepo)(http.HandlerFunc(handler.Me)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got struct {
		ID         uint64            `json:"id"`
		Username   string            `json:"username"`
		AuthGroups []model.AuthGroup `json:"auth_groups"`
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
	if got.AuthGroups[0] != model.AuthGroupReviewer || got.AuthGroups[1] != model.AuthGroupDBA {
		t.Fatalf("auth_groups = %#v, want [%q %q]", got.AuthGroups, model.AuthGroupReviewer, model.AuthGroupDBA)
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
	mock.ExpectQuery(`SELECT auth_group FROM auth_group_memberships`).
		WithArgs(userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"auth_group"}))

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxUsername, "bob")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	middleware.InjectAuthGroups(userRepo)(http.HandlerFunc(handler.Me)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"auth_groups":[]`) {
		t.Fatalf("body = %s, want auth_groups to be [] instead of null", body)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
