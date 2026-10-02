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
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sessionmanagement"
)

type prefixPreviewRequest struct {
	sessionTargetRequest
	Database          string  `json:"database"`
	Prefix            string  `json:"prefix"`
	MinimumAgeSeconds float64 `json:"minimum_age_seconds"`
}

type prefixCancelRequest struct {
	sessionTargetRequest
	PreviewToken string `json:"preview_token"`
}

type prefixPreviewItem struct {
	ID              string  `json:"id"`
	User            string  `json:"user,omitempty"`
	Database        string  `json:"database,omitempty"`
	Client          string  `json:"client,omitempty"`
	State           string  `json:"state,omitempty"`
	DurationSeconds float64 `json:"duration_seconds"`
	QueryShape      string  `json:"query_shape"`
	QueryHash       string  `json:"query_hash"`
	BackendStart    string  `json:"backend_start,omitempty"`
}

func (h *SessionManagementHandler) PreviewPrefix(w http.ResponseWriter, r *http.Request) {
	var req prefixPreviewRequest
	if !decodeSessionRequest(w, r, &req) {
		return
	}
	database := strings.TrimSpace(req.Database)
	if database == "" || len(database) > 256 {
		jsonErr(w, http.StatusUnprocessableEntity, "database is required")
		return
	}
	if req.MinimumAgeSeconds < 0 {
		jsonErr(w, http.StatusUnprocessableEntity, "minimum_age_seconds cannot be negative")
		return
	}
	normalizedPrefix, err := sessionmanagement.ValidatePrefix(req.Prefix)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	conn, ok := h.loadScopedConnection(w, r, req.ConnectionID)
	if !ok {
		return
	}
	engine := strings.ToLower(strings.TrimSpace(conn.DBType))
	if engine == "redis" {
		jsonErr(w, http.StatusUnprocessableEntity, "prefix cancellation is not supported for Redis")
		return
	}
	resolved, password, ok := h.resolveTarget(w, r, conn, req.sessionTargetRequest, false)
	if !ok {
		return
	}
	result, err := sessionmanagement.ListSessions(r.Context(), resolved, password)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, "read database sessions failed")
		return
	}
	matches := sessionmanagement.MatchPrefixSessions(result.Items, database, normalizedPrefix, req.MinimumAgeSeconds, engine)
	token, preview, err := h.previews.Create(sessionmanagement.PrefixPreview{ActorID: middleware.UserIDFromCtx(r.Context()), ConnectionID: conn.ID, TargetKey: sessionTargetKey(req.sessionTargetRequest), Engine: engine, Database: database, NormalizedPrefix: normalizedPrefix, MinimumAgeSeconds: req.MinimumAgeSeconds, Matches: matches})
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create prefix preview failed")
		return
	}
	items := make([]prefixPreviewItem, 0, len(matches))
	for _, item := range matches {
		items = append(items, prefixPreviewItem{ID: item.ID, User: item.User, Database: item.Database, Client: item.Client, State: item.State, DurationSeconds: item.DurationSeconds, QueryShape: sessionmanagement.SanitizeSQLShape(item.Query), QueryHash: item.QueryHash, BackendStart: item.BackendStart})
	}
	jsonOK(w, map[string]any{"preview_token": token, "expires_at": preview.ExpiresAt, "prefix_shape": sessionmanagement.SanitizeSQLShape(req.Prefix), "prefix_hash": sessionmanagement.PrefixHash(normalizedPrefix), "items": items})
}

func (h *SessionManagementHandler) CancelPrefix(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	var req prefixCancelRequest
	if !decodeSessionRequest(w, r, &req) {
		return
	}
	conn, ok := h.loadScopedConnection(w, r, req.ConnectionID)
	if !ok {
		return
	}
	preview, err := h.previews.Consume(strings.TrimSpace(req.PreviewToken), middleware.UserIDFromCtx(r.Context()), conn.ID, sessionTargetKey(req.sessionTargetRequest))
	if errors.Is(err, sessionmanagement.ErrPreviewMismatch) {
		h.auditPrefixSummary(r, conn.ID, conn.DBType, req.sessionTargetRequest, nil, "rejected", "preview actor or target mismatch", 0, 0, 0, startedAt)
		jsonErr(w, http.StatusConflict, "prefix preview does not match actor or target")
		return
	}
	if err != nil {
		h.auditPrefixSummary(r, conn.ID, conn.DBType, req.sessionTargetRequest, nil, "rejected", "preview missing, expired, or used", 0, 0, 0, startedAt)
		jsonErr(w, http.StatusConflict, "prefix preview is missing, expired, or already used")
		return
	}
	engine := strings.ToLower(strings.TrimSpace(conn.DBType))
	if preview.Engine != engine || engine == "redis" {
		h.auditPrefixSummary(r, conn.ID, conn.DBType, req.sessionTargetRequest, &preview, "rejected", "engine mismatch", 0, 0, 0, startedAt)
		jsonErr(w, http.StatusConflict, "prefix preview engine no longer matches")
		return
	}
	resolved, password, ok := h.resolveTarget(w, r, conn, req.sessionTargetRequest, true)
	if !ok {
		h.auditPrefixSummary(r, conn.ID, conn.DBType, req.sessionTargetRequest, &preview, "rejected", "target revalidation failed", 0, 0, 0, startedAt)
		return
	}
	result, err := sessionmanagement.ListSessions(r.Context(), resolved, password)
	if err != nil {
		h.auditPrefixSummary(r, conn.ID, conn.DBType, req.sessionTargetRequest, &preview, "failed", "session revalidation failed", 0, 0, 0, startedAt)
		jsonErr(w, http.StatusBadGateway, "revalidate database sessions failed")
		return
	}
	cancelled, skipped, failed := 0, 0, 0
	for i := range preview.Matches {
		snapshot := &preview.Matches[i]
		current := findSession(result.Items, snapshot.ID)
		eligible := current != nil && sessionIdentityMatches(*current, sessionIdentityFromSession(*snapshot)) && len(sessionmanagement.MatchPrefixSessions([]sessionmanagement.Session{*current}, preview.Database, preview.NormalizedPrefix, preview.MinimumAgeSeconds, preview.Engine)) == 1
		if !eligible {
			skipped++
			h.auditSignal(r, conn, resolved, snapshot.ID, "prefix_cancel", current, "skipped", "stale or no longer eligible", startedAt)
			continue
		}
		if err := sessionmanagement.SignalSession(r.Context(), resolved, password, current.ID, "cancel"); err != nil {
			failed++
			h.auditSignal(r, conn, resolved, current.ID, "prefix_cancel", current, "failed", "database signal failed", startedAt)
			continue
		}
		cancelled++
		h.auditSignal(r, conn, resolved, current.ID, "prefix_cancel", current, "succeeded", "", startedAt)
	}
	resultStatus := "succeeded"
	if failed > 0 {
		resultStatus = "partial_failure"
	}
	h.auditPrefixSummary(r, conn.ID, conn.DBType, req.sessionTargetRequest, &preview, resultStatus, "", cancelled, skipped, failed, startedAt)
	jsonOK(w, map[string]any{"matched_count": len(preview.Matches), "cancelled_count": cancelled, "skipped_count": skipped, "failed_count": failed})
}

func (h *SessionManagementHandler) auditPrefixSummary(r *http.Request, connectionID uint64, engine string, target sessionTargetRequest, preview *sessionmanagement.PrefixPreview, result, reason string, cancelled, skipped, failed int, startedAt time.Time) {
	if h.audit == nil {
		return
	}
	details := prefixSummaryAuditDetails(engine, target, preview, result, reason, cancelled, skipped, failed, time.Since(startedAt))
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := h.audit.Log(r.Context(), repository.AuditEntry{ActorID: &actorID, ActorName: middleware.UsernameFromCtx(r.Context()), ActionType: "db_session_prefix_cancel_summary", ResourceType: "db_connection", ResourceID: &connectionID, IPAddress: clientIP(r), Details: details}); err != nil {
		slog.Warn("session prefix cancellation audit failed", "connection_id", connectionID, "err", err)
	}
}

func prefixSummaryAuditDetails(engine string, target sessionTargetRequest, preview *sessionmanagement.PrefixPreview, result, reason string, cancelled, skipped, failed int, duration time.Duration) map[string]any {
	details := map[string]any{"engine": engine, "target_mode": strings.ToLower(strings.TrimSpace(target.Mode)), "region": strings.TrimSpace(target.Region), "cluster_id": strings.TrimSpace(target.ClusterID), "node_id": strings.TrimSpace(target.NodeID), "target_host": strings.TrimSpace(target.Host), "target_port": target.Port, "result": result, "reason": reason, "cancelled_count": cancelled, "skipped_count": skipped, "failed_count": failed, "duration_ms": duration.Milliseconds()}
	if preview != nil {
		details["matched_count"] = len(preview.Matches)
		details["database"] = preview.Database
		details["prefix_shape"] = sessionmanagement.SanitizeSQLShape(preview.NormalizedPrefix)
		details["prefix_hash"] = sessionmanagement.PrefixHash(preview.NormalizedPrefix)
	}
	return details
}

func decodeSessionRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		jsonErr(w, http.StatusBadRequest, "request body must contain exactly one JSON object")
		return false
	}
	return true
}

func sessionTargetKey(req sessionTargetRequest) string {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "manual" {
		return "manual|" + strings.ToLower(strings.TrimSpace(req.Host)) + "|" + strconv.FormatUint(uint64(req.Port), 10)
	}
	if mode == "aws" {
		return "aws|" + strings.TrimSpace(req.Region) + "|" + strings.TrimSpace(req.ClusterID) + "|" + strings.TrimSpace(req.NodeID)
	}
	return "invalid|" + mode
}

func sessionIdentityFromSession(item sessionmanagement.Session) sessionIdentityRequest {
	return sessionIdentityRequest{User: item.User, Database: item.Database, Client: item.Client, QueryHash: item.QueryHash, BackendStart: item.BackendStart}
}
