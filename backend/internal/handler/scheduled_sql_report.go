package handler

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/job"
	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/queryaccess"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
	"github.com/dbre-maestro/maestro/internal/sqlreview"
	"github.com/go-chi/chi/v5"
)

const scheduledSQLReportLimit = 10000

type ScheduledSQLReportHandler struct {
	reports     *repository.ScheduledSQLReportRepo
	dbConns     *repository.DBConnectionRepo
	users       *repository.UserRepo
	queryAccess *queryaccess.Service
	masking     *maskingRuntime
	audit       *repository.AuditRepo
	lark        *notification.Dispatcher
}

func NewScheduledSQLReportHandler(
	reports *repository.ScheduledSQLReportRepo,
	dbConns *repository.DBConnectionRepo,
	users *repository.UserRepo,
	queryAccessRepo *repository.QueryAccessRepo,
	maskingRules *repository.MaskingRuleRepo,
	whitelist *repository.MaskingWhitelistRepo,
	tickets *repository.TicketRepo,
	engine *masking.Engine,
	audit *repository.AuditRepo,
	lark *notification.Dispatcher,
) *ScheduledSQLReportHandler {
	return &ScheduledSQLReportHandler{
		reports:     reports,
		dbConns:     dbConns,
		users:       users,
		queryAccess: queryaccess.NewService(queryAccessRepo, users),
		masking:     newMaskingRuntime(users, maskingRules, whitelist, tickets, engine),
		audit:       audit,
		lark:        lark,
	}
}

func (h *ScheduledSQLReportHandler) List(w http.ResponseWriter, r *http.Request) {
	reports, err := h.reports.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list scheduled sql reports failed")
		return
	}
	jsonOK(w, map[string]any{"reports": reports})
}

func (h *ScheduledSQLReportHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUint64Param(w, r, "id")
	if !ok {
		return
	}
	report, err := h.reports.GetByID(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load scheduled sql report failed")
		return
	}
	if report == nil {
		jsonErr(w, http.StatusNotFound, "scheduled sql report not found")
		return
	}
	runs, err := h.reports.ListRuns(r.Context(), id, 50)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list scheduled sql report runs failed")
		return
	}
	jsonOK(w, map[string]any{"report": report, "runs": runs})
}

func (h *ScheduledSQLReportHandler) ListConnections(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	connections, err := listAccessibleConnections(r.Context(), h.dbConns, h.users, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list scheduled report connections failed")
		return
	}
	filtered := make([]model.DBConnection, 0, len(connections))
	for _, conn := range connections {
		if conn.DBType == "mysql" || conn.DBType == "postgres" || conn.DBType == "postgresql" {
			filtered = append(filtered, conn)
		}
	}
	jsonOK(w, map[string]any{"connections": filtered})
}

func (h *ScheduledSQLReportHandler) ListRecipients(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list scheduled report recipients failed")
		return
	}
	type recipient struct {
		ID                uint64 `json:"id"`
		Username          string `json:"username"`
		Email             string `json:"email"`
		LarkRecipient     string `json:"lark_recipient"`
		LarkRecipientType string `json:"lark_recipient_type"`
		LarkUnionID       string `json:"lark_union_id"`
	}
	items := make([]recipient, 0, len(users))
	for _, user := range users {
		if !user.IsActive {
			continue
		}
		items = append(items, recipient{
			ID:                user.ID,
			Username:          user.Username,
			Email:             user.Email,
			LarkRecipient:     user.LarkRecipient,
			LarkRecipientType: normalizeLarkRecipientType(user.LarkRecipientType),
			LarkUnionID:       user.LarkUnionID,
		})
	}
	jsonOK(w, map[string]any{"users": items})
}

func (h *ScheduledSQLReportHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	input, err := h.bindAndValidate(r.Context(), r, userID)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	report, err := h.reports.Create(r.Context(), input)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create scheduled sql report failed")
		return
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "scheduled_sql_report_create",
		ResourceType: "scheduled_sql_report",
		ResourceID:   &report.ID,
		Details:      map[string]any{"name": report.Name, "db_connection_id": report.DBConnectionID, "cron_expression": report.CronExpression},
		IPAddress:    clientIP(r),
	})
	jsonCreated(w, report)
}

func (h *ScheduledSQLReportHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUint64Param(w, r, "id")
	if !ok {
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	input, err := h.bindAndValidate(r.Context(), r, userID)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	report, err := h.reports.Update(r.Context(), id, input)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "update scheduled sql report failed")
		return
	}
	if report == nil {
		jsonErr(w, http.StatusNotFound, "scheduled sql report not found")
		return
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "scheduled_sql_report_update",
		ResourceType: "scheduled_sql_report",
		ResourceID:   &report.ID,
		Details:      map[string]any{"name": report.Name, "db_connection_id": report.DBConnectionID, "cron_expression": report.CronExpression},
		IPAddress:    clientIP(r),
	})
	jsonOK(w, report)
}

func (h *ScheduledSQLReportHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUint64Param(w, r, "id")
	if !ok {
		return
	}
	if err := h.reports.Delete(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "delete scheduled sql report failed")
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "scheduled_sql_report_delete",
		ResourceType: "scheduled_sql_report",
		ResourceID:   &id,
		IPAddress:    clientIP(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *ScheduledSQLReportHandler) bindAndValidate(ctx context.Context, r *http.Request, userID uint64) (repository.ScheduledSQLReportInput, error) {
	var req struct {
		Name             string   `json:"name"`
		Description      string   `json:"description"`
		DBConnectionID   uint64   `json:"db_connection_id"`
		DatabaseName     string   `json:"database_name"`
		SchemaName       string   `json:"schema_name"`
		SQLContent       string   `json:"sql_content"`
		CronExpression   string   `json:"cron_expression"`
		Timezone         string   `json:"timezone"`
		RecipientUserIDs []uint64 `json:"recipient_user_ids"`
		IsActive         bool     `json:"is_active"`
	}
	if err := bindJSON(r, &req); err != nil {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	req.SQLContent = strings.TrimSpace(req.SQLContent)
	req.CronExpression = strings.TrimSpace(req.CronExpression)
	req.Timezone = strings.TrimSpace(req.Timezone)
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	if req.Name == "" || req.DBConnectionID == 0 || req.SQLContent == "" || req.CronExpression == "" {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("name, db_connection_id, sql_content, and cron_expression are required")
	}
	if len(req.RecipientUserIDs) == 0 {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("at least one Lark recipient is required")
	}
	location, err := time.LoadLocation(req.Timezone)
	if err != nil {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("timezone is invalid")
	}
	if err := job.ValidateCronExpression(req.CronExpression); err != nil {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("cron_expression is invalid: %w", err)
	}
	nextRunAt, err := job.NextCronTime(req.CronExpression, location, time.Now())
	if err != nil {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("calculate next run failed: %w", err)
	}
	conn, err := h.dbConns.GetByID(ctx, req.DBConnectionID)
	if err != nil || conn == nil {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("db connection not found")
	}
	if conn.DBType != "mysql" && conn.DBType != "postgres" && conn.DBType != "postgresql" {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("scheduled sql reports only support mysql and postgresql connections")
	}
	if err := validateScheduledReportSQL(conn, req.SQLContent); err != nil {
		return repository.ScheduledSQLReportInput{}, err
	}
	hasAccess, err := userCanAccessConnection(ctx, h.users, userID, req.DBConnectionID)
	if err != nil {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("db scope check failed")
	}
	if !hasAccess {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("access to this connection is not allowed")
	}
	if err := h.queryAccess.CheckSQL(ctx, userID, conn, req.SQLContent, queryaccess.CheckContext{
		DatabaseName: strings.TrimSpace(req.DatabaseName),
		SchemaName:   strings.TrimSpace(req.SchemaName),
	}); err != nil {
		return repository.ScheduledSQLReportInput{}, err
	}
	analysis, err := analyzeSQLScopes(ctx, h.dbConns, h.masking, conn, req.SQLContent, buildQueryExecutionContext(req.DatabaseName, req.SchemaName))
	if err != nil {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("analyze scheduled report query failed: %w", err)
	}
	if analysis.ContainsSensitive {
		return repository.ScheduledSQLReportInput{}, fmt.Errorf("scheduled sql reports cannot include sensitive columns")
	}
	return repository.ScheduledSQLReportInput{
		Name:             req.Name,
		Description:      nullableTrimmedString(req.Description),
		DBConnectionID:   req.DBConnectionID,
		DatabaseName:     nullableTrimmedString(req.DatabaseName),
		SchemaName:       nullableTrimmedString(req.SchemaName),
		SQLContent:       req.SQLContent,
		CronExpression:   req.CronExpression,
		Timezone:         req.Timezone,
		RecipientUserIDs: dedupeUint64s(req.RecipientUserIDs),
		IsActive:         req.IsActive,
		NextRunAt:        &nextRunAt,
		UserID:           userID,
	}, nil
}

func (h *ScheduledSQLReportHandler) RunDueReports(ctx context.Context) {
	reports, err := h.reports.GetDue(ctx, time.Now().UTC(), 20)
	if err != nil {
		slog.Warn("scheduled sql reports: load due reports failed", "err", err)
		return
	}
	for i := range reports {
		report := reports[i]
		placeholder := time.Now().UTC().Add(24 * time.Hour)
		claimed, err := h.reports.ClaimDue(ctx, report.ID, report.NextRunAt, placeholder)
		if err != nil || !claimed {
			continue
		}
		go h.RunReport(context.Background(), &report)
	}
}

func (h *ScheduledSQLReportHandler) RunReport(ctx context.Context, report *model.ScheduledSQLReport) {
	startedAt := time.Now().UTC()
	runID, err := h.reports.CreateRun(ctx, report.ID, startedAt)
	if err != nil {
		slog.Warn("scheduled sql reports: create run failed", "report_id", report.ID, "err", err)
		return
	}
	status := "success"
	rowCount := 0
	var fileName *string
	var errMessage *string
	defer func() {
		finishedAt := time.Now().UTC()
		if finishErr := h.reports.FinishRun(ctx, runID, status, rowCount, fileName, errMessage, finishedAt); finishErr != nil {
			slog.Warn("scheduled sql reports: finish run failed", "run_id", runID, "err", finishErr)
		}
		nextRunAt, nextErr := h.nextRun(report, finishedAt)
		if nextErr != nil {
			message := nextErr.Error()
			errMessage = &message
			status = "failed"
		}
		if updateErr := h.reports.UpdateRunState(ctx, report.ID, finishedAt, status, errMessage, nextRunAt); updateErr != nil {
			slog.Warn("scheduled sql reports: update report run state failed", "report_id", report.ID, "err", updateErr)
		}
	}()

	csvData, generatedFileName, rows, runErr := h.executeReport(ctx, report)
	if runErr != nil {
		status = "failed"
		message := runErr.Error()
		errMessage = &message
		slog.Warn("scheduled sql report run failed", "report_id", report.ID, "name", report.Name, "err", message)
		h.audit.Log(ctx, repository.AuditEntry{
			ActorName:    "system",
			ActionType:   "scheduled_sql_report_run_failed",
			ResourceType: "scheduled_sql_report",
			ResourceID:   &report.ID,
			Details:      map[string]any{"name": report.Name, "error": message},
		})
		return
	}
	rowCount = rows
	fileName = &generatedFileName
	if h.lark == nil {
		status = "failed"
		message := "lark dispatcher is not configured"
		errMessage = &message
		slog.Warn("scheduled sql report delivery failed", "report_id", report.ID, "name", report.Name, "err", message)
		h.audit.Log(ctx, repository.AuditEntry{
			ActorName:    "system",
			ActionType:   "scheduled_sql_report_delivery_failed",
			ResourceType: "scheduled_sql_report",
			ResourceID:   &report.ID,
			Details:      map[string]any{"name": report.Name, "error": message, "recipient_user_ids": report.RecipientUserIDs},
		})
		return
	}
	result := h.lark.SendFileToUsers(ctx, report.RecipientUserIDs, generatedFileName, csvData)
	if result.Err != nil {
		status = "failed"
		message := result.Err.Error()
		errMessage = &message
		slog.Warn("scheduled sql report delivery failed", "report_id", report.ID, "name", report.Name, "recipient_count", len(report.RecipientUserIDs), "err", message)
		h.audit.Log(ctx, repository.AuditEntry{
			ActorName:    "system",
			ActionType:   "scheduled_sql_report_delivery_failed",
			ResourceType: "scheduled_sql_report",
			ResourceID:   &report.ID,
			Details:      map[string]any{"name": report.Name, "error": message, "recipient_user_ids": report.RecipientUserIDs},
		})
		return
	}
	if result.SkippedReason != "" {
		status = "failed"
		errMessage = &result.SkippedReason
		slog.Warn("scheduled sql report delivery failed", "report_id", report.ID, "name", report.Name, "recipient_count", len(report.RecipientUserIDs), "err", result.SkippedReason)
		h.audit.Log(ctx, repository.AuditEntry{
			ActorName:    "system",
			ActionType:   "scheduled_sql_report_delivery_failed",
			ResourceType: "scheduled_sql_report",
			ResourceID:   &report.ID,
			Details:      map[string]any{"name": report.Name, "error": result.SkippedReason, "recipient_user_ids": report.RecipientUserIDs},
		})
		return
	}
	slog.Info("scheduled sql report run complete", "report_id", report.ID, "name", report.Name, "row_count", rowCount, "file_name", generatedFileName, "recipient_count", len(report.RecipientUserIDs))
	h.audit.Log(ctx, repository.AuditEntry{
		ActorName:    "system",
		ActionType:   "scheduled_sql_report_run",
		ResourceType: "scheduled_sql_report",
		ResourceID:   &report.ID,
		Details:      map[string]any{"name": report.Name, "row_count": rowCount, "file_name": generatedFileName, "recipient_user_ids": report.RecipientUserIDs},
	})
}

func (h *ScheduledSQLReportHandler) executeReport(ctx context.Context, report *model.ScheduledSQLReport) ([]byte, string, int, error) {
	conn, err := h.dbConns.GetByID(ctx, report.DBConnectionID)
	if err != nil || conn == nil {
		return nil, "", 0, fmt.Errorf("db connection not found")
	}
	if err := h.queryAccess.CheckSQL(ctx, report.CreatedBy, conn, report.SQLContent, queryaccess.CheckContext{
		DatabaseName: nullableStringValue(report.DatabaseName),
		SchemaName:   nullableStringValue(report.SchemaName),
	}); err != nil {
		return nil, "", 0, err
	}
	resolvedConn, password, err := h.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		return nil, "", 0, err
	}
	driver, dsn := pool.BuildDSN(resolvedConn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		return nil, "", 0, err
	}
	queryCtx := buildQueryExecutionContext(nullableStringValue(report.DatabaseName), nullableStringValue(report.SchemaName))
	timeout := defaultSQLEditorTimeoutSettings()
	ctx, cancel := context.WithTimeout(ctx, timeout.AppTimeout)
	defer cancel()
	result, err := executeQueryForConnection(ctx, resolvedConn, password, pools.QueryPool, injectLimit(report.SQLContent, scheduledSQLReportLimit, conn.DBType), queryCtx, timeout, sqlQueryExecutionOptions{})
	if err != nil {
		return nil, "", 0, err
	}
	if _, _, err := h.masking.applyResult(ctx, resolvedConn, 0, result); err != nil {
		return nil, "", 0, fmt.Errorf("masking scheduled report result: %w", err)
	}
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write(result.Columns); err != nil {
		return nil, "", 0, err
	}
	for _, row := range result.Rows {
		record := make([]string, len(row))
		for i, value := range row {
			record[i] = fmt.Sprint(value)
		}
		if err := writer.Write(record); err != nil {
			return nil, "", 0, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, "", 0, err
	}
	name := safeReportFilename(report.Name, time.Now().UTC())
	return buf.Bytes(), name, len(result.Rows), nil
}

func (h *ScheduledSQLReportHandler) nextRun(report *model.ScheduledSQLReport, after time.Time) (*time.Time, error) {
	location, err := time.LoadLocation(report.Timezone)
	if err != nil {
		return nil, err
	}
	next, err := job.NextCronTime(report.CronExpression, location, after)
	if err != nil {
		return nil, err
	}
	return &next, nil
}

func validateScheduledReportSQL(conn *model.DBConnection, sqlContent string) error {
	trimmed := strings.TrimSpace(sqlContent)
	fields := strings.Fields(trimmed)
	firstToken := ""
	if len(fields) > 0 {
		firstToken = strings.ToUpper(fields[0])
	}
	if firstToken != "SELECT" && firstToken != "WITH" && firstToken != "SHOW" {
		return fmt.Errorf("scheduled sql reports only allow SELECT, WITH, or SHOW")
	}
	return sqlreview.CheckReadOnly(sqlparse.DialectFromDBType(conn.DBType), sqlContent)
}

func safeReportFilename(name string, t time.Time) string {
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	safe := strings.Trim(replacer.Replace(strings.ToLower(name)), "_")
	if safe == "" {
		safe = "scheduled_sql_report"
	}
	return fmt.Sprintf("%s_%s.csv", safe, t.Format("20060102_150405"))
}

func dedupeUint64s(items []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(items))
	result := make([]uint64, 0, len(items))
	for _, item := range items {
		if item == 0 {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func parseUint64Param(w http.ResponseWriter, r *http.Request, name string) (uint64, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, name), 10, 64)
	if err != nil || id == 0 {
		jsonErr(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}
