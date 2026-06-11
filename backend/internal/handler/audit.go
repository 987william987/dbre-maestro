package handler

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type AuditHandler struct {
	audit *repository.AuditRepo
}

func NewAuditHandler(audit *repository.AuditRepo) *AuditHandler {
	return &AuditHandler{audit: audit}
}

// GET /audit-logs — DBA/Admin only; supports filtering via query params.
//
// Query params:
//
//	action_type   — exact match (e.g. ticket_approve)
//	actor_id      — uint64 exact match
//	actor_name    — fuzzy match
//	resource_type — exact match (ticket|user|db_connection|export)
//	resource_id   — uint64 exact match
//	resource_name — fuzzy match against details.name
//	from          — RFC3339 lower bound on created_at
//	to            — RFC3339 upper bound on created_at
//	limit         — default 50, max 200
//	offset        — default 0
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	f, limit, offset := parseAuditFilters(r)

	logs, total, err := h.audit.List(r.Context(), f, limit, offset)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list audit logs failed")
		return
	}
	if logs == nil {
		logs = []model.AuditLog{}
	}

	jsonOK(w, map[string]any{
		"logs":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GET /audit-logs/export — DBA/Admin only; requires audit_logs.write.
func (h *AuditHandler) Export(w http.ResponseWriter, r *http.Request) {
	f, _, _ := parseAuditFilters(r)

	logs, _, err := h.audit.List(r.Context(), f, 5000, 0)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "export audit logs failed")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	_ = h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "audit_export",
		ResourceType: "audit_log",
		Details:      map[string]any{"count": len(logs)},
		IPAddress:    clientIP(r),
	})

	filename := "audit-logs-" + time.Now().UTC().Format("2006-01-02-150405") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{"時間", "操作人", "動作", "資源類型", "來源 IP", "明細"})
	for _, log := range logs {
		_ = writer.Write([]string{
			log.CreatedAt.UTC().Format(time.RFC3339),
			formatAuditActor(log),
			log.ActionType,
			formatAuditResource(log),
			formatAuditIP(log.IPAddress),
			formatAuditDetails(log.Details),
		})
	}
}

func parseAuditFilters(r *http.Request) (repository.AuditListFilter, int, int) {
	q := r.URL.Query()

	limit := 50
	offset := 0

	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	f := repository.AuditListFilter{}

	if v := q.Get("action_type"); v != "" {
		f.ActionType = &v
	}
	if v := q.Get("actor_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			f.ActorID = &n
		}
	}
	if v := q.Get("actor_name"); v != "" {
		f.ActorName = &v
	}
	if v := q.Get("resource_type"); v != "" {
		f.ResourceType = &v
	}
	if v := q.Get("resource_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			f.ResourceID = &n
		}
	}
	if v := q.Get("resource_name"); v != "" {
		f.ResourceName = &v
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = &t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = &t
		}
	}

	return f, limit, offset
}

func formatAuditActor(log model.AuditLog) string {
	if log.ActorName != "" {
		return log.ActorName
	}
	if log.ActorID != nil {
		return "user:" + strconv.FormatUint(*log.ActorID, 10)
	}
	return "system"
}

func formatAuditResource(log model.AuditLog) string {
	if log.ResourceType == nil {
		return ""
	}
	if log.ResourceID != nil {
		return *log.ResourceType + ":" + strconv.FormatUint(*log.ResourceID, 10)
	}
	return *log.ResourceType
}

func formatAuditIP(ip *string) string {
	if ip == nil {
		return ""
	}
	return *ip
}

func formatAuditDetails(details json.RawMessage) string {
	if len(details) == 0 {
		return ""
	}
	return string(details)
}
