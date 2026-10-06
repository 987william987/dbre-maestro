package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/onlineddl"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

func newOnlineDDLRepoTest(t *testing.T) (*OnlineDDLRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewOnlineDDLRepo(sqlx.NewDb(db, "sqlmock")), mock
}

func TestOnlineDDLGetByIDAllowsPlannedRunWithoutArtifactSummary(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectQuery("SELECT \\* FROM ticket_online_ddl_runs WHERE id = \\?").
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"artifact_summary"}).AddRow(nil))

	run, err := repo.GetByID(context.Background(), 7)
	if err != nil {
		t.Fatalf("planned run without artifacts must remain readable: %v", err)
	}
	if run == nil || run.ArtifactSummary != nil {
		t.Fatalf("run = %#v, want run with nil artifact summary", run)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLTransitionUsesOCCAndReleasesTerminalLock(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET status").WithArgs(onlineddl.StatusCompleted, nil, onlineddl.StatusCompleted, sqlmock.AnyArg(), onlineddl.StatusCompleted, sqlmock.AnyArg(), sqlmock.AnyArg(), uint64(7), onlineddl.StatusRunning, uint64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.Transition(context.Background(), 7, 3, onlineddl.StatusRunning, onlineddl.StatusCompleted); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET status").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.Transition(context.Background(), 7, 3, onlineddl.StatusRunning, onlineddl.StatusCompleted); !errors.Is(err, onlineddl.ErrStaleVersion) {
		t.Fatalf("error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLDoubleConnectionClaimReturnsStableConflict(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET status").WillReturnError(&mysqlDriver.MySQLError{Number: 1062})
	if err := repo.Transition(context.Background(), 9, 1, onlineddl.StatusPlanned, onlineddl.StatusQueued); !errors.Is(err, onlineddl.ErrConflict) {
		t.Fatalf("error = %v", err)
	}
}

func TestOnlineDDLInterruptActiveReleasesAllLocks(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET status = 'interrupted', eta_seconds = NULL, eta_display = NULL, active_connection_id = NULL").WillReturnResult(sqlmock.NewResult(0, 4))
	count, err := repo.InterruptActive(context.Background())
	if err != nil || count != 4 {
		t.Fatalf("count = %d, error = %v", count, err)
	}
}

func TestOnlineDDLEventInsertAndBoundedCleanupAreAtomic(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO ticket_online_ddl_events").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("DELETE FROM ticket_online_ddl_events").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	if err := repo.AppendEvent(context.Background(), model.OnlineDDLEvent{RunID: 5, EventType: "progress"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLRunnerLifecycleUpdatesAreConditional(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET status = 'running', phase = 'starting'").WillReturnResult(sqlmock.NewResult(0, 1))
	claimed, err := repo.Claim(context.Background(), 7, 2)
	if err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET heartbeat_at = \\?, updated_at = \\? WHERE id = \\? AND status IN \\('running','paused','cancel_requested'\\)").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	alive, err := repo.Heartbeat(context.Background(), 7)
	if err != nil || !alive {
		t.Fatalf("alive=%v err=%v", alive, err)
	}
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET status = \\?, phase = 'finished', eta_seconds = NULL").WithArgs(onlineddl.StatusFailed, "heartbeat_failed", sqlmock.AnyArg(), sqlmock.AnyArg(), uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	finished, err := repo.FinishRunning(context.Background(), 7, onlineddl.StatusFailed, "heartbeat_failed")
	if err != nil || !finished {
		t.Fatalf("finished=%v err=%v", finished, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLSaveProgressUpdatesLatestAndBoundedHistoryAtomically(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	percent := 25.0
	copied, eta := uint64(500), "00:01:30"
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET phase").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO ticket_online_ddl_events").WithArgs(uint64(8), "copy", &percent, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("DELETE FROM ticket_online_ddl_events").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	ok, err := repo.SaveProgress(context.Background(), 8, onlineddl.ProgressSnapshot{Phase: "copy", ProgressPercent: &percent, CopiedRows: &copied, ETADisplay: &eta}, true)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLControlStateUsesOCC(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET status = \\?, pause_requested").WithArgs(onlineddl.StatusPaused, true, sqlmock.AnyArg(), uint64(9), onlineddl.StatusRunning, uint64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err := repo.SetPaused(context.Background(), 9, 3, true)
	if err != nil || !ok {
		t.Fatalf("pause ok=%v err=%v", ok, err)
	}
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET status = 'cancel_requested'").WithArgs(sqlmock.AnyArg(), uint64(9), uint64(4)).WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err = repo.RequestCancel(context.Background(), 9, 4)
	if err != nil || !ok {
		t.Fatalf("cancel ok=%v err=%v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLRuntimeTuningUpdateAndEventAreAtomic(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	before, after := []byte(`{"schema_version":"v1"}`), []byte(`{"schema_version":"v1","ghost":{}}`)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET effective_parameters").WithArgs(after, sqlmock.AnyArg(), uint64(9), uint64(4)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO ticket_online_ddl_events").WithArgs(uint64(9), sqlmock.AnyArg(), before, after, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("DELETE FROM ticket_online_ddl_events").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	actor := uint64(3)
	ok, err := repo.UpdateEffectiveParameters(context.Background(), 9, 4, &actor, before, after)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLRuntimeTuningStaleUpdateDoesNotAppendEvent(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET effective_parameters").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	ok, err := repo.UpdateEffectiveParameters(context.Background(), 9, 4, nil, []byte(`{}`), []byte(`{}`))
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLRuntimeTuningEventFailureRollsBackParameterUpdate(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET effective_parameters").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO ticket_online_ddl_events").WillReturnError(errors.New("event insert failed"))
	mock.ExpectRollback()
	ok, err := repo.UpdateEffectiveParameters(context.Background(), 9, 4, nil, []byte(`{}`), []byte(`{}`))
	if err == nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLCreateQueuedAtomicallyPersistsParametersAndClaimsStatement(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM ticket_online_ddl_runs").WithArgs(uint64(2), uint64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO ticket_online_ddl_runs").WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectExec("UPDATE ticket_executions SET status = 'running'").WithArgs(sqlmock.AnyArg(), uint64(3), uint64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT \\* FROM ticket_online_ddl_runs").WithArgs(uint64(9)).WillReturnRows(sqlmock.NewRows([]string{"status", "artifact_summary"}).AddRow(onlineddl.StatusQueued, nil))
	run, err := repo.CreateQueued(context.Background(), OnlineDDLCreateInput{TicketID: 2, ExecutionID: 3, ConnectionID: 8, ExecutorID: 4, Mode: onlineddl.ModeGhost, SQLSHA256: "sql", PreflightSHA256: "preflight", ToolVersion: "1.1.6", Parameters: onlineddl.DefaultParameters(onlineddl.ModeGhost)})
	if err != nil || run == nil || run.Status != onlineddl.StatusQueued {
		t.Fatalf("run=%#v err=%v", run, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLFinishOutcomePersistsArtifactsAndReleasesLock(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET status = \\?, phase = 'finished', eta_seconds = NULL").WithArgs(onlineddl.StatusCancelledArtifacts, onlineddl.ErrCancelledArtifacts.Error(), "verified", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), uint64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err := repo.FinishOutcome(context.Background(), 11, onlineddl.StatusCancelledArtifacts, onlineddl.ErrCancelledArtifacts.Error(), "verified", onlineddl.ArtifactSummary{Items: []onlineddl.Artifact{{Kind: "ghost_table", Name: "_orders_gho", Exists: true}}})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineDDLRecordRecoveryOnlyTouchesInterruptedRun(t *testing.T) {
	repo, mock := newOnlineDDLRepoTest(t)
	mock.ExpectExec("UPDATE ticket_online_ddl_runs SET outcome_confidence").WithArgs("artifacts_present", sqlmock.AnyArg(), sqlmock.AnyArg(), uint64(12)).WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err := repo.RecordRecovery(context.Background(), 12, "artifacts_present", onlineddl.ArtifactSummary{Items: []onlineddl.Artifact{{Kind: "old_table", Exists: true}}})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
