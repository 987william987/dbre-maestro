package repository

import (
	"context"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestExportRepoMarkDownloadedConsumesTokenOnce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewExportRepo(sqlx.NewDb(db, "sqlmock"))
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE export_requests SET downloaded_at = ? WHERE download_token = ? AND downloaded_at IS NULL`)).
		WithArgs(sqlmock.AnyArg(), token).
		WillReturnResult(sqlmock.NewResult(0, 1))

	downloaded, err := repo.MarkDownloaded(context.Background(), token)
	if err != nil {
		t.Fatalf("MarkDownloaded() error = %v", err)
	}
	if !downloaded {
		t.Fatal("MarkDownloaded() should report first token consumption")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func TestExportRepoMarkDownloadedRejectsUsedToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewExportRepo(sqlx.NewDb(db, "sqlmock"))
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE export_requests SET downloaded_at = ? WHERE download_token = ? AND downloaded_at IS NULL`)).
		WithArgs(sqlmock.AnyArg(), token).
		WillReturnResult(sqlmock.NewResult(0, 0))

	downloaded, err := repo.MarkDownloaded(context.Background(), token)
	if err != nil {
		t.Fatalf("MarkDownloaded() error = %v", err)
	}
	if downloaded {
		t.Fatal("MarkDownloaded() should reject an already-used token")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}
