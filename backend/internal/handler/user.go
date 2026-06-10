package handler

import (
	"net/http"
	"strconv"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

type UserHandler struct {
	users *repository.UserRepo
	audit *repository.AuditRepo
}

func NewUserHandler(users *repository.UserRepo, audit *repository.AuditRepo) *UserHandler {
	return &UserHandler{users: users, audit: audit}
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
		ID        uint64 `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		CreatedAt string `json:"created_at"`
	}
	views := make([]userView, 0, len(users))
	for _, u := range users {
		views = append(views, userView{
			ID:        u.ID,
			Username:  u.Username,
			Email:     u.Email,
			CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	jsonOK(w, map[string]any{"users": views})
}

// POST /users — Admin only
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Email == "" || req.Password == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "username, email, and password are required")
		return
	}
	if err := validatePassword(req.Password); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := h.users.Create(r.Context(), req.Username, req.Email, string(hash))
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
		Details:      map[string]string{"username": user.Username, "email": user.Email},
		IPAddress:    clientIP(r),
	})

	jsonCreated(w, map[string]any{"id": user.ID, "username": user.Username, "email": user.Email})
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

	jsonOK(w, map[string]any{
		"id":          user.ID,
		"username":    user.Username,
		"email":       user.Email,
		"created_at":  user.CreatedAt,
		"memberships": memberships,
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
	group := model.AuthGroup(req.AuthGroup)
	switch group {
	case model.AuthGroupDeveloper, model.AuthGroupReviewer, model.AuthGroupDBA, model.AuthGroupAdmin:
	default:
		jsonErr(w, http.StatusUnprocessableEntity, "auth_group must be developer, reviewer, dba, or admin")
		return
	}

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	if err := h.users.AddMembership(r.Context(), id, group, &actorID, nil); err != nil {
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
// Body: { "username": "...", "email": "...", "password": "..." }  — all optional
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
		Username *string `json:"username"`
		Email    *string `json:"email"`
		Password *string `json:"password"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
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

	if err := h.users.Update(r.Context(), id, username, email); err != nil {
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

	actorID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "user_update",
		ResourceType: "user",
		ResourceID:   &id,
		Details:      map[string]string{"username": username, "email": email},
		IPAddress:    clientIP(r),
	})

	jsonOK(w, map[string]any{"id": id, "username": username, "email": email})
}

// DELETE /users/{id}/memberships/{group} — Admin only
func (h *UserHandler) RemoveMembership(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	group := model.AuthGroup(chi.URLParam(r, "group"))
	switch group {
	case model.AuthGroupDeveloper, model.AuthGroupReviewer, model.AuthGroupDBA, model.AuthGroupAdmin:
	default:
		jsonErr(w, http.StatusUnprocessableEntity, "invalid auth_group")
		return
	}

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil || user == nil {
		jsonErr(w, http.StatusNotFound, "user not found")
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
