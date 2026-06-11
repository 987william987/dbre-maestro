package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

type MaskingRuleRepo struct {
	db *sqlx.DB
}

func NewMaskingRuleRepo(db *sqlx.DB) *MaskingRuleRepo {
	return &MaskingRuleRepo{db: db}
}

func (r *MaskingRuleRepo) List(ctx context.Context) ([]model.MaskingRule, error) {
	var rules []model.MaskingRule
	err := r.db.SelectContext(ctx, &rules,
		`SELECT id, column_name, mask_mode, created_by, created_at
		 FROM masking_rules
		 WHERE db_connection_id IS NULL
		   AND COALESCE(database_name, '') = ''
		   AND COALESCE(schema_name, '') = ''
		   AND COALESCE(table_name, '') = ''
		 ORDER BY column_name`)
	return rules, err
}

func (r *MaskingRuleRepo) Create(ctx context.Context, rule *model.MaskingRule) (*model.MaskingRule, error) {
	exists, err := r.ExistsGlobal(ctx, rule.ColumnName, 0)
	if err != nil {
		return nil, fmt.Errorf("check masking rule exists: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("masking rule already exists")
	}

	res, err := r.db.ExecContext(ctx,
		`INSERT INTO masking_rules (db_connection_id, database_name, schema_name, table_name, column_name, mask_mode, created_by)
		 VALUES (NULL, '', '', '', ?, ?, ?)`,
		rule.ColumnName, rule.MaskMode, rule.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("create masking rule: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, uint64(id))
}

func (r *MaskingRuleRepo) GetByID(ctx context.Context, id uint64) (*model.MaskingRule, error) {
	var rule model.MaskingRule
	err := r.db.GetContext(ctx, &rule,
		`SELECT id, column_name, mask_mode, created_by, created_at
		 FROM masking_rules
		 WHERE id = ?
		   AND db_connection_id IS NULL
		   AND COALESCE(database_name, '') = ''
		   AND COALESCE(schema_name, '') = ''
		   AND COALESCE(table_name, '') = ''`,
		id,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &rule, err
}

func (r *MaskingRuleRepo) Delete(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM masking_rules WHERE id = ?`, id)
	return err
}

func (r *MaskingRuleRepo) Patch(ctx context.Context, rule *model.MaskingRule) (*model.MaskingRule, error) {
	exists, err := r.ExistsGlobal(ctx, rule.ColumnName, rule.ID)
	if err != nil {
		return nil, fmt.Errorf("check masking rule exists: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("masking rule already exists")
	}

	_, err = r.db.ExecContext(ctx,
		`UPDATE masking_rules
		 SET db_connection_id = NULL, database_name = '', schema_name = '', table_name = '', column_name = ?, mask_mode = ?
		 WHERE id = ?`,
		rule.ColumnName, rule.MaskMode, rule.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("patch masking rule: %w", err)
	}
	return r.GetByID(ctx, rule.ID)
}

// ListForConnection returns global rules that apply to MySQL query/export masking.
func (r *MaskingRuleRepo) ListForConnection(ctx context.Context, connID uint64) ([]model.MaskingRule, error) {
	var rules []model.MaskingRule
	err := r.db.SelectContext(ctx, &rules,
		`SELECT id, column_name, mask_mode, created_by, created_at
		 FROM masking_rules
		 WHERE db_connection_id IS NULL
		   AND COALESCE(database_name, '') = ''
		   AND COALESCE(schema_name, '') = ''
		   AND COALESCE(table_name, '') = ''
		 ORDER BY column_name`,
	)
	return rules, err
}

func (r *MaskingRuleRepo) ExistsGlobal(ctx context.Context, columnName string, excludeID uint64) (bool, error) {
	var count int
	query := `
		SELECT COUNT(*)
		FROM masking_rules
		WHERE db_connection_id IS NULL
		  AND COALESCE(database_name, '') = ''
		  AND COALESCE(schema_name, '') = ''
		  AND COALESCE(table_name, '') = ''
		  AND LOWER(column_name) = LOWER(?)`
	args := []any{strings.TrimSpace(columnName)}
	if excludeID != 0 {
		query += ` AND id <> ?`
		args = append(args, excludeID)
	}
	if err := r.db.GetContext(ctx, &count, query, args...); err != nil {
		return false, err
	}
	return count > 0, nil
}
