package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

type TicketRepo struct {
	db *sqlx.DB
}

func NewTicketRepo(db *sqlx.DB) *TicketRepo {
	return &TicketRepo{db: db}
}

func (r *TicketRepo) Create(ctx context.Context, t *model.Ticket) (*model.Ticket, error) {
	// Generate T-001 style ticket number atomically
	var nextID int64
	err := r.db.QueryRowContext(ctx, `SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'tickets'`).Scan(&nextID)
	if err != nil {
		nextID = 1
	}
	ticketNo := fmt.Sprintf("T-%03d", nextID)

	res, err := r.db.ExecContext(ctx,
		`INSERT INTO tickets (ticket_no, title, description, sql_content, ticket_type, db_connection_id, status, submitter_id)
         VALUES (?, ?, ?, ?, ?, ?, 'pending_review', ?)`,
		ticketNo, t.Title, t.Description, t.SQLContent, t.TicketType, t.DBConnectionID, t.SubmitterID,
	)
	if err != nil {
		return nil, fmt.Errorf("create ticket: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, uint64(id))
}

func (r *TicketRepo) GetByID(ctx context.Context, id uint64) (*model.Ticket, error) {
	var t model.Ticket
	err := r.db.GetContext(ctx, &t, `SELECT * FROM tickets WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &t, err
}

func (r *TicketRepo) List(ctx context.Context, submitterID *uint64, status *model.TicketStatus, limit, offset int) ([]model.Ticket, error) {
	query := `SELECT * FROM tickets WHERE 1=1`
	args := []any{}
	if submitterID != nil {
		query += ` AND submitter_id = ?`
		args = append(args, *submitterID)
	}
	if status != nil {
		query += ` AND status = ?`
		args = append(args, *status)
	}
	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	var tickets []model.Ticket
	err := r.db.SelectContext(ctx, &tickets, query, args...)
	return tickets, err
}

func (r *TicketRepo) UpdateStatus(ctx context.Context, id uint64, fromStatus, toStatus model.TicketStatus, reviewerID *uint64, comment *string, rejectionReason *string) (bool, error) {
	query := `UPDATE tickets SET status = ?, updated_at = ?`
	args := []any{toStatus, time.Now()}

	if reviewerID != nil {
		query += `, reviewer_id = ?`
		args = append(args, *reviewerID)
	}
	if comment != nil {
		query += `, review_comment = ?`
		args = append(args, *comment)
	}
	if rejectionReason != nil {
		query += `, rejection_reason = ?`
		args = append(args, *rejectionReason)
	}

	query += ` WHERE id = ? AND status = ?`
	args = append(args, id, fromStatus)

	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("update ticket status: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// T9: OCC — atomically transition to executing, returns false if already taken
func (r *TicketRepo) StartExecution(ctx context.Context, id, executorID uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET status = 'executing', executor_id = ?, started_at = ?, updated_at = ?
         WHERE id = ? AND status = 'pending_execution'`,
		executorID, time.Now(), time.Now(), id,
	)
	if err != nil {
		return false, fmt.Errorf("start execution OCC: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *TicketRepo) MarkCompleted(ctx context.Context, id uint64, status model.TicketStatus) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
		status, time.Now(), time.Now(), id,
	)
	return err
}

func (r *TicketRepo) CreateExecution(ctx context.Context, e *model.TicketExecution) (uint64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO ticket_executions (ticket_id, seq, sql_stmt, status) VALUES (?, ?, ?, 'pending')`,
		e.TicketID, e.Seq, e.SQLStmt,
	)
	if err != nil {
		return 0, fmt.Errorf("create ticket_execution: %w", err)
	}
	id, _ := res.LastInsertId()
	return uint64(id), nil
}

func (r *TicketRepo) MarkExecutionRunning(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions SET status = 'running', started_at = ? WHERE id = ?`,
		time.Now(), id,
	)
	return err
}

func (r *TicketRepo) MarkExecutionDone(ctx context.Context, id uint64, rowsAffected int64, errMsg *string) error {
	status := "completed"
	if errMsg != nil {
		status = "failed"
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions SET status = ?, rows_affected = ?, error_msg = ?, completed_at = ? WHERE id = ?`,
		status, rowsAffected, errMsg, time.Now(), id,
	)
	return err
}

// Crash recovery: on startup, scan executing → mark interrupted
func (r *TicketRepo) MarkInterruptedAll(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET status = 'interrupted', updated_at = ? WHERE status = 'executing'`,
		time.Now(),
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
