package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/tableschema"
	"github.com/go-chi/chi/v5"
)

type TableSchemaHandler struct {
	connections *repository.DBConnectionRepo
	list        func(context.Context, uint64) ([]model.DBConnection, error)
	canAccess   func(context.Context, uint64, uint64) (bool, error)
	get         func(context.Context, uint64) (*model.DBConnection, error)
	open        func(context.Context, tableschema.CredentialResolver, *model.DBConnection) (*sql.DB, error)
	openRW      func(context.Context, tableschema.CredentialResolver, *model.DBConnection) (*sql.DB, error)
	buildExport func(context.Context, tableschema.Queryer, tableschema.ExportRequest) (*tableschema.ExportResult, error)
	buildSync   func(context.Context, tableschema.Queryer, tableschema.Queryer, tableschema.SyncPreviewRequest) (*tableschema.ExportResult, string, error)
	buildRetry  func(context.Context, tableschema.Queryer, tableschema.Queryer, *model.TableSchemaSyncJob, []model.TableSchemaSyncJobItem) ([]tableschema.RetryItem, error)
	previews    *tableschema.SyncPreviewStore
	syncJobs    tableSchemaSyncJobStore
	logAudit    func(context.Context, repository.AuditEntry) error
}

type tableSchemaSyncJobStore interface {
	Create(context.Context, repository.TableSchemaSyncCreateInput) (*model.TableSchemaSyncJob, error)
	List(context.Context, uint, uint) ([]model.TableSchemaSyncJob, error)
	ListScoped(context.Context, []uint64, uint, uint) ([]model.TableSchemaSyncJob, uint64, error)
	GetByID(context.Context, uint64) (*model.TableSchemaSyncJob, error)
	ListItems(context.Context, uint64) ([]model.TableSchemaSyncJobItem, error)
	RequestCancel(context.Context, uint64, uint64) (bool, error)
}

const tableSchemaExportTimeout = 30 * time.Second

func NewTableSchemaHandler(connections *repository.DBConnectionRepo, users *repository.UserRepo, audit *repository.AuditRepo, syncJobs *repository.TableSchemaSyncRepo) *TableSchemaHandler {
	return &TableSchemaHandler{
		connections: connections,
		list: func(ctx context.Context, userID uint64) ([]model.DBConnection, error) {
			return listAccessibleConnections(ctx, connections, users, userID)
		},
		canAccess: func(ctx context.Context, userID, connectionID uint64) (bool, error) {
			return userCanAccessConnection(ctx, users, userID, connectionID)
		},
		get:         connections.GetByID,
		open:        tableschema.OpenReadonly,
		openRW:      tableschema.OpenReadwrite,
		buildExport: tableschema.BuildExport,
		buildSync:   tableschema.BuildSyncPreview,
		buildRetry:  tableschema.BuildRetryPlan,
		previews:    tableschema.NewSyncPreviewStore(),
		syncJobs:    syncJobs,
		logAudit:    audit.Log,
	}
}

func (h *TableSchemaHandler) Connections(w http.ResponseWriter, r *http.Request) {
	connections, err := h.list(r.Context(), middleware.UserIDFromCtx(r.Context()))
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "load accessible connections failed")
		return
	}
	items := make([]map[string]any, 0, len(connections))
	for _, connection := range connections {
		if strings.EqualFold(connection.DBType, "mysql") {
			items = append(items, map[string]any{"id": connection.ID, "name": connection.Name})
		}
	}
	jsonOK(w, map[string]any{"items": items})
}

func (h *TableSchemaHandler) Databases(w http.ResponseWriter, r *http.Request) {
	db, ok := h.openScopedMySQL(w, r)
	if !ok {
		return
	}
	defer db.Close()
	items, err := tableschema.ListDatabases(r.Context(), db)
	if err != nil {
		tableSchemaError(w, http.StatusBadGateway, tableschema.ErrorMetadataQueryFailed, "read MySQL databases failed")
		return
	}
	jsonOK(w, map[string]any{"items": items})
}

func (h *TableSchemaHandler) Tables(w http.ResponseWriter, r *http.Request) {
	database := strings.TrimSpace(r.URL.Query().Get("database"))
	if database == "" {
		tableSchemaError(w, http.StatusBadRequest, tableschema.ErrorInvalidTableSelection, "database is required")
		return
	}
	db, ok := h.openScopedMySQL(w, r)
	if !ok {
		return
	}
	defer db.Close()
	items, err := tableschema.ListTables(r.Context(), db, database)
	if err != nil {
		writeTableSchemaMetadataError(w, err, "read MySQL tables failed")
		return
	}
	dependencies, err := tableschema.ListDependencies(r.Context(), db, database)
	if err != nil {
		writeTableSchemaMetadataError(w, err, "read foreign key dependencies failed")
		return
	}
	jsonOK(w, map[string]any{"items": items, "dependencies": dependencies})
}

func (h *TableSchemaHandler) openScopedMySQL(w http.ResponseWriter, r *http.Request) (*sql.DB, bool) {
	connectionID, err := strconv.ParseUint(chi.URLParam(r, "connectionID"), 10, 64)
	if err != nil || connectionID == 0 {
		tableSchemaError(w, http.StatusBadRequest, tableschema.ErrorInvalidTableSelection, "invalid connection id")
		return nil, false
	}
	return h.openScopedMySQLID(w, r, connectionID)
}

func (h *TableSchemaHandler) openScopedMySQLID(w http.ResponseWriter, r *http.Request, connectionID uint64) (*sql.DB, bool) {
	if connectionID == 0 {
		tableSchemaError(w, http.StatusBadRequest, tableschema.ErrorInvalidTableSelection, "source connection is required")
		return nil, false
	}
	allowed, err := h.canAccess(r.Context(), middleware.UserIDFromCtx(r.Context()), connectionID)
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "database scope check failed")
		return nil, false
	}
	if !allowed {
		tableSchemaError(w, http.StatusForbidden, tableschema.ErrorSourceScopeDenied, "access to this connection is not allowed")
		return nil, false
	}
	connection, err := h.get(r.Context(), connectionID)
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "load connection failed")
		return nil, false
	}
	if connection == nil || !strings.EqualFold(connection.DBType, "mysql") {
		tableSchemaError(w, http.StatusNotFound, tableschema.ErrorUnsupportedEngine, "MySQL connection not found")
		return nil, false
	}
	db, err := h.open(r.Context(), h.connections, connection)
	if err != nil {
		tableSchemaError(w, http.StatusUnprocessableEntity, tableschema.ErrorConnectionUnavailable, "readonly connection is unavailable")
		return nil, false
	}
	return db, true
}

func writeTableSchemaMetadataError(w http.ResponseWriter, err error, message string) {
	if stable, ok := err.(*tableschema.StableError); ok {
		tableSchemaError(w, http.StatusBadRequest, stable.Code, stable.Message)
		return
	}
	tableSchemaError(w, http.StatusBadGateway, tableschema.ErrorMetadataQueryFailed, message)
}

func (h *TableSchemaHandler) ExportPreview(w http.ResponseWriter, r *http.Request) {
	result, request, ok := h.prepareExport(w, r, "table_schema_export_preview")
	if !ok {
		return
	}
	h.auditExport(r, request, result, "table_schema_export_preview", "succeeded", "")
	jsonOK(w, result)
}

func (h *TableSchemaHandler) Export(w http.ResponseWriter, r *http.Request) {
	result, request, ok := h.prepareExport(w, r, "table_schema_export_download")
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/sql; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, tableSchemaExportFilename(request.Source.Database)))
	w.Header().Set("Content-Length", strconv.Itoa(len(result.Script)))
	_, err := io.CopyBuffer(w, strings.NewReader(result.Script), make([]byte, 32<<10))
	status, errorCode := "succeeded", ""
	if err != nil {
		status, errorCode = "failed", "client_write_failed"
	}
	h.auditExport(r, request, result, "table_schema_export_download", status, errorCode)
}

func (h *TableSchemaHandler) prepareExport(w http.ResponseWriter, r *http.Request, action string) (*tableschema.ExportResult, tableschema.ExportRequest, bool) {
	var request tableschema.ExportRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		tableSchemaError(w, http.StatusBadRequest, tableschema.ErrorInvalidTableSelection, "invalid request body")
		return nil, request, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		tableSchemaError(w, http.StatusBadRequest, tableschema.ErrorInvalidTableSelection, "request body must contain exactly one JSON object")
		return nil, request, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), tableSchemaExportTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	db, ok := h.openScopedMySQLID(w, r, request.Source.ConnectionID)
	if !ok {
		return nil, request, false
	}
	defer db.Close()
	result, err := h.buildExport(ctx, db, request)
	if err != nil {
		code := tableSchemaExportErrorCode(err)
		h.auditExport(r, request, nil, action, "failed", string(code))
		writeTableSchemaExportError(w, err, code)
		return nil, request, false
	}
	return result, request, true
}

func writeTableSchemaExportError(w http.ResponseWriter, err error, code tableschema.ErrorCode) {
	var stable *tableschema.StableError
	if errors.As(err, &stable) {
		status := http.StatusUnprocessableEntity
		if stable.Code == tableschema.ErrorExportTooLarge {
			status = http.StatusRequestEntityTooLarge
		}
		tableSchemaError(w, status, stable.Code, stable.Message)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		tableSchemaError(w, http.StatusGatewayTimeout, code, "table schema export timed out")
		return
	}
	if errors.Is(err, context.Canceled) {
		tableSchemaError(w, http.StatusRequestTimeout, code, "table schema export was cancelled")
		return
	}
	tableSchemaError(w, http.StatusBadGateway, code, "read or transform table schema failed")
}

func tableSchemaExportErrorCode(err error) tableschema.ErrorCode {
	var stable *tableschema.StableError
	if errors.As(err, &stable) {
		return stable.Code
	}
	return tableschema.ErrorMetadataQueryFailed
}

func (h *TableSchemaHandler) auditExport(r *http.Request, request tableschema.ExportRequest, result *tableschema.ExportResult, action, status, errorCode string) {
	if h.logAudit == nil || request.Source.ConnectionID == 0 {
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	details := tableSchemaExportAuditDetails(request, result, status, errorCode)
	_ = h.logAudit(context.WithoutCancel(r.Context()), repository.AuditEntry{
		ActorID: &actorID, ActorName: middleware.UsernameFromCtx(r.Context()), ActionType: action,
		ResourceType: "db_connection", ResourceID: &request.Source.ConnectionID, Details: details, IPAddress: clientIP(r),
	})
}

func tableSchemaExportAuditDetails(request tableschema.ExportRequest, result *tableschema.ExportResult, status, errorCode string) map[string]any {
	tables := append([]string(nil), request.Source.Tables...)
	sort.Strings(tables)
	details := map[string]any{
		"source_database": request.Source.Database, "tables": tables,
		"transformation": request.Transformation, "status": status,
	}
	if result != nil {
		details["table_count"] = len(result.Tables)
		details["external_dependency_count"] = len(result.ExternalDependencies)
	}
	if errorCode != "" {
		details["error_code"] = errorCode
	}
	return details
}

func tableSchemaExportFilename(database string) string {
	var safe strings.Builder
	for _, char := range strings.ToLower(database) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			safe.WriteRune(char)
		} else if safe.Len() == 0 || !strings.HasSuffix(safe.String(), "-") {
			safe.WriteByte('-')
		}
	}
	name := strings.Trim(safe.String(), "-")
	if name == "" {
		name = "database"
	}
	return "table-schema-" + name + ".sql"
}
func (h *TableSchemaHandler) SyncPreview(w http.ResponseWriter, r *http.Request) {
	var request tableschema.SyncPreviewRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		tableSchemaError(w, http.StatusBadRequest, tableschema.ErrorInvalidTableSelection, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		tableSchemaError(w, http.StatusBadRequest, tableschema.ErrorInvalidTableSelection, "request body must contain exactly one JSON object")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), tableSchemaExportTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	source, ok := h.openScopedMySQLID(w, r, request.Source.ConnectionID)
	if !ok {
		return
	}
	defer source.Close()
	target, ok := h.openScopedTargetMySQL(w, r, request.Target.ConnectionID)
	if !ok {
		return
	}
	defer target.Close()
	result, fingerprint, err := h.buildSync(ctx, source, target, request)
	if err != nil {
		h.auditSyncPreview(r, request, nil, "failed", string(tableSchemaExportErrorCode(err)))
		writeTableSchemaExportError(w, err, tableSchemaExportErrorCode(err))
		return
	}
	token, expiresAt, err := h.previews.Issue(middleware.UserIDFromCtx(ctx), fingerprint)
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "create sync preview token failed")
		return
	}
	h.auditSyncPreview(r, request, result, "succeeded", "")
	jsonOK(w, tableschema.SyncPreviewResult{ExportResult: *result, PreviewToken: token, ExpiresAt: expiresAt})
}

func (h *TableSchemaHandler) auditSyncPreview(r *http.Request, request tableschema.SyncPreviewRequest, result *tableschema.ExportResult, status, errorCode string) {
	if h.logAudit == nil || request.Source.ConnectionID == 0 {
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	tables := append([]string(nil), request.Source.Tables...)
	sort.Strings(tables)
	details := map[string]any{"source_connection_id": request.Source.ConnectionID, "source_database": request.Source.Database, "target_connection_id": request.Target.ConnectionID, "target_database": request.Target.Database, "tables": tables, "transformation": request.Transformation, "status": status}
	if result != nil {
		details["table_count"] = len(result.Tables)
		details["external_dependency_count"] = len(result.ExternalDependencies)
	}
	if errorCode != "" {
		details["error_code"] = errorCode
	}
	_ = h.logAudit(context.WithoutCancel(r.Context()), repository.AuditEntry{ActorID: &actorID, ActorName: middleware.UsernameFromCtx(r.Context()), ActionType: "table_schema_sync_preview", ResourceType: "db_connection", ResourceID: &request.Target.ConnectionID, Details: details, IPAddress: clientIP(r)})
}

func (h *TableSchemaHandler) openScopedTargetMySQL(w http.ResponseWriter, r *http.Request, connectionID uint64) (*sql.DB, bool) {
	if connectionID == 0 {
		tableSchemaError(w, http.StatusBadRequest, tableschema.ErrorInvalidTableSelection, "target connection is required")
		return nil, false
	}
	allowed, err := h.canAccess(r.Context(), middleware.UserIDFromCtx(r.Context()), connectionID)
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "database scope check failed")
		return nil, false
	}
	if !allowed {
		tableSchemaError(w, http.StatusForbidden, tableschema.ErrorTargetScopeDenied, "access to target connection is not allowed")
		return nil, false
	}
	connection, err := h.get(r.Context(), connectionID)
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "load target connection failed")
		return nil, false
	}
	if connection == nil || !strings.EqualFold(connection.DBType, "mysql") {
		tableSchemaError(w, http.StatusNotFound, tableschema.ErrorUnsupportedEngine, "target MySQL connection not found")
		return nil, false
	}
	db, err := h.openRW(r.Context(), h.connections, connection)
	if err != nil {
		tableSchemaError(w, http.StatusUnprocessableEntity, tableschema.ErrorConnectionUnavailable, "target readwrite connection is unavailable")
		return nil, false
	}
	return db, true
}

type createTableSchemaSyncJobRequest struct {
	tableschema.SyncPreviewRequest
	PreviewToken string `json:"preview_token"`
}

func (h *TableSchemaHandler) CreateSyncJob(w http.ResponseWriter, r *http.Request) {
	var request createTableSchemaSyncJobRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.PreviewToken) == "" {
		tableSchemaError(w, http.StatusBadRequest, tableschema.ErrorInvalidTableSelection, "invalid request body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), tableSchemaExportTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	source, ok := h.openScopedMySQLID(w, r, request.Source.ConnectionID)
	if !ok {
		return
	}
	defer source.Close()
	target, ok := h.openScopedTargetMySQL(w, r, request.Target.ConnectionID)
	if !ok {
		return
	}
	defer target.Close()
	result, err := tableschema.RevalidateSyncPreview(ctx, h.previews, request.PreviewToken, middleware.UserIDFromCtx(ctx), source, target, request.SyncPreviewRequest)
	if err != nil {
		writeTableSchemaExportError(w, err, tableSchemaExportErrorCode(err))
		return
	}
	items := make([]repository.TableSchemaSyncItemInput, 0, len(result.Tables))
	byName := make(map[string]string, len(result.Tables))
	for _, table := range result.Tables {
		byName[table.Name] = table.SourceDDLSHA256
	}
	for order, name := range result.Order {
		items = append(items, repository.TableSchemaSyncItemInput{TableName: name, DependencyOrder: uint(order), SourceDDLSHA256: byName[name]})
	}
	job, err := h.syncJobs.Create(ctx, repository.TableSchemaSyncCreateInput{RequestedBy: middleware.UserIDFromCtx(ctx), SourceConnectionID: request.Source.ConnectionID, SourceDatabase: request.Source.Database, TargetConnectionID: request.Target.ConnectionID, TargetDatabase: request.Target.Database, Transformation: request.Transformation, Items: items})
	if errors.Is(err, repository.ErrTableSchemaSyncTargetActive) {
		tableSchemaError(w, http.StatusConflict, tableschema.ErrorCode("sync_conflict"), err.Error())
		return
	}
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "create sync job failed")
		return
	}
	h.auditSyncJob(r, job, "table_schema_sync_job_create", "succeeded")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(job)
}
func (h *TableSchemaHandler) ListSyncJobs(w http.ResponseWriter, r *http.Request) {
	limit, offset := uint(20), uint(0)
	if value, err := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 32); err == nil && value > 0 && value <= 100 {
		limit = uint(value)
	}
	if value, err := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 32); err == nil {
		offset = uint(value)
	}
	connections, err := h.list(r.Context(), middleware.UserIDFromCtx(r.Context()))
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "load accessible connections failed")
		return
	}
	ids := make([]uint64, 0, len(connections))
	for _, connection := range connections {
		ids = append(ids, connection.ID)
	}
	jobs, total, err := h.syncJobs.ListScoped(r.Context(), ids, limit, offset)
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "list sync jobs failed")
		return
	}
	jsonOK(w, map[string]any{"items": jobs, "total": total})
}
func (h *TableSchemaHandler) GetSyncJob(w http.ResponseWriter, r *http.Request) {
	job, ok := h.scopedSyncJob(w, r)
	if !ok {
		return
	}
	items, err := h.syncJobs.ListItems(r.Context(), job.ID)
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "load sync job items failed")
		return
	}
	jsonOK(w, map[string]any{"job": job, "items": items})
}
func (h *TableSchemaHandler) CancelSyncJob(w http.ResponseWriter, r *http.Request) {
	job, ok := h.scopedSyncJob(w, r)
	if !ok {
		return
	}
	changed, err := h.syncJobs.RequestCancel(r.Context(), job.ID, middleware.UserIDFromCtx(r.Context()))
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "cancel sync job failed")
		return
	}
	if !changed {
		tableSchemaError(w, http.StatusConflict, tableschema.ErrorCode("invalid_sync_state"), "sync job is no longer cancellable")
		return
	}
	h.auditSyncJob(r, job, "table_schema_sync_job_cancel", "succeeded")
	jsonOK(w, map[string]any{"cancel_requested": true})
}
func (h *TableSchemaHandler) RetrySyncJob(w http.ResponseWriter, r *http.Request) {
	prior, ok := h.scopedSyncJob(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), tableSchemaExportTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	items, err := h.syncJobs.ListItems(ctx, prior.ID)
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "load prior sync job items failed")
		return
	}
	source, ok := h.openScopedMySQLID(w, r, prior.SourceConnectionID)
	if !ok {
		return
	}
	defer source.Close()
	target, ok := h.openScopedTargetMySQL(w, r, prior.TargetConnectionID)
	if !ok {
		return
	}
	defer target.Close()
	plan, err := h.buildRetry(ctx, source, target, prior, items)
	if err != nil {
		writeTableSchemaExportError(w, err, tableSchemaExportErrorCode(err))
		return
	}
	var transformation tableschema.TransformationConfig
	if err := json.Unmarshal(prior.TransformationConfig, &transformation); err != nil {
		tableSchemaError(w, http.StatusConflict, tableschema.ErrorInvalidRetryState, "stored transformation is invalid")
		return
	}
	inputs := make([]repository.TableSchemaSyncItemInput, len(plan))
	for i, item := range plan {
		inputs[i] = repository.TableSchemaSyncItemInput{TableName: item.TableName, DependencyOrder: item.DependencyOrder, SourceDDLSHA256: item.SourceDDLSHA256}
	}
	retryOf := prior.ID
	job, err := h.syncJobs.Create(ctx, repository.TableSchemaSyncCreateInput{RequestedBy: middleware.UserIDFromCtx(ctx), SourceConnectionID: prior.SourceConnectionID, SourceDatabase: prior.SourceDatabase, TargetConnectionID: prior.TargetConnectionID, TargetDatabase: prior.TargetDatabase, Transformation: transformation, RetryOfJobID: &retryOf, Items: inputs})
	if errors.Is(err, repository.ErrTableSchemaSyncTargetActive) {
		tableSchemaError(w, http.StatusConflict, tableschema.ErrorCode("sync_conflict"), err.Error())
		return
	}
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "create retry job failed")
		return
	}
	h.auditSyncJob(r, job, "table_schema_sync_job_retry", "succeeded")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(job)
}

func (h *TableSchemaHandler) scopedSyncJob(w http.ResponseWriter, r *http.Request) (*model.TableSchemaSyncJob, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		tableSchemaError(w, http.StatusBadRequest, tableschema.ErrorInvalidTableSelection, "invalid sync job id")
		return nil, false
	}
	job, err := h.syncJobs.GetByID(r.Context(), id)
	if err != nil {
		tableSchemaError(w, http.StatusInternalServerError, tableschema.ErrorMetadataQueryFailed, "load sync job failed")
		return nil, false
	}
	if job == nil || !h.canReadSyncJob(r.Context(), middleware.UserIDFromCtx(r.Context()), job) {
		tableSchemaError(w, http.StatusNotFound, tableschema.ErrorInvalidTableSelection, "sync job not found")
		return nil, false
	}
	return job, true
}
func (h *TableSchemaHandler) canReadSyncJob(ctx context.Context, actorID uint64, job *model.TableSchemaSyncJob) bool {
	source, err := h.canAccess(ctx, actorID, job.SourceConnectionID)
	if err != nil || !source {
		return false
	}
	target, err := h.canAccess(ctx, actorID, job.TargetConnectionID)
	return err == nil && target
}

func (h *TableSchemaHandler) auditSyncJob(r *http.Request, job *model.TableSchemaSyncJob, action, status string) {
	if h.logAudit == nil || job == nil {
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	_ = h.logAudit(context.WithoutCancel(r.Context()), repository.AuditEntry{
		ActorID: &actorID, ActorName: middleware.UsernameFromCtx(r.Context()), ActionType: action,
		ResourceType: "table_schema_sync_job", ResourceID: &job.ID, IPAddress: clientIP(r),
		Details: map[string]any{"status": status, "source_connection_id": job.SourceConnectionID, "source_database": job.SourceDatabase, "target_connection_id": job.TargetConnectionID, "target_database": job.TargetDatabase, "table_count": job.TableCount},
	})
}

func tableSchemaNotReady(w http.ResponseWriter) {
	tableSchemaError(w, http.StatusNotImplemented, tableschema.ErrorFeatureNotReady, "table schema endpoint is not available yet")
}

func tableSchemaError(w http.ResponseWriter, status int, code tableschema.ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": string(code), "error": message})
}
