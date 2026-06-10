package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

type MaskingWhitelistRepo struct {
	db *sqlx.DB
}

func NewMaskingWhitelistRepo(db *sqlx.DB) *MaskingWhitelistRepo {
	return &MaskingWhitelistRepo{db: db}
}

func (r *MaskingWhitelistRepo) List(ctx context.Context) ([]model.MaskingWhitelist, error) {
	var entries []model.MaskingWhitelist
	err := r.db.SelectContext(ctx, &entries,
		`SELECT * FROM masking_whitelist ORDER BY table_name, column_name`,
	)
	return entries, err
}

func (r *MaskingWhitelistRepo) Create(ctx context.Context, entry *model.MaskingWhitelist) (*model.MaskingWhitelist, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO masking_whitelist (db_connection_id, table_name, column_name, user_id, auth_group_id, auth_group, created_by)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.DBConnectionID, entry.TableName, entry.ColumnName, entry.UserID, entry.AuthGroupID, entry.AuthGroup, entry.CreatedBy,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, uint64(id))
}

func (r *MaskingWhitelistRepo) GetByID(ctx context.Context, id uint64) (*model.MaskingWhitelist, error) {
	var e model.MaskingWhitelist
	err := r.db.GetContext(ctx, &e, `SELECT * FROM masking_whitelist WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

func (r *MaskingWhitelistRepo) Delete(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM masking_whitelist WHERE id = ?`, id)
	return err
}

// IsExempt returns true if the given user is directly whitelisted or belongs to a
// whitelisted auth group for the target table.column on the target connection.
func (r *MaskingWhitelistRepo) IsExempt(ctx context.Context, userID uint64, connID uint64, tableName, columnName string) (bool, error) {
	var count int
	err := r.db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM masking_whitelist mw
		WHERE (mw.db_connection_id IS NULL OR mw.db_connection_id = ?)
		  AND mw.table_name = ? AND mw.column_name = ?
		  AND (
			mw.user_id = ?
			OR mw.auth_group IN (
				SELECT DISTINCT ag.group_key
				FROM auth_groups ag
				INNER JOIN user_auth_groups uag ON uag.auth_group_id = ag.id
				WHERE uag.user_id = ? AND (uag.expires_at IS NULL OR uag.expires_at > ?)
				UNION
				SELECT DISTINCT ag.group_key
				FROM auth_groups ag
				INNER JOIN auth_group_memberships agm ON agm.auth_group = ag.group_key
				WHERE agm.user_id = ? AND (agm.expires_at IS NULL OR agm.expires_at > ?)
			)
		  )
	`, connID, tableName, columnName, userID, userID, time.Now(), userID, time.Now())
	return count > 0, err
}
