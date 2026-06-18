package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type QueryAccessRepo struct {
	db *sqlx.DB
}

func NewQueryAccessRepo(db *sqlx.DB) *QueryAccessRepo {
	return &QueryAccessRepo{db: db}
}

func (r *QueryAccessRepo) CreateTicketItems(ctx context.Context, ticketID uint64, items []model.QueryAccessTicketItem) error {
	if len(items) == 0 {
		return nil
	}
	for _, item := range items {
		if strings.TrimSpace(item.DatabaseName) == "" {
			return fmt.Errorf("database_name is required")
		}
		if item.ScopeMode == model.QueryAccessScopeModeTable && strings.TrimSpace(nullableString(item.TableName)) == "" {
			return fmt.Errorf("table_name is required for table scope")
		}
		if _, err := r.db.ExecContext(ctx,
			`INSERT INTO query_access_ticket_items (ticket_id, connection_id, scope_mode, database_name, table_name, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			ticketID,
			item.ConnectionID,
			item.ScopeMode,
			strings.TrimSpace(item.DatabaseName),
			trimmedNullableString(item.TableName),
			timeutil.NowUTC(),
		); err != nil {
			return fmt.Errorf("create query access ticket item: %w", err)
		}
	}
	return nil
}

func (r *QueryAccessRepo) ListTicketItems(ctx context.Context, ticketID uint64) ([]model.QueryAccessTicketItem, error) {
	items := make([]model.QueryAccessTicketItem, 0)
	if err := r.db.SelectContext(ctx, &items,
		`SELECT id, ticket_id, connection_id, scope_mode, database_name, table_name, created_at
		 FROM query_access_ticket_items
		 WHERE ticket_id = ?
		 ORDER BY id ASC`,
		ticketID,
	); err != nil {
		return nil, fmt.Errorf("list query access ticket items: %w", err)
	}
	return items, nil
}

func (r *QueryAccessRepo) CreateGrantsForTicket(ctx context.Context, ticketID, subjectID, actorID uint64, expiresAt *time.Time) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create query access grants tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if err := r.createGrantsForTicketTx(ctx, tx, ticketID, subjectID, actorID, expiresAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create query access grants tx: %w", err)
	}
	tx = nil
	return nil
}

func (r *QueryAccessRepo) ApproveTicket(ctx context.Context, ticketID uint64, fromStatus model.TicketStatus, reviewerID uint64, comment *string, subjectID uint64, expiresAt *time.Time) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin approve query access tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	query := `UPDATE tickets SET status = ?, updated_at = ?, reviewer_id = ?`
	args := []any{model.TicketStatusApproved, timeutil.NowUTC(), reviewerID}
	if comment != nil {
		query += `, review_comment = ?`
		args = append(args, *comment)
	}
	query += ` WHERE id = ? AND status = ? AND ticket_type = ?`
	args = append(args, ticketID, fromStatus, model.TicketTypeQueryAccess)

	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("approve query access ticket: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return false, nil
	}

	if err := r.createGrantsForTicketTx(ctx, tx, ticketID, subjectID, reviewerID, expiresAt); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit approve query access tx: %w", err)
	}
	tx = nil
	return true, nil
}

func (r *QueryAccessRepo) createGrantsForTicketTx(ctx context.Context, tx *sqlx.Tx, ticketID, subjectID, actorID uint64, expiresAt *time.Time) error {
	items, err := r.ListTicketItems(ctx, ticketID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("query access ticket has no items")
	}

	now := timeutil.NowUTC()
	for _, item := range items {
		var sourceTicketID *uint64
		sourceTicketID = &ticketID
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO query_access_grants
			 (subject_type, subject_id, connection_id, database_name, table_name, granted_via, source_ticket_id, expires_at, revoked_at, revoked_by, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, 'ticket', ?, ?, NULL, NULL, ?, ?, ?)`,
			"user",
			subjectID,
			item.ConnectionID,
			strings.TrimSpace(item.DatabaseName),
			trimmedNullableString(item.TableName),
			sourceTicketID,
			expiresAt,
			actorID,
			now,
			now,
		); err != nil {
			return fmt.Errorf("create query access grant: %w", err)
		}
	}
	return nil
}

func (r *QueryAccessRepo) RevokeGrantsByTicket(ctx context.Context, ticketID, actorID uint64) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin revoke query access grants tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	ok, err := r.revokeGrantsByTicketTx(ctx, tx, ticketID, actorID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit revoke query access grants tx: %w", err)
	}
	tx = nil
	return ok, nil
}

func (r *QueryAccessRepo) RevokeTicket(ctx context.Context, ticketID uint64, actorID uint64) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin revoke query access ticket tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	ok, err := r.revokeGrantsByTicketTx(ctx, tx, ticketID, actorID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE tickets
		 SET status = ?, revoked_at = ?, revoked_by = ?, updated_at = ?
		 WHERE id = ? AND ticket_type = ? AND status = ? AND revoked_at IS NULL`,
		model.TicketStatusStopped, timeutil.NowUTC(), actorID, timeutil.NowUTC(), ticketID, model.TicketTypeQueryAccess, model.TicketStatusApproved,
	)
	if err != nil {
		return false, fmt.Errorf("update query access ticket stopped: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit revoke query access ticket tx: %w", err)
	}
	tx = nil
	return true, nil
}

func (r *QueryAccessRepo) revokeGrantsByTicketTx(ctx context.Context, tx *sqlx.Tx, ticketID, actorID uint64) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE query_access_grants
		 SET revoked_at = ?, revoked_by = ?, updated_at = ?
		 WHERE source_ticket_id = ? AND revoked_at IS NULL`,
		timeutil.NowUTC(), actorID, timeutil.NowUTC(), ticketID,
	)
	if err != nil {
		return false, fmt.Errorf("revoke query access grants: %w", err)
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (r *QueryAccessRepo) ListActiveGrants(ctx context.Context, subjectID, connectionID uint64) ([]model.QueryAccessGrant, error) {
	grants := make([]model.QueryAccessGrant, 0)
	if err := r.db.SelectContext(ctx, &grants,
		`SELECT id, subject_type, subject_id, connection_id, database_name, table_name, granted_via, source_ticket_id, expires_at, revoked_at, revoked_by, created_by, created_at, updated_at
		 FROM query_access_grants
		 WHERE subject_type = 'user'
		   AND subject_id = ?
		   AND connection_id = ?
		   AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > ?)
		 ORDER BY id ASC`,
		subjectID,
		connectionID,
		timeutil.NowUTC(),
	); err != nil {
		return nil, fmt.Errorf("list active query access grants: %w", err)
	}
	return grants, nil
}

func nullableString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func trimmedNullableString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
