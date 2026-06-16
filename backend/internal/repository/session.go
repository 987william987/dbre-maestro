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

type SessionRepo struct {
	db *sqlx.DB
}

func NewSessionRepo(db *sqlx.DB) *SessionRepo {
	return &SessionRepo{db: db}
}

func (r *SessionRepo) Create(ctx context.Context, userID uint64, tokenHash, userAgent, ipAddress string, expiresAt time.Time) (*model.Session, error) {
	var ua, ip *string
	if userAgent != "" {
		ua = &userAgent
	}
	if ipAddress != "" {
		ip = &ipAddress
	}

	res, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, user_agent, ip_address, expires_at) VALUES (?, ?, ?, ?, ?)`,
		userID, tokenHash, ua, ip, expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	id, _ := res.LastInsertId()

	var s model.Session
	err = r.db.GetContext(ctx, &s, `SELECT * FROM sessions WHERE id = ?`, id)
	return &s, err
}

func (r *SessionRepo) GetByTokenHash(ctx context.Context, hash string) (*model.Session, error) {
	var s model.Session
	err := r.db.GetContext(ctx, &s, `SELECT * FROM sessions WHERE token_hash = ?`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

func (r *SessionRepo) Revoke(ctx context.Context, tokenHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`,
		time.Now(), tokenHash,
	)
	return err
}

func (r *SessionRepo) RevokeAllForUser(ctx context.Context, userID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		time.Now(), userID,
	)
	return err
}
