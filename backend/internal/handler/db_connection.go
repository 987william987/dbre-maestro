package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/netguard"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type DBConnectionHandler struct {
	repo       *repository.DBConnectionRepo
	users      *repository.UserRepo
	auths      *repository.AuthGroupRepo
	audit      *repository.AuditRepo
	settings   *repository.SettingsRepo
	hostPolicy *netguard.Policy
}

type dbConnectionTestResponse struct {
	OK             bool                             `json:"ok"`
	Error          string                           `json:"error,omitempty"`
	LastTestStatus string                           `json:"last_test_status"`
	LastTestError  string                           `json:"last_test_error,omitempty"`
	LastTestedAt   *time.Time                       `json:"last_tested_at,omitempty"`
	Results        []dbConnectionEndpointTestResult `json:"results,omitempty"`
}

type dbConnectionEndpointTestResult struct {
	CredentialRole string `json:"credential_role"`
	OK             bool   `json:"ok"`
	Error          string `json:"error,omitempty"`
}

type dbConnectionRollbackCapabilityResponse struct {
	OK      bool                                      `json:"ok"`
	Message string                                    `json:"message"`
	Checks  []dbConnectionRollbackCapabilityCheck     `json:"checks"`
	Binlog  *dbConnectionRollbackCapabilityBinlogInfo `json:"binlog,omitempty"`
}

type dbConnectionRollbackCapabilityCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type dbConnectionRollbackCapabilityBinlogInfo struct {
	File string `json:"file"`
	Pos  uint64 `json:"pos"`
}

type connectionCredentialPayload struct {
	CredentialRole string `json:"credential_role"`
	Username       string `json:"username"`
	Password       string `json:"password"`
}

func NewDBConnectionHandler(repo *repository.DBConnectionRepo, users *repository.UserRepo, auths *repository.AuthGroupRepo, audit *repository.AuditRepo, options ...DBConnectionHandlerOption) *DBConnectionHandler {
	h := &DBConnectionHandler{repo: repo, users: users, auths: auths, audit: audit}
	for _, option := range options {
		option(h)
	}
	return h
}

type DBConnectionHandlerOption func(*DBConnectionHandler)

func WithDBConnectionHandlerHostPolicy(policy *netguard.Policy) DBConnectionHandlerOption {
	return func(h *DBConnectionHandler) {
		h.hostPolicy = policy
	}
}

func WithDBConnectionHandlerSettings(settings *repository.SettingsRepo) DBConnectionHandlerOption {
	return func(h *DBConnectionHandler) {
		h.settings = settings
	}
}

// GET /db-connections
// Users with write permission see all connections; readers are filtered by their effective DB scope.
func (h *DBConnectionHandler) List(w http.ResponseWriter, r *http.Request) {
	conns, err := h.repo.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	if conns == nil {
		conns = []model.DBConnection{}
	}

	if !middleware.HasPermission(r.Context(), "db_connections.write") {
		userID := middleware.UserIDFromCtx(r.Context())
		accessibleIDs, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), userID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "db scope check failed")
			return
		}
		accessible := make(map[uint64]bool, len(accessibleIDs))
		for _, id := range accessibleIDs {
			accessible[id] = true
		}
		filtered := conns[:0]
		for _, c := range conns {
			if accessible[c.ID] {
				filtered = append(filtered, c)
			}
		}
		conns = filtered
		if conns == nil {
			conns = []model.DBConnection{}
		}
	}

	jsonOK(w, map[string]any{"connections": conns})
}

// POST /db-connections
func (h *DBConnectionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string                        `json:"name"`
		DBType        string                        `json:"db_type"`
		Host          string                        `json:"host"`
		Port          uint16                        `json:"port"`
		ReadonlyHost  string                        `json:"readonly_host"`
		ReadonlyPort  uint16                        `json:"readonly_port"`
		ReadwriteHost string                        `json:"readwrite_host"`
		ReadwritePort uint16                        `json:"readwrite_port"`
		DatabaseName  *string                       `json:"database_name"`
		Username      string                        `json:"username"`
		Password      string                        `json:"password"`
		SSLMode       string                        `json:"ssl_mode"`
		Credentials   []connectionCredentialPayload `json:"credentials"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DBType == "" {
		req.DBType = "mysql"
	}
	host, port, readonlyHost, readonlyPort, readwriteHost, readwritePort := normalizeConnectionEndpoints(
		req.Host,
		req.Port,
		req.ReadonlyHost,
		req.ReadonlyPort,
		req.ReadwriteHost,
		req.ReadwritePort,
	)
	if req.Name == "" || readonlyHost == "" || readonlyPort == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "name, readonly_host, and readonly_port are required")
		return
	}
	if req.Password == "" && len(req.Credentials) == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "name, host, port, password are required")
		return
	}
	credentials := normalizeCredentialPayloads(req.Credentials)
	if req.DBType != "redis" && req.Username == "" && !hasCredentialRole(credentials, model.DBCredentialRoleReadonly) {
		jsonErr(w, http.StatusUnprocessableEntity, "readonly username is required for mysql/postgres connections")
		return
	}
	if req.SSLMode == "" {
		req.SSLMode = "prefer"
	}

	userID := middleware.UserIDFromCtx(r.Context())
	if ok := h.checkEndpointPolicy(w, r, nil, "readonly", readonlyHost, readonlyPort); !ok {
		return
	}
	if ok := h.checkEndpointPolicy(w, r, nil, "readwrite", readwriteHost, readwritePort); !ok {
		return
	}
	c := &model.DBConnection{
		Name:          req.Name,
		DBType:        req.DBType,
		Host:          host,
		Port:          port,
		ReadonlyHost:  readonlyHost,
		ReadonlyPort:  readonlyPort,
		ReadwriteHost: readwriteHost,
		ReadwritePort: readwritePort,
		DatabaseName:  normalizeDatabaseName(req.DBType, req.DatabaseName),
		Username:      req.Username,
		SSLMode:       req.SSLMode,
		CreatedBy:     userID,
	}

	created, err := h.repo.Create(r.Context(), c, req.Password, credentials)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "db_connection",
		ResourceID:   &created.ID,
		Details: map[string]any{
			"action":           "create",
			"name":             created.Name,
			"credential_roles": extractCredentialRoles(credentials),
		},
	})

	jsonCreated(w, created)
}

// POST /db-connections/{id}/test — verify connectivity using query_pool
func (h *DBConnectionHandler) Test(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	conn, err := h.repo.GetByID(r.Context(), id)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "connection not found")
		return
	}

	role := strings.TrimSpace(r.URL.Query().Get("credential_role"))
	if role != "" {
		ok, message := h.testConnectionByRole(r.Context(), conn, role)
		h.writeTestResult(w, r, conn.ID, ok, message, []dbConnectionEndpointTestResult{{
			CredentialRole: role,
			OK:             ok,
			Error:          strings.TrimSpace(message),
		}})
		return
	}

	results := make([]dbConnectionEndpointTestResult, 0, 2)
	roles := []string{model.DBCredentialRoleReadonly, model.DBCredentialRoleReadwrite}
	overallOK := true
	failures := make([]string, 0, len(roles))
	for _, targetRole := range roles {
		ok, message := h.testConnectionByRole(r.Context(), conn, targetRole)
		if !ok {
			overallOK = false
			failures = append(failures, targetRole+": "+strings.TrimSpace(message))
		}
		results = append(results, dbConnectionEndpointTestResult{
			CredentialRole: targetRole,
			OK:             ok,
			Error:          strings.TrimSpace(message),
		})
	}

	h.writeTestResult(w, r, conn.ID, overallOK, strings.Join(failures, "; "), results)
}

func (h *DBConnectionHandler) TestRollbackCapability(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	conn, err := h.repo.GetByID(r.Context(), id)
	if err != nil || conn == nil {
		jsonErr(w, http.StatusNotFound, "connection not found")
		return
	}
	result := h.testRollbackCapability(r.Context(), conn)
	actorID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "db_connection_rollback_capability_test",
		ResourceType: "db_connection",
		ResourceID:   &id,
		Details: map[string]any{
			"ok":      result.OK,
			"message": truncate(result.Message, 300),
			"checks":  result.Checks,
		},
		IPAddress: clientIP(r),
	})
	jsonOK(w, result)
}

// PATCH /db-connections/{id}
// All fields are optional; omit to leave unchanged.
// Password is only updated when provided and non-empty.
func (h *DBConnectionHandler) Patch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	existing, err := h.repo.GetByID(r.Context(), id)
	if err != nil || existing == nil {
		jsonErr(w, http.StatusNotFound, "connection not found")
		return
	}

	var req struct {
		Name          *string                        `json:"name"`
		DBType        *string                        `json:"db_type"`
		Host          *string                        `json:"host"`
		Port          *uint16                        `json:"port"`
		ReadonlyHost  *string                        `json:"readonly_host"`
		ReadonlyPort  *uint16                        `json:"readonly_port"`
		ReadwriteHost *string                        `json:"readwrite_host"`
		ReadwritePort *uint16                        `json:"readwrite_port"`
		DatabaseName  json.RawMessage                `json:"database_name"`
		Username      *string                        `json:"username"`
		Password      *string                        `json:"password"`
		SSLMode       *string                        `json:"ssl_mode"`
		Credentials   *[]connectionCredentialPayload `json:"credentials"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := existing.Name
	if req.Name != nil {
		name = *req.Name
	}
	dbType := existing.DBType
	if req.DBType != nil {
		dbType = *req.DBType
	}
	host := existing.Host
	if req.Host != nil {
		host = *req.Host
	}
	port := existing.Port
	if req.Port != nil {
		port = *req.Port
	}
	readonlyHost := existing.EffectiveReadonlyHost()
	if req.ReadonlyHost != nil {
		readonlyHost = strings.TrimSpace(*req.ReadonlyHost)
	} else if req.Host != nil {
		readonlyHost = strings.TrimSpace(*req.Host)
	}
	readonlyPort := existing.EffectiveReadonlyPort()
	if req.ReadonlyPort != nil {
		readonlyPort = *req.ReadonlyPort
	} else if req.Port != nil {
		readonlyPort = *req.Port
	}
	readwriteHost := existing.EffectiveReadwriteHost()
	if req.ReadwriteHost != nil {
		readwriteHost = strings.TrimSpace(*req.ReadwriteHost)
	}
	readwritePort := existing.EffectiveReadwritePort()
	if req.ReadwritePort != nil {
		readwritePort = *req.ReadwritePort
	}
	host, port, readonlyHost, readonlyPort, readwriteHost, readwritePort = normalizeConnectionEndpoints(
		host,
		port,
		readonlyHost,
		readonlyPort,
		readwriteHost,
		readwritePort,
	)
	databaseName := existing.DatabaseName
	if req.DatabaseName != nil {
		if string(req.DatabaseName) == "null" {
			databaseName = nil
		} else {
			var nextDatabaseName string
			if err := json.Unmarshal(req.DatabaseName, &nextDatabaseName); err != nil {
				jsonErr(w, http.StatusBadRequest, "invalid request body")
				return
			}
			databaseName = &nextDatabaseName
		}
	}
	databaseName = normalizeDatabaseName(dbType, databaseName)
	username := existing.Username
	if req.Username != nil {
		username = *req.Username
	}
	sslMode := existing.SSLMode
	if req.SSLMode != nil {
		sslMode = *req.SSLMode
	}

	credentials := normalizeCredentialPayloads(existingCredentials(existing, req.Credentials))
	if err := validateEndpointCredentialRefresh(existing, readonlyHost, readonlyPort, readwriteHost, readwritePort, req.Password, credentials); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if ok := h.checkEndpointPolicy(w, r, &id, "readonly", readonlyHost, readonlyPort); !ok {
		return
	}
	if ok := h.checkEndpointPolicy(w, r, &id, "readwrite", readwriteHost, readwritePort); !ok {
		return
	}

	if err := h.repo.Update(r.Context(), id, name, dbType, host, port, readonlyHost, readonlyPort, readwriteHost, readwritePort, databaseName, username, sslMode); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if req.Password != nil && *req.Password != "" {
		if err := h.repo.UpdatePassword(r.Context(), id, *req.Password); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update password failed")
			return
		}
	}
	if req.Credentials != nil {
		if err := h.repo.ReplaceCredentials(r.Context(), id, credentials); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update credentials failed")
			return
		}
	}

	pool.Global().Invalidate(id)
	pool.RedisGlobal().Invalidate(id)

	userID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "db_connection",
		ResourceID:   &id,
		Details: map[string]any{
			"action":           "update",
			"name":             name,
			"credential_roles": extractCredentialRoles(credentials),
		},
	})

	updated, _ := h.repo.GetByID(r.Context(), id)
	jsonOK(w, updated)
}

func normalizeDatabaseName(dbType string, databaseName *string) *string {
	if dbType != "postgres" && dbType != "postgresql" {
		return databaseName
	}
	if databaseName == nil || strings.TrimSpace(*databaseName) == "" {
		defaultDatabase := "postgres"
		return &defaultDatabase
	}
	trimmed := strings.TrimSpace(*databaseName)
	return &trimmed
}

// GET /db-connections/{id}/bindings
func (h *DBConnectionHandler) Bindings(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	conn, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	if conn == nil {
		jsonErr(w, http.StatusNotFound, "connection not found")
		return
	}

	directUsers, err := h.users.ListDirectUsersByDBConnection(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list direct users failed")
		return
	}
	effectiveUsers, err := h.users.ListEffectiveUsersByDBConnection(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list effective users failed")
		return
	}
	authGroups := []repository.ResourceBoundAuthGroup{}
	if h.auths != nil {
		authGroups, err = h.auths.ListByDBConnection(r.Context(), id)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list auth groups failed")
			return
		}
	}

	if directUsers == nil {
		directUsers = []repository.ResourceBoundUser{}
	}
	if effectiveUsers == nil {
		effectiveUsers = []repository.ResourceBoundUser{}
	}
	if authGroups == nil {
		authGroups = []repository.ResourceBoundAuthGroup{}
	}

	jsonOK(w, map[string]any{
		"db_connection_id": id,
		"direct_users":     directUsers,
		"effective_users":  effectiveUsers,
		"auth_groups":      authGroups,
	})
}

func normalizeConnectionEndpoints(host string, port uint16, readonlyHost string, readonlyPort uint16, readwriteHost string, readwritePort uint16) (string, uint16, string, uint16, string, uint16) {
	normalizedReadonlyHost := strings.TrimSpace(readonlyHost)
	if normalizedReadonlyHost == "" {
		normalizedReadonlyHost = strings.TrimSpace(host)
	}
	normalizedReadonlyPort := readonlyPort
	if normalizedReadonlyPort == 0 {
		normalizedReadonlyPort = port
	}

	normalizedReadwriteHost := strings.TrimSpace(readwriteHost)
	if normalizedReadwriteHost == "" {
		normalizedReadwriteHost = normalizedReadonlyHost
	}
	normalizedReadwritePort := readwritePort
	if normalizedReadwritePort == 0 {
		normalizedReadwritePort = normalizedReadonlyPort
	}

	return normalizedReadonlyHost, normalizedReadonlyPort, normalizedReadonlyHost, normalizedReadonlyPort, normalizedReadwriteHost, normalizedReadwritePort
}

func (h *DBConnectionHandler) testConnectionByRole(ctx context.Context, conn *model.DBConnection, role string) (bool, string) {
	resolvedConn, password, err := h.repo.ResolveCredential(conn, role)
	if err != nil {
		return false, sanitizeConnectionTestError(err)
	}

	if conn.DBType == "redis" {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := pool.RedisGlobal().Ping(pingCtx, pool.RedisConnOptions{
			ConnID:   resolvedConn.ID,
			Host:     resolvedConn.Host,
			Port:     resolvedConn.Port,
			Username: resolvedConn.Username,
			Password: password,
			DB:       0,
			SSLMode:  resolvedConn.SSLMode,
		}); err != nil {
			return false, sanitizeConnectionTestError(err)
		}
		return true, ""
	}

	driver, dsn := pool.BuildDSN(resolvedConn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		return false, sanitizeConnectionTestError(err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pools.QueryPool.PingContext(pingCtx); err != nil {
		return false, sanitizeConnectionTestError(err)
	}

	return true, ""
}

func (h *DBConnectionHandler) testRollbackCapability(ctx context.Context, conn *model.DBConnection) dbConnectionRollbackCapabilityResponse {
	result := dbConnectionRollbackCapabilityResponse{OK: true, Message: "rollback capability test passed"}
	addCheck := func(name string, ok bool, message string) {
		result.Checks = append(result.Checks, dbConnectionRollbackCapabilityCheck{Name: name, OK: ok, Message: message})
		if !ok {
			result.OK = false
			if result.Message == "rollback capability test passed" {
				result.Message = message
			}
		}
	}
	if conn.DBType != "mysql" {
		addCheck("db_type", false, "only mysql connections are supported")
		return result
	}
	addCheck("db_type", true, "")
	if h.settings == nil {
		addCheck("settings", false, "settings repository is not configured")
		return result
	}
	settings, err := h.settings.Get(ctx)
	if err != nil {
		addCheck("settings", false, "load rollback settings failed")
		return result
	}
	if settings == nil || !settings.MySQLRollbackEnabled {
		addCheck("settings", false, "mysql rollback is disabled")
		return result
	}
	addCheck("settings", true, "")
	engine := model.NormalizeMySQLRollbackEngine(settings.MySQLRollbackEngine)
	addCheck("rollback_engine", true, engine+" (beta)")
	if engine == model.MySQLRollbackEnginePriorBackup {
		addCheck("prior_backup_parser", true, "prior backup parser is checked at ticket execution time")
		return result
	}
	my2sqlPath := strings.TrimSpace(settings.MySQLRollbackMy2SQLPath)
	if my2sqlPath == "" {
		addCheck("my2sql", false, "my2sql path is not configured")
		return result
	}
	if strings.Contains(my2sqlPath, "/") {
		if info, err := os.Stat(my2sqlPath); err != nil || info.IsDir() {
			addCheck("my2sql", false, "my2sql binary path is not valid")
		} else {
			addCheck("my2sql", true, "")
		}
	} else if _, err := exec.LookPath(my2sqlPath); err != nil {
		addCheck("my2sql", false, "my2sql binary was not found in PATH")
	} else {
		addCheck("my2sql", true, "")
	}
	resolvedConn, password, err := h.repo.ResolveCredential(conn, model.DBCredentialRoleRollback)
	if err != nil {
		addCheck("rollback_credential", false, "rollback credential is not configured")
		return result
	}
	addCheck("rollback_credential", true, "")
	db, cleanup, err := openResolvedSQLDB(ctx, resolvedConn, password)
	if err != nil {
		addCheck("rollback_connection", false, "rollback credential connection failed")
		return result
	}
	defer cleanup()
	addCheck("rollback_connection", true, "")
	if err := checkMySQLRollbackVariables(ctx, db); err != nil {
		addCheck("mysql_binlog_settings", false, err.Error())
		return result
	}
	addCheck("mysql_binlog_settings", true, "")
	pos, err := readMySQLBinlogPosition(ctx, db)
	if err != nil {
		addCheck("mysql_binlog_position", false, "read mysql binlog position failed")
		return result
	}
	result.Binlog = &dbConnectionRollbackCapabilityBinlogInfo{File: pos.File, Pos: pos.Pos}
	addCheck("mysql_binlog_position", true, "")
	return result
}

func (h *DBConnectionHandler) checkEndpointPolicy(w http.ResponseWriter, r *http.Request, resourceID *uint64, endpoint string, host string, port uint16) bool {
	if h == nil || h.hostPolicy == nil || !h.hostPolicy.Enabled() {
		return true
	}
	report, err := h.hostPolicy.Check(r.Context(), endpoint, host, port)
	if len(report.Violations) == 0 {
		return true
	}
	userID := middleware.UserIDFromCtx(r.Context())
	if h.audit != nil {
		actionType := "db_connection_host_policy_warning"
		if err != nil {
			actionType = "db_connection_host_policy_blocked"
		}
		_ = h.audit.Log(r.Context(), repository.AuditEntry{
			ActorID:      &userID,
			ActorName:    middleware.UsernameFromCtx(r.Context()),
			ActionType:   actionType,
			ResourceType: "db_connection",
			ResourceID:   resourceID,
			Details: map[string]any{
				"endpoint":    report.Endpoint,
				"host":        report.Host,
				"port":        report.Port,
				"ips":         report.IPs,
				"violations":  report.Violations,
				"enforcement": report.Enforcement,
			},
			IPAddress: clientIP(r),
		})
	}
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	return true
}

func sanitizeConnectionTestError(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(err.Error(), "not configured") {
		return "credential is not configured"
	}
	return "connection test failed"
}

func validateEndpointCredentialRefresh(existing *model.DBConnection, readonlyHost string, readonlyPort uint16, readwriteHost string, readwritePort uint16, legacyPassword *string, credentials []model.DBConnectionCredentialInput) error {
	legacyPasswordProvided := legacyPassword != nil && *legacyPassword != ""
	if endpointChanged(existing.EffectiveReadonlyHost(), existing.EffectiveReadonlyPort(), readonlyHost, readonlyPort) && !credentialPasswordProvided(credentials, model.DBCredentialRoleReadonly, legacyPasswordProvided) {
		return errStr("readonly endpoint changed; readonly password is required")
	}
	if endpointChanged(existing.EffectiveReadwriteHost(), existing.EffectiveReadwritePort(), readwriteHost, readwritePort) && !credentialPasswordProvided(credentials, model.DBCredentialRoleReadwrite, legacyPasswordProvided) {
		return errStr("readwrite endpoint changed; readwrite password is required")
	}
	return nil
}

func endpointChanged(currentHost string, currentPort uint16, nextHost string, nextPort uint16) bool {
	return strings.TrimSpace(currentHost) != strings.TrimSpace(nextHost) || currentPort != nextPort
}

func credentialPasswordProvided(credentials []model.DBConnectionCredentialInput, role string, legacyPasswordProvided bool) bool {
	roleConfigured := false
	for _, credential := range credentials {
		if credential.CredentialRole != role {
			continue
		}
		if strings.TrimSpace(credential.Username) == "" {
			continue
		}
		roleConfigured = true
		if credential.Password != "" {
			return true
		}
	}
	if roleConfigured {
		return false
	}
	return legacyPasswordProvided
}

func (h *DBConnectionHandler) writeTestResult(w http.ResponseWriter, r *http.Request, connectionID uint64, ok bool, message string, results []dbConnectionEndpointTestResult) {
	testedAt, err := h.repo.RecordTestResult(r.Context(), connectionID, ok, message)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "persist test result failed")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "db_connection_test",
		ResourceType: "db_connection",
		ResourceID:   &connectionID,
		Details: map[string]any{
			"ok":      ok,
			"message": truncate(strings.TrimSpace(message), 300),
			"results": results,
		},
		IPAddress: clientIP(r),
	})

	response := dbConnectionTestResponse{
		OK:             ok,
		LastTestStatus: "passed",
		LastTestedAt:   &testedAt,
		Results:        results,
	}
	if !ok {
		response.Error = message
		response.LastTestStatus = "failed"
		response.LastTestError = message
	}
	jsonOK(w, response)
}

func normalizeCredentialPayloads(payloads []connectionCredentialPayload) []model.DBConnectionCredentialInput {
	items := make([]model.DBConnectionCredentialInput, 0, len(payloads))
	for _, payload := range payloads {
		role := strings.TrimSpace(payload.CredentialRole)
		username := strings.TrimSpace(payload.Username)
		if role == "" {
			continue
		}
		items = append(items, model.DBConnectionCredentialInput{
			CredentialRole: role,
			Username:       username,
			Password:       payload.Password,
		})
	}
	return items
}

func hasCredentialRole(credentials []model.DBConnectionCredentialInput, role string) bool {
	for _, credential := range credentials {
		if credential.CredentialRole == role && credential.Username != "" {
			return true
		}
	}
	return false
}

func extractCredentialRoles(credentials []model.DBConnectionCredentialInput) []string {
	roles := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		if credential.CredentialRole == "" {
			continue
		}
		roles = append(roles, credential.CredentialRole)
	}
	return roles
}

func existingCredentials(existing *model.DBConnection, patch *[]connectionCredentialPayload) []connectionCredentialPayload {
	if patch != nil {
		return *patch
	}
	items := make([]connectionCredentialPayload, 0, len(existing.Credentials))
	for _, credential := range existing.Credentials {
		items = append(items, connectionCredentialPayload{
			CredentialRole: credential.CredentialRole,
			Username:       credential.Username,
		})
	}
	return items
}

// DELETE /db-connections/{id}
func (h *DBConnectionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	conn, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		slog.Warn("load db connection before delete failed", "connection_id", id, "err", err)
		jsonErr(w, http.StatusInternalServerError, "load connection failed")
		return
	}
	if conn == nil {
		jsonErr(w, http.StatusNotFound, "connection not found")
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	if err := h.repo.Delete(r.Context(), id, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonErr(w, http.StatusNotFound, "connection not found")
			return
		}
		slog.Warn("delete db connection failed", "connection_id", id, "err", err)
		jsonErr(w, http.StatusInternalServerError, "delete failed")
		return
	}

	pool.Global().Invalidate(id)
	pool.RedisGlobal().Invalidate(id)

	details := auditConnectionDetails(conn)
	details["action"] = "delete"
	details["cleaned_dependency_types"] = []string{
		"credentials",
		"direct_user_db_scope",
		"auth_group_db_scope",
		"metadata_object_snapshots",
		"rollback_artifacts",
		"masking_whitelist",
		"masking_rules",
		"redis_sensitive_key_prefixes",
		"query_access_rules",
		"query_access_grants",
		"scheduled_sql_reports",
		"metadata_scan_settings",
	}
	details["retained_history_types"] = []string{
		"tickets",
		"audit_logs",
		"query_history",
		"saved_queries",
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "setting_change",
		ResourceType: "db_connection",
		ResourceID:   &id,
		Details:      details,
	})

	w.WriteHeader(http.StatusNoContent)
}
