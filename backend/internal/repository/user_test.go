package repository

import (
	"context"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestUserRepoUpdateLarkUnionIDSkipsAlreadyBoundUnionRecipient(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewUserRepo(sqlx.NewDb(db, "sqlmock"))
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET lark_union_id = ?, lark_recipient_type = 'union_id', updated_at = ?
		WHERE id = ? AND (lark_union_id = '' OR (lark_union_id = ? AND lark_recipient_type <> 'union_id'))
	`)).
		WithArgs("on_union", sqlmock.AnyArg(), uint64(42), "on_union").
		WillReturnResult(sqlmock.NewResult(0, 0))

	updated, err := repo.UpdateLarkUnionID(context.Background(), 42, "on_union")
	if err != nil {
		t.Fatalf("UpdateLarkUnionID() error = %v", err)
	}
	if updated {
		t.Fatalf("UpdateLarkUnionID() updated = true, want false for already-bound union recipient")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}
