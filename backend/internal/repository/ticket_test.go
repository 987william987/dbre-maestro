package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

func TestTicketListIncludesWorkflowSnapshotReviewer(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewTicketRepo(sqlx.NewDb(db, "sqlmock"))
	reviewerID := uint64(7)
	createdAt := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM tickets t LEFT JOIN users u ON u.id = t.submitter_id WHERE 1=1 AND (t.submitter_id = ? OR EXISTS (
			SELECT 1
			FROM ticket_workflow_snapshots tws
			WHERE tws.ticket_id = t.id
			  AND (
			    JSON_CONTAINS(tws.approval_user_ids, ?)
			    OR JSON_CONTAINS(tws.admin_user_ids, ?)
			    OR (t.status = ? AND JSON_CONTAINS(tws.executor_user_ids, ?))
			  )
		))`)).
		WithArgs(reviewerID, "7", "7", model.TicketStatusPendingExecution, "7").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT t.* FROM tickets t
		LEFT JOIN users u ON u.id = t.submitter_id WHERE 1=1 AND (t.submitter_id = ? OR EXISTS (
			SELECT 1
			FROM ticket_workflow_snapshots tws
			WHERE tws.ticket_id = t.id
			  AND (
			    JSON_CONTAINS(tws.approval_user_ids, ?)
			    OR JSON_CONTAINS(tws.admin_user_ids, ?)
			    OR (t.status = ? AND JSON_CONTAINS(tws.executor_user_ids, ?))
			  )
		)) ORDER BY CASE WHEN t.status IN (?, ?, ?) THEN 0 ELSE 1 END, t.created_at DESC LIMIT ? OFFSET ?`)).
		WithArgs(
			reviewerID,
			"7",
			"7",
			model.TicketStatusPendingExecution,
			"7",
			model.TicketStatusPendingReview,
			model.TicketStatusPendingExecution,
			model.TicketStatusNeedsAdminAttention,
			20,
			0,
		).
		WillReturnRows(ticketRows().AddRow(
			uint64(1),
			"TK-1",
			"Query Access",
			nil,
			"",
			model.TicketTypeQueryAccess,
			nil,
			nil,
			nil,
			model.TicketStatusPendingReview,
			uint64(2),
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			createdAt,
			updatedAt,
		))

	tickets, total, err := repo.List(context.Background(), TicketListFilter{VisibleToUserID: &reviewerID}, 20, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(tickets) != 1 || tickets[0].ID != 1 {
		t.Fatalf("tickets = %#v, want ticket 1", tickets)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func TestTicketListFiltersByTicketNoTitleAndSubmitter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewTicketRepo(sqlx.NewDb(db, "sqlmock"))
	ticketNo := "TK-2026"
	title := "Export"
	submitter := "alice"
	createdAt := time.Date(2026, 6, 24, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM tickets t LEFT JOIN users u ON u.id = t.submitter_id WHERE 1=1 AND t.ticket_no LIKE ? AND t.title LIKE ? AND u.username LIKE ?`)).
		WithArgs("%TK-2026%", "%Export%", "%alice%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT t.* FROM tickets t
		LEFT JOIN users u ON u.id = t.submitter_id WHERE 1=1 AND t.ticket_no LIKE ? AND t.title LIKE ? AND u.username LIKE ? ORDER BY CASE WHEN t.status IN (?, ?, ?) THEN 0 ELSE 1 END, t.created_at DESC LIMIT ? OFFSET ?`)).
		WithArgs(
			"%TK-2026%",
			"%Export%",
			"%alice%",
			model.TicketStatusPendingReview,
			model.TicketStatusPendingExecution,
			model.TicketStatusNeedsAdminAttention,
			20,
			0,
		).
		WillReturnRows(ticketRows().AddRow(
			uint64(10),
			"TK-20260624-080000000-ABCDEF",
			"Export data",
			nil,
			"",
			model.TicketTypeSQLExport,
			nil,
			nil,
			nil,
			model.TicketStatusPendingReview,
			uint64(2),
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			createdAt,
			updatedAt,
		))

	tickets, total, err := repo.List(context.Background(), TicketListFilter{TicketNo: &ticketNo, Title: &title, Submitter: &submitter}, 20, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(tickets) != 1 || tickets[0].ID != 10 {
		t.Fatalf("tickets = %#v, want ticket 10", tickets)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func TestTicketGetByTicketNo(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewTicketRepo(sqlx.NewDb(db, "sqlmock"))
	createdAt := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM tickets WHERE ticket_no = ?`)).
		WithArgs("TK-20260622-080000000-ABCDEF").
		WillReturnRows(ticketRows().AddRow(
			uint64(9),
			"TK-20260622-080000000-ABCDEF",
			"DDL",
			nil,
			"ALTER TABLE orders ADD COLUMN note VARCHAR(255);",
			model.TicketTypeDDL,
			nil,
			uint64(1),
			"orders",
			model.TicketStatusPendingReview,
			uint64(2),
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			createdAt,
			updatedAt,
		))

	ticket, err := repo.GetByTicketNo(context.Background(), "TK-20260622-080000000-ABCDEF")
	if err != nil {
		t.Fatalf("GetByTicketNo() error = %v", err)
	}
	if ticket == nil || ticket.ID != 9 {
		t.Fatalf("ticket = %#v, want ticket 9", ticket)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func TestTicketUpdateStatusStoresWithdrawReason(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewTicketRepo(sqlx.NewDb(db, "sqlmock"))
	reason := "需求改變，先撤回"

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE tickets SET status = ?, updated_at = ?, rejection_reason = ? WHERE id = ? AND status = ?`)).
		WithArgs(model.TicketStatusWithdrawn, sqlmock.AnyArg(), reason, uint64(12), model.TicketStatusPendingReview).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ok, err := repo.UpdateStatus(context.Background(), 12, model.TicketStatusPendingReview, model.TicketStatusWithdrawn, nil, nil, &reason)
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	if !ok {
		t.Fatal("UpdateStatus() ok = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("mock expectations not met: %v", err)
	}
}

func ticketRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"ticket_no",
		"title",
		"description",
		"sql_content",
		"ticket_type",
		"contains_sensitive",
		"db_connection_id",
		"database_name",
		"status",
		"submitter_id",
		"reviewer_id",
		"executor_id",
		"review_comment",
		"rejection_reason",
		"scheduled_at",
		"started_at",
		"completed_at",
		"approved_duration_minutes",
		"approved_until",
		"revoked_at",
		"revoked_by",
		"created_at",
		"updated_at",
	})
}
