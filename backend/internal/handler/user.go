package handler

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

type UserHandler struct {
	users    *repository.UserRepo
	auths    *repository.AuthGroupRepo
	sessions *repository.SessionRepo
	audit    *repository.AuditRepo
	dbConns  *repository.DBConnectionRepo
}

func NewUserHandler(users *repository.UserRepo, auths *repository.AuthGroupRepo, sessions *repository.SessionRepo, audit *repository.AuditRepo, dbConns *repository.DBConnectionRepo) *UserHandler {
	return &UserHandler{users: users, auths: auths, sessions: sessions, audit: audit, dbConns: dbConns}
}

func (h *UserHandler) logForbiddenUserMutation(r *http.Request, actorID, targetID uint64, action, reason string) {
	if h.audit == nil {
		return
	}
	_ = h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_security_denied",
		ResourceType: "user",
		ResourceID:   &targetID,
		Details: map[string]string{
			"action": action,
			"reason": reason,
		},
		IPAddress: clientIP(r),
	})
}

func (h *UserHandler) usersAudit(r *http.Request, actorID, targetID uint64, action string, details map[string]any) {
	if h.audit == nil {
		return
	}
	_ = h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   action,
		ResourceType: "user",
		ResourceID:   &targetID,
		Details:      details,
		IPAddress:    clientIP(r),
	})
}

// GET /users/db-connections
func (h *UserHandler) ListDBConnections(w http.ResponseWriter, r *http.Request) {
	if h.dbConns == nil {
		jsonErr(w, http.StatusInternalServerError, "db connection repo unavailable")
		return
	}
	connections, err := h.dbConns.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list user db connections failed")
		return
	}
	if connections == nil {
		connections = []model.DBConnection{}
	}
	jsonOK(w, map[string]any{"connections": connections})
}

// GET /users — Admin only
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list users failed")
		return
	}
	// Strip password hash before returning
	type userView struct {
		ID                uint64            `json:"id"`
		Username          string            `json:"username"`
		Email             string            `json:"email"`
		LarkRecipient     string            `json:"lark_recipient"`
		LarkRecipientType string            `json:"lark_recipient_type"`
		LarkUnionID       string            `json:"lark_union_id"`
		AuthGroups        []model.AuthGroup `json:"auth_groups"`
		Permissions       []string          `json:"permissions"`
		DirectPermissions []string          `json:"direct_permissions"`
		DBConnectionIDs   []uint64          `json:"db_connection_ids"`
		Protected         bool              `json:"protected"`
		IsActive          bool              `json:"is_active"`
		LastLoginAt       *string           `json:"last_login_at"`
		Online            bool              `json:"online"`
		CreatedAt         string            `json:"created_at"`
		UpdatedAt         string            `json:"updated_at"`
	}
	userIDs := make([]uint64, 0, len(users))
	for _, u := range users {
		userIDs = append(userIDs, u.ID)
	}
	sessionSummaries := map[uint64]repository.UserSessionSummary{}
	if h.sessions != nil {
		var err error
		sessionSummaries, err = h.sessions.SummariesForUsers(r.Context(), userIDs, time.Now().UTC())
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list user session summaries failed")
			return
		}
	}
	views := make([]userView, 0, len(users))
	for _, u := range users {
		groups, err := h.users.GetAuthGroups(r.Context(), u.ID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list user groups failed")
			return
		}
		if groups == nil {
			groups = []model.AuthGroup{}
		}
		permissionKeys, err := h.users.GetEffectivePermissionKeys(r.Context(), u.ID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list user permissions failed")
			return
		}
		if permissionKeys == nil {
			permissionKeys = []string{}
		}
		directPermissionKeys, err := h.users.ListDirectPermissionKeys(r.Context(), u.ID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list user direct permissions failed")
			return
		}
		if directPermissionKeys == nil {
			directPermissionKeys = []string{}
		}
		dbConnectionIDs, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), u.ID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list user db scope failed")
			return
		}
		if dbConnectionIDs == nil {
			dbConnectionIDs = []uint64{}
		}

		sessionSummary := sessionSummaries[u.ID]
		var lastLoginAt *string
		if sessionSummary.LastLoginAt != nil {
			formatted := sessionSummary.LastLoginAt.UTC().Format("2006-01-02T15:04:05Z")
			lastLoginAt = &formatted
		}

		views = append(views, userView{
			ID:                u.ID,
			Username:          u.Username,
			Email:             u.Email,
			LarkRecipient:     u.LarkRecipient,
			LarkRecipientType: normalizeLarkRecipientType(u.LarkRecipientType),
			LarkUnionID:       u.LarkUnionID,
			AuthGroups:        groups,
			Permissions:       permissionKeys,
			DirectPermissions: directPermissionKeys,
			DBConnectionIDs:   dbConnectionIDs,
			Protected:         u.IsProtected,
			IsActive:          u.IsActive,
			LastLoginAt:       lastLoginAt,
			Online:            sessionSummary.ActiveSessionCount > 0,
			CreatedAt:         u.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:         u.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	jsonOK(w, map[string]any{"users": views})
}

// POST /users — Admin only
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username          string `json:"username"`
		Email             string `json:"email"`
		LarkRecipient     string `json:"lark_recipient"`
		LarkRecipientType string `json:"lark_recipient_type"`
		LarkUnionID       string `json:"lark_union_id"`
		Password          string `json:"password"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	if req.Username == "" || req.Email == "" || req.Password == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "username, email, and password are required")
		return
	}
	if err := validatePassword(req.Password); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	existingUsername, err := h.users.GetByUsername(r.Context(), req.Username)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "check username failed")
		return
	}
	if existingUsername != nil {
		jsonErr(w, http.StatusConflict, "username already exists")
		return
	}
	existingEmail, err := h.users.GetByEmail(r.Context(), req.Email)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "check email failed")
		return
	}
	if existingEmail != nil {
		jsonErr(w, http.StatusConflict, "email already exists")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	larkRecipientType := normalizeLarkRecipientType(req.LarkRecipientType)
	larkUnionID := strings.TrimSpace(req.LarkUnionID)
	user, err := h.users.Create(r.Context(), req.Username, req.Email, strings.TrimSpace(req.LarkRecipient), larkRecipientType, larkUnionID, string(hash), false)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create user failed")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_create",
		ResourceType: "user",
		ResourceID:   &user.ID,
		Details:      map[string]string{"username": user.Username, "email": user.Email, "lark_recipient": user.LarkRecipient, "lark_recipient_type": larkRecipientType, "lark_union_id_present": strconv.FormatBool(larkUnionID != "")},
		IPAddress:    clientIP(r),
	})

	jsonCreated(w, map[string]any{"id": user.ID, "username": user.Username, "email": user.Email, "lark_recipient": user.LarkRecipient, "lark_recipient_type": larkRecipientType, "lark_union_id": larkUnionID})
}

// GET /users/{id} — Admin only
func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}

	memberships, err := h.users.ListMemberships(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list memberships failed")
		return
	}
	if memberships == nil {
		memberships = []model.Membership{}
	}
	permissions, err := h.users.GetEffectivePermissionKeys(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list user permissions failed")
		return
	}
	if permissions == nil {
		permissions = []string{}
	}
	dbConnectionIDs, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list user db scope failed")
		return
	}
	if dbConnectionIDs == nil {
		dbConnectionIDs = []uint64{}
	}
	directPermissions, err := h.users.ListDirectPermissionKeys(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list direct user permissions failed")
		return
	}
	if directPermissions == nil {
		directPermissions = []string{}
	}
	directDBConnectionIDs, err := h.users.ListDirectDBConnectionIDs(r.Context(), id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list direct user db scope failed")
		return
	}
	if directDBConnectionIDs == nil {
		directDBConnectionIDs = []uint64{}
	}
	jsonOK(w, map[string]any{
		"id":                       user.ID,
		"username":                 user.Username,
		"email":                    user.Email,
		"lark_recipient":           user.LarkRecipient,
		"lark_recipient_type":      normalizeLarkRecipientType(user.LarkRecipientType),
		"lark_union_id":            user.LarkUnionID,
		"protected":                user.IsProtected,
		"is_active":                user.IsActive,
		"created_at":               user.CreatedAt,
		"updated_at":               user.UpdatedAt,
		"memberships":              memberships,
		"permissions":              permissions,
		"db_connection_ids":        dbConnectionIDs,
		"direct_permissions":       directPermissions,
		"direct_db_connection_ids": directDBConnectionIDs,
	})
}

// GET /users/{id}/sessions — Admin only
func (h *UserHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if h.sessions == nil {
		jsonErr(w, http.StatusInternalServerError, "session repo unavailable")
		return
	}
	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	sessions, err := h.sessions.ListForUserLimit(r.Context(), id, sessionListLimit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load sessions failed")
		return
	}
	if sessions == nil {
		sessions = []model.Session{}
	}
	jsonOK(w, map[string]any{"sessions": sessions})
}

// DELETE /users/{id}/sessions/{sessionID} — Admin only
func (h *UserHandler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	sessionID, err := strconv.ParseUint(chi.URLParam(r, "sessionID"), 10, 64)
	if err != nil || sessionID == 0 {
		jsonErr(w, http.StatusBadRequest, "invalid session id")
		return
	}
	if h.sessions == nil {
		jsonErr(w, http.StatusInternalServerError, "session repo unavailable")
		return
	}
	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "revoke_session", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	revoked, err := h.sessions.RevokeByIDForUser(r.Context(), sessionID, id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "revoke session failed")
		return
	}
	if !revoked {
		jsonErr(w, http.StatusNotFound, "session not found")
		return
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_session_revoke",
		ResourceType: "user",
		ResourceID:   &id,
		Details: map[string]any{
			"session_id": sessionID,
			"username":   user.Username,
		},
		IPAddress: clientIP(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /users/{id}/sessions — Admin only
func (h *UserHandler) RevokeSessions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if h.sessions == nil {
		jsonErr(w, http.StatusInternalServerError, "session repo unavailable")
		return
	}
	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "revoke_sessions", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if err := h.sessions.RevokeAllForUser(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "revoke sessions failed")
		return
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_session_revoke_all",
		ResourceType: "user",
		ResourceID:   &id,
		Details: map[string]any{
			"username": user.Username,
		},
		IPAddress: clientIP(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

// POST /users/{id}/mfa/reset — Admin only
func (h *UserHandler) ResetMFA(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if h.sessions == nil {
		jsonErr(w, http.StatusInternalServerError, "session repo unavailable")
		return
	}
	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "reset_mfa", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if err := h.users.ResetMFA(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "reset mfa failed")
		return
	}
	if err := h.sessions.RevokeAllForUser(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "revoke sessions failed")
		return
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_mfa_reset",
		ResourceType: "user",
		ResourceID:   &id,
		Details: map[string]any{
			"username": user.Username,
		},
		IPAddress: clientIP(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

// POST /users/{id}/memberships — Admin only
// Body: { "auth_group": "dba", "expires_at": "2026-12-31T00:00:00Z" }
func (h *UserHandler) AddMembership(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var req struct {
		AuthGroup string  `json:"auth_group"`
		ExpiresAt *string `json:"expires_at"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	group := model.AuthGroup(strings.TrimSpace(req.AuthGroup))
	if h.auths == nil {
		jsonErr(w, http.StatusInternalServerError, "auth group repo unavailable")
		return
	}
	authGroup, err := h.auths.GetByKey(r.Context(), string(group))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load auth group failed")
		return
	}
	if authGroup == nil {
		jsonErr(w, http.StatusUnprocessableEntity, "invalid auth_group")
		return
	}

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "add_membership", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if err := requireAuthGroupContentsGrantAllowed(r.Context(), h.users, h.auths, actorID, authGroup); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "add_membership", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, "expires_at must be RFC3339 format (e.g. 2026-12-31T00:00:00Z)")
			return
		}
		expiresAt = &t
	}

	if err := h.users.AddMembership(r.Context(), id, group, &actorID, expiresAt); err != nil {
		jsonErr(w, http.StatusInternalServerError, "add membership failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_membership_add",
		ResourceType: "user",
		ResourceID:   &id,
		Details:      map[string]string{"auth_group": req.AuthGroup},
		IPAddress:    clientIP(r),
	})

	w.WriteHeader(http.StatusNoContent)
}

// PATCH /users/{id} — Admin only
// Body: { "username": "...", "email": "...", "password": "...", "is_active": true }  — all optional
func (h *UserHandler) Patch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}

	var req struct {
		Username              *string   `json:"username"`
		Email                 *string   `json:"email"`
		LarkRecipient         *string   `json:"lark_recipient"`
		LarkRecipientType     *string   `json:"lark_recipient_type"`
		LarkUnionID           *string   `json:"lark_union_id"`
		Password              *string   `json:"password"`
		IsActive              *bool     `json:"is_active"`
		AuthGroups            *[]string `json:"auth_groups"`
		DirectPermissions     *[]string `json:"direct_permissions"`
		DirectDBConnectionIDs *[]uint64 `json:"direct_db_connection_ids"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "patch_user", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}

	username := user.Username
	if req.Username != nil && *req.Username != "" {
		username = *req.Username
	}
	email := user.Email
	if req.Email != nil && *req.Email != "" {
		email = *req.Email
	}
	larkRecipient := user.LarkRecipient
	if req.LarkRecipient != nil {
		larkRecipient = strings.TrimSpace(*req.LarkRecipient)
	}
	larkRecipientType := normalizeLarkRecipientType(user.LarkRecipientType)
	if req.LarkRecipientType != nil {
		larkRecipientType = normalizeLarkRecipientType(*req.LarkRecipientType)
	}
	larkUnionID := user.LarkUnionID
	if req.LarkUnionID != nil {
		larkUnionID = strings.TrimSpace(*req.LarkUnionID)
	}

	if err := h.users.Update(r.Context(), id, username, email, larkRecipient, larkRecipientType, larkUnionID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update user failed")
		return
	}

	if req.Password != nil && *req.Password != "" {
		if err := validatePassword(*req.Password); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := h.users.UpdatePassword(r.Context(), id, string(hash)); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update password failed")
			return
		}
	}

	isActive := user.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
		if err := h.users.UpdateActive(r.Context(), id, isActive); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update user status failed")
			return
		}
		if !isActive {
			if err := h.sessions.RevokeAllForUser(r.Context(), id); err != nil {
				jsonErr(w, http.StatusInternalServerError, "revoke sessions failed")
				return
			}
		}
	}

	if req.AuthGroups != nil {
		if h.auths == nil {
			jsonErr(w, http.StatusInternalServerError, "auth group repo unavailable")
			return
		}
		currentMemberships, err := h.users.ListMemberships(r.Context(), id)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list memberships failed")
			return
		}
		currentGroups := make(map[string]bool, len(currentMemberships))
		for _, membership := range currentMemberships {
			currentGroups[string(membership.AuthGroup)] = true
		}
		nextGroupKeys := map[string]bool{}
		nextGroups := make([]model.AuthGroup, 0, len(*req.AuthGroups))
		seen := map[string]bool{}
		for _, groupKey := range *req.AuthGroups {
			normalized := strings.TrimSpace(groupKey)
			if normalized == "" || seen[normalized] {
				continue
			}
			group, err := h.auths.GetByKey(r.Context(), normalized)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, "load auth group failed")
				return
			}
			if group == nil {
				jsonErr(w, http.StatusUnprocessableEntity, "invalid auth_group")
				return
			}
			if err := requireAuthGroupContentsGrantAllowed(r.Context(), h.users, h.auths, actorID, group); err != nil {
				h.logForbiddenUserMutation(r, actorID, id, "replace_memberships", err.Error())
				jsonErr(w, http.StatusForbidden, err.Error())
				return
			}
			seen[normalized] = true
			nextGroupKeys[normalized] = true
			nextGroups = append(nextGroups, model.AuthGroup(normalized))
		}
		for currentGroup := range currentGroups {
			if nextGroupKeys[currentGroup] {
				continue
			}
			group, err := h.auths.GetByKey(r.Context(), currentGroup)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, "load auth group failed")
				return
			}
			if err := requireAuthGroupMutationAllowed(r.Context(), h.users, actorID, group); err != nil {
				h.logForbiddenUserMutation(r, actorID, id, "replace_memberships", err.Error())
				jsonErr(w, http.StatusForbidden, err.Error())
				return
			}
		}
		if err := h.users.ReplaceMemberships(r.Context(), id, nextGroups, &actorID); err != nil {
			jsonErr(w, http.StatusInternalServerError, "replace memberships failed")
			return
		}
	}
	if req.DirectPermissions != nil {
		nextPermissions := make([]string, 0, len(*req.DirectPermissions))
		seen := map[string]bool{}
		for _, permissionKey := range *req.DirectPermissions {
			trimmed := strings.TrimSpace(permissionKey)
			if trimmed == "" || seen[trimmed] {
				continue
			}
			if err := requirePermissionGrantAllowed(r.Context(), h.users, actorID, trimmed); err != nil {
				h.logForbiddenUserMutation(r, actorID, id, "replace_direct_permissions", err.Error())
				jsonErr(w, http.StatusForbidden, err.Error())
				return
			}
			seen[trimmed] = true
			nextPermissions = append(nextPermissions, trimmed)
		}
		if err := h.users.ReplaceDirectPermissionKeys(r.Context(), id, nextPermissions, &actorID); err != nil {
			if err == sql.ErrNoRows {
				jsonErr(w, http.StatusUnprocessableEntity, "invalid permission_key")
				return
			}
			jsonErr(w, http.StatusInternalServerError, "replace direct permissions failed")
			return
		}
	}
	if req.DirectDBConnectionIDs != nil {
		nextConnectionIDs := make([]uint64, 0, len(*req.DirectDBConnectionIDs))
		seen := map[uint64]bool{}
		for _, connectionID := range *req.DirectDBConnectionIDs {
			if connectionID == 0 || seen[connectionID] {
				continue
			}
			if err := requireDBConnectionGrantAllowed(r.Context(), h.users, actorID, connectionID); err != nil {
				h.logForbiddenUserMutation(r, actorID, id, "replace_direct_db_connections", err.Error())
				jsonErr(w, http.StatusForbidden, err.Error())
				return
			}
			seen[connectionID] = true
			nextConnectionIDs = append(nextConnectionIDs, connectionID)
		}
		if err := h.users.ReplaceDirectDBConnectionIDs(r.Context(), id, nextConnectionIDs, &actorID); err != nil {
			jsonErr(w, http.StatusInternalServerError, "replace direct db connections failed")
			return
		}
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_update",
		ResourceType: "user",
		ResourceID:   &id,
		Details:      map[string]string{"username": username, "email": email, "lark_recipient": larkRecipient, "lark_recipient_type": larkRecipientType, "lark_union_id_present": strconv.FormatBool(larkUnionID != ""), "is_active": strconv.FormatBool(isActive)},
		IPAddress:    clientIP(r),
	})

	jsonOK(w, map[string]any{"id": id, "username": username, "email": email, "lark_recipient": larkRecipient, "lark_recipient_type": larkRecipientType, "lark_union_id": larkUnionID, "is_active": isActive})
}

func normalizeLarkRecipientType(value string) string {
	if strings.TrimSpace(value) == "union_id" {
		return "union_id"
	}
	return "open_id"
}

// DELETE /users/{id} — Admin only
func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	if user.IsProtected {
		jsonErr(w, http.StatusConflict, "protected system user cannot be deleted")
		return
	}

	if err := h.users.Delete(r.Context(), id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "delete user failed")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_delete",
		ResourceType: "user",
		ResourceID:   &id,
		Details:      map[string]string{"username": user.Username, "email": user.Email},
		IPAddress:    clientIP(r),
	})

	w.WriteHeader(http.StatusNoContent)
}

// DELETE /users/{id}/memberships/{group} — Admin only
func (h *UserHandler) RemoveMembership(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	group := model.AuthGroup(strings.TrimSpace(chi.URLParam(r, "group")))
	if h.auths == nil {
		jsonErr(w, http.StatusInternalServerError, "auth group repo unavailable")
		return
	}
	authGroup, err := h.auths.GetByKey(r.Context(), string(group))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load auth group failed")
		return
	}
	if authGroup == nil {
		jsonErr(w, http.StatusUnprocessableEntity, "invalid auth_group")
		return
	}

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "remove_membership", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if err := requireAuthGroupMutationAllowed(r.Context(), h.users, actorID, authGroup); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "remove_membership", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}

	if err := h.users.RemoveMembership(r.Context(), id, group); err != nil {
		jsonErr(w, http.StatusInternalServerError, "remove membership failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_membership_remove",
		ResourceType: "user",
		ResourceID:   &id,
		Details:      map[string]string{"auth_group": string(group)},
		IPAddress:    clientIP(r),
	})

	w.WriteHeader(http.StatusNoContent)
}

// POST /users/{id}/permissions
func (h *UserHandler) AddDirectPermission(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "add_direct_permission", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}

	var req struct {
		PermissionKey string `json:"permission_key"`
	}
	if err := bindJSON(r, &req); err != nil || strings.TrimSpace(req.PermissionKey) == "" {
		jsonErr(w, http.StatusBadRequest, "permission_key is required")
		return
	}

	permissionKey := strings.TrimSpace(req.PermissionKey)
	if err := requirePermissionGrantAllowed(r.Context(), h.users, actorID, permissionKey); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "add_direct_permission", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}

	if err := h.users.AddDirectPermission(r.Context(), id, permissionKey, &actorID); err != nil {
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusUnprocessableEntity, "invalid permission_key")
			return
		}
		jsonErr(w, http.StatusInternalServerError, "add direct permission failed")
		return
	}

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_permission_add",
		ResourceType: "user",
		ResourceID:   &id,
		Details:      map[string]string{"permission_key": permissionKey},
		IPAddress:    clientIP(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /users/{id}/permissions/{permissionKey}
func (h *UserHandler) RemoveDirectPermission(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "remove_direct_permission", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	permissionKey := strings.TrimSpace(chi.URLParam(r, "permissionKey"))
	if permissionKey == "" {
		jsonErr(w, http.StatusBadRequest, "invalid permissionKey")
		return
	}
	if err := h.users.RemoveDirectPermission(r.Context(), id, permissionKey); err != nil {
		jsonErr(w, http.StatusInternalServerError, "remove direct permission failed")
		return
	}
	h.usersAudit(r, actorID, id, "user_permission_remove", map[string]any{"permission_key": permissionKey})
	w.WriteHeader(http.StatusNoContent)
}

// POST /users/{id}/db-connections
func (h *UserHandler) AddDirectDBConnection(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "add_direct_db_connection", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	var req struct {
		DBConnectionID uint64 `json:"db_connection_id"`
	}
	if err := bindJSON(r, &req); err != nil || req.DBConnectionID == 0 {
		jsonErr(w, http.StatusBadRequest, "db_connection_id is required")
		return
	}
	if err := requireDBConnectionGrantAllowed(r.Context(), h.users, actorID, req.DBConnectionID); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "add_direct_db_connection", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if err := h.users.AddDirectDBConnection(r.Context(), id, req.DBConnectionID, &actorID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "add direct db connection failed")
		return
	}
	h.usersAudit(r, actorID, id, "user_db_connection_add", map[string]any{"db_connection_id": req.DBConnectionID})
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /users/{id}/db-connections/{connID}
func (h *UserHandler) RemoveDirectDBConnection(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
		h.logForbiddenUserMutation(r, actorID, id, "remove_direct_db_connection", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	connID, err := strconv.ParseUint(chi.URLParam(r, "connID"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid connID")
		return
	}
	if err := h.users.RemoveDirectDBConnection(r.Context(), id, connID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "remove direct db connection failed")
		return
	}
	h.usersAudit(r, actorID, id, "user_db_connection_remove", map[string]any{"db_connection_id": connID})
	w.WriteHeader(http.StatusNoContent)
}
