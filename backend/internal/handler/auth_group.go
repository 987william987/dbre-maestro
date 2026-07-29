package handler

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type AuthGroupHandler struct {
	authGroups *repository.AuthGroupRepo
	users      *repository.UserRepo
	audit      *repository.AuditRepo
}

func NewAuthGroupHandler(authGroups *repository.AuthGroupRepo, users *repository.UserRepo, audit *repository.AuditRepo) *AuthGroupHandler {
	return &AuthGroupHandler{authGroups: authGroups, users: users, audit: audit}
}

func (h *AuthGroupHandler) logForbiddenAuthGroupMutation(r *http.Request, actorID, groupID uint64, action, reason string) {
	if h.audit == nil {
		return
	}
	_ = h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "auth_group_security_denied",
		ResourceType: "auth_group",
		ResourceID:   &groupID,
		Details: map[string]string{
			"action": action,
			"reason": reason,
		},
		IPAddress: clientIP(r),
	})
}

func (h *AuthGroupHandler) List(w http.ResponseWriter, r *http.Request) {
	type authGroupView struct {
		ID                uint64   `json:"id"`
		Name              string   `json:"name"`
		Label             string   `json:"label"`
		Description       string   `json:"description"`
		SystemDefined     bool     `json:"system_defined"`
		Protected         bool     `json:"protected"`
		AllPermissions    bool     `json:"all_permissions"`
		UserCount         int      `json:"user_count"`
		PermissionCount   int      `json:"permission_count"`
		DBConnectionCount int      `json:"db_connection_count"`
		Permissions       []string `json:"permissions"`
		DBConnectionIDs   []uint64 `json:"db_connection_ids"`
		CreatedAt         string   `json:"created_at"`
		UpdatedAt         string   `json:"updated_at"`
	}

	groups, err := h.authGroups.List(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list auth groups failed")
		return
	}

	views := make([]authGroupView, 0, len(groups))
	for _, item := range groups {
		users, err := h.users.ListUsersByAuthGroup(r.Context(), model.AuthGroup(item.GroupKey))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list auth group users failed")
			return
		}
		permissions, err := h.authGroups.ListPermissionKeys(r.Context(), item.ID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list auth group permissions failed")
			return
		}
		if permissions == nil {
			permissions = []string{}
		}
		dbConnectionIDs, err := h.authGroups.ListDBConnectionIDs(r.Context(), item.ID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list auth group db scope failed")
			return
		}
		if dbConnectionIDs == nil {
			dbConnectionIDs = []uint64{}
		}

		views = append(views, authGroupView{
			ID:                item.ID,
			Name:              item.GroupKey,
			Label:             item.Name,
			Description:       item.Description,
			SystemDefined:     item.IsSystem,
			Protected:         item.IsProtected,
			AllPermissions:    item.IsAllPermissions,
			UserCount:         len(users),
			PermissionCount:   len(permissions),
			DBConnectionCount: len(dbConnectionIDs),
			Permissions:       permissions,
			DBConnectionIDs:   dbConnectionIDs,
			CreatedAt:         item.CreatedAt,
			UpdatedAt:         item.UpdatedAt,
		})
	}

	jsonOK(w, map[string]any{"auth_groups": views})
}

func (h *AuthGroupHandler) Get(w http.ResponseWriter, r *http.Request) {
	groupKey := chi.URLParam(r, "group")
	group, err := h.authGroups.GetByKey(r.Context(), groupKey)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load auth group failed")
		return
	}
	if group == nil {
		jsonErr(w, http.StatusNotFound, "auth group not found")
		return
	}

	users, err := h.users.ListUsersByAuthGroup(r.Context(), model.AuthGroup(group.GroupKey))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list auth group users failed")
		return
	}
	if users == nil {
		users = []model.User{}
	}

	permissions, err := h.authGroups.ListPermissionKeys(r.Context(), group.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list auth group permissions failed")
		return
	}
	if permissions == nil {
		permissions = []string{}
	}
	dbConnectionIDs, err := h.authGroups.ListDBConnectionIDs(r.Context(), group.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list auth group db scope failed")
		return
	}
	if dbConnectionIDs == nil {
		dbConnectionIDs = []uint64{}
	}

	type userView struct {
		ID          uint64 `json:"id"`
		Username    string `json:"username"`
		Email       string `json:"email"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
		IsProtected bool   `json:"protected"`
	}

	userViews := make([]userView, 0, len(users))
	for _, user := range users {
		userViews = append(userViews, userView{
			ID:          user.ID,
			Username:    user.Username,
			Email:       user.Email,
			CreatedAt:   user.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:   user.UpdatedAt.Format("2006-01-02T15:04:05Z"),
			IsProtected: user.IsProtected,
		})
	}

	jsonOK(w, map[string]any{
		"id":                group.ID,
		"name":              group.GroupKey,
		"label":             group.Name,
		"description":       group.Description,
		"system_defined":    group.IsSystem,
		"protected":         group.IsProtected,
		"all_permissions":   group.IsAllPermissions,
		"created_at":        group.CreatedAt,
		"updated_at":        group.UpdatedAt,
		"users":             userViews,
		"permissions":       permissions,
		"db_connection_ids": dbConnectionIDs,
	})
}

func (h *AuthGroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string   `json:"name"`
		Description     string   `json:"description"`
		UserIDs         []uint64 `json:"user_ids"`
		Permissions     []string `json:"permissions"`
		DBConnectionIDs []uint64 `json:"db_connection_ids"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	groupKey := repository.GenerateAuthGroupKey(req.Name)
	if err := repository.ValidateAuthGroupKey(groupKey); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "name is required")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	for _, permissionKey := range req.Permissions {
		trimmed := strings.TrimSpace(permissionKey)
		if trimmed == "" {
			continue
		}
		if err := requirePermissionGrantAllowed(r.Context(), h.users, actorID, trimmed); err != nil {
			h.logForbiddenAuthGroupMutation(r, actorID, 0, "create_auth_group", err.Error())
			jsonErr(w, http.StatusForbidden, err.Error())
			return
		}
	}
	for _, dbConnectionID := range req.DBConnectionIDs {
		if dbConnectionID == 0 {
			continue
		}
		if err := requireDBConnectionGrantAllowed(r.Context(), h.users, actorID, dbConnectionID); err != nil {
			h.logForbiddenAuthGroupMutation(r, actorID, 0, "create_auth_group", err.Error())
			jsonErr(w, http.StatusForbidden, err.Error())
			return
		}
	}

	group, err := h.authGroups.Create(r.Context(), groupKey, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), false, false)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create auth group failed")
		return
	}

	for _, userID := range req.UserIDs {
		if err := h.users.AddMembership(r.Context(), userID, model.AuthGroup(group.GroupKey), &actorID, nil); err != nil {
			jsonErr(w, http.StatusInternalServerError, "bind auth group users failed")
			return
		}
	}
	if err := h.authGroups.ReplacePermissionKeys(r.Context(), group.ID, req.Permissions); err != nil {
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusUnprocessableEntity, "invalid permission_key")
			return
		}
		jsonErr(w, http.StatusInternalServerError, "bind auth group permissions failed")
		return
	}
	if err := h.authGroups.ReplaceDBConnectionIDs(r.Context(), group.ID, req.DBConnectionIDs); err != nil {
		jsonErr(w, http.StatusInternalServerError, "bind auth group db scope failed")
		return
	}
	h.usersAudit(r, actorID, "auth_group_create", group.ID, map[string]string{"group_key": group.GroupKey})
	created, err := h.authGroups.GetByKey(r.Context(), group.GroupKey)
	if err != nil || created == nil {
		jsonErr(w, http.StatusInternalServerError, "reload auth group failed")
		return
	}
	jsonCreated(w, created)
}

func (h *AuthGroupHandler) Patch(w http.ResponseWriter, r *http.Request) {
	groupKey := chi.URLParam(r, "group")
	group, err := h.authGroups.GetByKey(r.Context(), groupKey)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load auth group failed")
		return
	}
	if group == nil {
		jsonErr(w, http.StatusNotFound, "auth group not found")
		return
	}

	var req struct {
		Name            *string   `json:"name"`
		Description     *string   `json:"description"`
		UserIDs         *[]uint64 `json:"user_ids"`
		Permissions     *[]string `json:"permissions"`
		DBConnectionIDs *[]uint64 `json:"db_connection_ids"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == nil && req.Description == nil && req.UserIDs == nil && req.Permissions == nil && req.DBConnectionIDs == nil {
		jsonErr(w, http.StatusUnprocessableEntity, "at least one mutable field is required")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireAuthGroupMutationAllowed(r.Context(), h.users, actorID, group); err != nil {
		h.logForbiddenAuthGroupMutation(r, actorID, group.ID, "patch_auth_group", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if req.UserIDs != nil {
		if err := requireAuthGroupContentsGrantAllowed(r.Context(), h.users, h.authGroups, actorID, group); err != nil {
			h.logForbiddenAuthGroupMutation(r, actorID, group.ID, "replace_auth_group_users", err.Error())
			jsonErr(w, http.StatusForbidden, err.Error())
			return
		}
		currentUsers, err := h.users.ListUsersByAuthGroup(r.Context(), model.AuthGroup(group.GroupKey))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list auth group users failed")
			return
		}
		currentUserIDs := map[uint64]model.User{}
		for _, user := range currentUsers {
			currentUserIDs[user.ID] = user
		}
		nextUserIDs := map[uint64]bool{}
		for _, userID := range *req.UserIDs {
			if userID == 0 {
				continue
			}
			nextUserIDs[userID] = true
			if _, ok := currentUserIDs[userID]; ok {
				continue
			}
			user, err := h.users.GetByID(r.Context(), userID)
			if err != nil || user == nil {
				jsonErr(w, http.StatusNotFound, "user not found")
				return
			}
			if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, user); err != nil {
				h.logForbiddenAuthGroupMutation(r, actorID, group.ID, "replace_auth_group_users", err.Error())
				jsonErr(w, http.StatusForbidden, err.Error())
				return
			}
		}
		for userID, user := range currentUserIDs {
			if nextUserIDs[userID] {
				continue
			}
			if err := requireProtectedUserAdmin(r.Context(), h.users, actorID, &user); err != nil {
				h.logForbiddenAuthGroupMutation(r, actorID, group.ID, "replace_auth_group_users", err.Error())
				jsonErr(w, http.StatusForbidden, err.Error())
				return
			}
		}
	}
	if req.Permissions != nil {
		for _, permissionKey := range *req.Permissions {
			trimmed := strings.TrimSpace(permissionKey)
			if trimmed == "" {
				continue
			}
			if err := requirePermissionGrantAllowed(r.Context(), h.users, actorID, trimmed); err != nil {
				h.logForbiddenAuthGroupMutation(r, actorID, group.ID, "replace_auth_group_permissions", err.Error())
				jsonErr(w, http.StatusForbidden, err.Error())
				return
			}
		}
	}
	if req.DBConnectionIDs != nil {
		for _, dbConnectionID := range *req.DBConnectionIDs {
			if dbConnectionID == 0 {
				continue
			}
			if err := requireDBConnectionGrantAllowed(r.Context(), h.users, actorID, dbConnectionID); err != nil {
				h.logForbiddenAuthGroupMutation(r, actorID, group.ID, "replace_auth_group_db_connections", err.Error())
				jsonErr(w, http.StatusForbidden, err.Error())
				return
			}
		}
	}

	nextGroupKey := group.GroupKey
	nextName := group.Name
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			jsonErr(w, http.StatusUnprocessableEntity, "name is required")
			return
		}
		nextName = strings.TrimSpace(*req.Name)
	}
	nextDescription := group.Description
	if req.Description != nil {
		nextDescription = strings.TrimSpace(*req.Description)
	}

	if err := h.authGroups.Update(r.Context(), group.ID, nextGroupKey, nextName, nextDescription); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update auth group failed")
		return
	}
	if req.UserIDs != nil {
		currentUsers, err := h.users.ListUsersByAuthGroup(r.Context(), model.AuthGroup(group.GroupKey))
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list auth group users failed")
			return
		}
		currentUserIDs := map[uint64]bool{}
		for _, user := range currentUsers {
			currentUserIDs[user.ID] = true
		}
		nextUserIDs := map[uint64]bool{}
		for _, userID := range *req.UserIDs {
			if userID == 0 || nextUserIDs[userID] {
				continue
			}
			nextUserIDs[userID] = true
			if !currentUserIDs[userID] {
				if err := h.users.AddMembership(r.Context(), userID, model.AuthGroup(group.GroupKey), &actorID, nil); err != nil {
					jsonErr(w, http.StatusInternalServerError, "bind auth group users failed")
					return
				}
			}
		}
		for userID := range currentUserIDs {
			if nextUserIDs[userID] {
				continue
			}
			if err := h.users.RemoveMembership(r.Context(), userID, model.AuthGroup(group.GroupKey)); err != nil {
				jsonErr(w, http.StatusInternalServerError, "unbind auth group users failed")
				return
			}
		}
	}
	if req.Permissions != nil {
		if err := h.authGroups.ReplacePermissionKeys(r.Context(), group.ID, *req.Permissions); err != nil {
			if err == sql.ErrNoRows {
				jsonErr(w, http.StatusUnprocessableEntity, "invalid permission_key")
				return
			}
			jsonErr(w, http.StatusInternalServerError, "replace auth group permissions failed")
			return
		}
	}
	if req.DBConnectionIDs != nil {
		if err := h.authGroups.ReplaceDBConnectionIDs(r.Context(), group.ID, *req.DBConnectionIDs); err != nil {
			jsonErr(w, http.StatusInternalServerError, "replace auth group db scope failed")
			return
		}
	}
	updated, err := h.authGroups.GetByKey(r.Context(), nextGroupKey)
	if err != nil || updated == nil {
		jsonErr(w, http.StatusInternalServerError, "reload auth group failed")
		return
	}

	h.usersAudit(r, actorID, "auth_group_update", group.ID, map[string]string{"group_key": updated.GroupKey})
	jsonOK(w, updated)
}

func (h *AuthGroupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	groupKey := chi.URLParam(r, "group")
	group, err := h.authGroups.GetByKey(r.Context(), groupKey)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load auth group failed")
		return
	}
	if group == nil {
		jsonErr(w, http.StatusNotFound, "auth group not found")
		return
	}
	if group.IsProtected {
		jsonErr(w, http.StatusConflict, "protected auth group cannot be deleted")
		return
	}

	if err := h.authGroups.Delete(r.Context(), group.ID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "delete auth group failed")
		return
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	h.usersAudit(r, actorID, "auth_group_delete", group.ID, map[string]string{"group_key": group.GroupKey})
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthGroupHandler) AddPermission(w http.ResponseWriter, r *http.Request) {
	group, ok := h.loadMutableAuthGroup(w, r)
	if !ok {
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
	permissionKey := strings.TrimSpace(req.PermissionKey)
	if err := requirePermissionGrantAllowed(r.Context(), h.users, actorID, permissionKey); err != nil {
		h.logForbiddenAuthGroupMutation(r, actorID, group.ID, "add_auth_group_permission", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if err := h.authGroups.AddPermission(r.Context(), group.ID, permissionKey); err != nil {
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusUnprocessableEntity, "invalid permission_key")
			return
		}
		jsonErr(w, http.StatusInternalServerError, "add auth group permission failed")
		return
	}
	h.usersAudit(r, actorID, "auth_group_permission_add", group.ID, map[string]string{"group_key": group.GroupKey, "permission_key": permissionKey})
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthGroupHandler) RemovePermission(w http.ResponseWriter, r *http.Request) {
	group, ok := h.loadMutableAuthGroup(w, r)
	if !ok {
		return
	}
	permissionKey := chi.URLParam(r, "permissionKey")
	if strings.TrimSpace(permissionKey) == "" {
		jsonErr(w, http.StatusBadRequest, "invalid permissionKey")
		return
	}
	if err := h.authGroups.RemovePermission(r.Context(), group.ID, permissionKey); err != nil {
		jsonErr(w, http.StatusInternalServerError, "remove auth group permission failed")
		return
	}
	h.usersAudit(r, middleware.UserIDFromCtx(r.Context()), "auth_group_permission_remove", group.ID, map[string]string{"group_key": group.GroupKey, "permission_key": strings.TrimSpace(permissionKey)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthGroupHandler) AddDBConnection(w http.ResponseWriter, r *http.Request) {
	group, ok := h.loadMutableAuthGroup(w, r)
	if !ok {
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
	if err := requireDBConnectionGrantAllowed(r.Context(), h.users, actorID, req.DBConnectionID); err != nil {
		h.logForbiddenAuthGroupMutation(r, actorID, group.ID, "add_auth_group_db_connection", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if err := h.authGroups.AddDBConnection(r.Context(), group.ID, req.DBConnectionID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "add auth group db connection failed")
		return
	}
	h.usersAudit(r, actorID, "auth_group_db_connection_add", group.ID, map[string]string{"group_key": group.GroupKey, "db_connection_id": strconv.FormatUint(req.DBConnectionID, 10)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthGroupHandler) RemoveDBConnection(w http.ResponseWriter, r *http.Request) {
	group, ok := h.loadMutableAuthGroup(w, r)
	if !ok {
		return
	}
	connID, err := strconv.ParseUint(chi.URLParam(r, "connID"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid connID")
		return
	}
	if err := h.authGroups.RemoveDBConnection(r.Context(), group.ID, connID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "remove auth group db connection failed")
		return
	}
	h.usersAudit(r, middleware.UserIDFromCtx(r.Context()), "auth_group_db_connection_remove", group.ID, map[string]string{"group_key": group.GroupKey, "db_connection_id": strconv.FormatUint(connID, 10)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthGroupHandler) loadMutableAuthGroup(w http.ResponseWriter, r *http.Request) (*repository.AuthGroupEntity, bool) {
	group, err := h.authGroups.GetByKey(r.Context(), chi.URLParam(r, "group"))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load auth group failed")
		return nil, false
	}
	if group == nil {
		jsonErr(w, http.StatusNotFound, "auth group not found")
		return nil, false
	}
	actorID := middleware.UserIDFromCtx(r.Context())
	if err := requireAuthGroupMutationAllowed(r.Context(), h.users, actorID, group); err != nil {
		h.logForbiddenAuthGroupMutation(r, actorID, group.ID, "mutate_auth_group", err.Error())
		jsonErr(w, http.StatusForbidden, err.Error())
		return nil, false
	}
	return group, true
}

func (h *AuthGroupHandler) usersAudit(r *http.Request, actorID uint64, action string, resourceID uint64, details map[string]string) {
	if h.audit == nil {
		return
	}
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   action,
		ResourceType: "auth_group",
		ResourceID:   &resourceID,
		Details:      details,
		IPAddress:    clientIP(r),
	})
}
