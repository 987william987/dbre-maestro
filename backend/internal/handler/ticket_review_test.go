package handler

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jmoiron/sqlx"
)

func TestSanitizeMySQLShadowValidationError(t *testing.T) {
	t.Run("sanitizes shadow database privilege failure", func(t *testing.T) {
		got := sanitizeMySQLShadowValidationError(assertErr("create shadow database failed: Error 1044 (42000): Access denied for user 'maestro_app'@'%' to database 'shadow_demo'"))
		want := "shadow validation is not available because the platform validation database privilege is not configured"
		if got != want {
			t.Fatalf("expected sanitized message %q, got %q", want, got)
		}
	})

	t.Run("keeps business validation errors readable", func(t *testing.T) {
		got := sanitizeMySQLShadowValidationError(assertErr("database \"foo\" already exists"))
		if got != "database \"foo\" already exists" {
			t.Fatalf("unexpected sanitized message: %q", got)
		}
	})
}

func assertErr(message string) error {
	return testError(message)
}

type testError string

func (e testError) Error() string {
	return string(e)
}

func TestCanViewFullTicketQueueAllowsDBAGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	userID := uint64(7)
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM users WHERE id = \?`).
		WithArgs(userID).
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
		}).AddRow(userID, "fly", "fly@example.com", "", "hash", false, false, true, now, now))
	mock.ExpectQuery(`SELECT EXISTS \(`).
		WithArgs(userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT DISTINCT auth_group`).
		WithArgs(userID, sqlmock.AnyArg(), userID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"auth_group"}).AddRow(model.AuthGroupDBA))

	handler := &TicketHandler{users: repository.NewUserRepo(sqlx.NewDb(db, "sqlmock"))}
	allowed, err := handler.canViewFullTicketQueue(context.Background(), userID)
	if err != nil {
		t.Fatalf("canViewFullTicketQueue() error = %v", err)
	}
	if !allowed {
		t.Fatal("DBA group should be allowed to view the full ticket queue")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}
