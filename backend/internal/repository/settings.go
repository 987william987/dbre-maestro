package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

const (
	settingSensitiveExportReviewers      = "sensitive_export_reviewer_user_ids"
	settingSensitiveQueryAccessReviewers = "sensitive_query_access_reviewer_user_ids"
)

type SettingsRepo struct {
	db *sqlx.DB
}

func NewSettingsRepo(db *sqlx.DB) *SettingsRepo {
	return &SettingsRepo{db: db}
}

func (r *SettingsRepo) Get(ctx context.Context) (*model.PlatformSettings, error) {
	settings := &model.PlatformSettings{}
	exportReviewerIDs, err := r.getUint64List(ctx, settingSensitiveExportReviewers)
	if err != nil {
		return nil, err
	}
	sensitiveReviewerIDs, err := r.getUint64List(ctx, settingSensitiveQueryAccessReviewers)
	if err != nil {
		return nil, err
	}
	settings.SensitiveExportReviewerUserIDs = exportReviewerIDs
	settings.SensitiveQueryAccessReviewerUserIDs = sensitiveReviewerIDs
	return settings, nil
}

func (r *SettingsRepo) Replace(ctx context.Context, settings *model.PlatformSettings) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if err := upsertUint64List(ctx, tx, settingSensitiveExportReviewers, settings.SensitiveExportReviewerUserIDs); err != nil {
		return err
	}
	if err := upsertUint64List(ctx, tx, settingSensitiveQueryAccessReviewers, settings.SensitiveQueryAccessReviewerUserIDs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings tx: %w", err)
	}
	tx = nil
	return nil
}

func (r *SettingsRepo) IsSensitiveExportReviewer(ctx context.Context, userID uint64) (bool, error) {
	return r.containsUserID(ctx, settingSensitiveExportReviewers, userID)
}

func (r *SettingsRepo) IsSensitiveQueryAccessReviewer(ctx context.Context, userID uint64) (bool, error) {
	return r.containsUserID(ctx, settingSensitiveQueryAccessReviewers, userID)
}

func (r *SettingsRepo) containsUserID(ctx context.Context, key string, userID uint64) (bool, error) {
	items, err := r.getUint64List(ctx, key)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item == userID {
			return true, nil
		}
	}
	return false, nil
}

func (r *SettingsRepo) getUint64List(ctx context.Context, key string) ([]uint64, error) {
	var raw string
	err := r.db.GetContext(ctx, &raw, `SELECT value FROM platform_settings WHERE key_name = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return []uint64{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get setting %s: %w", key, err)
	}
	if raw == "" {
		return []uint64{}, nil
	}
	var items []uint64
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("decode setting %s: %w", key, err)
	}
	return items, nil
}

func upsertUint64List(ctx context.Context, tx *sqlx.Tx, key string, items []uint64) error {
	raw, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("encode setting %s: %w", key, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO platform_settings (key_name, value) VALUES (?, ?)
		 ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = CURRENT_TIMESTAMP`,
		key, string(raw),
	); err != nil {
		return fmt.Errorf("upsert setting %s: %w", key, err)
	}
	return nil
}
