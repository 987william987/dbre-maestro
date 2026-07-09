package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type LarkLoginRepo struct {
	db *sqlx.DB
}

type LarkLoginState struct {
	ID              uint64     `db:"id"`
	State           string     `db:"state"`
	ReturnTo        string     `db:"return_to"`
	UserID          *uint64    `db:"user_id"`
	Ticket          string     `db:"ticket"`
	Error           string     `db:"error"`
	ExpiresAt       time.Time  `db:"expires_at"`
	UsedAt          *time.Time `db:"used_at"`
	TicketExpiresAt *time.Time `db:"ticket_expires_at"`
	TicketUsedAt    *time.Time `db:"ticket_used_at"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
}

func NewLarkLoginRepo(db *sqlx.DB) *LarkLoginRepo {
	return &LarkLoginRepo{db: db}
}

func (r *LarkLoginRepo) Create(ctx context.Context, state string, returnTo string, expiresAt time.Time) error {
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO lark_login_states (state, return_to, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, state, returnTo, expiresAt.UTC(), now, now)
	return err
}

func (r *LarkLoginRepo) ClaimState(ctx context.Context, state string, now time.Time) (*LarkLoginState, bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var record LarkLoginState
	if err := tx.GetContext(ctx, &record, `
		SELECT *
		FROM lark_login_states
		WHERE state = ? AND used_at IS NULL AND expires_at > ?
		LIMIT 1
	`, state, now.UTC()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE lark_login_states
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

func (r *LarkLoginRepo) StoreTicket(ctx context.Context, id uint64, userID uint64, ticket string, expiresAt time.Time) error {
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE lark_login_states
		SET user_id = ?, ticket = ?, ticket_expires_at = ?, updated_at = ?
		WHERE id = ?
	`, userID, ticket, expiresAt.UTC(), now, id)
	return err
}

func (r *LarkLoginRepo) MarkFailed(ctx context.Context, id uint64, reason string) error {
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx, `
		UPDATE lark_login_states
		SET error = ?, updated_at = ?
		WHERE id = ?
	`, reason, now, id)
	return err
}

func (r *LarkLoginRepo) ConsumeTicket(ctx context.Context, ticket string, now time.Time) (*LarkLoginState, bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var record LarkLoginState
	if err := tx.GetContext(ctx, &record, `
		SELECT *
		FROM lark_login_states
		WHERE ticket = ? AND ticket_used_at IS NULL AND ticket_expires_at > ? AND user_id IS NOT NULL
		LIMIT 1
	`, ticket, now.UTC()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE lark_login_states
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

func (s LarkLoginState) RequiredUserID() (uint64, error) {
	if s.UserID == nil || *s.UserID == 0 {
		return 0, fmt.Errorf("lark login state missing user id")
	}
	return *s.UserID, nil
}
