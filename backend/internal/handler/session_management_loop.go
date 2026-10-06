package handler

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sessionmanagement"
	"github.com/go-chi/chi/v5"
)

type loopJobCreateRequest struct {
	sessionTargetRequest
	PreviewToken    string `json:"preview_token"`
	IntervalSeconds uint   `json:"interval_seconds"`
	DurationSeconds uint   `json:"duration_seconds"`
	MaxKills        uint   `json:"max_kills"`
}

func (h *SessionManagementHandler) ListLoopJobs(w http.ResponseWriter, r *http.Request) {
	ids, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), middleware.UserIDFromCtx(r.Context()))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "db scope check failed")
		return
	}
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 50)
	if limit > 100 {
		limit = 100
	}
	offset := parsePositiveInt(r.URL.Query().Get("offset"), 0)
	items, err := h.loopJobs.ListForConnections(r.Context(), ids, uint(limit), uint(offset))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load loop jobs failed")
		return
	}
	jsonOK(w, map[string]any{"items": items, "limit": limit, "offset": offset})
}

func (h *SessionManagementHandler) CreateLoopJob(w http.ResponseWriter, r *http.Request) {
	var req loopJobCreateRequest
	if !decodeSessionRequest(w, r, &req) {
		return
	}
	if req.IntervalSeconds == 0 {
		req.IntervalSeconds = 2
	}
	if req.DurationSeconds == 0 {
		req.DurationSeconds = 600
	}
	if req.MaxKills == 0 {
		req.MaxKills = 100
	}
	if req.IntervalSeconds < 1 || req.IntervalSeconds > 30 || req.DurationSeconds > 3600 || req.MaxKills > 1000 {
		jsonErr(w, http.StatusUnprocessableEntity, "loop limits are outside the allowed range")
		return
	}
	conn, ok := h.loadScopedConnection(w, r, req.ConnectionID)
	if !ok {
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	preview, err := h.previews.Consume(strings.TrimSpace(req.PreviewToken), actorID, conn.ID, sessionTargetKey(req.sessionTargetRequest))
	if err != nil {
		jsonErr(w, http.StatusConflict, "prefix preview is missing, expired, used, or does not match target")
		return
	}
	engine := strings.ToLower(strings.TrimSpace(conn.DBType))
	if engine == "redis" || preview.Engine != engine {
		jsonErr(w, http.StatusUnprocessableEntity, "loop cancellation supports only MySQL and PostgreSQL")
		return
	}
	if preview.MinimumAgeSeconds > float64(^uint32(0)) {
		jsonErr(w, http.StatusUnprocessableEntity, "minimum age is too large for a loop job")
		return
	}
	if _, _, ok := h.resolveTarget(w, r, conn, req.sessionTargetRequest, true); !ok {
		return
	}
	targetKey := sessionmanagement.PrefixHash(sessionTargetKey(req.sessionTargetRequest))
	job, err := h.loopJobs.Create(r.Context(), repository.SessionLoopJobInput{RequestedBy: actorID, ConnectionID: conn.ID, Engine: engine, TargetMode: strings.ToLower(strings.TrimSpace(req.Mode)), Region: strings.TrimSpace(req.Region), ClusterID: strings.TrimSpace(req.ClusterID), NodeID: strings.TrimSpace(req.NodeID), Host: strings.TrimSpace(req.Host), Port: req.Port, TargetKey: targetKey, Database: preview.Database, Prefix: preview.NormalizedPrefix, PrefixShape: sessionmanagement.SanitizeSQLShape(preview.NormalizedPrefix), PrefixHash: sessionmanagement.PrefixHash(preview.NormalizedPrefix), MinimumAgeSeconds: uint(math.Ceil(preview.MinimumAgeSeconds)), IntervalSeconds: req.IntervalSeconds, DurationSeconds: req.DurationSeconds, MaxKills: req.MaxKills})
	if errors.Is(err, repository.ErrSessionLoopTargetActive) {
		jsonErr(w, http.StatusConflict, "an active loop job already exists for this physical node")
		return
	}
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create loop job failed")
		return
	}
	h.auditLoopChange(r, job, "db_session_loop_created")
	jsonCreated(w, job)
}

func (h *SessionManagementHandler) StopLoopJob(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(strings.TrimSpace(chi.URLParam(r, "id")), 10, 64)
	if err != nil || id == 0 {
		jsonErr(w, http.StatusBadRequest, "invalid loop job id")
		return
	}
	job, err := h.loopJobs.GetByID(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load loop job failed")
		return
	}
	if job == nil {
		jsonErr(w, http.StatusNotFound, "loop job not found")
		return
	}
	if _, ok := h.loadScopedConnection(w, r, job.ConnectionID); !ok {
		return
	}
	changed, err := h.loopJobs.Stop(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "stop loop job failed")
		return
	}
	if !changed {
		jsonErr(w, http.StatusConflict, "loop job is already terminal")
		return
	}
	job.Status = model.SessionLoopStatusStopped
	h.auditLoopChange(r, job, "db_session_loop_stopped")
	jsonOK(w, map[string]any{"ok": true})
}

func (h *SessionManagementHandler) auditLoopChange(r *http.Request, job *model.SessionLoopJob, action string) {
	if h.audit == nil || job == nil {
		return
	}
	actorID, connectionID := middleware.UserIDFromCtx(r.Context()), job.ConnectionID
	if err := h.audit.Log(r.Context(), repository.AuditEntry{ActorID: &actorID, ActorName: middleware.UsernameFromCtx(r.Context()), ActionType: action, ResourceType: "db_connection", ResourceID: &connectionID, IPAddress: clientIP(r), Details: map[string]any{"loop_job_id": job.ID, "engine": job.Engine, "target_mode": job.TargetMode, "region": job.Region, "cluster_id": job.ClusterID, "node_id": job.NodeID, "target_host": job.TargetHost, "target_port": job.TargetPort, "database": job.DatabaseName, "prefix_shape": job.PrefixShape, "prefix_hash": job.PrefixHash, "status": job.Status}}); err != nil {
		slog.Warn("session loop job audit failed", "job_id", job.ID, "action", action, "err", err)
	}
}
