package repository

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

func newTableSchemaSyncRepoTest(t *testing.T) (*TableSchemaSyncRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewTableSchemaSyncRepo(sqlx.NewDb(db, "sqlmock")), mock
}

func TestTableSchemaSyncCreateRollsBackWhenAnyItemFails(t *testing.T) {
	repo, mock := newTableSchemaSyncRepoTest(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO table_schema_sync_jobs").WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectExec("INSERT INTO table_schema_sync_job_items").WillReturnError(errors.New("item insert failed"))
	mock.ExpectRollback()
	_, err := repo.Create(context.Background(), TableSchemaSyncCreateInput{RequestedBy: 7, SourceConnectionID: 1, TargetConnectionID: 2, SourceDatabase: "source", TargetDatabase: "target", Items: []TableSchemaSyncItemInput{{TableName: "orders", SourceDDLSHA256: "hash"}}})
	if err == nil {
		t.Fatal("partial job must roll back when an item cannot be persisted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTableSchemaSyncCreateMapsActiveTargetConflict(t *testing.T) {
	repo, mock := newTableSchemaSyncRepoTest(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO table_schema_sync_jobs").WillReturnError(&mysqlDriver.MySQLError{Number: 1062, Message: "duplicate active target"})
	mock.ExpectRollback()
	_, err := repo.Create(context.Background(), TableSchemaSyncCreateInput{TargetConnectionID: 2, TargetDatabase: "target", Items: []TableSchemaSyncItemInput{{TableName: "orders"}}})
	if !errors.Is(err, ErrTableSchemaSyncTargetActive) {
		t.Fatalf("error = %v", err)
	}
}

func TestTableSchemaSyncClaimIsConditional(t *testing.T) {
	repo, mock := newTableSchemaSyncRepoTest(t)
	mock.ExpectExec("UPDATE table_schema_sync_jobs SET status = 'running'").WillReturnResult(sqlmock.NewResult(0, 0))
	ok, err := repo.Claim(context.Background(), 42)
	if err != nil || ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	mock.ExpectExec("UPDATE table_schema_sync_jobs SET status = 'running'").WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err = repo.Claim(context.Background(), 43)
	if err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
}

func TestTableSchemaSyncInterruptReleasesActiveLocks(t *testing.T) {
	repo, mock := newTableSchemaSyncRepoTest(t)
	mock.ExpectExec("UPDATE table_schema_sync_jobs SET status = 'interrupted', active_target_key = NULL").WillReturnResult(sqlmock.NewResult(0, 2))
	count, err := repo.InterruptRunning(context.Background())
	if err != nil || count != 2 {
		t.Fatalf("interrupt = %d, %v", count, err)
	}
}

func TestTableSchemaSyncListScopedAppliesBothConnectionScopesBeforePagination(t *testing.T) {
	repo, mock := newTableSchemaSyncRepoTest(t)
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM table_schema_sync_jobs WHERE source_connection_id IN \\(\\?,\\?\\) AND target_connection_id IN \\(\\?,\\?\\)").WithArgs(uint64(1), uint64(2), uint64(1), uint64(2)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery("SELECT \\* FROM table_schema_sync_jobs WHERE source_connection_id IN \\(\\?,\\?\\) AND target_connection_id IN \\(\\?,\\?\\) ORDER BY created_at DESC, id DESC LIMIT \\? OFFSET \\?").WithArgs(uint64(1), uint64(2), uint64(1), uint64(2), uint(20), uint(40)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	jobs, total, err := repo.ListScoped(context.Background(), []uint64{1, 2}, 20, 40)
	if err != nil || total != 3 || len(jobs) != 0 { t.Fatalf("jobs = %+v, total = %d, err = %v", jobs, total, err) }
	if err := mock.ExpectationsWereMet(); err != nil { t.Fatal(err) }
}
