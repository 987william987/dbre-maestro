package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

type SQLReviewRuleRepo struct {
	db *sqlx.DB
}

func NewSQLReviewRuleRepo(db *sqlx.DB) *SQLReviewRuleRepo {
	return &SQLReviewRuleRepo{db: db}
}

func (r *SQLReviewRuleRepo) List(ctx context.Context) ([]model.SQLReviewRule, error) {
	var rules []model.SQLReviewRule
	err := r.db.SelectContext(ctx, &rules,
		`SELECT * FROM sql_review_rules ORDER BY rule_name`)
	return rules, err
}

func (r *SQLReviewRuleRepo) GetByName(ctx context.Context, name string) (*model.SQLReviewRule, error) {
	var rule model.SQLReviewRule
	err := r.db.GetContext(ctx, &rule,
		`SELECT * FROM sql_review_rules WHERE rule_name = ?`, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &rule, err
}

// Patch updates enabled and/or threshold for an existing rule.
// Only fields present in the patch map are written.
func (r *SQLReviewRuleRepo) Patch(ctx context.Context, name string, enabled *bool, threshold *int64, updatedBy uint64) error {
	if enabled == nil && threshold == nil {
		return nil
	}

	query := `UPDATE sql_review_rules SET updated_by = ?`
	args := []any{updatedBy}

	if enabled != nil {
		query += `, enabled = ?`
		args = append(args, *enabled)
	}
	if threshold != nil {
		query += `, threshold = ?`
		args = append(args, *threshold)
	}
	query += ` WHERE rule_name = ?`
	args = append(args, name)

	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}
