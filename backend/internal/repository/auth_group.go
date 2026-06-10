package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

type AuthGroupRepo struct {
	db *sqlx.DB
}

type AuthGroupEntity struct {
	ID          uint64 `db:"id"`
	GroupKey    string `db:"group_key"`
	Name        string `db:"name"`
	Description string `db:"description"`
	IsSystem    bool   `db:"is_system"`
	IsProtected bool   `db:"is_protected"`
	CreatedAt   string `db:"created_at"`
	UpdatedAt   string `db:"updated_at"`
}

func NewAuthGroupRepo(db *sqlx.DB) *AuthGroupRepo {
	return &AuthGroupRepo{db: db}
}

func (r *AuthGroupRepo) List(ctx context.Context) ([]AuthGroupEntity, error) {
	var groups []AuthGroupEntity
	err := r.db.SelectContext(ctx, &groups, `
		SELECT id, group_key, name, description, is_system, is_protected,
		       DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ') AS created_at,
		       DATE_FORMAT(updated_at, '%Y-%m-%dT%H:%i:%sZ') AS updated_at
		FROM auth_groups
		ORDER BY id
	`)
	return groups, err
}

func (r *AuthGroupRepo) GetByKey(ctx context.Context, groupKey string) (*AuthGroupEntity, error) {
	var group AuthGroupEntity
	err := r.db.GetContext(ctx, &group, `
		SELECT id, group_key, name, description, is_system, is_protected,
		       DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ') AS created_at,
		       DATE_FORMAT(updated_at, '%Y-%m-%dT%H:%i:%sZ') AS updated_at
		FROM auth_groups
		WHERE group_key = ?
	`, groupKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &group, err
}

func (r *AuthGroupRepo) ListPermissionKeys(ctx context.Context, authGroupID uint64) ([]string, error) {
	var keys []string
	err := r.db.SelectContext(ctx, &keys, `
		SELECT p.permission_key
		FROM permissions p
		INNER JOIN auth_group_permissions agp ON agp.permission_id = p.id
		WHERE agp.auth_group_id = ?
		ORDER BY p.permission_key
	`, authGroupID)
	return keys, err
}

func (r *AuthGroupRepo) ListDBConnectionIDs(ctx context.Context, authGroupID uint64) ([]uint64, error) {
	var ids []uint64
	err := r.db.SelectContext(ctx, &ids, `
		SELECT db_connection_id
		FROM auth_group_db_connections
		WHERE auth_group_id = ?
		ORDER BY db_connection_id
	`, authGroupID)
	return ids, err
}

func (r *AuthGroupRepo) Create(ctx context.Context, groupKey, name, description string, isSystem, isProtected bool) (*AuthGroupEntity, error) {
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO auth_groups (group_key, name, description, is_system, is_protected)
		VALUES (?, ?, ?, ?, ?)
	`, groupKey, name, description, isSystem, isProtected)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	var group AuthGroupEntity
	if err := r.db.GetContext(ctx, &group, `
		SELECT id, group_key, name, description, is_system, is_protected,
		       DATE_FORMAT(created_at, '%Y-%m-%dT%H:%i:%sZ') AS created_at,
		       DATE_FORMAT(updated_at, '%Y-%m-%dT%H:%i:%sZ') AS updated_at
		FROM auth_groups
		WHERE id = ?
	`, id); err != nil {
		return nil, err
	}
	return &group, nil
}

func (r *AuthGroupRepo) Update(ctx context.Context, id uint64, groupKey, name, description string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE auth_groups
		SET group_key = ?, name = ?, description = ?, updated_at = NOW()
		WHERE id = ?
	`, groupKey, name, description, id)
	return err
}

func (r *AuthGroupRepo) Delete(ctx context.Context, id uint64) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	queries := []string{
		`DELETE FROM auth_group_permissions WHERE auth_group_id = ?`,
		`DELETE FROM auth_group_db_connections WHERE auth_group_id = ?`,
		`DELETE FROM user_auth_groups WHERE auth_group_id = ?`,
		`DELETE FROM auth_groups WHERE id = ?`,
	}
	for _, query := range queries {
		if _, err := tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *AuthGroupRepo) AddPermission(ctx context.Context, authGroupID uint64, permissionKey string) error {
	res, err := r.db.ExecContext(ctx, `
		INSERT IGNORE INTO auth_group_permissions (auth_group_id, permission_id)
		SELECT ?, p.id
		FROM permissions p
		WHERE p.permission_key = ?
	`, authGroupID, permissionKey)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		var exists int
		if err := r.db.GetContext(ctx, &exists, `SELECT COUNT(*) FROM permissions WHERE permission_key = ?`, permissionKey); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}

func (r *AuthGroupRepo) RemovePermission(ctx context.Context, authGroupID uint64, permissionKey string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE agp FROM auth_group_permissions agp
		INNER JOIN permissions p ON p.id = agp.permission_id
		WHERE agp.auth_group_id = ? AND p.permission_key = ?
	`, authGroupID, permissionKey)
	return err
}

func (r *AuthGroupRepo) AddDBConnection(ctx context.Context, authGroupID, dbConnectionID uint64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT IGNORE INTO auth_group_db_connections (auth_group_id, db_connection_id)
		VALUES (?, ?)
	`, authGroupID, dbConnectionID)
	return err
}

func (r *AuthGroupRepo) RemoveDBConnection(ctx context.Context, authGroupID, dbConnectionID uint64) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM auth_group_db_connections
		WHERE auth_group_id = ? AND db_connection_id = ?
	`, authGroupID, dbConnectionID)
	return err
}

func (r *AuthGroupRepo) ReplacePermissionKeys(ctx context.Context, authGroupID uint64, permissionKeys []string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_group_permissions WHERE auth_group_id = ?`, authGroupID); err != nil {
		return err
	}
	for _, permissionKey := range permissionKeys {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO auth_group_permissions (auth_group_id, permission_id)
			SELECT ?, p.id
			FROM permissions p
			WHERE p.permission_key = ?
		`, authGroupID, permissionKey)
		if err != nil {
			return err
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			return sql.ErrNoRows
		}
	}
	return tx.Commit()
}

func (r *AuthGroupRepo) ReplaceDBConnectionIDs(ctx context.Context, authGroupID uint64, dbConnectionIDs []uint64) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_group_db_connections WHERE auth_group_id = ?`, authGroupID); err != nil {
		return err
	}
	for _, connectionID := range dbConnectionIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO auth_group_db_connections (auth_group_id, db_connection_id)
			VALUES (?, ?)
		`, authGroupID, connectionID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func GenerateAuthGroupKey(name string) string {
	normalized := NormalizeAuthGroupKey(strings.ReplaceAll(name, " ", "-"))
	normalized = strings.ReplaceAll(normalized, "--", "-")
	return strings.Trim(normalized, "-")
}

func NormalizeAuthGroupKey(input string) string {
	return strings.TrimSpace(strings.ToLower(input))
}

func ValidateAuthGroupKey(groupKey string) error {
	if groupKey == "" {
		return fmt.Errorf("group_key is required")
	}
	for _, ch := range groupKey {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' && ch != '-' {
			return fmt.Errorf("group_key must contain only lowercase letters, digits, underscore, or dash")
		}
	}
	return nil
}
