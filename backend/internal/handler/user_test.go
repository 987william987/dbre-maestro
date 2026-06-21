package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jmoiron/sqlx"
)

func userCreateRows(id uint64, username string, email string) *sqlmock.Rows {
	now := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{
		"id",
		"username",
		"email",
		"lark_recipient",
		"password",
		"is_setup",
		"is_protected",
		"is_active",
		"created_at",
		"updated_at",
	}).AddRow(id, username, email, "", "hash", 0, 0, 1, now, now)
}

func TestUserHandlerCreateRejectsDuplicateUsername(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewUserHandler(repository.NewUserRepo(sqlxDB), nil, nil, nil, nil)

	mock.ExpectQuery(`SELECT \* FROM users WHERE username = \?`).
		WithArgs("william").
		WillReturnRows(userCreateRows(7, "william", "old@example.com"))

	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"username":" william ","email":"william@edgex.exchange","password":"Secret123!"}`))
	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "username already exists") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUserHandlerCreateRejectsDuplicateEmail(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := NewUserHandler(repository.NewUserRepo(sqlxDB), nil, nil, nil, nil)

	mock.ExpectQuery(`SELECT \* FROM users WHERE username = \?`).
		WithArgs("william").
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"username",
			"email",
			"lark_recipient",
			"password",
			"is_setup",
			"is_protected",
			"is_active",
			"created_at",
			"updated_at",
		}))
	mock.ExpectQuery(`SELECT \* FROM users WHERE email = \?`).
		WithArgs("william@edgex.exchange").
		WillReturnRows(userCreateRows(8, "someone", "william@edgex.exchange"))

	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"username":"william","email":" william@edgex.exchange ","password":"Secret123!"}`))
	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "email already exists") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
