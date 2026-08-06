package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dbre-maestro/maestro/internal/crypto"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

const (
	settingSensitiveExportReviewers      = "sensitive_export_reviewer_user_ids"
	settingSensitiveQueryAccessReviewers = "sensitive_query_access_reviewer_user_ids"
	settingRequireNonSensitiveExportRev  = "require_non_sensitive_export_review"
	settingLarkAppID                     = "lark_app_id"
	settingLarkAppSecret                 = "lark_app_secret"
	settingLarkInteractiveCardsEnabled   = "lark_interactive_cards_enabled"
	settingLarkCardCallbackMode          = "lark_card_callback_mode"
	settingLarkCardVerificationToken     = "lark_card_verification_token"
	settingLarkOAuthEnabled              = "lark_oauth_enabled"
	settingLarkOAuthSite                 = "lark_oauth_site"
	settingLarkOAuthRedirectURL          = "lark_oauth_redirect_url"
	settingSSOOIDCEnabled                = "sso_oidc_enabled"
	settingSSOOIDCDisplayName            = "sso_oidc_display_name"
	settingSSOOIDCIssuerURL              = "sso_oidc_issuer_url"
	settingSSOOIDCClientID               = "sso_oidc_client_id"
	settingSSOOIDCClientSecret           = "sso_oidc_client_secret"
	settingSSOOIDCRedirectURL            = "sso_oidc_redirect_url"
	settingSSOOIDCScopes                 = "sso_oidc_scopes"
	settingSSOOIDCTrustMFA               = "sso_oidc_trust_mfa"
	settingSQLEditorAppTimeoutSeconds    = "sql_editor_app_timeout_seconds"
	settingSQLEditorMySQLMaxExecTimeMs   = "sql_editor_mysql_max_execution_time_ms"
	settingSQLEditorPGStatementTimeoutMs = "sql_editor_postgres_statement_timeout_ms"
	settingSQLExportAppTimeoutSeconds    = "sql_export_app_timeout_seconds"
	settingSQLExportMySQLMaxExecTimeMs   = "sql_export_mysql_max_execution_time_ms"
	settingSQLExportPGStatementTimeoutMs = "sql_export_postgres_statement_timeout_ms"
	settingMySQLRollbackEnabled          = "mysql_rollback_enabled"
	settingMySQLRollbackMy2SQLPath       = "mysql_rollback_my2sql_path"
	settingMySQLRollbackTimeoutSeconds   = "mysql_rollback_generation_timeout_seconds"
	settingMySQLRollbackMaxSQLBytes      = "mysql_rollback_max_sql_bytes"
	settingDBMetadataInventoryEnabled    = "db_metadata_inventory_enabled"
	settingDBMetadataInventoryRegions    = "db_metadata_inventory_regions"
	settingDBMetadataInventoryEngines    = "db_metadata_inventory_engines"
	settingDBMetadataInventoryCron       = "db_metadata_inventory_cron"
	settingDBMetadataInventorySyncMins   = "db_metadata_inventory_sync_interval_minutes"
	settingDBMetadataObjectEnabled       = "db_metadata_object_enabled"
	settingDBMetadataObjectConnectionIDs = "db_metadata_object_enabled_connection_ids"
	settingDBMetadataObjectCron          = "db_metadata_object_cron"
	settingDBMetadataObjectSyncMins      = "db_metadata_object_sync_interval_minutes"
	settingDBMetadataCronTimezone        = "db_metadata_cron_timezone"
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
		SQLEditorAppTimeoutSeconds:            30,
		RequireNonSensitiveExportReview:       true,
		SQLEditorMySQLMaxExecutionTimeMs:      25000,
		SQLEditorPostgresStatementTimeoutMs:   25000,
		SQLExportAppTimeoutSeconds:            30,
		SQLExportMySQLMaxExecutionTimeMs:      25000,
		SQLExportPostgresStatementTimeoutMs:   25000,
		MySQLRollbackMy2SQLPath:               "my2sql",
		MySQLRollbackGenerationTimeoutSeconds: 30,
		MySQLRollbackMaxSQLBytes:              5 * 1024 * 1024,
		DBMetadataInventoryEnabled:            true,
		DBMetadataInventoryRegions:            []string{},
		DBMetadataInventoryEngines:            []string{"aurora-mysql", "aurora-postgresql", "redis"},
		DBMetadataInventoryCron:               "0 9 * * *",
		DBMetadataInventorySyncIntervalMins:   5,
		DBMetadataObjectEnabled:               true,
		DBMetadataObjectEnabledConnectionIDs:  []uint64{},
		DBMetadataObjectCron:                  "0 10 * * *",
		DBMetadataObjectSyncIntervalMins:      60,
		DBMetadataCronTimezone:                "Asia/Taipei",
		LarkOAuthSite:                         "lark",
		LarkCardCallbackMode:                  "http",
		SSOOIDCDisplayName:                    "Authentik",
		SSOOIDCScopes:                         []string{"openid", "profile", "email", "dbre"},
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
	settings.ApprovalPolicies = []model.ApprovalPolicy{}
	workflowRules, err := r.ListWorkflowRules(ctx)
	if err != nil {
		return nil, err
	}
	settings.WorkflowRules = workflowRules
	requireNonSensitiveExportReview, err := r.getBool(ctx, settingRequireNonSensitiveExportRev)
	if err != nil {
		return nil, err
	}
	if requireNonSensitiveExportReview != nil {
		settings.RequireNonSensitiveExportReview = *requireNonSensitiveExportReview
	}
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
	larkInteractiveCardsEnabled, err := r.getBool(ctx, settingLarkInteractiveCardsEnabled)
	if err != nil {
		return nil, err
	}
	if larkInteractiveCardsEnabled != nil {
		settings.LarkInteractiveCardsEnabled = *larkInteractiveCardsEnabled
	}
	larkCardCallbackMode, err := r.getString(ctx, settingLarkCardCallbackMode)
	if err != nil {
		return nil, err
	}
	if larkCardCallbackMode != nil {
		settings.LarkCardCallbackMode = normalizeLarkCardCallbackMode(*larkCardCallbackMode)
	}
	larkCardVerificationTokenConfigured, err := r.hasValue(ctx, settingLarkCardVerificationToken)
	if err != nil {
		return nil, err
	}
	settings.LarkCardVerificationTokenConfigured = larkCardVerificationTokenConfigured
	larkOAuthEnabled, err := r.getBool(ctx, settingLarkOAuthEnabled)
	if err != nil {
		return nil, err
	}
	if larkOAuthEnabled != nil {
		settings.LarkOAuthEnabled = *larkOAuthEnabled
	}
	larkOAuthSite, err := r.getString(ctx, settingLarkOAuthSite)
	if err != nil {
		return nil, err
	}
	if larkOAuthSite != nil && *larkOAuthSite != "" {
		settings.LarkOAuthSite = normalizeLarkOAuthSite(*larkOAuthSite)
	}
	larkOAuthRedirectURL, err := r.getString(ctx, settingLarkOAuthRedirectURL)
	if err != nil {
		return nil, err
	}
	if larkOAuthRedirectURL != nil {
		settings.LarkOAuthRedirectURL = strings.TrimSpace(*larkOAuthRedirectURL)
	}
	ssoOIDCEnabled, err := r.getBool(ctx, settingSSOOIDCEnabled)
	if err != nil {
		return nil, err
	}
	if ssoOIDCEnabled != nil {
		settings.SSOOIDCEnabled = *ssoOIDCEnabled
	}
	ssoOIDCDisplayName, err := r.getString(ctx, settingSSOOIDCDisplayName)
	if err != nil {
		return nil, err
	}
	if ssoOIDCDisplayName != nil && strings.TrimSpace(*ssoOIDCDisplayName) != "" {
		settings.SSOOIDCDisplayName = strings.TrimSpace(*ssoOIDCDisplayName)
	}
	if settings.SSOOIDCDisplayName == "" {
		settings.SSOOIDCDisplayName = "Authentik"
	}
	ssoOIDCIssuerURL, err := r.getString(ctx, settingSSOOIDCIssuerURL)
	if err != nil {
		return nil, err
	}
	if ssoOIDCIssuerURL != nil {
		settings.SSOOIDCIssuerURL = strings.TrimRight(strings.TrimSpace(*ssoOIDCIssuerURL), "/")
	}
	ssoOIDCClientID, err := r.getString(ctx, settingSSOOIDCClientID)
	if err != nil {
		return nil, err
	}
	if ssoOIDCClientID != nil {
		settings.SSOOIDCClientID = strings.TrimSpace(*ssoOIDCClientID)
	}
	ssoOIDCSecretConfigured, err := r.hasValue(ctx, settingSSOOIDCClientSecret)
	if err != nil {
		return nil, err
	}
	settings.SSOOIDCClientSecretConfigured = ssoOIDCSecretConfigured
	ssoOIDCRedirectURL, err := r.getString(ctx, settingSSOOIDCRedirectURL)
	if err != nil {
		return nil, err
	}
	if ssoOIDCRedirectURL != nil {
		settings.SSOOIDCRedirectURL = strings.TrimSpace(*ssoOIDCRedirectURL)
	}
	ssoOIDCScopes, err := r.getStringList(ctx, settingSSOOIDCScopes)
	if err != nil {
		return nil, err
	}
	if len(ssoOIDCScopes) > 0 {
		settings.SSOOIDCScopes = ssoOIDCScopes
	}
	ssoOIDCTrustMFA, err := r.getBool(ctx, settingSSOOIDCTrustMFA)
	if err != nil {
		return nil, err
	}
	if ssoOIDCTrustMFA != nil {
		settings.SSOOIDCTrustMFA = *ssoOIDCTrustMFA
	}
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
	sqlExportAppTimeoutSeconds, err := r.getInt(ctx, settingSQLExportAppTimeoutSeconds)
	if err != nil {
		return nil, err
	}
	if sqlExportAppTimeoutSeconds != nil {
		settings.SQLExportAppTimeoutSeconds = *sqlExportAppTimeoutSeconds
	}
	sqlExportMySQLMaxExecTimeMs, err := r.getInt(ctx, settingSQLExportMySQLMaxExecTimeMs)
	if err != nil {
		return nil, err
	}
	if sqlExportMySQLMaxExecTimeMs != nil {
		settings.SQLExportMySQLMaxExecutionTimeMs = *sqlExportMySQLMaxExecTimeMs
	}
	sqlExportPGStatementTimeoutMs, err := r.getInt(ctx, settingSQLExportPGStatementTimeoutMs)
	if err != nil {
		return nil, err
	}
	if sqlExportPGStatementTimeoutMs != nil {
		settings.SQLExportPostgresStatementTimeoutMs = *sqlExportPGStatementTimeoutMs
	}
	mysqlRollbackEnabled, err := r.getBool(ctx, settingMySQLRollbackEnabled)
	if err != nil {
		return nil, err
	}
	if mysqlRollbackEnabled != nil {
		settings.MySQLRollbackEnabled = *mysqlRollbackEnabled
	}
	mysqlRollbackMy2SQLPath, err := r.getString(ctx, settingMySQLRollbackMy2SQLPath)
	if err != nil {
		return nil, err
	}
	if mysqlRollbackMy2SQLPath != nil && strings.TrimSpace(*mysqlRollbackMy2SQLPath) != "" {
		settings.MySQLRollbackMy2SQLPath = strings.TrimSpace(*mysqlRollbackMy2SQLPath)
	}
	mysqlRollbackTimeoutSeconds, err := r.getInt(ctx, settingMySQLRollbackTimeoutSeconds)
	if err != nil {
		return nil, err
	}
	if mysqlRollbackTimeoutSeconds != nil {
		settings.MySQLRollbackGenerationTimeoutSeconds = *mysqlRollbackTimeoutSeconds
	}
	mysqlRollbackMaxSQLBytes, err := r.getInt(ctx, settingMySQLRollbackMaxSQLBytes)
	if err != nil {
		return nil, err
	}
	if mysqlRollbackMaxSQLBytes != nil {
		settings.MySQLRollbackMaxSQLBytes = *mysqlRollbackMaxSQLBytes
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
	inventoryCron, err := r.getString(ctx, settingDBMetadataInventoryCron)
	if err != nil {
		return nil, err
	}
	if inventoryCron != nil && *inventoryCron != "" {
		settings.DBMetadataInventoryCron = *inventoryCron
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
	objectCron, err := r.getString(ctx, settingDBMetadataObjectCron)
	if err != nil {
		return nil, err
	}
	if objectCron != nil && *objectCron != "" {
		settings.DBMetadataObjectCron = *objectCron
	}

	objectSyncMins, err := r.getInt(ctx, settingDBMetadataObjectSyncMins)
	if err != nil {
		return nil, err
	}
	if objectSyncMins != nil {
		settings.DBMetadataObjectSyncIntervalMins = *objectSyncMins
	}
	cronTimezone, err := r.getString(ctx, settingDBMetadataCronTimezone)
	if err != nil {
		return nil, err
	}
	if cronTimezone != nil && *cronTimezone != "" {
		settings.DBMetadataCronTimezone = *cronTimezone
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
	if err := upsertBool(ctx, tx, settingRequireNonSensitiveExportRev, settings.RequireNonSensitiveExportReview); err != nil {
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
	if err := upsertBool(ctx, tx, settingLarkInteractiveCardsEnabled, settings.LarkInteractiveCardsEnabled); err != nil {
		return err
	}
	if err := upsertString(ctx, tx, settingLarkCardCallbackMode, normalizeLarkCardCallbackMode(settings.LarkCardCallbackMode)); err != nil {
		return err
	}
	if settings.LarkCardVerificationToken != "" {
		if err := r.upsertEncryptedString(ctx, tx, settingLarkCardVerificationToken, settings.LarkCardVerificationToken); err != nil {
			return err
		}
	}
	if err := upsertBool(ctx, tx, settingLarkOAuthEnabled, settings.LarkOAuthEnabled); err != nil {
		return err
	}
	if err := upsertString(ctx, tx, settingLarkOAuthSite, normalizeLarkOAuthSite(settings.LarkOAuthSite)); err != nil {
		return err
	}
	if err := upsertString(ctx, tx, settingLarkOAuthRedirectURL, strings.TrimSpace(settings.LarkOAuthRedirectURL)); err != nil {
		return err
	}
	if err := upsertBool(ctx, tx, settingSSOOIDCEnabled, settings.SSOOIDCEnabled); err != nil {
		return err
	}
	if err := upsertString(ctx, tx, settingSSOOIDCDisplayName, strings.TrimSpace(settings.SSOOIDCDisplayName)); err != nil {
		return err
	}
	if err := upsertString(ctx, tx, settingSSOOIDCIssuerURL, strings.TrimRight(strings.TrimSpace(settings.SSOOIDCIssuerURL), "/")); err != nil {
		return err
	}
	if err := upsertString(ctx, tx, settingSSOOIDCClientID, strings.TrimSpace(settings.SSOOIDCClientID)); err != nil {
		return err
	}
	if settings.SSOOIDCClientSecret != "" {
		if err := r.upsertEncryptedString(ctx, tx, settingSSOOIDCClientSecret, settings.SSOOIDCClientSecret); err != nil {
			return err
		}
	}
	if err := upsertString(ctx, tx, settingSSOOIDCRedirectURL, strings.TrimSpace(settings.SSOOIDCRedirectURL)); err != nil {
		return err
	}
	if err := upsertStringList(ctx, tx, settingSSOOIDCScopes, defaultOIDCScopes(settings.SSOOIDCScopes)); err != nil {
		return err
	}
	if err := upsertBool(ctx, tx, settingSSOOIDCTrustMFA, settings.SSOOIDCTrustMFA); err != nil {
		return err
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
	if err := upsertInt(ctx, tx, settingSQLExportAppTimeoutSeconds, settings.SQLExportAppTimeoutSeconds); err != nil {
		return err
	}
	if err := upsertInt(ctx, tx, settingSQLExportMySQLMaxExecTimeMs, settings.SQLExportMySQLMaxExecutionTimeMs); err != nil {
		return err
	}
	if err := upsertInt(ctx, tx, settingSQLExportPGStatementTimeoutMs, settings.SQLExportPostgresStatementTimeoutMs); err != nil {
		return err
	}
	if err := upsertBool(ctx, tx, settingMySQLRollbackEnabled, settings.MySQLRollbackEnabled); err != nil {
		return err
	}
	if err := upsertString(ctx, tx, settingMySQLRollbackMy2SQLPath, strings.TrimSpace(settings.MySQLRollbackMy2SQLPath)); err != nil {
		return err
	}
	if err := upsertInt(ctx, tx, settingMySQLRollbackTimeoutSeconds, settings.MySQLRollbackGenerationTimeoutSeconds); err != nil {
		return err
	}
	if err := upsertInt(ctx, tx, settingMySQLRollbackMaxSQLBytes, settings.MySQLRollbackMaxSQLBytes); err != nil {
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
	if err := upsertString(ctx, tx, settingDBMetadataInventoryCron, settings.DBMetadataInventoryCron); err != nil {
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
	if err := upsertString(ctx, tx, settingDBMetadataObjectCron, settings.DBMetadataObjectCron); err != nil {
		return err
	}
	if err := upsertInt(ctx, tx, settingDBMetadataObjectSyncMins, settings.DBMetadataObjectSyncIntervalMins); err != nil {
		return err
	}
	if err := upsertString(ctx, tx, settingDBMetadataCronTimezone, settings.DBMetadataCronTimezone); err != nil {
		return err
	}
	if settings.WorkflowRules != nil {
		if err := replaceWorkflowRules(ctx, tx, settings.WorkflowRules); err != nil {
			return err
		}
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

func (r *SettingsRepo) RequireNonSensitiveExportReview(ctx context.Context) (bool, error) {
	value, err := r.getBool(ctx, settingRequireNonSensitiveExportRev)
	if err != nil {
		return true, err
	}
	if value == nil {
		return true, nil
	}
	return *value, nil
}

func (r *SettingsRepo) ListApprovalPolicies(ctx context.Context) ([]model.ApprovalPolicy, error) {
	var rows []struct {
		WorkflowType       model.ApprovalWorkflowType `db:"workflow_type"`
		ReviewerUserIDs    string                     `db:"reviewer_user_ids"`
		ReviewerAuthGroups string                     `db:"reviewer_auth_groups"`
		Enabled            bool                       `db:"enabled"`
	}
	if err := r.db.SelectContext(ctx, &rows, `SELECT workflow_type, reviewer_user_ids, reviewer_auth_groups, enabled FROM approval_policies ORDER BY workflow_type`); err != nil {
		return nil, fmt.Errorf("list approval policies: %w", err)
	}
	if len(rows) == 0 {
		return defaultApprovalPolicies(), nil
	}
	policiesByType := make(map[model.ApprovalWorkflowType]model.ApprovalPolicy, len(rows))
	for _, row := range rows {
		var userIDs []uint64
		if row.ReviewerUserIDs != "" {
			if err := json.Unmarshal([]byte(row.ReviewerUserIDs), &userIDs); err != nil {
				return nil, fmt.Errorf("decode approval policy users %s: %w", row.WorkflowType, err)
			}
		}
		var authGroups []model.AuthGroup
		if row.ReviewerAuthGroups != "" {
			if err := json.Unmarshal([]byte(row.ReviewerAuthGroups), &authGroups); err != nil {
				return nil, fmt.Errorf("decode approval policy auth groups %s: %w", row.WorkflowType, err)
			}
		}
		policiesByType[row.WorkflowType] = model.ApprovalPolicy{
			WorkflowType:       row.WorkflowType,
			ReviewerUserIDs:    userIDs,
			ReviewerAuthGroups: authGroups,
			Enabled:            row.Enabled,
		}
	}
	policies := defaultApprovalPolicies()
	for index, policy := range policies {
		if stored, ok := policiesByType[policy.WorkflowType]; ok {
			policies[index] = stored
		}
	}
	return policies, nil
}

func (r *SettingsRepo) GetApprovalPolicy(ctx context.Context, workflowType model.ApprovalWorkflowType) (*model.ApprovalPolicy, error) {
	policies, err := r.ListApprovalPolicies(ctx)
	if err != nil {
		return nil, err
	}
	for i := range policies {
		if policies[i].WorkflowType == workflowType {
			return &policies[i], nil
		}
	}
	return nil, nil
}

func (r *SettingsRepo) GetLarkAppSecret(ctx context.Context) (string, error) {
	return r.getEncryptedString(ctx, settingLarkAppSecret)
}

func (r *SettingsRepo) GetLarkCardVerificationToken(ctx context.Context) (string, error) {
	return r.getEncryptedString(ctx, settingLarkCardVerificationToken)
}

func (r *SettingsRepo) GetSSOOIDCClientSecret(ctx context.Context) (string, error) {
	return r.getEncryptedString(ctx, settingSSOOIDCClientSecret)
}

func defaultOIDCScopes(scopes []string) []string {
	if len(scopes) > 0 {
		return scopes
	}
	return []string{"openid", "profile", "email", "dbre"}
}

func normalizeLarkOAuthSite(site string) string {
	switch strings.ToLower(strings.TrimSpace(site)) {
	case "feishu":
		return "feishu"
	default:
		return "lark"
	}
}

func normalizeLarkCardCallbackMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "long_connection":
		return "long_connection"
	default:
		return "http"
	}
}

func normalizeWorkflowExecutionMode(mode string) string {
	if strings.TrimSpace(mode) == "auto_after_approval" {
		return "auto_after_approval"
	}
	return "manual"
}

func (r *SettingsRepo) ListWorkflowRules(ctx context.Context) ([]model.WorkflowRule, error) {
	var rows []struct {
		ID                 uint64           `db:"id"`
		RuleName           string           `db:"rule_name"`
		TicketType         model.TicketType `db:"ticket_type"`
		DBConnectionID     *uint64          `db:"db_connection_id"`
		ExportSensitivity  *string          `db:"export_sensitivity"`
		ApprovalEnabled    bool             `db:"approval_enabled"`
		ApprovalAuthGroups string           `db:"approval_auth_groups"`
		ExecutorAuthGroups string           `db:"executor_auth_groups"`
		Priority           int              `db:"priority"`
		ExecutionMode      string           `db:"execution_mode"`
		Enabled            bool             `db:"enabled"`
	}
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT id, rule_name, ticket_type, db_connection_id, export_sensitivity,
		       approval_enabled, approval_auth_groups, executor_auth_groups, priority, execution_mode, enabled
		FROM workflow_rules
		ORDER BY priority ASC, id ASC
	`); err != nil {
		return nil, fmt.Errorf("list workflow rules: %w", err)
	}
	rules := make([]model.WorkflowRule, 0, len(rows))
	for _, row := range rows {
		approvalGroups, err := decodeAuthGroups(row.ApprovalAuthGroups)
		if err != nil {
			return nil, fmt.Errorf("decode workflow rule approval groups %d: %w", row.ID, err)
		}
		executorGroups, err := decodeAuthGroups(row.ExecutorAuthGroups)
		if err != nil {
			return nil, fmt.Errorf("decode workflow rule executor groups %d: %w", row.ID, err)
		}
		rules = append(rules, model.WorkflowRule{
			ID:                 row.ID,
			RuleName:           row.RuleName,
			TicketType:         row.TicketType,
			DBConnectionID:     row.DBConnectionID,
			ExportSensitivity:  row.ExportSensitivity,
			ApprovalEnabled:    row.ApprovalEnabled,
			ApprovalAuthGroups: approvalGroups,
			ExecutorAuthGroups: executorGroups,
			Priority:           row.Priority,
			ExecutionMode:      normalizeWorkflowExecutionMode(row.ExecutionMode),
			Enabled:            row.Enabled,
		})
	}
	return rules, nil
}

func (r *SettingsRepo) ReplaceWorkflowRules(ctx context.Context, rules []model.WorkflowRule) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin workflow rules tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if err := replaceWorkflowRules(ctx, tx, rules); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow rules tx: %w", err)
	}
	tx = nil
	return nil
}

func (r *SettingsRepo) MatchWorkflowRule(ctx context.Context, ticketType model.TicketType, dbConnectionID *uint64, exportSensitivity *string) (*model.WorkflowRule, error) {
	rules, err := r.ListWorkflowRules(ctx)
	if err != nil {
		return nil, err
	}
	var best *model.WorkflowRule
	bestScore := -1
	for i := range rules {
		rule := &rules[i]
		if !rule.Enabled || rule.TicketType != ticketType {
			continue
		}
		if rule.DBConnectionID != nil {
			if dbConnectionID == nil || *rule.DBConnectionID != *dbConnectionID {
				continue
			}
		}
		if rule.ExportSensitivity != nil {
			if exportSensitivity == nil || *rule.ExportSensitivity != *exportSensitivity {
				continue
			}
		}
		score := 0
		if rule.DBConnectionID != nil {
			score += 2
		}
		if rule.ExportSensitivity != nil {
			score++
		}
		if best == nil || score > bestScore || (score == bestScore && (rule.Priority < best.Priority || (rule.Priority == best.Priority && rule.ID < best.ID))) {
			copyRule := *rule
			best = &copyRule
			bestScore = score
		}
	}
	return best, nil
}

func defaultApprovalPolicies() []model.ApprovalPolicy {
	return []model.ApprovalPolicy{
		{WorkflowType: model.ApprovalWorkflowDDL, ReviewerAuthGroups: []model.AuthGroup{model.AuthGroupDataOwner}, Enabled: true},
		{WorkflowType: model.ApprovalWorkflowDML, ReviewerAuthGroups: []model.AuthGroup{model.AuthGroupDataOwner}, Enabled: true},
		{WorkflowType: model.ApprovalWorkflowRedisCommand, ReviewerAuthGroups: []model.AuthGroup{model.AuthGroupDataOwner}, Enabled: true},
		{WorkflowType: model.ApprovalWorkflowQueryAccess, ReviewerAuthGroups: []model.AuthGroup{model.AuthGroupDataOwner}, Enabled: true},
		{WorkflowType: model.ApprovalWorkflowSQLExportNormal, ReviewerAuthGroups: []model.AuthGroup{model.AuthGroupSecurity}, Enabled: true},
		{WorkflowType: model.ApprovalWorkflowSQLExportSensitive, ReviewerAuthGroups: []model.AuthGroup{model.AuthGroupSecurity}, Enabled: true},
		{WorkflowType: model.ApprovalWorkflowSensitiveQueryAccess, ReviewerAuthGroups: []model.AuthGroup{model.AuthGroupSecurity}, Enabled: true},
	}
}

func replaceApprovalPolicies(ctx context.Context, tx *sqlx.Tx, policies []model.ApprovalPolicy) error {
	if len(policies) == 0 {
		policies = defaultApprovalPolicies()
	}
	now := timeutil.NowUTC()
	for _, policy := range policies {
		userIDsRaw, err := json.Marshal(policy.ReviewerUserIDs)
		if err != nil {
			return fmt.Errorf("encode approval policy users %s: %w", policy.WorkflowType, err)
		}
		authGroupsRaw, err := json.Marshal(policy.ReviewerAuthGroups)
		if err != nil {
			return fmt.Errorf("encode approval policy auth groups %s: %w", policy.WorkflowType, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO approval_policies (workflow_type, reviewer_user_ids, reviewer_auth_groups, enabled, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON DUPLICATE KEY UPDATE reviewer_user_ids = VALUES(reviewer_user_ids), reviewer_auth_groups = VALUES(reviewer_auth_groups), enabled = VALUES(enabled), updated_at = VALUES(updated_at)`,
			policy.WorkflowType, string(userIDsRaw), string(authGroupsRaw), policy.Enabled, now, now,
		); err != nil {
			return fmt.Errorf("upsert approval policy %s: %w", policy.WorkflowType, err)
		}
	}
	return nil
}

func replaceWorkflowRules(ctx context.Context, tx *sqlx.Tx, rules []model.WorkflowRule) error {
	now := timeutil.NowUTC()
	if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_rules`); err != nil {
		return fmt.Errorf("delete workflow rules: %w", err)
	}
	for _, rule := range rules {
		rule.ApprovalAuthGroups = normalizeAuthGroups(rule.ApprovalAuthGroups)
		rule.ExecutorAuthGroups = normalizeAuthGroups(rule.ExecutorAuthGroups)
		approvalRaw, err := json.Marshal(rule.ApprovalAuthGroups)
		if err != nil {
			return fmt.Errorf("encode workflow rule approval groups %s: %w", rule.RuleName, err)
		}
		executorRaw, err := json.Marshal(rule.ExecutorAuthGroups)
		if err != nil {
			return fmt.Errorf("encode workflow rule executor groups %s: %w", rule.RuleName, err)
		}
		if strings.TrimSpace(rule.RuleName) == "" {
			rule.RuleName = string(rule.TicketType)
		}
		if rule.Priority == 0 {
			rule.Priority = 100
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workflow_rules
			 (rule_name, ticket_type, db_connection_id, export_sensitivity, approval_enabled, approval_auth_groups, executor_auth_groups, priority, execution_mode, enabled, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rule.RuleName, rule.TicketType, rule.DBConnectionID, rule.ExportSensitivity, rule.ApprovalEnabled,
			string(approvalRaw), string(executorRaw), rule.Priority, normalizeWorkflowExecutionMode(rule.ExecutionMode), rule.Enabled, now, now,
		); err != nil {
			return fmt.Errorf("insert workflow rule %s: %w", rule.RuleName, err)
		}
	}
	return nil
}

func decodeAuthGroups(raw string) ([]model.AuthGroup, error) {
	if strings.TrimSpace(raw) == "" {
		return []model.AuthGroup{}, nil
	}
	var groups []model.AuthGroup
	if err := json.Unmarshal([]byte(raw), &groups); err != nil {
		return nil, err
	}
	return normalizeAuthGroups(groups), nil
}

func normalizeAuthGroups(groups []model.AuthGroup) []model.AuthGroup {
	seen := make(map[model.AuthGroup]struct{}, len(groups))
	normalized := make([]model.AuthGroup, 0, len(groups))
	for _, group := range groups {
		group = model.AuthGroup(strings.TrimSpace(string(group)))
		if group == "" {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		normalized = append(normalized, group)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i] < normalized[j]
	})
	return normalized
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
	now := timeutil.NowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO platform_settings (key_name, value, created_at, updated_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = VALUES(updated_at)`,
		key, string(raw), now, now,
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
	now := timeutil.NowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO platform_settings (key_name, value, created_at, updated_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = VALUES(updated_at)`,
		key, string(raw), now, now,
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
	now := timeutil.NowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO platform_settings (key_name, value, created_at, updated_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = VALUES(updated_at)`,
		key, string(raw), now, now,
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
	now := timeutil.NowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO platform_settings (key_name, value, created_at, updated_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = VALUES(updated_at)`,
		key, string(raw), now, now,
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
	now := timeutil.NowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO platform_settings (key_name, value, created_at, updated_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = VALUES(updated_at)`,
		key, encoded, now, now,
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
	now := timeutil.NowUTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO platform_settings (key_name, value, created_at, updated_at) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE value = VALUES(value), updated_at = VALUES(updated_at)`,
		key, string(raw), now, now,
	); err != nil {
		return fmt.Errorf("upsert setting %s: %w", key, err)
	}
	return nil
}
