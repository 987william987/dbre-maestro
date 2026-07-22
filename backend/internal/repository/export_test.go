package repository

import (
	"context"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestExportRepoMarkDownloadedRecordsFirstDownloadByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewExportRepo(sqlx.NewDb(db, "sqlmock"))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE export_requests SET downloaded_at = COALESCE(downloaded_at, ?) WHERE id = ?`)).
		WithArgs(sqlmock.AnyArg(), uint64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.MarkDownloaded(context.Background(), 42); err != nil {
		t.Fatalf("MarkDownloaded() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func TestExportRepoMarkDownloadedByTokenAllowsRepeatedDownloads(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewExportRepo(sqlx.NewDb(db, "sqlmock"))
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE export_requests SET downloaded_at = COALESCE(downloaded_at, ?) WHERE download_token = ?`)).
		WithArgs(sqlmock.AnyArg(), token).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.MarkDownloadedByToken(context.Background(), token); err != nil {
		t.Fatalf("MarkDownloadedByToken() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}
