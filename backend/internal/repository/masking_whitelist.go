package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
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
		`SELECT id, db_connection_id, database_name, schema_name, table_name, column_name, created_by, created_at
		 FROM masking_whitelist
		 ORDER BY db_connection_id, database_name, schema_name, table_name, column_name`,
	)
	return entries, err
}

func (r *MaskingWhitelistRepo) Create(ctx context.Context, entry *model.MaskingWhitelist) (*model.MaskingWhitelist, error) {
	exists, err := r.Exists(ctx, entry.DBConnectionID, entry.DatabaseName, entry.SchemaName, entry.TableName, entry.ColumnName, 0)
	if err != nil {
		return nil, fmt.Errorf("check masking whitelist exists: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("masking whitelist already exists")
	}

	res, err := r.db.ExecContext(ctx,
		`INSERT INTO masking_whitelist (db_connection_id, database_name, schema_name, table_name, column_name, created_by, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.DBConnectionID, entry.DatabaseName, entry.SchemaName, entry.TableName, entry.ColumnName, entry.CreatedBy, timeutil.NowUTC(),
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, uint64(id))
}

func (r *MaskingWhitelistRepo) GetByID(ctx context.Context, id uint64) (*model.MaskingWhitelist, error) {
	var e model.MaskingWhitelist
	err := r.db.GetContext(ctx, &e,
		`SELECT id, db_connection_id, database_name, schema_name, table_name, column_name, created_by, created_at
		 FROM masking_whitelist
		 WHERE id = ?`,
		id,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

func (r *MaskingWhitelistRepo) Delete(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM masking_whitelist WHERE id = ?`, id)
	return err
}

func (r *MaskingWhitelistRepo) Patch(ctx context.Context, entry *model.MaskingWhitelist) (*model.MaskingWhitelist, error) {
	exists, err := r.Exists(ctx, entry.DBConnectionID, entry.DatabaseName, entry.SchemaName, entry.TableName, entry.ColumnName, entry.ID)
	if err != nil {
		return nil, fmt.Errorf("check masking whitelist exists: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("masking whitelist already exists")
	}

	_, err = r.db.ExecContext(ctx,
		`UPDATE masking_whitelist
		 SET db_connection_id = ?, database_name = ?, schema_name = ?, table_name = ?, column_name = ?
		 WHERE id = ?`,
		entry.DBConnectionID, entry.DatabaseName, entry.SchemaName, entry.TableName, entry.ColumnName, entry.ID,
	)
	if err != nil {
		return nil, err
	}
	return r.GetByID(ctx, entry.ID)
}

func (r *MaskingWhitelistRepo) Match(ctx context.Context, connID uint64, databaseName, schemaName, tableName, columnName string) (bool, error) {
	var count int
	err := r.db.GetContext(ctx, &count, `
		SELECT COUNT(*)
		FROM masking_whitelist
		WHERE db_connection_id = ?
		  AND LOWER(database_name) = LOWER(?)
		  AND LOWER(schema_name) = LOWER(?)
		  AND LOWER(table_name) = LOWER(?)
		  AND LOWER(column_name) = LOWER(?)
	`, connID, strings.TrimSpace(databaseName), strings.TrimSpace(schemaName), strings.TrimSpace(tableName), strings.TrimSpace(columnName))
	return count > 0, err
}

func (r *MaskingWhitelistRepo) Exists(ctx context.Context, connID uint64, databaseName, schemaName, tableName, columnName string, excludeID uint64) (bool, error) {
	var count int
	query := `
		SELECT COUNT(*)
		FROM masking_whitelist
		WHERE db_connection_id = ?
		  AND LOWER(database_name) = LOWER(?)
		  AND LOWER(schema_name) = LOWER(?)
		  AND LOWER(table_name) = LOWER(?)
		  AND LOWER(column_name) = LOWER(?)`
	args := []any{connID, strings.TrimSpace(databaseName), strings.TrimSpace(schemaName), strings.TrimSpace(tableName), strings.TrimSpace(columnName)}
	if excludeID != 0 {
		query += ` AND id <> ?`
		args = append(args, excludeID)
	}
	if err := r.db.GetContext(ctx, &count, query, args...); err != nil {
		return false, err
	}
	return count > 0, nil
}
