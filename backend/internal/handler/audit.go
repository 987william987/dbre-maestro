package handler

import (
	"net/http"
	"strconv"
	"time"

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
//	resource_type — exact match (ticket|user|db_connection|export)
//	resource_id   — uint64 exact match
//	from          — RFC3339 lower bound on created_at
//	to            — RFC3339 upper bound on created_at
//	limit         — default 50, max 200
//	offset        — default 0
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
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
	if v := q.Get("resource_type"); v != "" {
		f.ResourceType = &v
	}
	if v := q.Get("resource_id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			f.ResourceID = &n
		}
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

	logs, total, err := h.audit.List(r.Context(), f, limit, offset)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list audit logs failed")
		return
	}

	jsonOK(w, map[string]any{
		"logs":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
