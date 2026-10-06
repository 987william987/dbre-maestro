package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dbre-maestro/maestro/internal/binlogprobe"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

const (
	filteredBinlogExportMaxRange   = 24 * time.Hour
	unfilteredBinlogExportMaxRange = 15 * time.Minute
	binlogExportMaxFiles           = 20
	binlogTimestampProbeTimeout    = 10 * time.Second
)

var (
	binlogFilePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)
	mysqlNamePattern  = regexp.MustCompile(`^[A-Za-z0-9_$-]{1,64}$`)
)

type MySQLBinlogExportHandler struct {
	jobs           *repository.MySQLBinlogExportRepo
	connections    *repository.DBConnectionRepo
	users          *repository.UserRepo
	audit          *repository.AuditRepo
	settings       *repository.SettingsRepo
	cacheMu        sync.Mutex
	inventoryCache map[uint64]binlogInventoryCache
	timestampCache map[string]binlogTimestampCache
}

func NewMySQLBinlogExportHandler(jobs *repository.MySQLBinlogExportRepo, connections *repository.DBConnectionRepo, users *repository.UserRepo, audit *repository.AuditRepo, settings *repository.SettingsRepo) *MySQLBinlogExportHandler {
	return &MySQLBinlogExportHandler{jobs: jobs, connections: connections, users: users, audit: audit, settings: settings,
		inventoryCache: make(map[uint64]binlogInventoryCache), timestampCache: make(map[string]binlogTimestampCache)}
}

type binlogInventoryCache struct {
	items     []binlogFileInfo
	expiresAt time.Time
}
type binlogTimestampCache struct {
	items     []binlogprobe.FileTimestamp
	expiresAt time.Time
}

type createMySQLBinlogExportRequest struct {
	SourceConnectionID    uint64           `json:"source_connection_id"`
	RangeMode             string           `json:"range_mode"`
	Timezone              string           `json:"timezone"`
	StartTime             *time.Time       `json:"start_time"`
	EndTime               *time.Time       `json:"end_time"`
	StartFile             *string          `json:"start_file"`
	StartPos              *uint64          `json:"start_pos"`
	EndFile               *string          `json:"end_file"`
	EndPos                *uint64          `json:"end_pos"`
	Database              *string          `json:"database"`
	Tables                model.StringList `json:"tables"`
	DMLTypes              model.StringList `json:"dml_types"`
	AcknowledgeUnfiltered bool             `json:"acknowledge_unfiltered"`
}

func (h *MySQLBinlogExportHandler) Connections(w http.ResponseWriter, r *http.Request) {
	items, err := listAccessibleConnections(r.Context(), h.connections, h.users, middleware.UserIDFromCtx(r.Context()))
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "load accessible connections failed")
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(item.DBType, "mysql") {
			result = append(result, map[string]any{"id": item.ID, "name": item.Name})
		}
	}
	jsonOK(w, map[string]any{"items": result})
}

func (h *MySQLBinlogExportHandler) List(w http.ResponseWriter, r *http.Request) {
	ids, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), middleware.UserIDFromCtx(r.Context()))
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "load db scope failed")
		return
	}
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	if limit > 200 {
		limit = 200
	}
	offset := parsePositiveInt(r.URL.Query().Get("offset"), 0)
	items, total, err := h.jobs.ListScoped(r.Context(), ids, limit, offset)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "list binlog export jobs failed")
		return
	}
	jsonOK(w, map[string]any{"items": items, "total": total})
}

func (h *MySQLBinlogExportHandler) Get(w http.ResponseWriter, r *http.Request) {
	job, ok := h.scopedJob(w, r)
	if !ok {
		return
	}
	jsonOK(w, job)
}

func (h *MySQLBinlogExportHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createMySQLBinlogExportRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		jsonBinlogMessageErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		jsonBinlogMessageErr(w, http.StatusBadRequest, "request body must contain exactly one JSON object")
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	if allowed, err := userCanAccessConnection(r.Context(), h.users, userID, req.SourceConnectionID); err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	} else if !allowed {
		jsonBinlogMessageErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return
	}
	conn, err := h.connections.GetByID(r.Context(), req.SourceConnectionID)
	if err != nil || conn == nil || !strings.EqualFold(conn.DBType, "mysql") {
		jsonBinlogMessageErr(w, http.StatusUnprocessableEntity, "active MySQL connection is required")
		return
	}
	resolved, password, err := h.connections.ResolveCredential(conn, model.DBCredentialRoleRollback)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusUnprocessableEntity, "binlog / rollback credential is not available")
		return
	}
	job, validationErr := validateBinlogExportRequest(req, userID)
	if validationErr != "" {
		jsonBinlogMessageErr(w, http.StatusUnprocessableEntity, validationErr)
		return
	}
	db, err := openBinlogMySQL(resolved, password)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusBadGateway, "connect to MySQL failed")
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	files, err := readBinlogFiles(ctx, db)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusBadGateway, "read MySQL binlog inventory failed")
		return
	}
	active, err := readMySQLBinlogPosition(ctx, db)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusBadGateway, "read active MySQL binlog position failed")
		return
	}
	if code, message := validateAndSnapshotBinlogRange(job, files, active); code != "" {
		jsonBinlogErr(w, http.StatusUnprocessableEntity, code, message)
		return
	}
	id, err := h.jobs.Create(r.Context(), job)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "create binlog export job failed")
		return
	}
	h.auditAction(r, "mysql_binlog_export_created", id, map[string]any{"connection_id": req.SourceConnectionID, "range_mode": req.RangeMode})
	w.Header().Set("Location", fmt.Sprintf("/api/binlog-exports/%d", id))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": model.MySQLBinlogExportStatusQueued})
}

func (h *MySQLBinlogExportHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	job, ok := h.scopedJob(w, r)
	if !ok {
		return
	}
	changed, err := h.jobs.RequestCancel(r.Context(), job.ID, middleware.UserIDFromCtx(r.Context()))
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "cancel binlog export job failed")
		return
	}
	if !changed {
		jsonBinlogMessageErr(w, http.StatusConflict, "binlog export job cannot be cancelled in its current state")
		return
	}
	h.auditAction(r, "mysql_binlog_export_cancel_requested", job.ID, map[string]any{"connection_id": job.SourceConnectionID})
	jsonOK(w, map[string]any{"ok": true})
}

func (h *MySQLBinlogExportHandler) Retry(w http.ResponseWriter, r *http.Request) {
	original, ok := h.scopedJob(w, r)
	if !ok {
		return
	}
	switch original.Status {
	case model.MySQLBinlogExportStatusFailed, model.MySQLBinlogExportStatusCancelled, model.MySQLBinlogExportStatusInterrupted:
	default:
		jsonBinlogMessageErr(w, http.StatusConflict, "only failed, cancelled, or interrupted jobs can be retried")
		return
	}
	retryID := original.ID
	retry := *original
	retry.ID, retry.RequestedBy, retry.RetryOfJobID = 0, middleware.UserIDFromCtx(r.Context()), &retryID
	id, err := h.jobs.Create(r.Context(), &retry)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "retry binlog export job failed")
		return
	}
	h.auditAction(r, "mysql_binlog_export_retried", id, map[string]any{"original_job_id": original.ID, "connection_id": original.SourceConnectionID})
	w.Header().Set("Location", fmt.Sprintf("/api/binlog-exports/%d", id))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "status": model.MySQLBinlogExportStatusQueued})
}

func (h *MySQLBinlogExportHandler) DownloadArtifact(w http.ResponseWriter, r *http.Request) {
	job, ok := h.scopedJob(w, r)
	if !ok {
		return
	}
	kind := model.MySQLBinlogExportArtifactKind(chi.URLParam(r, "kind"))
	if kind != model.MySQLBinlogExportArtifactForwardSQL && kind != model.MySQLBinlogExportArtifactRollbackSQL {
		jsonBinlogMessageErr(w, http.StatusNotFound, "artifact not found")
		return
	}
	artifact, err := h.jobs.GetArtifact(r.Context(), job.ID, kind)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "load artifact failed")
		return
	}
	if artifact == nil || time.Now().UTC().After(artifact.ExpiresAt) {
		jsonBinlogMessageErr(w, http.StatusGone, "artifact has expired or is unavailable")
		return
	}
	sqlText, err := h.jobs.DecryptArtifact(artifact)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "decrypt artifact failed")
		return
	}
	h.auditAction(r, "mysql_binlog_export_artifact_downloaded", job.ID, map[string]any{"kind": kind, "connection_id": job.SourceConnectionID})
	w.Header().Set("Content-Type", "application/sql; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="binlog-export-%d-%s.sql"`, job.ID, kind))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(sqlText))
}

func (h *MySQLBinlogExportHandler) PreviewArtifact(w http.ResponseWriter, r *http.Request) {
	job, ok := h.scopedJob(w, r)
	if !ok {
		return
	}
	kind := model.MySQLBinlogExportArtifactKind(chi.URLParam(r, "kind"))
	if kind != model.MySQLBinlogExportArtifactForwardSQL && kind != model.MySQLBinlogExportArtifactRollbackSQL {
		jsonBinlogErr(w, http.StatusNotFound, "artifact_not_found", "artifact not found")
		return
	}
	artifact, err := h.jobs.GetArtifact(r.Context(), job.ID, kind)
	if err != nil {
		jsonBinlogErr(w, http.StatusInternalServerError, "artifact_load_failed", "load artifact failed")
		return
	}
	if artifact == nil || time.Now().UTC().After(artifact.ExpiresAt) {
		jsonBinlogErr(w, http.StatusGone, "artifact_expired", "artifact has expired or is unavailable")
		return
	}
	sqlText, err := h.jobs.DecryptArtifact(artifact)
	if err != nil {
		jsonBinlogErr(w, http.StatusInternalServerError, "artifact_decrypt_failed", "decrypt artifact failed")
		return
	}
	const previewBytes = 64 << 10
	truncated := len(sqlText) > previewBytes
	if truncated {
		sqlText = sqlText[:previewBytes]
	}
	h.auditAction(r, "mysql_binlog_export_artifact_previewed", job.ID, map[string]any{"kind": kind, "connection_id": job.SourceConnectionID})
	w.Header().Set("Cache-Control", "no-store")
	jsonOK(w, map[string]any{"sql": sqlText, "truncated": truncated, "expires_at": artifact.ExpiresAt})
}

type binlogFileInfo struct {
	Name            string     `json:"name"`
	SizeBytes       uint64     `json:"size_bytes"`
	Active          bool       `json:"active"`
	CurrentPosition *uint64    `json:"current_position,omitempty"`
	LastWrittenAt   *time.Time `json:"last_written_at"`
}

func (h *MySQLBinlogExportHandler) ListBinlogs(w http.ResponseWriter, r *http.Request) {
	connectionID, _ := strconv.ParseUint(chi.URLParam(r, "connectionID"), 10, 64)
	db, ok := h.openScopedMySQL(w, r)
	if !ok {
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	h.cacheMu.Lock()
	cached, hit := h.inventoryCache[connectionID]
	h.cacheMu.Unlock()
	if hit && time.Now().Before(cached.expiresAt) {
		jsonOK(w, map[string]any{"items": cached.items, "time_metadata_available": true})
		return
	}
	items, err := readBinlogFiles(ctx, db)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusBadGateway, "list MySQL binlogs failed")
		return
	}
	active, err := readMySQLBinlogPosition(ctx, db)
	if err == nil {
		for i := range items {
			if items[i].Name == active.File {
				items[i].Active = true
				items[i].CurrentPosition = &active.Pos
				items[i].SizeBytes = active.Pos
			}
		}
	}
	h.cacheMu.Lock()
	if previous, exists := h.inventoryCache[connectionID]; exists && binlogInventoryChanged(previous.items, items) {
		prefix := fmt.Sprintf("%d:", connectionID)
		for key := range h.timestampCache {
			if strings.HasPrefix(key, prefix) {
				delete(h.timestampCache, key)
			}
		}
	}
	h.inventoryCache[connectionID] = binlogInventoryCache{items: items, expiresAt: time.Now().Add(30 * time.Second)}
	h.cacheMu.Unlock()
	jsonOK(w, map[string]any{"items": items, "time_metadata_available": true})
}

func binlogInventoryChanged(previous, current []binlogFileInfo) bool {
	if len(previous) != len(current) {
		return true
	}
	for i := range previous {
		if previous[i].Name != current[i].Name || previous[i].SizeBytes != current[i].SizeBytes {
			return true
		}
	}
	return false
}

func (h *MySQLBinlogExportHandler) ProbeBinlogTimestamps(w http.ResponseWriter, r *http.Request) {
	connectionID, err := strconv.ParseUint(chi.URLParam(r, "connectionID"), 10, 64)
	if err != nil || connectionID == 0 {
		jsonBinlogErr(w, 400, "invalid_identifier", "invalid connection id")
		return
	}
	db, ok := h.openScopedMySQL(w, r)
	if !ok {
		return
	}
	defer db.Close()
	var req struct {
		File string `json:"file"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil || !binlogFilePattern.MatchString(req.File) {
		jsonBinlogErr(w, 422, "invalid_probe_files", "one valid binlog file is required")
		return
	}
	cacheKey := fmt.Sprintf("%d:%s", connectionID, req.File)
	h.cacheMu.Lock()
	inventory, inventoryHit := h.inventoryCache[connectionID]
	if inventoryHit && time.Now().Before(inventory.expiresAt) {
		for _, file := range inventory.items {
			if file.Name == req.File {
				cacheKey = fmt.Sprintf("%d:%s:%d", connectionID, req.File, file.SizeBytes)
				break
			}
		}
	}
	cached, cacheHit := h.timestampCache[cacheKey]
	h.cacheMu.Unlock()
	if inventoryHit && cacheHit && time.Now().Before(inventory.expiresAt) && time.Now().Before(cached.expiresAt) {
		jsonOK(w, map[string]any{"items": cached.items})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), binlogTimestampProbeTimeout)
	defer cancel()
	files, err := readBinlogFiles(ctx, db)
	if err != nil {
		jsonBinlogErr(w, 502, "binlog_inventory_failed", "read MySQL binlog inventory failed")
		return
	}
	found := false
	for _, item := range files {
		if item.Name == req.File {
			found = true
			cacheKey = fmt.Sprintf("%d:%s:%d", connectionID, req.File, item.SizeBytes)
			break
		}
	}
	if !found {
		jsonBinlogErr(w, 422, "binlog_purged", "selected binlog file is no longer available")
		return
	}
	h.cacheMu.Lock()
	cached, hit := h.timestampCache[cacheKey]
	h.cacheMu.Unlock()
	if hit && time.Now().Before(cached.expiresAt) {
		jsonOK(w, map[string]any{"items": cached.items})
		return
	}
	conn, err := h.connections.GetByID(ctx, connectionID)
	if err != nil || conn == nil {
		jsonBinlogErr(w, 422, "connection_unavailable", "MySQL connection not found")
		return
	}
	resolved, password, err := h.connections.ResolveCredential(conn, model.DBCredentialRoleRollback)
	if err != nil {
		jsonBinlogErr(w, 422, "credential_unavailable", "binlog / rollback credential is not available")
		return
	}
	item, err := binlogprobe.ProbeStart(ctx, binlogprobe.Request{Host: resolved.Host, Port: resolved.Port, Username: resolved.Username, Password: password, File: req.File, Timeout: binlogTimestampProbeTimeout})
	if err != nil {
		slog.Warn("binlog export: timestamp probe failed", "connection_id", connectionID, "file", req.File, "err", err)
		jsonBinlogErr(w, 502, "timestamp_probe_failed", "probe binlog timestamps failed")
		return
	}
	h.cacheMu.Lock()
	items := []binlogprobe.FileTimestamp{item}
	h.timestampCache[cacheKey] = binlogTimestampCache{items: items, expiresAt: time.Now().Add(5 * time.Minute)}
	h.cacheMu.Unlock()
	jsonOK(w, map[string]any{"items": items})
}

func (h *MySQLBinlogExportHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	db, ok := h.openScopedMySQL(w, r)
	if !ok {
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT SCHEMA_NAME FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys') ORDER BY SCHEMA_NAME`)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusBadGateway, "list MySQL databases failed")
		return
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			jsonBinlogMessageErr(w, http.StatusBadGateway, "read MySQL databases failed")
			return
		}
		items = append(items, name)
	}
	if err := rows.Err(); err != nil {
		jsonBinlogMessageErr(w, http.StatusBadGateway, "read MySQL databases failed")
		return
	}
	jsonOK(w, map[string]any{"items": items})
}

func (h *MySQLBinlogExportHandler) ListTables(w http.ResponseWriter, r *http.Request) {
	databaseName := strings.TrimSpace(r.URL.Query().Get("database"))
	if !mysqlNamePattern.MatchString(databaseName) {
		jsonBinlogMessageErr(w, http.StatusUnprocessableEntity, "database is required and must be valid")
		return
	}
	db, ok := h.openScopedMySQL(w, r)
	if !ok {
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT TABLE_NAME FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`, databaseName)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusBadGateway, "list MySQL tables failed")
		return
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			jsonBinlogMessageErr(w, http.StatusBadGateway, "read MySQL tables failed")
			return
		}
		items = append(items, name)
	}
	if err := rows.Err(); err != nil {
		jsonBinlogMessageErr(w, http.StatusBadGateway, "read MySQL tables failed")
		return
	}
	jsonOK(w, map[string]any{"items": items})
}

func (h *MySQLBinlogExportHandler) openScopedMySQL(w http.ResponseWriter, r *http.Request) (*sql.DB, bool) {
	connectionID, err := strconv.ParseUint(chi.URLParam(r, "connectionID"), 10, 64)
	if err != nil || connectionID == 0 {
		jsonBinlogMessageErr(w, http.StatusBadRequest, "invalid connection id")
		return nil, false
	}
	if allowed, err := userCanAccessConnection(r.Context(), h.users, middleware.UserIDFromCtx(r.Context()), connectionID); err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "db scope check failed")
		return nil, false
	} else if !allowed {
		jsonBinlogMessageErr(w, http.StatusForbidden, "access to this connection is not allowed")
		return nil, false
	}
	conn, err := h.connections.GetByID(r.Context(), connectionID)
	if err != nil || conn == nil || !strings.EqualFold(conn.DBType, "mysql") {
		jsonBinlogMessageErr(w, http.StatusNotFound, "MySQL connection not found")
		return nil, false
	}
	resolved, password, err := h.connections.ResolveCredential(conn, model.DBCredentialRoleRollback)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusUnprocessableEntity, "binlog / rollback credential is not available")
		return nil, false
	}
	db, err := openBinlogMySQL(resolved, password)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusBadGateway, "connect to MySQL failed")
		return nil, false
	}
	return db, true
}

func openBinlogMySQL(conn *model.DBConnection, password string) (*sql.DB, error) {
	driver, dsn := pool.BuildDSN(conn, password)
	return pool.Open(driver, dsn, pool.ProfileMetadata)
}

func readBinlogFiles(ctx context.Context, db *sql.DB) ([]binlogFileInfo, error) {
	rows, err := db.QueryContext(ctx, "SHOW BINARY LOGS")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil || len(columns) < 2 || len(columns) > 3 {
		return nil, errors.New("unexpected MySQL binlog columns")
	}
	items := []binlogFileInfo{}
	for rows.Next() {
		var item binlogFileInfo
		if len(columns) == 3 {
			var encrypted any
			err = rows.Scan(&item.Name, &item.SizeBytes, &encrypted)
		} else {
			err = rows.Scan(&item.Name, &item.SizeBytes)
		}
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func validateAndSnapshotBinlogRange(job *model.MySQLBinlogExportJob, files []binlogFileInfo, active mysqlBinlogPosition) (string, string) {
	indices := make(map[string]int, len(files))
	for i := range files {
		indices[files[i].Name] = i
	}
	activeIndex, exists := indices[active.File]
	if !exists || active.Pos == 0 {
		return "binlog_snapshot_unavailable", "active binlog position is unavailable"
	}
	files[activeIndex].SizeBytes = active.Pos
	if job.RangeMode == "time" {
		job.RequestedEndFile, job.RequestedEndPos = &active.File, &active.Pos
		return "", ""
	}
	startIndex, startExists := indices[*job.RequestedStartFile]
	endIndex, endExists := indices[*job.RequestedEndFile]
	if !startExists || !endExists {
		return "binlog_purged", "selected binlog file is no longer available"
	}
	if endIndex-startIndex+1 > binlogExportMaxFiles {
		return "binlog_file_limit_exceeded", "binlog range exceeds the 20 file limit"
	}
	if *job.RequestedStartPos < 4 || *job.RequestedStartPos > files[startIndex].SizeBytes ||
		*job.RequestedEndPos < 4 || *job.RequestedEndPos > files[endIndex].SizeBytes {
		return "binlog_position_invalid", "binlog position is outside the selected file"
	}
	return "", ""
}

func jsonBinlogErr(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message, "code": code})
}

func jsonBinlogMessageErr(w http.ResponseWriter, status int, message string) {
	code := "binlog_export_failed"
	switch {
	case strings.Contains(message, "request body"):
		code = "invalid_request"
	case strings.Contains(message, "permission") || strings.Contains(message, "not allowed"):
		code = "access_denied"
	case strings.Contains(message, "scope"):
		code = "scope_check_failed"
	case strings.Contains(message, "connection id"), strings.Contains(message, "job id"):
		code = "invalid_identifier"
	case strings.Contains(message, "connection not found"), strings.Contains(message, "connection is required"):
		code = "connection_unavailable"
	case strings.Contains(message, "credential"):
		code = "credential_unavailable"
	case strings.Contains(message, "job not found"):
		code = "job_not_found"
	case strings.Contains(message, "artifact not found"):
		code = "artifact_not_found"
	case strings.Contains(message, "expired"):
		code = "artifact_expired"
	case strings.Contains(message, "decrypt"):
		code = "artifact_decrypt_failed"
	case strings.Contains(message, "cannot be cancelled"):
		code = "invalid_job_state"
	case strings.Contains(message, "retried"):
		code = "invalid_retry_state"
	case strings.Contains(message, "timezone"):
		code = "invalid_timezone"
	case strings.Contains(message, "range"), strings.Contains(message, "position"), strings.Contains(message, "start_time"), strings.Contains(message, "end_time"):
		code = "invalid_range"
	case strings.Contains(message, "database"), strings.Contains(message, "table"), strings.Contains(message, "dml_types"), strings.Contains(message, "acknowledge_unfiltered"):
		code = "invalid_filter"
	case strings.Contains(message, "binlog"):
		code = "binlog_inventory_failed"
	case strings.Contains(message, "artifact"):
		code = "artifact_load_failed"
	case strings.Contains(message, "job"):
		code = "job_operation_failed"
	}
	jsonBinlogErr(w, status, code, message)
}

func (h *MySQLBinlogExportHandler) scopedJob(w http.ResponseWriter, r *http.Request) (*model.MySQLBinlogExportJob, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		jsonBinlogMessageErr(w, http.StatusBadRequest, "invalid job id")
		return nil, false
	}
	job, err := h.jobs.GetByID(r.Context(), id)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "load binlog export job failed")
		return nil, false
	}
	if job == nil {
		jsonBinlogMessageErr(w, http.StatusNotFound, "binlog export job not found")
		return nil, false
	}
	allowed, err := userCanAccessConnection(r.Context(), h.users, middleware.UserIDFromCtx(r.Context()), job.SourceConnectionID)
	if err != nil {
		jsonBinlogMessageErr(w, http.StatusInternalServerError, "db scope check failed")
		return nil, false
	}
	if !allowed {
		jsonBinlogMessageErr(w, http.StatusNotFound, "binlog export job not found")
		return nil, false
	}
	return job, true
}

func validateBinlogExportRequest(req createMySQLBinlogExportRequest, userID uint64) (*model.MySQLBinlogExportJob, string) {
	job := &model.MySQLBinlogExportJob{RequestedBy: userID, SourceConnectionID: req.SourceConnectionID, RangeMode: strings.TrimSpace(req.RangeMode),
		Timezone: strings.TrimSpace(req.Timezone), RequestedStartTime: req.StartTime, RequestedEndTime: req.EndTime,
		RequestedStartFile: trimmedString(req.StartFile), RequestedStartPos: req.StartPos, RequestedEndFile: trimmedString(req.EndFile), RequestedEndPos: req.EndPos,
		SourceDatabaseName: trimmedString(req.Database), SourceTables: normalizeUniqueTrimmed(req.Tables), DMLTypes: normalizeUniqueLower(req.DMLTypes),
		AcknowledgedUnfiltered: req.AcknowledgeUnfiltered, Generator: "my2sql"}
	if job.SourceConnectionID == 0 {
		return nil, "source_connection_id is required"
	}
	if job.Timezone == "" {
		job.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(job.Timezone); err != nil {
		return nil, "timezone is invalid"
	}
	if job.SourceDatabaseName != nil && !mysqlNamePattern.MatchString(*job.SourceDatabaseName) {
		return nil, "database name is invalid"
	}
	if len(job.SourceTables) > 0 && job.SourceDatabaseName == nil {
		return nil, "database is required when tables are selected"
	}
	for _, table := range job.SourceTables {
		if !mysqlNamePattern.MatchString(table) {
			return nil, "table name is invalid"
		}
	}
	validDML := map[string]bool{"insert": true, "update": true, "delete": true}
	if len(job.DMLTypes) == 0 {
		job.DMLTypes = model.StringList{"insert", "update", "delete"}
	}
	for _, dml := range job.DMLTypes {
		if !validDML[dml] {
			return nil, "dml_types may only contain insert, update, and delete"
		}
	}
	unfiltered := job.SourceDatabaseName == nil && len(job.SourceTables) == 0
	if unfiltered && !job.AcknowledgedUnfiltered {
		return nil, "acknowledge_unfiltered is required without database and table filters"
	}
	switch job.RangeMode {
	case "time":
		if job.RequestedStartTime == nil || job.RequestedEndTime == nil || !job.RequestedEndTime.After(*job.RequestedStartTime) {
			return nil, "a valid start_time and end_time range is required"
		}
		maxRange := filteredBinlogExportMaxRange
		if unfiltered {
			maxRange = unfilteredBinlogExportMaxRange
		}
		if job.RequestedEndTime.Sub(*job.RequestedStartTime) > maxRange {
			return nil, fmt.Sprintf("time range exceeds the %s limit", maxRange)
		}
		if job.RequestedEndTime.After(time.Now().UTC()) {
			return nil, "end_time cannot be in the future"
		}
	case "position":
		if job.RequestedStartFile == nil || job.RequestedEndFile == nil || job.RequestedStartPos == nil || job.RequestedEndPos == nil ||
			!binlogFilePattern.MatchString(*job.RequestedStartFile) || !binlogFilePattern.MatchString(*job.RequestedEndFile) {
			return nil, "a valid start and end binlog position is required"
		}
		if *job.RequestedStartFile > *job.RequestedEndFile || *job.RequestedStartFile == *job.RequestedEndFile && *job.RequestedStartPos >= *job.RequestedEndPos {
			return nil, "end binlog position must be after start position"
		}
		if unfiltered && *job.RequestedStartFile != *job.RequestedEndFile {
			return nil, "unfiltered position export must stay within one binlog file"
		}
	default:
		return nil, "range_mode must be time or position"
	}
	return job, ""
}

func (h *MySQLBinlogExportHandler) auditAction(r *http.Request, action string, resourceID uint64, details any) {
	actorID := middleware.UserIDFromCtx(r.Context())
	_ = h.audit.Log(r.Context(), repository.AuditEntry{ActorID: &actorID, ActorName: middleware.UsernameFromCtx(r.Context()),
		ActionType: action, ResourceType: "mysql_binlog_export", ResourceID: &resourceID, Details: details, IPAddress: r.RemoteAddr})
}

func trimmedString(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func normalizeUniqueLower(values []string) model.StringList {
	seen := map[string]bool{}
	result := model.StringList{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func normalizeUniqueTrimmed(values []string) model.StringList {
	seen := map[string]bool{}
	result := model.StringList{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
