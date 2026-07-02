package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type MFAChallengeRepo struct {
	db *sqlx.DB
}

func NewMFAChallengeRepo(db *sqlx.DB) *MFAChallengeRepo {
	return &MFAChallengeRepo{db: db}
}

func (r *MFAChallengeRepo) Create(ctx context.Context, challenge model.MFAChallenge) error {
	var createdIP *string
	if challenge.CreatedIP != nil && *challenge.CreatedIP != "" {
		createdIP = challenge.CreatedIP
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO mfa_challenges (token_id, user_id, setup, expires_at, created_ip, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		challenge.TokenID, challenge.UserID, challenge.Setup, challenge.ExpiresAt.UTC(), createdIP, timeutil.NowUTC(),
	)
	if err != nil {
		return fmt.Errorf("create mfa challenge: %w", err)
	}
	return nil
}

func (r *MFAChallengeRepo) GetByTokenID(ctx context.Context, tokenID string) (*model.MFAChallenge, error) {
	var challenge model.MFAChallenge
	err := r.db.GetContext(ctx, &challenge, `SELECT * FROM mfa_challenges WHERE token_id = ?`, tokenID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &challenge, nil
}

func (r *MFAChallengeRepo) RecordFailedAttempt(ctx context.Context, id uint64, maxAttempts int) error {
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE mfa_challenges
		 SET attempt_count = attempt_count + 1,
		     revoked_at = CASE WHEN attempt_count + 1 >= ? THEN COALESCE(revoked_at, ?) ELSE revoked_at END
		 WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL`,
		maxAttempts, now, id,
	)
	if err != nil {
		return fmt.Errorf("record mfa failed attempt: %w", err)
	}
	return nil
}

func (r *MFAChallengeRepo) MarkUsed(ctx context.Context, id uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE mfa_challenges
		 SET used_at = ?
		 WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?`,
		timeutil.NowUTC(), id, timeutil.NowUTC(),
	)
	if err != nil {
		return false, fmt.Errorf("mark mfa challenge used: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (r *MFAChallengeRepo) RevokeExpired(ctx context.Context, before time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE mfa_challenges SET revoked_at = ? WHERE expires_at <= ? AND used_at IS NULL AND revoked_at IS NULL`,
		timeutil.NowUTC(), before.UTC(),
	)
	return err
}
