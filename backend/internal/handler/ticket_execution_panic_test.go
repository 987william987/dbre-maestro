package handler

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/jmoiron/sqlx"
)

func TestRecoverTicketExecutionPanicMarksRunningStatementAndTicketFailed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := &TicketHandler{
		tickets: repository.NewTicketRepo(sqlxDB),
		audit:   repository.NewAuditRepo(sqlxDB),
	}
	ticketID := uint64(77)
	executionID := uint64(501)
	executorID := uint64(2)
	ticket := &model.Ticket{
		ID:          ticketID,
		TicketNo:    "TK-PANIC",
		Title:       "panic recovery",
		SQLContent:  "ALTER TABLE t ADD COLUMN c INT",
		TicketType:  model.TicketTypeDDL,
		Status:      model.TicketStatusExecuting,
		SubmitterID: 1,
		ExecutorID:  &executorID,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM ticket_executions WHERE ticket_id = ? ORDER BY seq`)).
		WithArgs(ticketID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"ticket_id",
			"seq",
			"sql_stmt",
			"status",
			"rows_affected",
			"error_msg",
			"started_at",
			"completed_at",
			"duration_ms",
			"sent_to_db_at",
			"db_process_type",
			"db_process_id",
			"interruption_reason",
			"outcome_confidence",
		}).AddRow(executionID, ticketID, 1, ticket.SQLContent, "running", nil, nil, time.Now(), nil, nil, time.Now(), "mysql_thread_id", uint64(123), nil, "sent_to_db"))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE ticket_executions
		 SET status = 'failed',
		     rows_affected = NULL,
		     error_msg = ?,
		     completed_at = ?,
		     duration_ms = NULL,
		     interruption_reason = ?,
		     outcome_confidence = ?
		 WHERE id = ?`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "execution_panic", ticketExecutionOutcomeUnknown, executionID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE tickets SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?`)).
		WithArgs(model.TicketStatusFailed, sqlmock.AnyArg(), sqlmock.AnyArg(), ticketID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	func() {
		defer handler.recoverTicketExecutionPanic(ticket, executorID, ticketExecutionRunOptions{}, nil)
		panic("boom")
	}()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func TestRecoverTicketExecutionPanicDoesNothingWithoutPanic(t *testing.T) {
	handler := &TicketHandler{}
	func() {
		defer handler.recoverTicketExecutionPanic(&model.Ticket{ID: 1}, 0, ticketExecutionRunOptions{}, nil)
	}()
}

func TestCancelActiveExecutionsForShutdownCancelsAndMarksExecutionFailed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "sqlmock")
	handler := &TicketHandler{
		tickets:          repository.NewTicketRepo(sqlxDB),
		activeExecutions: newActiveSQLQueryRegistry(),
	}
	ticketID := uint64(77)
	executionID := uint64(501)
	cancelCalled := false

	handler.activeExecutions.register(ticketExecutionQueryID(executionID), activeSQLQuery{
		UserID:       2,
		ConnectionID: 9,
		TicketID:     ticketID,
		DBType:       "postgres",
		PostgresPID:  1234,
		Statement:    "SELECT pg_sleep(60)",
		Conn:         &model.DBConnection{ID: 9, Name: "pg-prod", DBType: "postgres"},
		Cancel:       func() { cancelCalled = true },
		CancelDBOpener: func(context.Context) (*sql.DB, string, func(), error) {
			return db, model.DBCredentialRoleReadwrite, func() {}, nil
		},
		RegisteredAt: time.Now(),
	})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT pg_cancel_backend($1)`)).
		WithArgs(uint64(1234)).
		WillReturnRows(sqlmock.NewRows([]string{"pg_cancel_backend"}).AddRow(true))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE ticket_executions
		 SET status = 'failed',
		     rows_affected = NULL,
		     error_msg = ?,
		     completed_at = ?,
		     duration_ms = NULL,
		     interruption_reason = ?,
		     outcome_confidence = ?
		 WHERE id = ?`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "service_shutdown", ticketExecutionOutcomeServiceShutdown, executionID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM tickets WHERE id = ?`)).
		WithArgs(ticketID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "execution_run_mode"}).AddRow(ticketID, model.TicketExecutionRunModeBatch))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM ticket_executions WHERE ticket_id = ? ORDER BY seq`)).
		WithArgs(ticketID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"ticket_id",
			"seq",
			"sql_stmt",
			"status",
			"rows_affected",
			"error_msg",
			"started_at",
			"completed_at",
			"duration_ms",
			"sent_to_db_at",
			"db_process_type",
			"db_process_id",
			"interruption_reason",
			"outcome_confidence",
		}).AddRow(executionID, ticketID, 1, "SELECT pg_sleep(60)", "failed", nil, "service shutdown during execution; database query cancellation completed", time.Now(), time.Now(), nil, time.Now(), "postgres_pid", uint64(1234), "service_shutdown", ticketExecutionOutcomeServiceShutdown))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE tickets SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?`)).
		WithArgs(model.TicketStatusFailed, sqlmock.AnyArg(), sqlmock.AnyArg(), ticketID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	canceled := handler.CancelActiveExecutionsForShutdown(context.Background())
	if canceled != 1 {
		t.Fatalf("CancelActiveExecutionsForShutdown() = %d, want 1", canceled)
	}
	if !cancelCalled {
		t.Fatal("active execution context cancel was not called")
	}
	if _, ok := handler.activeExecutions.cancelAny(ticketExecutionQueryID(executionID)); ok {
		t.Fatal("active execution should be removed after shutdown cancellation")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}
