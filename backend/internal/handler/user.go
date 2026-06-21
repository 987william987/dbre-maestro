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
		ID              uint64            `json:"id"`
		Username        string            `json:"username"`
		Email           string            `json:"email"`
		LarkRecipient   string            `json:"lark_recipient"`
		AuthGroups      []model.AuthGroup `json:"auth_groups"`
		Permissions     []string          `json:"permissions"`
		DBConnectionIDs []uint64          `json:"db_connection_ids"`
		Protected       bool              `json:"protected"`
		IsActive        bool              `json:"is_active"`
		CreatedAt       string            `json:"created_at"`
		UpdatedAt       string            `json:"updated_at"`
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
		dbConnectionIDs, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), u.ID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list user db scope failed")
			return
		}
		if dbConnectionIDs == nil {
			dbConnectionIDs = []uint64{}
		}

		views = append(views, userView{
			ID:              u.ID,
			Username:        u.Username,
			Email:           u.Email,
			LarkRecipient:   u.LarkRecipient,
			AuthGroups:      groups,
			Permissions:     permissionKeys,
			DBConnectionIDs: dbConnectionIDs,
			Protected:       u.IsProtected,
			IsActive:        u.IsActive,
			CreatedAt:       u.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:       u.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	jsonOK(w, map[string]any{"users": views})
}

// POST /users — Admin only
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username      string `json:"username"`
		Email         string `json:"email"`
		LarkRecipient string `json:"lark_recipient"`
		Password      string `json:"password"`
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

	user, err := h.users.Create(r.Context(), req.Username, req.Email, strings.TrimSpace(req.LarkRecipient), string(hash), false)
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
		Details:      map[string]string{"username": user.Username, "email": user.Email, "lark_recipient": user.LarkRecipient},
		IPAddress:    clientIP(r),
	})

	jsonCreated(w, map[string]any{"id": user.ID, "username": user.Username, "email": user.Email, "lark_recipient": user.LarkRecipient})
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
	if user.IsProtected && group != model.AuthGroupAdmin {
		jsonErr(w, http.StatusConflict, protectedUserErrorMessage())
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

	actorID := middleware.UserIDFromCtx(r.Context())
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
	if user.IsProtected {
		onlyPasswordChange := req.Password != nil && strings.TrimSpace(*req.Password) != "" &&
			req.Username == nil && req.Email == nil && req.LarkRecipient == nil && req.IsActive == nil &&
			req.AuthGroups == nil && req.DirectPermissions == nil && req.DirectDBConnectionIDs == nil
		if !onlyPasswordChange {
			jsonErr(w, http.StatusConflict, protectedUserErrorMessage())
			return
		}
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

	if err := h.users.Update(r.Context(), id, username, email, larkRecipient); err != nil {
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

	actorID := middleware.UserIDFromCtx(r.Context())
	if req.AuthGroups != nil {
		if h.auths == nil {
			jsonErr(w, http.StatusInternalServerError, "auth group repo unavailable")
			return
		}
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
			seen[normalized] = true
			nextGroups = append(nextGroups, model.AuthGroup(normalized))
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
		Details:      map[string]string{"username": username, "email": email, "lark_recipient": larkRecipient, "is_active": strconv.FormatBool(isActive)},
		IPAddress:    clientIP(r),
	})

	jsonOK(w, map[string]any{"id": id, "username": username, "email": email, "lark_recipient": larkRecipient, "is_active": isActive})
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
	if user.IsProtected {
		jsonErr(w, http.StatusConflict, protectedUserErrorMessage())
		return
	}

	if err := h.users.RemoveMembership(r.Context(), id, group); err != nil {
		jsonErr(w, http.StatusInternalServerError, "remove membership failed")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
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
	if user.IsProtected {
		jsonErr(w, http.StatusConflict, protectedUserErrorMessage())
		return
	}

	var req struct {
		PermissionKey string `json:"permission_key"`
	}
	if err := bindJSON(r, &req); err != nil || strings.TrimSpace(req.PermissionKey) == "" {
		jsonErr(w, http.StatusBadRequest, "permission_key is required")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	if err := h.users.AddDirectPermission(r.Context(), id, strings.TrimSpace(req.PermissionKey), &actorID); err != nil {
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
		Details:      map[string]string{"permission_key": strings.TrimSpace(req.PermissionKey)},
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
	if user.IsProtected {
		jsonErr(w, http.StatusConflict, protectedUserErrorMessage())
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
	if user.IsProtected {
		jsonErr(w, http.StatusConflict, protectedUserErrorMessage())
		return
	}
	var req struct {
		DBConnectionID uint64 `json:"db_connection_id"`
	}
	if err := bindJSON(r, &req); err != nil || req.DBConnectionID == 0 {
		jsonErr(w, http.StatusBadRequest, "db_connection_id is required")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := h.users.AddDirectDBConnection(r.Context(), id, req.DBConnectionID, &actorID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "add direct db connection failed")
		return
	}
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
	if user.IsProtected {
		jsonErr(w, http.StatusConflict, protectedUserErrorMessage())
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
	w.WriteHeader(http.StatusNoContent)
}
