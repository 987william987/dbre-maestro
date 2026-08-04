package handler

import (
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
		}).AddRow(executionID, ticketID, 1, ticket.SQLContent, "running", nil, nil, time.Now(), nil, nil))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE ticket_executions SET status = ?, rows_affected = ?, error_msg = ?, completed_at = ?, duration_ms = ? WHERE id = ?`)).
		WithArgs("failed", nil, sqlmock.AnyArg(), sqlmock.AnyArg(), nil, executionID).
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
