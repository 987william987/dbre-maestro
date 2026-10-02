package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sessionmanagement"
	"github.com/go-chi/chi/v5"
)

type sessionTargetRequest struct {
	ConnectionID uint64 `json:"connection_id"`
	Mode         string `json:"mode"`
	Region       string `json:"region"`
	ClusterID    string `json:"cluster_id"`
	NodeID       string `json:"node_id"`
	Host         string `json:"host"`
	Port         uint16 `json:"port"`
}

type sessionIdentityRequest struct {
	User         string `json:"user"`
	Database     string `json:"database"`
	Client       string `json:"client"`
	QueryHash    string `json:"query_hash"`
	BackendStart string `json:"backend_start"`
}

type sessionActionRequest struct {
	sessionTargetRequest
	Expected sessionIdentityRequest `json:"expected"`
}

func (h *SessionManagementHandler) Connections(w http.ResponseWriter, r *http.Request) {
	items, err := listAccessibleConnections(r.Context(), h.connections, h.users, middleware.UserIDFromCtx(r.Context()))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load accessible connections failed")
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item.DBType != "mysql" && item.DBType != "postgres" && item.DBType != "postgresql" && item.DBType != "redis" {
			continue
		}
		result = append(result, map[string]any{"id": item.ID, "name": item.Name, "db_type": item.DBType, "operations_configured": hasConfiguredCredentialRole(item.Credentials, model.DBCredentialRoleOperations)})
	}
	jsonOK(w, map[string]any{"items": result})
}

func (h *SessionManagementHandler) Sessions(w http.ResponseWriter, r *http.Request) {
	var req sessionTargetRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		jsonErr(w, http.StatusBadRequest, "request body must contain exactly one JSON object")
		return
	}
	conn, ok := h.loadScopedConnection(w, r, req.ConnectionID)
	if !ok {
		return
	}
	resolved, password, ok := h.resolveTarget(w, r, conn, req, false)
	if !ok {
		return
	}
	result, err := sessionmanagement.ListSessions(r.Context(), resolved, password)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, "read database sessions failed")
		return
	}
	jsonOK(w, result)
}

type SessionManagementHandler struct {
	connections *repository.DBConnectionRepo
	users       *repository.UserRepo
	settings    *repository.SettingsRepo
	topology    *sessionmanagement.Service
	audit       *repository.AuditRepo
	previews    *sessionmanagement.PreviewStore
	loopJobs    *repository.SessionLoopJobRepo
}

func NewSessionManagementHandler(connections *repository.DBConnectionRepo, users *repository.UserRepo, settings *repository.SettingsRepo, topology *sessionmanagement.Service, audit *repository.AuditRepo, loopJobs *repository.SessionLoopJobRepo) *SessionManagementHandler {
	return &SessionManagementHandler{connections: connections, users: users, settings: settings, topology: topology, audit: audit, previews: sessionmanagement.NewPreviewStore(), loopJobs: loopJobs}
}

func (h *SessionManagementHandler) CancelSession(w http.ResponseWriter, r *http.Request) {
	h.signalSession(w, r, "cancel")
}
func (h *SessionManagementHandler) TerminateSession(w http.ResponseWriter, r *http.Request) {
	h.signalSession(w, r, "terminate")
}

func (h *SessionManagementHandler) signalSession(w http.ResponseWriter, r *http.Request, action string) {
	startedAt := time.Now()
	sessionID := strings.TrimSpace(chi.URLParam(r, "id"))
	if sessionID == "" {
		jsonErr(w, http.StatusBadRequest, "session id is required")
		return
	}
	var req sessionActionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		jsonErr(w, http.StatusBadRequest, "request body must contain exactly one JSON object")
		return
	}
	conn, ok := h.loadScopedConnection(w, r, req.ConnectionID)
	if !ok {
		return
	}
	resolved, password, ok := h.resolveTarget(w, r, conn, req.sessionTargetRequest, true)
	if !ok {
		return
	}
	if strings.EqualFold(conn.DBType, "redis") && action == "cancel" {
		h.auditSignal(r, conn, resolved, sessionID, action, nil, "rejected", "unsupported action", startedAt)
		jsonErr(w, http.StatusUnprocessableEntity, "Redis sessions only support disconnect")
		return
	}
	result, err := sessionmanagement.ListSessions(r.Context(), resolved, password)
	if err != nil {
		h.auditSignal(r, conn, resolved, sessionID, action, nil, "failed", "revalidation failed", startedAt)
		jsonErr(w, http.StatusBadGateway, "revalidate database session failed")
		return
	}
	current := findSession(result.Items, sessionID)
	if current == nil || !sessionIdentityMatches(*current, req.Expected) {
		h.auditSignal(r, conn, resolved, sessionID, action, current, "skipped", "stale session identity", startedAt)
		jsonErr(w, http.StatusConflict, "session changed or no longer exists")
		return
	}
	if current.Protected {
		h.auditSignal(r, conn, resolved, sessionID, action, current, "rejected", current.ProtectedReason, startedAt)
		jsonErr(w, http.StatusForbidden, "protected sessions cannot be modified")
		return
	}
	if err := sessionmanagement.SignalSession(r.Context(), resolved, password, sessionID, action); err != nil {
		h.auditSignal(r, conn, resolved, sessionID, action, current, "failed", "database signal failed", startedAt)
		jsonErr(w, http.StatusBadGateway, "database session action failed")
		return
	}
	h.auditSignal(r, conn, resolved, sessionID, action, current, "succeeded", "", startedAt)
	jsonOK(w, map[string]any{"ok": true})
}

func (h *SessionManagementHandler) resolveTarget(w http.ResponseWriter, r *http.Request, conn *model.DBConnection, req sessionTargetRequest, live bool) (*model.DBConnection, string, bool) {
	resolved, password, err := h.connections.ResolveCredential(conn, model.DBCredentialRoleOperations)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, "operations credential is not configured")
		return nil, "", false
	}
	switch strings.ToLower(strings.TrimSpace(req.Mode)) {
	case "aws":
		settings, err := h.settings.Get(r.Context())
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "load AWS regions failed")
			return nil, "", false
		}
		var node *sessionmanagement.Node
		if live {
			node, err = h.topology.ResolveOwnedNodeLive(r.Context(), conn, settings.DBMetadataInventoryRegions, req.Region, req.ClusterID, req.NodeID)
		} else {
			node, err = h.topology.ResolveOwnedNode(r.Context(), conn, settings.DBMetadataInventoryRegions, req.Region, req.ClusterID, req.NodeID)
		}
		if errors.Is(err, sessionmanagement.ErrTargetNotOwned) {
			jsonErr(w, http.StatusForbidden, "AWS target is not owned by DB connection")
			return nil, "", false
		}
		if err != nil {
			jsonErr(w, http.StatusBadGateway, "load live AWS topology failed")
			return nil, "", false
		}
		resolved.Host, resolved.Port = node.Host, node.Port
	case "manual":
		if _, err := h.topology.ValidateManualTarget(r.Context(), req.Host, req.Port); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, "manual target is not allowed")
			return nil, "", false
		}
		resolved.Host, resolved.Port = strings.TrimSpace(req.Host), req.Port
	default:
		jsonErr(w, http.StatusUnprocessableEntity, "mode must be aws or manual")
		return nil, "", false
	}
	return resolved, password, true
}

func findSession(items []sessionmanagement.Session, id string) *sessionmanagement.Session {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}
func sessionIdentityMatches(current sessionmanagement.Session, expected sessionIdentityRequest) bool {
	return expected.QueryHash != "" && current.User == expected.User && current.Database == expected.Database && current.Client == expected.Client && current.QueryHash == expected.QueryHash && current.BackendStart == expected.BackendStart
}

func sessionSignalAuditDetails(conn, target *model.DBConnection, sessionID string, current *sessionmanagement.Session, result, reason string, duration time.Duration) map[string]any {
	details := map[string]any{"engine": conn.DBType, "target_host": target.Host, "target_port": target.Port, "session_id": sessionID, "result": result, "reason": reason, "duration_ms": duration.Milliseconds()}
	if current != nil {
		details["db_user"] = current.User
		details["database"] = current.Database
		details["client"] = current.Client
		details["query_hash"] = current.QueryHash
		details["backend_start"] = current.BackendStart
		details["query_shape"] = sessionmanagement.SanitizeSQLShape(current.Query)
	}
	return details
}

func (h *SessionManagementHandler) auditSignal(r *http.Request, conn, target *model.DBConnection, sessionID, action string, current *sessionmanagement.Session, result, reason string, startedAt time.Time) {
	if h.audit == nil {
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	err := h.audit.Log(r.Context(), repository.AuditEntry{ActorID: &actorID, ActorName: middleware.UsernameFromCtx(r.Context()), ActionType: "db_session_" + action, ResourceType: "db_connection", ResourceID: &conn.ID, IPAddress: clientIP(r), Details: sessionSignalAuditDetails(conn, target, sessionID, current, result, reason, time.Since(startedAt))})
	if err != nil {
		slog.Warn("session management audit failed", "connection_id", conn.ID, "action", action, "err", err)
	}
}

func (h *SessionManagementHandler) AWSClusters(w http.ResponseWriter, r *http.Request) {
	conn, regions, ok := h.scopedConnection(w, r)
	if !ok {
		return
	}
	items, err := h.topology.OwnedClusters(r.Context(), conn, regions)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, "load live AWS clusters failed")
		return
	}
	jsonOK(w, map[string]any{"items": items})
}

func (h *SessionManagementHandler) AWSTopology(w http.ResponseWriter, r *http.Request) {
	conn, regions, ok := h.scopedConnection(w, r)
	if !ok {
		return
	}
	cluster, err := h.topology.OwnedTopology(r.Context(), conn, regions, r.URL.Query().Get("region"), r.URL.Query().Get("cluster_id"))
	if errors.Is(err, sessionmanagement.ErrTargetNotOwned) {
		jsonErr(w, http.StatusForbidden, "AWS target is not owned by DB connection")
		return
	}
	if err != nil {
		jsonErr(w, http.StatusBadGateway, "load live AWS topology failed")
		return
	}
	jsonOK(w, cluster)
}

func (h *SessionManagementHandler) scopedConnection(w http.ResponseWriter, r *http.Request) (*model.DBConnection, []string, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(r.URL.Query().Get("connection_id")), 10, 64)
	if err != nil || id == 0 {
		jsonErr(w, http.StatusBadRequest, "connection_id is required")
		return nil, nil, false
	}
	conn, ok := h.loadScopedConnection(w, r, id)
	if !ok {
		return nil, nil, false
	}
	settings, err := h.settings.Get(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load AWS regions failed")
		return nil, nil, false
	}
	if len(settings.DBMetadataInventoryRegions) == 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "no AWS regions configured")
		return nil, nil, false
	}
	return conn, settings.DBMetadataInventoryRegions, true
}

func (h *SessionManagementHandler) loadScopedConnection(w http.ResponseWriter, r *http.Request, id uint64) (*model.DBConnection, bool) {
	if id == 0 {
		jsonErr(w, http.StatusBadRequest, "connection_id is required")
		return nil, false
	}
	ids, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), middleware.UserIDFromCtx(r.Context()))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return nil, false
	}
	if !containsUint64(ids, id) {
		jsonErr(w, http.StatusForbidden, "DB connection is outside current scope")
		return nil, false
	}
	conn, err := h.connections.GetByID(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load DB connection failed")
		return nil, false
	}
	if conn == nil {
		jsonErr(w, http.StatusNotFound, "DB connection not found")
		return nil, false
	}
	return conn, true
}
