package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dbre-maestro/maestro/internal/export"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type ExportRepo struct {
	db *sqlx.DB
}

func NewExportRepo(db *sqlx.DB) *ExportRepo {
	return &ExportRepo{db: db}
}

// Create inserts a new export request with the given status and returns the generated token.
// For non-sensitive exports pass ExportStatusReady; for sensitive ones pass ExportStatusPending.
func (r *ExportRepo) Create(ctx context.Context, req *model.ExportRequest, status model.ExportStatus) (id uint64, token string, err error) {
	token, err = export.GenerateToken()
	if err != nil {
		return 0, "", fmt.Errorf("generate token: %w", err)
	}

	expiresAt := timeutil.NowUTC().Add(24 * time.Hour)
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO export_requests
         (ticket_id, requester_id, download_token, sql_content, db_connection_id, status, expires_at, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		req.TicketID, req.RequesterID, token, req.SQLContent, req.DBConnectionID, string(status), expiresAt, timeutil.NowUTC(),
	)
	if err != nil {
		return 0, "", fmt.Errorf("create export_request: %w", err)
	}
	rawID, _ := res.LastInsertId()
	return uint64(rawID), token, nil
}

// GetByToken retrieves an export request by download token.
func (r *ExportRepo) GetByToken(ctx context.Context, token string) (*model.ExportRequest, error) {
	var req model.ExportRequest
	err := r.db.GetContext(ctx, &req,
		`SELECT * FROM export_requests WHERE download_token = ?`, token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &req, err
}

// GetByID retrieves an export request by ID (includes download_token for internal use).
func (r *ExportRepo) GetByID(ctx context.Context, id uint64) (*model.ExportRequest, error) {
	var req model.ExportRequest
	err := r.db.GetContext(ctx, &req, `SELECT * FROM export_requests WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &req, err
}

func (r *ExportRepo) GetByTicketID(ctx context.Context, ticketID uint64) (*model.ExportRequest, error) {
	var req model.ExportRequest
	err := r.db.GetContext(ctx, &req, `SELECT * FROM export_requests WHERE ticket_id = ? ORDER BY id DESC LIMIT 1`, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &req, err
}

// List returns export requests. If requesterID is nil, returns all (DBA/Admin view).
func (r *ExportRepo) List(ctx context.Context, requesterID *uint64) ([]model.ExportRequest, error) {
	var exports []model.ExportRequest
	if requesterID == nil {
		err := r.db.SelectContext(ctx, &exports,
			`SELECT * FROM export_requests ORDER BY created_at DESC LIMIT 200`,
		)
		return exports, err
	}
	err := r.db.SelectContext(ctx, &exports,
		`SELECT * FROM export_requests WHERE requester_id = ? ORDER BY created_at DESC`,
		*requesterID,
	)
	return exports, err
}

// UpdateStatus updates the status and optionally sets the approver.
func (r *ExportRepo) UpdateStatus(ctx context.Context, id uint64, status model.ExportStatus, approverID *uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE export_requests SET status = ?, approver_id = ? WHERE id = ?`,
		string(status), approverID, id,
	)
	return err
}

// MarkDownloaded sets downloaded_at to now (first download timestamp).
func (r *ExportRepo) MarkDownloaded(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE export_requests SET downloaded_at = ? WHERE download_token = ? AND downloaded_at IS NULL`,
		timeutil.NowUTC(), token,
	)
	return err
}
