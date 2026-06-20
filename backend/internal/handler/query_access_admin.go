package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type QueryAccessAdminHandler struct {
	queryAccess *repository.QueryAccessRepo
	users       *repository.UserRepo
	auths       *repository.AuthGroupRepo
	audit       *repository.AuditRepo
}

func NewQueryAccessAdminHandler(queryAccess *repository.QueryAccessRepo, users *repository.UserRepo, auths *repository.AuthGroupRepo, audit *repository.AuditRepo) *QueryAccessAdminHandler {
	return &QueryAccessAdminHandler{queryAccess: queryAccess, users: users, auths: auths, audit: audit}
}

func (h *QueryAccessAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	rules, err := h.queryAccess.ListRules(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list query access rules failed")
		return
	}
	if rules == nil {
		rules = []model.QueryAccessRule{}
	}
	jsonOK(w, map[string]any{"rules": rules})
}

func (h *QueryAccessAdminHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SubjectType     model.QueryAccessSubjectType `json:"subject_type"`
		SubjectID       uint64                       `json:"subject_id"`
		Effect          model.QueryAccessEffect      `json:"effect"`
		ConnectionID    uint64                       `json:"connection_id"`
		DatabasePattern string                       `json:"database_pattern"`
		TablePattern    string                       `json:"table_pattern"`
		DurationMinutes int                          `json:"duration_minutes"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Effect != model.QueryAccessEffectAllow && req.Effect != model.QueryAccessEffectDeny {
		jsonErr(w, http.StatusUnprocessableEntity, "effect must be allow or deny")
		return
	}
	durationMinutes, err := normalizeQueryAccessDurationMinutes(req.DurationMinutes)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.validateSubjectDBScope(r.Context(), req.SubjectType, req.SubjectID, req.ConnectionID); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	expiresAt := time.Now().UTC().Add(time.Duration(durationMinutes) * time.Minute)
	actorID := middleware.UserIDFromCtx(r.Context())
	rule, err := h.queryAccess.CreateManualRule(r.Context(), model.QueryAccessRule{
		SubjectType:     req.SubjectType,
		SubjectID:       req.SubjectID,
		Effect:          req.Effect,
		ConnectionID:    req.ConnectionID,
		DatabasePattern: req.DatabasePattern,
		TablePattern:    req.TablePattern,
		ExpiresAt:       &expiresAt,
	}, actorID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create query access rule failed")
		return
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "query_access_rule_create",
		ResourceType: "query_access_rule",
		ResourceID:   &rule.ID,
		Details: map[string]any{
			"subject_type":     rule.SubjectType,
			"subject_id":       rule.SubjectID,
			"effect":           rule.Effect,
			"connection_id":    rule.ConnectionID,
			"database_pattern": rule.DatabasePattern,
			"table_pattern":    rule.TablePattern,
			"expires_at":       expiresAt,
		},
		IPAddress: clientIP(r),
	})
	jsonCreated(w, rule)
}

func (h *QueryAccessAdminHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		jsonErr(w, http.StatusBadRequest, "invalid query access rule id")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	ok, err := h.queryAccess.RevokeRule(r.Context(), id, actorID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "revoke query access rule failed")
		return
	}
	if !ok {
		jsonErr(w, http.StatusNotFound, "query access rule not found or already revoked")
		return
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "query_access_rule_revoke",
		ResourceType: "query_access_rule",
		ResourceID:   &id,
		IPAddress:    clientIP(r),
	})
	jsonOK(w, map[string]any{"ok": true})
}

func (h *QueryAccessAdminHandler) validateSubjectDBScope(ctx context.Context, subjectType model.QueryAccessSubjectType, subjectID, connectionID uint64) error {
	if subjectID == 0 || connectionID == 0 {
		return errInvalidQueryAccessRule("subject_id and connection_id are required")
	}
	switch subjectType {
	case model.QueryAccessSubjectTypeUser:
		user, err := h.users.GetByID(ctx, subjectID)
		if err != nil {
			return err
		}
		if user == nil {
			return errInvalidQueryAccessRule("user not found")
		}
		ids, err := h.users.GetEffectiveDBConnectionIDs(ctx, subjectID)
		if err != nil {
			return err
		}
		if !containsUint64(ids, connectionID) {
			return errInvalidQueryAccessRule("subject does not have DB Scope for this connection")
		}
		return nil
	case model.QueryAccessSubjectTypeAuthGroup:
		groups, err := h.auths.List(ctx)
		if err != nil {
			return err
		}
		found := false
		allPermissions := false
		for _, group := range groups {
			if group.ID == subjectID {
				found = true
				allPermissions = group.IsAllPermissions
				break
			}
		}
		if !found {
			return errInvalidQueryAccessRule("auth group not found")
		}
		if allPermissions {
			return nil
		}
		ids, err := h.auths.ListDBConnectionIDs(ctx, subjectID)
		if err != nil {
			return err
		}
		if !containsUint64(ids, connectionID) {
			return errInvalidQueryAccessRule("subject does not have DB Scope for this connection")
		}
		return nil
	default:
		return errInvalidQueryAccessRule("subject_type must be user or auth_group")
	}
}

type errInvalidQueryAccessRule string

func (e errInvalidQueryAccessRule) Error() string {
	return string(e)
}

func containsUint64(values []uint64, target uint64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
