package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/dbre-maestro/maestro/internal/job"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type SettingsHandler struct {
	settings *repository.SettingsRepo
	users    *repository.UserRepo
	auths    *repository.AuthGroupRepo
	dbConns  *repository.DBConnectionRepo
	audit    *repository.AuditRepo
}

func NewSettingsHandler(settings *repository.SettingsRepo, users *repository.UserRepo, auths *repository.AuthGroupRepo, dbConns *repository.DBConnectionRepo, audit *repository.AuditRepo) *SettingsHandler {
	return &SettingsHandler{
		settings: settings,
		users:    users,
		auths:    auths,
		dbConns:  dbConns,
		audit:    audit,
	}
}

func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings.Get(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load settings failed")
		return
	}
	jsonOK(w, settings)
}

func (h *SettingsHandler) ListDBConnections(w http.ResponseWriter, r *http.Request) {
	connections, err := h.dbConns.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list db connections failed")
		return
	}

	type item struct {
		ID     uint64 `json:"id"`
		Name   string `json:"name"`
		DBType string `json:"db_type"`
		Host   string `json:"host"`
		Port   uint16 `json:"port"`
	}

	items := make([]item, 0, len(connections))
	for _, connection := range connections {
		items = append(items, item{
			ID:     connection.ID,
			Name:   connection.Name,
			DBType: connection.DBType,
			Host:   connection.Host,
			Port:   connection.Port,
		})
	}

	jsonOK(w, map[string]any{
		"connections": items,
	})
}

func (h *SettingsHandler) Patch(w http.ResponseWriter, r *http.Request) {
	var req model.PlatformSettings
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, userID := range append([]uint64{}, req.SensitiveExportReviewerUserIDs...) {
		if err := h.validateUserExists(r, userID); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	for _, userID := range append([]uint64{}, req.SensitiveQueryAccessReviewerUserIDs...) {
		if err := h.validateUserExists(r, userID); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	if req.LarkAppID != "" && req.LarkAppSecret == "" && !req.LarkAppSecretConfigured {
		jsonErr(w, http.StatusUnprocessableEntity, "lark_app_secret is required when configuring lark for the first time")
		return
	}
	for _, connectionID := range append([]uint64{}, req.DBMetadataObjectEnabledConnectionIDs...) {
		if err := h.validateConnectionExists(r, connectionID); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	for _, policy := range req.ApprovalPolicies {
		for _, userID := range policy.ReviewerUserIDs {
			if err := h.validateUserExists(r, userID); err != nil {
				jsonErr(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
		}
		for _, authGroup := range policy.ReviewerAuthGroups {
			if err := h.validateAuthGroupExists(r, authGroup); err != nil {
				jsonErr(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
		}
	}
	if req.SQLEditorAppTimeoutSeconds <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_editor_app_timeout_seconds must be greater than 0")
		return
	}
	if req.SQLEditorMySQLMaxExecutionTimeMs <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_editor_mysql_max_execution_time_ms must be greater than 0")
		return
	}
	if req.SQLEditorPostgresStatementTimeoutMs <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "sql_editor_postgres_statement_timeout_ms must be greater than 0")
		return
	}
	if req.DBMetadataInventorySyncIntervalMins <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_inventory_sync_interval_minutes must be greater than 0")
		return
	}
	if err := job.ValidateCronExpression(req.DBMetadataInventoryCron); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_inventory_cron is invalid: "+err.Error())
		return
	}
	if req.DBMetadataObjectSyncIntervalMins <= 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_object_sync_interval_minutes must be greater than 0")
		return
	}
	if err := job.ValidateCronExpression(req.DBMetadataObjectCron); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_object_cron is invalid: "+err.Error())
		return
	}
	if strings.TrimSpace(req.DBMetadataCronTimezone) == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_cron_timezone is required")
		return
	}
	if _, err := time.LoadLocation(strings.TrimSpace(req.DBMetadataCronTimezone)); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "db_metadata_cron_timezone is invalid")
		return
	}

	if err := h.settings.Replace(r.Context(), &req); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update settings failed")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "settings_update",
		ResourceType: "settings",
		Details: map[string]any{
			"sensitive_export_reviewer_user_ids":          req.SensitiveExportReviewerUserIDs,
			"sensitive_query_access_reviewer_user_ids":    req.SensitiveQueryAccessReviewerUserIDs,
			"require_non_sensitive_export_review":         req.RequireNonSensitiveExportReview,
			"lark_app_id":                                 req.LarkAppID,
			"lark_app_secret_configured":                  req.LarkAppSecretConfigured || req.LarkAppSecret != "",
			"sql_editor_app_timeout_seconds":              req.SQLEditorAppTimeoutSeconds,
			"sql_editor_mysql_max_execution_time_ms":      req.SQLEditorMySQLMaxExecutionTimeMs,
			"sql_editor_postgres_statement_timeout_ms":    req.SQLEditorPostgresStatementTimeoutMs,
			"db_metadata_inventory_enabled":               req.DBMetadataInventoryEnabled,
			"db_metadata_inventory_regions":               req.DBMetadataInventoryRegions,
			"db_metadata_inventory_engines":               req.DBMetadataInventoryEngines,
			"db_metadata_inventory_cron":                  req.DBMetadataInventoryCron,
			"db_metadata_inventory_sync_interval_minutes": req.DBMetadataInventorySyncIntervalMins,
			"db_metadata_object_enabled":                  req.DBMetadataObjectEnabled,
			"db_metadata_object_enabled_connection_ids":   req.DBMetadataObjectEnabledConnectionIDs,
			"db_metadata_object_cron":                     req.DBMetadataObjectCron,
			"db_metadata_object_sync_interval_minutes":    req.DBMetadataObjectSyncIntervalMins,
			"db_metadata_cron_timezone":                   req.DBMetadataCronTimezone,
			"approval_policies":                           req.ApprovalPolicies,
		},
		IPAddress: clientIP(r),
	})

	jsonOK(w, req)
}

func (h *SettingsHandler) validateUserExists(r *http.Request, userID uint64) error {
	user, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user %d does not exist", userID)
	}
	return nil
}

func (h *SettingsHandler) validateConnectionExists(r *http.Request, connectionID uint64) error {
	conn, err := h.dbConns.GetByID(r.Context(), connectionID)
	if err != nil {
		return err
	}
	if conn == nil {
		return fmt.Errorf("db connection %d does not exist", connectionID)
	}
	return nil
}

func (h *SettingsHandler) validateAuthGroupExists(r *http.Request, authGroup model.AuthGroup) error {
	if h.auths == nil {
		return fmt.Errorf("auth group validation is not configured")
	}
	group, err := h.auths.GetByKey(r.Context(), string(authGroup))
	if err != nil {
		return err
	}
	if group == nil {
		return fmt.Errorf("auth group %s does not exist", authGroup)
	}
	return nil
}
