package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

type PermissionRepo struct {
	db *sqlx.DB
}

type PermissionEntity struct {
	ID            uint64 `db:"id"`
	PermissionKey string `db:"permission_key"`
	Name          string `db:"name"`
	Description   string `db:"description"`
	Category      string `db:"category"`
}

func NewPermissionRepo(db *sqlx.DB) *PermissionRepo {
	return &PermissionRepo{db: db}
}

func (r *PermissionRepo) List(ctx context.Context) ([]PermissionEntity, error) {
	var permissions []PermissionEntity
	err := r.db.SelectContext(ctx, &permissions, `
		SELECT id, permission_key, name, description, category
		FROM permissions
		ORDER BY category, permission_key
	`)
	return permissions, err
}

func (r *PermissionRepo) GetByKey(ctx context.Context, permissionKey string) (*PermissionEntity, error) {
	var permission PermissionEntity
	err := r.db.GetContext(ctx, &permission, `
		SELECT id, permission_key, name, description, category
		FROM permissions
		WHERE permission_key = ?
	`, permissionKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &permission, err
}
