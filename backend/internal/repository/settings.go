package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dbre-maestro/maestro/internal/crypto"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

const (
	settingSensitiveExportReviewers      = "sensitive_export_reviewer_user_ids"
	settingSensitiveQueryAccessReviewers = "sensitive_query_access_reviewer_user_ids"
	settingLarkAppID                     = "lark_app_id"
	settingLarkAppSecret                 = "lark_app_secret"
	settingSQLEditorAppTimeoutSeconds    = "sql_editor_app_timeout_seconds"
	settingSQLEditorMySQLMaxExecTimeMs   = "sql_editor_mysql_max_execution_time_ms"
	settingSQLEditorPGStatementTimeoutMs = "sql_editor_postgres_statement_timeout_ms"
	settingDBMetadataInventoryEnabled    = "db_metadata_inventory_enabled"
	settingDBMetadataInventoryRegions    = "db_metadata_inventory_regions"
	settingDBMetadataInventoryEngines    = "db_metadata_inventory_engines"
	settingDBMetadataInventorySyncMins   = "db_metadata_inventory_sync_interval_minutes"
	settingDBMetadataObjectEnabled       = "db_metadata_object_enabled"
	settingDBMetadataObjectConnectionIDs = "db_metadata_object_enabled_connection_ids"
	settingDBMetadataObjectSyncMins      = "db_metadata_object_sync_interval_minutes"
)

type SettingsRepo struct {
	db     *sqlx.DB
	encKey []byte
}

func NewSettingsRepo(db *sqlx.DB, encKey []byte) *SettingsRepo {
	return &SettingsRepo{db: db, encKey: encKey}
}

func (r *SettingsRepo) Get(ctx context.Context) (*model.PlatformSettings, error) {
	settings := &model.PlatformSettings{
		SQLEditorAppTimeoutSeconds:           30,
		SQLEditorMySQLMaxExecutionTimeMs:     25000,
		SQLEditorPostgresStatementTimeoutMs:  25000,
		DBMetadataInventoryEnabled:           true,
		DBMetadataInventoryRegions:           []string{},
		DBMetadataInventoryEngines:           []string{"aurora-mysql", "aurora-postgresql", "redis"},
		DBMetadataInventorySyncIntervalMins:  5,
		DBMetadataObjectEnabled:              true,
		DBMetadataObjectEnabledConnectionIDs: []uint64{},
		DBMetadataObjectSyncIntervalMins:     60,
	}
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
	larkAppID, err := r.getString(ctx, settingLarkAppID)
	if err != nil {
		return nil, err
	}
	if larkAppID != nil {
		settings.LarkAppID = *larkAppID
	}
	larkAppSecretConfigured, err := r.hasValue(ctx, settingLarkAppSecret)
	if err != nil {
		return nil, err
	}
	settings.LarkAppSecretConfigured = larkAppSecretConfigured
	sqlEditorAppTimeoutSeconds, err := r.getInt(ctx, settingSQLEditorAppTimeoutSeconds)
	if err != nil {
		return nil, err
	}
	if sqlEditorAppTimeoutSeconds != nil {
		settings.SQLEditorAppTimeoutSeconds = *sqlEditorAppTimeoutSeconds
	}
	sqlEditorMySQLMaxExecTimeMs, err := r.getInt(ctx, settingSQLEditorMySQLMaxExecTimeMs)
	if err != nil {
		return nil, err
	}
	if sqlEditorMySQLMaxExecTimeMs != nil {
		settings.SQLEditorMySQLMaxExecutionTimeMs = *sqlEditorMySQLMaxExecTimeMs
	}
	sqlEditorPGStatementTimeoutMs, err := r.getInt(ctx, settingSQLEditorPGStatementTimeoutMs)
	if err != nil {
		return nil, err
	}
	if sqlEditorPGStatementTimeoutMs != nil {
		settings.SQLEditorPostgresStatementTimeoutMs = *sqlEditorPGStatementTimeoutMs
	}

	inventoryEnabled, err := r.getBool(ctx, settingDBMetadataInventoryEnabled)
	if err != nil {
		return nil, err
	}
	if inventoryEnabled != nil {
		settings.DBMetadataInventoryEnabled = *inventoryEnabled
	}

	regions, err := r.getStringList(ctx, settingDBMetadataInventoryRegions)
	if err != nil {
		return nil, err
	}
	if regions != nil {
		settings.DBMetadataInventoryRegions = regions
	}

	engines, err := r.getStringList(ctx, settingDBMetadataInventoryEngines)
	if err != nil {
		return nil, err
	}
	if engines != nil {
		settings.DBMetadataInventoryEngines = engines
	}

	inventorySyncMins, err := r.getInt(ctx, settingDBMetadataInventorySyncMins)
	if err != nil {
		return nil, err
	}
	if inventorySyncMins != nil {
		settings.DBMetadataInventorySyncIntervalMins = *inventorySyncMins
	}

	objectEnabled, err := r.getBool(ctx, settingDBMetadataObjectEnabled)
	if err != nil {
		return nil, err
	}
	if objectEnabled != nil {
		settings.DBMetadataObjectEnabled = *objectEnabled
	}

	objectConnectionIDs, err := r.getUint64List(ctx, settingDBMetadataObjectConnectionIDs)
	if err != nil {
		return nil, err
	}
	settings.DBMetadataObjectEnabledConnectionIDs = objectConnectionIDs

	objectSyncMins, err := r.getInt(ctx, settingDBMetadataObjectSyncMins)
	if err != nil {
		return nil, err
	}
	if objectSyncMins != nil {
		settings.DBMetadataObjectSyncIntervalMins = *objectSyncMins
	}
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
	if err := upsertString(ctx, tx, settingLarkAppID, settings.LarkAppID); err != nil {
		return err
	}
	if settings.LarkAppSecret != "" {
		if err := r.upsertEncryptedString(ctx, tx, settingLarkAppSecret, settings.LarkAppSecret); err != nil {
			return err
		}
	}
	if err := upsertInt(ctx, tx, settingSQLEditorAppTimeoutSeconds, settings.SQLEditorAppTimeoutSeconds); err != nil {
		return err
	}
	if err := upsertInt(ctx, tx, settingSQLEditorMySQLMaxExecTimeMs, settings.SQLEditorMySQLMaxExecutionTimeMs); err != nil {
		return err
	}
	if err := upsertInt(ctx, tx, settingSQLEditorPGStatementTimeoutMs, settings.SQLEditorPostgresStatementTimeoutMs); err != nil {
		return err
	}
	if err := upsertBool(ctx, tx, settingDBMetadataInventoryEnabled, settings.DBMetadataInventoryEnabled); err != nil {
		return err
	}
	if err := upsertStringList(ctx, tx, settingDBMetadataInventoryRegions, settings.DBMetadataInventoryRegions); err != nil {
		return err
	}
	if err := upsertStringList(ctx, tx, settingDBMetadataInventoryEngines, settings.DBMetadataInventoryEngines); err != nil {
		return err
	}
	if err := upsertInt(ctx, tx, settingDBMetadataInventorySyncMins, settings.DBMetadataInventorySyncIntervalMins); err != nil {
		return err
	}
	if err := upsertBool(ctx, tx, settingDBMetadataObjectEnabled, settings.DBMetadataObjectEnabled); err != nil {
		return err
	}
	if err := upsertUint64List(ctx, tx, settingDBMetadataObjectConnectionIDs, settings.DBMetadataObjectEnabledConnectionIDs); err != nil {
		return err
	}
	if err := upsertInt(ctx, tx, settingDBMetadataObjectSyncMins, settings.DBMetadataObjectSyncIntervalMins); err != nil {
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

func (r *SettingsRepo) GetLarkAppSecret(ctx context.Context) (string, error) {
	return r.getEncryptedString(ctx, settingLarkAppSecret)
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

func (r *SettingsRepo) getStringList(ctx context.Context, key string) ([]string, error) {
	var raw string
	err := r.db.GetContext(ctx, &raw, `SELECT value FROM platform_settings WHERE key_name = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get setting %s: %w", key, err)
	}
	if raw == "" {
		return []string{}, nil
	}
	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("decode setting %s: %w", key, err)
	}
	return items, nil
}

func (r *SettingsRepo) getString(ctx context.Context, key string) (*string, error) {
	var raw string
	err := r.db.GetContext(ctx, &raw, `SELECT value FROM platform_settings WHERE key_name = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get setting %s: %w", key, err)
	}
	var value string
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf("decode setting %s: %w", key, err)
	}
	return &value, nil
}

func (r *SettingsRepo) getBool(ctx context.Context, key string) (*bool, error) {
	var raw string
	err := r.db.GetContext(ctx, &raw, `SELECT value FROM platform_settings WHERE key_name = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get setting %s: %w", key, err)
	}
	var value bool
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf("decode setting %s: %w", key, err)
	}
	return &value, nil
}

func (r *SettingsRepo) getInt(ctx context.Context, key string) (*int, error) {
	var raw string
	err := r.db.GetContext(ctx, &raw, `SELECT value FROM platform_settings WHERE key_name = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get setting %s: %w", key, err)
	}
	var value int
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf("decode setting %s: %w", key, err)
	}
	return &value, nil
}

func (r *SettingsRepo) hasValue(ctx context.Context, key string) (bool, error) {
	var count int
	if err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM platform_settings WHERE key_name = ? AND value <> ''`, key); err != nil {
		return false, fmt.Errorf("count setting %s: %w", key, err)
	}
	return count > 0, nil
}

func (r *SettingsRepo) getEncryptedString(ctx context.Context, key string) (string, error) {
	if len(r.encKey) == 0 {
		return "", errors.New("settings encryption key is not configured")
	}
	var raw string
	err := r.db.GetContext(ctx, &raw, `SELECT value FROM platform_settings WHERE key_name = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting %s: %w", key, err)
	}
	if raw == "" {
		return "", nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("decode encrypted setting %s: %w", key, err)
	}
	plain, err := crypto.Decrypt(r.encKey, ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt setting %s: %w", key, err)
	}
	return string(plain), nil
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

func upsertStringList(ctx context.Context, tx *sqlx.Tx, key string, items []string) error {
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

func upsertBool(ctx context.Context, tx *sqlx.Tx, key string, value bool) error {
	raw, err := json.Marshal(value)
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

func upsertString(ctx context.Context, tx *sqlx.Tx, key, value string) error {
	raw, err := json.Marshal(value)
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

func (r *SettingsRepo) upsertEncryptedString(ctx context.Context, tx *sqlx.Tx, key, value string) error {
	if len(r.encKey) == 0 {
		return errors.New("settings encryption key is not configured")
	}
	ciphertext, err := crypto.Encrypt(r.encKey, []byte(value))
	if err != nil {
		return fmt.Errorf("encrypt setting %s: %w", key, err)
	}
	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO platform_settings (key_name, value) VALUES (?, ?)
		 ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = CURRENT_TIMESTAMP`,
		key, encoded,
	); err != nil {
		return fmt.Errorf("upsert encrypted setting %s: %w", key, err)
	}
	return nil
}

func upsertInt(ctx context.Context, tx *sqlx.Tx, key string, value int) error {
	raw, err := json.Marshal(value)
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
