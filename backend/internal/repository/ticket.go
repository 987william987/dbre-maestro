package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	return r.CreateWithScopes(ctx, t, nil)
}

func (r *TicketRepo) CreateWithScopes(ctx context.Context, t *model.Ticket, scopes []model.TicketScope) (*model.Ticket, error) {
	// Generate T-001 style ticket number atomically
	var nextID int64
	err := r.db.QueryRowContext(ctx, `SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'tickets'`).Scan(&nextID)
	if err != nil {
		nextID = 1
	}
	ticketNo := fmt.Sprintf("T-%03d", nextID)

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create ticket tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO tickets (ticket_no, title, description, sql_content, ticket_type, db_connection_id, status, submitter_id, approved_duration_minutes, approved_until, revoked_at, revoked_by)
         VALUES (?, ?, ?, ?, ?, ?, 'pending_review', ?, ?, ?, ?, ?)`,
		ticketNo, t.Title, t.Description, t.SQLContent, t.TicketType, t.DBConnectionID, t.SubmitterID,
		t.ApprovedDurationMinutes, t.ApprovedUntil, t.RevokedAt, t.RevokedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("create ticket: %w", err)
	}
	id, _ := res.LastInsertId()
	if len(scopes) > 0 {
		for _, scope := range scopes {
			if strings.TrimSpace(scope.ColumnName) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO ticket_scopes (ticket_id, connection_id, database_name, schema_name, table_name, column_name, is_sensitive, source_kind)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				id, scope.ConnectionID, scope.DatabaseName, scope.SchemaName, scope.TableName, scope.ColumnName, scope.IsSensitive, scope.SourceKind,
			); err != nil {
				return nil, fmt.Errorf("create ticket scope: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create ticket tx: %w", err)
	}
	tx = nil
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

func (r *TicketRepo) ListScopes(ctx context.Context, ticketID uint64) ([]model.TicketScope, error) {
	scopes := []model.TicketScope{}
	if err := r.db.SelectContext(ctx, &scopes,
		`SELECT id, ticket_id, connection_id, database_name, schema_name, table_name, column_name, is_sensitive, source_kind, created_at
		 FROM ticket_scopes
		 WHERE ticket_id = ?
		 ORDER BY id ASC`,
		ticketID,
	); err != nil {
		return nil, fmt.Errorf("list ticket scopes: %w", err)
	}
	return scopes, nil
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

func (r *TicketRepo) ApproveSensitiveAccess(ctx context.Context, id uint64, fromStatus model.TicketStatus, reviewerID uint64, approvedUntil time.Time) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets
		 SET status = ?, reviewer_id = ?, approved_until = ?, updated_at = ?
		 WHERE id = ? AND status = ? AND ticket_type = ?`,
		model.TicketStatusApproved, reviewerID, approvedUntil, time.Now(), id, fromStatus, model.TicketTypeSensitiveQueryAccess,
	)
	if err != nil {
		return false, fmt.Errorf("approve sensitive access: %w", err)
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (r *TicketRepo) RevokeSensitiveAccess(ctx context.Context, id uint64, actorID uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets
		 SET status = ?, revoked_at = ?, revoked_by = ?, updated_at = ?
		 WHERE id = ? AND ticket_type = ? AND status = ? AND revoked_at IS NULL`,
		model.TicketStatusStopped, time.Now(), actorID, time.Now(), id, model.TicketTypeSensitiveQueryAccess, model.TicketStatusApproved,
	)
	if err != nil {
		return false, fmt.Errorf("revoke sensitive access: %w", err)
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (r *TicketRepo) ListActiveSensitiveAccessScopes(ctx context.Context, userID, connectionID uint64) ([]model.TicketScope, error) {
	scopes := []model.TicketScope{}
	if err := r.db.SelectContext(ctx, &scopes,
		`SELECT ts.id, ts.ticket_id, ts.connection_id, ts.database_name, ts.schema_name, ts.table_name, ts.column_name, ts.is_sensitive, ts.source_kind, ts.created_at
		 FROM ticket_scopes ts
		 JOIN tickets t ON t.id = ts.ticket_id
		 WHERE t.submitter_id = ?
		   AND t.ticket_type = ?
		   AND t.status = ?
		   AND t.approved_until IS NOT NULL
		   AND t.approved_until > ?
		   AND t.revoked_at IS NULL
		   AND ts.connection_id = ?
		 ORDER BY ts.id ASC`,
		userID, model.TicketTypeSensitiveQueryAccess, model.TicketStatusApproved, time.Now(), connectionID,
	); err != nil {
		return nil, fmt.Errorf("list active sensitive access scopes: %w", err)
	}
	return scopes, nil
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

// ListExecutions returns all execution rows for a ticket, ordered by seq.
func (r *TicketRepo) ListExecutions(ctx context.Context, ticketID uint64) ([]model.TicketExecution, error) {
	var execs []model.TicketExecution
	err := r.db.SelectContext(ctx, &execs,
		`SELECT * FROM ticket_executions WHERE ticket_id = ? ORDER BY seq`,
		ticketID,
	)
	return execs, err
}

// MarkStopped transitions an executing ticket to stopped.
// Returns false if the ticket was not in executing state (idempotent).
func (r *TicketRepo) MarkStopped(ctx context.Context, id uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET status = 'stopped', updated_at = ? WHERE id = ? AND status = 'executing'`,
		time.Now(), id,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetScheduled stores executor_id and scheduled_at for deferred execution.
// The ticket must be in pending_execution status.
func (r *TicketRepo) SetScheduled(ctx context.Context, id, executorID uint64, scheduledAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET executor_id = ?, scheduled_at = ?, updated_at = ? WHERE id = ? AND status = 'pending_execution'`,
		executorID, scheduledAt, time.Now(), id,
	)
	return err
}

// GetDueScheduled returns pending_execution tickets whose scheduled_at has arrived.
func (r *TicketRepo) GetDueScheduled(ctx context.Context) ([]model.Ticket, error) {
	var tickets []model.Ticket
	err := r.db.SelectContext(ctx, &tickets,
		`SELECT * FROM tickets WHERE status = 'pending_execution' AND scheduled_at IS NOT NULL AND scheduled_at <= ?`,
		time.Now(),
	)
	return tickets, err
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
