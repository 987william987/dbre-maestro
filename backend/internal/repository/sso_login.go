package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type SSOLoginRepo struct {
	db *sqlx.DB
}

func NewSSOLoginRepo(db *sqlx.DB) *SSOLoginRepo {
	return &SSOLoginRepo{db: db}
}

func (r *SSOLoginRepo) Create(ctx context.Context, state string, returnTo string, expiresAt time.Time) error {
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sso_login_states (state, return_to, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, state, returnTo, expiresAt.UTC(), now, now)
	return err
}

func (r *SSOLoginRepo) ClaimState(ctx context.Context, state string, now time.Time) (*model.SSOLoginState, bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var record model.SSOLoginState
	if err := tx.GetContext(ctx, &record, `
		SELECT *
		FROM sso_login_states
		WHERE state = ? AND used_at IS NULL AND expires_at > ?
		LIMIT 1
	`, state, now.UTC()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE sso_login_states
		SET used_at = ?, updated_at = ?
		WHERE id = ? AND used_at IS NULL
	`, now.UTC(), now.UTC(), record.ID)
	if err != nil {
		return nil, false, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return nil, false, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return &record, true, nil
}

func (r *SSOLoginRepo) StoreTicket(ctx context.Context, id uint64, userID uint64, ticket string, identity any, expiresAt time.Time) error {
	raw, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("encode sso identity: %w", err)
	}
	now := timeutil.NowUTC()
	_, err = r.db.ExecContext(ctx, `
		UPDATE sso_login_states
		SET user_id = ?, ticket = ?, identity_json = ?, ticket_expires_at = ?, updated_at = ?
		WHERE id = ?
	`, userID, ticket, raw, expiresAt.UTC(), now, id)
	return err
}

func (r *SSOLoginRepo) MarkFailed(ctx context.Context, id uint64, reason string) error {
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE sso_login_states
		SET error = ?, updated_at = ?
		WHERE id = ?
	`, reason, now, id)
	return err
}

func (r *SSOLoginRepo) ConsumeTicket(ctx context.Context, ticket string, now time.Time) (*model.SSOLoginState, bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var record model.SSOLoginState
	if err := tx.GetContext(ctx, &record, `
		SELECT *
		FROM sso_login_states
		WHERE ticket = ? AND ticket_used_at IS NULL AND ticket_expires_at > ? AND user_id IS NOT NULL
		LIMIT 1
	`, ticket, now.UTC()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE sso_login_states
		SET ticket_used_at = ?, updated_at = ?
		WHERE id = ? AND ticket_used_at IS NULL
	`, now.UTC(), now.UTC(), record.ID)
	if err != nil {
		return nil, false, err
	}
	rows, _ := res.RowsAffected()
	if rows != 1 {
		return nil, false, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return &record, true, nil
}
