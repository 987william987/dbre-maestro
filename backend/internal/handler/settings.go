package handler

import (
	"fmt"
	"net/http"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type SettingsHandler struct {
	settings *repository.SettingsRepo
	users    *repository.UserRepo
	audit    *repository.AuditRepo
}

func NewSettingsHandler(settings *repository.SettingsRepo, users *repository.UserRepo, audit *repository.AuditRepo) *SettingsHandler {
	return &SettingsHandler{
		settings: settings,
		users:    users,
		audit:    audit,
	}
}

func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings.Get(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load settings failed")
		return
	}
	jsonOK(w, settings)
}

func (h *SettingsHandler) Patch(w http.ResponseWriter, r *http.Request) {
	var req model.PlatformSettings
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, userID := range append([]uint64{}, req.SensitiveExportReviewerUserIDs...) {
		if err := h.validateUserExists(r, userID); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	for _, userID := range append([]uint64{}, req.SensitiveQueryAccessReviewerUserIDs...) {
		if err := h.validateUserExists(r, userID); err != nil {
			jsonErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}

	if err := h.settings.Replace(r.Context(), &req); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update settings failed")
		return
	}

	actorID := middleware.UserIDFromCtx(r.Context())
	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:      &actorID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "settings_update",
		ResourceType: "settings",
		Details: map[string]any{
			"sensitive_export_reviewer_user_ids":       req.SensitiveExportReviewerUserIDs,
			"sensitive_query_access_reviewer_user_ids": req.SensitiveQueryAccessReviewerUserIDs,
		},
		IPAddress: clientIP(r),
	})

	jsonOK(w, req)
}

func (h *SettingsHandler) validateUserExists(r *http.Request, userID uint64) error {
	user, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user %d does not exist", userID)
	}
	return nil
}
