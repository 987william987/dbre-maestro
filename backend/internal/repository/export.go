package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dbre-maestro/maestro/internal/export"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

type ExportRepo struct {
	db *sqlx.DB
}

func NewExportRepo(db *sqlx.DB) *ExportRepo {
	return &ExportRepo{db: db}
}

// Create inserts a new export request and returns the generated download token.
func (r *ExportRepo) Create(ctx context.Context, req *model.ExportRequest) (token string, err error) {
	token, err = export.GenerateToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO export_requests
         (requester_id, download_token, sql_content, db_connection_id, status, expires_at)
         VALUES (?, ?, ?, ?, 'ready', ?)`,
		req.RequesterID, token, req.SQLContent, req.DBConnectionID, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("create export_request: %w", err)
	}
	id, _ := res.LastInsertId()
	_ = id
	return token, nil
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

// MarkDownloaded sets downloaded_at to now (first download timestamp).
func (r *ExportRepo) MarkDownloaded(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE export_requests SET downloaded_at = ? WHERE download_token = ? AND downloaded_at IS NULL`,
		time.Now(), token,
	)
	return err
}
