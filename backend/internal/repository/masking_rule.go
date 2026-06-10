package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
		`SELECT * FROM masking_rules ORDER BY table_name, column_name`)
	return rules, err
}

func (r *MaskingRuleRepo) Create(ctx context.Context, rule *model.MaskingRule) (*model.MaskingRule, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO masking_rules (db_connection_id, table_name, column_name, mask_mode, created_by)
		 VALUES (?, ?, ?, ?, ?)`,
		rule.DBConnectionID, rule.TableName, rule.ColumnName, rule.MaskMode, rule.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("create masking rule: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, uint64(id))
}

func (r *MaskingRuleRepo) GetByID(ctx context.Context, id uint64) (*model.MaskingRule, error) {
	var rule model.MaskingRule
	err := r.db.GetContext(ctx, &rule, `SELECT * FROM masking_rules WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &rule, err
}

func (r *MaskingRuleRepo) Delete(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM masking_rules WHERE id = ?`, id)
	return err
}

// ListForConnection returns rules that apply to a given connection (global rules + connection-specific rules).
func (r *MaskingRuleRepo) ListForConnection(ctx context.Context, connID uint64) ([]model.MaskingRule, error) {
	var rules []model.MaskingRule
	err := r.db.SelectContext(ctx, &rules,
		`SELECT * FROM masking_rules
		 WHERE db_connection_id IS NULL OR db_connection_id = ?
		 ORDER BY db_connection_id DESC, table_name, column_name`,
		connID,
	)
	return rules, err
}
