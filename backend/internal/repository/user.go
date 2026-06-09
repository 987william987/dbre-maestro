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

type UserRepo struct {
	db *sqlx.DB
}

func NewUserRepo(db *sqlx.DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Create(ctx context.Context, username, email, passwordHash string) (*model.User, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO users (username, email, password) VALUES (?, ?, ?)`,
		username, email, passwordHash,
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, uint64(id))
}

func (r *UserRepo) GetByID(ctx context.Context, id uint64) (*model.User, error) {
	var u model.User
	err := r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	err := r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE username = ?`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User
	err := r.db.GetContext(ctx, &u, `SELECT * FROM users WHERE email = ?`, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (r *UserRepo) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM users`)
	return count, err
}

func (r *UserRepo) GetAuthGroups(ctx context.Context, userID uint64) ([]model.AuthGroup, error) {
	var groups []model.AuthGroup
	err := r.db.SelectContext(ctx, &groups,
		`SELECT auth_group FROM auth_group_memberships
         WHERE user_id = ? AND (expires_at IS NULL OR expires_at > ?)`,
		userID, time.Now(),
	)
	return groups, err
}

func (r *UserRepo) AddMembership(ctx context.Context, userID uint64, group model.AuthGroup, grantedBy *uint64, expiresAt *time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO auth_group_memberships (user_id, auth_group, granted_by, expires_at) VALUES (?, ?, ?, ?)
         ON DUPLICATE KEY UPDATE expires_at = VALUES(expires_at), granted_by = VALUES(granted_by)`,
		userID, group, grantedBy, expiresAt,
	)
	return err
}
