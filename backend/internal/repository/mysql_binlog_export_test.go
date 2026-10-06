package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

func newMySQLBinlogExportTestRepo(t *testing.T) (*MySQLBinlogExportRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewMySQLBinlogExportRepo(sqlx.NewDb(db, "sqlmock"), []byte("01234567890123456789012345678901")), mock
}

func TestMySQLBinlogExportClaimIsConditional(t *testing.T) {
	repo, mock := newMySQLBinlogExportTestRepo(t)
	mock.ExpectExec("UPDATE mysql_binlog_export_jobs").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), uint64(42)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	changed, err := repo.Claim(context.Background(), 42)
	if err != nil || changed {
		t.Fatalf("Claim() = (%v, %v), want (false, nil)", changed, err)
	}
	mock.ExpectExec("UPDATE mysql_binlog_export_jobs").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), uint64(43)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	changed, err = repo.Claim(context.Background(), 43)
	if err != nil || !changed {
		t.Fatalf("Claim() = (%v, %v), want (true, nil)", changed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLBinlogExportRequestCancelPreservesQueuedAndRunningSemantics(t *testing.T) {
	repo, mock := newMySQLBinlogExportTestRepo(t)
	mock.ExpectExec("phase = CASE WHEN status = 'queued' THEN 'cancelled' ELSE phase END").
		WithArgs(sqlmock.AnyArg(), uint64(7), sqlmock.AnyArg(), uint64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	changed, err := repo.RequestCancel(context.Background(), 42, 7)
	if err != nil || !changed {
		t.Fatalf("RequestCancel() = (%v, %v), want (true, nil)", changed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLBinlogExportArtifactRoundTripAndTamperDetection(t *testing.T) {
	repo, _ := newMySQLBinlogExportTestRepo(t)
	inputs := []MySQLBinlogExportArtifactInput{
		{Kind: model.MySQLBinlogExportArtifactForwardSQL, SQL: "INSERT INTO t VALUES (1);", StatementCount: 1, ExpiresAt: time.Now()},
		{Kind: model.MySQLBinlogExportArtifactRollbackSQL, SQL: "DELETE FROM t WHERE id = 1;", StatementCount: 1, ExpiresAt: time.Now()},
	}
	artifacts, err := repo.encodeArtifacts(inputs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.DecryptArtifact(&artifacts[0])
	if err != nil || got != inputs[0].SQL {
		t.Fatalf("DecryptArtifact() = (%q, %v)", got, err)
	}
	artifacts[0].PlaintextSHA256 = strings.Repeat("0", 64)
	if _, err := repo.DecryptArtifact(&artifacts[0]); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum error, got %v", err)
	}
	artifacts[1].SQLEncrypted[len(artifacts[1].SQLEncrypted)-1] ^= 1
	if _, err := repo.DecryptArtifact(&artifacts[1]); err == nil || !strings.Contains(err.Error(), "decrypt") {
		t.Fatalf("expected decrypt error, got %v", err)
	}
}

func TestMySQLBinlogExportArtifactsRequireForwardAndRollback(t *testing.T) {
	repo, _ := newMySQLBinlogExportTestRepo(t)
	duplicate := []MySQLBinlogExportArtifactInput{
		{Kind: model.MySQLBinlogExportArtifactForwardSQL},
		{Kind: model.MySQLBinlogExportArtifactForwardSQL},
	}
	if _, err := repo.encodeArtifacts(duplicate); err == nil {
		t.Fatal("expected duplicate artifact kinds to fail")
	}
}

func TestMySQLBinlogExportCompleteCommitsArtifactsAndStatusTogether(t *testing.T) {
	repo, mock := newMySQLBinlogExportTestRepo(t)
	mock.ExpectBegin()
	for range 2 {
		mock.ExpectExec("INSERT INTO mysql_binlog_export_artifacts").
			WithArgs(uint64(42), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
	}
	mock.ExpectExec("UPDATE mysql_binlog_export_jobs SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	changed, err := repo.Complete(context.Background(), 42, MySQLBinlogExportCompletion{}, validArtifactInputs())
	if err != nil || !changed {
		t.Fatalf("Complete() = (%v, %v), want (true, nil)", changed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLBinlogExportCompletePersistsArtifactExpiryOnJob(t *testing.T) {
	repo, mock := newMySQLBinlogExportTestRepo(t)
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	inputs := validArtifactInputs()
	inputs[0].ExpiresAt, inputs[1].ExpiresAt = expiresAt, expiresAt
	mock.ExpectBegin()
	for range 2 {
		mock.ExpectExec("INSERT INTO mysql_binlog_export_artifacts").WillReturnResult(sqlmock.NewResult(1, 1))
	}
	mock.ExpectExec("artifact_expires_at = \\?").WithArgs(
		sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		expiresAt, sqlmock.AnyArg(), sqlmock.AnyArg(), uint64(42),
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	changed, err := repo.Complete(context.Background(), 42, MySQLBinlogExportCompletion{}, inputs)
	if err != nil || !changed {
		t.Fatalf("Complete() = (%v, %v), want persisted expiry", changed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLBinlogExportCompleteRollsBackWhenJobNoLongerRunning(t *testing.T) {
	repo, mock := newMySQLBinlogExportTestRepo(t)
	mock.ExpectBegin()
	for range 2 {
		mock.ExpectExec("INSERT INTO mysql_binlog_export_artifacts").WillReturnResult(sqlmock.NewResult(1, 1))
	}
	mock.ExpectExec("UPDATE mysql_binlog_export_jobs SET").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	changed, err := repo.Complete(context.Background(), 42, MySQLBinlogExportCompletion{}, validArtifactInputs())
	if err != nil || changed {
		t.Fatalf("Complete() = (%v, %v), want (false, nil)", changed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLBinlogExportCompleteRollsBackWhenSecondArtifactInsertFails(t *testing.T) {
	repo, mock := newMySQLBinlogExportTestRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO mysql_binlog_export_artifacts").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO mysql_binlog_export_artifacts").WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()
	changed, err := repo.Complete(context.Background(), 42, MySQLBinlogExportCompletion{}, validArtifactInputs())
	if err == nil || changed {
		t.Fatalf("Complete() = (%v, %v), want rollback error", changed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLBinlogExportGetArtifactHidesPurgedRows(t *testing.T) {
	repo, mock := newMySQLBinlogExportTestRepo(t)
	mock.ExpectQuery("purged_at IS NULL").WithArgs(uint64(42), model.MySQLBinlogExportArtifactForwardSQL).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	artifact, err := repo.GetArtifact(context.Background(), 42, model.MySQLBinlogExportArtifactForwardSQL)
	if err != nil || artifact != nil {
		t.Fatalf("GetArtifact() = (%v, %v), want unavailable", artifact, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLBinlogExportInterruptAndPurgeAreBounded(t *testing.T) {
	repo, mock := newMySQLBinlogExportTestRepo(t)
	mock.ExpectExec("WHERE status IN \\('queued', 'running', 'cancel_requested'\\)").
		WillReturnResult(sqlmock.NewResult(0, 3))
	count, err := repo.InterruptActive(context.Background())
	if err != nil || count != 3 {
		t.Fatalf("InterruptActive() = (%d, %v)", count, err)
	}
	mock.ExpectExec("ORDER BY expires_at ASC LIMIT \\?").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), uint(25)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	count, err = repo.PurgeExpired(context.Background(), 25)
	if err != nil || count != 2 {
		t.Fatalf("PurgeExpired() = (%d, %v)", count, err)
	}
	before := time.Now().UTC().Add(-90 * 24 * time.Hour)
	mock.ExpectExec("DELETE FROM mysql_binlog_export_jobs").WithArgs(before, uint(25)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	count, err = repo.PurgeMetadata(context.Background(), before, 25)
	if err != nil || count != 2 {
		t.Fatalf("PurgeMetadata() = (%d, %v)", count, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLBinlogExportListScopedDoesNotReturnOutOfScopeJobs(t *testing.T) {
	repo, mock := newMySQLBinlogExportTestRepo(t)
	mock.ExpectQuery("SELECT \\* FROM mysql_binlog_export_jobs").WithArgs(uint64(7), uint64(9), 25, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(12))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM mysql_binlog_export_jobs").WithArgs(uint64(7), uint64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	jobs, total, err := repo.ListScoped(context.Background(), []uint64{7, 9}, 25, 0)
	if err != nil || total != 1 || len(jobs) != 1 || jobs[0].ID != 12 {
		t.Fatalf("ListScoped() = (%v, %d, %v)", jobs, total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func validArtifactInputs() []MySQLBinlogExportArtifactInput {
	return []MySQLBinlogExportArtifactInput{
		{Kind: model.MySQLBinlogExportArtifactForwardSQL, SQL: "INSERT;", ExpiresAt: time.Now()},
		{Kind: model.MySQLBinlogExportArtifactRollbackSQL, SQL: "DELETE;", ExpiresAt: time.Now()},
	}
}
