package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type SessionRepo struct {
	db *sqlx.DB
}

type SessionCreateOptions struct {
	AuthMethod   string
	AuthProvider string
	MFASatisfied bool
	MFASource    string
}

type UserSessionSummary struct {
	UserID             uint64
	LastLoginAt        *time.Time
	ActiveSessionCount int
}

func NewSessionRepo(db *sqlx.DB) *SessionRepo {
	return &SessionRepo{db: db}
}

func (r *SessionRepo) Create(ctx context.Context, userID uint64, tokenHash, userAgent, ipAddress string, expiresAt time.Time) (*model.Session, error) {
	return r.CreateWithOptions(ctx, userID, tokenHash, userAgent, ipAddress, expiresAt, SessionCreateOptions{AuthMethod: "password"})
}

func (r *SessionRepo) CreateWithOptions(ctx context.Context, userID uint64, tokenHash, userAgent, ipAddress string, expiresAt time.Time, options SessionCreateOptions) (*model.Session, error) {
	var ua, ip *string
	if userAgent != "" {
		ua = &userAgent
	}
	if ipAddress != "" {
		ip = &ipAddress
	}
	authMethod := strings.TrimSpace(options.AuthMethod)
	if authMethod == "" {
		authMethod = "password"
	}

	res, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, user_agent, ip_address, auth_method, auth_provider, mfa_satisfied, mfa_source, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, tokenHash, ua, ip, authMethod, strings.TrimSpace(options.AuthProvider), options.MFASatisfied, strings.TrimSpace(options.MFASource), expiresAt.UTC(), timeutil.NowUTC(),
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

func (r *SessionRepo) GetByID(ctx context.Context, id uint64) (*model.Session, error) {
	if id == 0 {
		return nil, nil
	}
	var s model.Session
	err := r.db.GetContext(ctx, &s, `SELECT * FROM sessions WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

func (r *SessionRepo) ListForUser(ctx context.Context, userID uint64) ([]model.Session, error) {
	return r.ListForUserLimit(ctx, userID, 0)
}

func (r *SessionRepo) ListForUserLimit(ctx context.Context, userID uint64, limit int) ([]model.Session, error) {
	var sessions []model.Session
	query := `SELECT * FROM sessions WHERE user_id = ? ORDER BY CASE WHEN revoked_at IS NULL AND expires_at > ? THEN 0 ELSE 1 END, created_at DESC`
	args := []any{userID, timeutil.NowUTC()}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	err := r.db.SelectContext(ctx, &sessions, query, args...)
	return sessions, err
}

func (r *SessionRepo) SummariesForUsers(ctx context.Context, userIDs []uint64, now time.Time) (map[uint64]UserSessionSummary, error) {
	summaries := make(map[uint64]UserSessionSummary, len(userIDs))
	if len(userIDs) == 0 {
		return summaries, nil
	}

	query, args, err := sqlx.In(`
		SELECT
			user_id,
			MAX(created_at) AS last_login_at,
			SUM(CASE WHEN revoked_at IS NULL AND expires_at > ? THEN 1 ELSE 0 END) AS active_session_count
		FROM sessions
		WHERE user_id IN (?)
		GROUP BY user_id
	`, now.UTC(), userIDs)
	if err != nil {
		return nil, fmt.Errorf("build session summaries query: %w", err)
	}
	query = r.db.Rebind(query)

	var rows []struct {
		UserID             uint64        `db:"user_id"`
		LastLoginAt        *time.Time    `db:"last_login_at"`
		ActiveSessionCount sql.NullInt64 `db:"active_session_count"`
	}
	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("list session summaries: %w", err)
	}
	for _, row := range rows {
		activeSessionCount := 0
		if row.ActiveSessionCount.Valid {
			activeSessionCount = int(row.ActiveSessionCount.Int64)
		}
		summaries[row.UserID] = UserSessionSummary{
			UserID:             row.UserID,
			LastLoginAt:        row.LastLoginAt,
			ActiveSessionCount: activeSessionCount,
		}
	}
	return summaries, nil
}

func (r *SessionRepo) Revoke(ctx context.Context, tokenHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`,
		timeutil.NowUTC(), tokenHash,
	)
	return err
}

func (r *SessionRepo) RevokeByIDForUser(ctx context.Context, sessionID, userID uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		timeutil.NowUTC(), sessionID, userID,
	)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (r *SessionRepo) RevokeAllForUser(ctx context.Context, userID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		timeutil.NowUTC(), userID,
	)
	return err
}
