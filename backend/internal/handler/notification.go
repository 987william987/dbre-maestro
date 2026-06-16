package handler

import (
	"net/http"
	"strconv"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
)

type NotificationHandler struct {
	notifs *repository.NotificationRepo
}

func NewNotificationHandler(notifs *repository.NotificationRepo) *NotificationHandler {
	return &NotificationHandler{notifs: notifs}
}

// GET /notifications
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	notifs, total, err := h.notifs.List(r.Context(), userID, limit, offset)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list notifications failed")
		return
	}
	if notifs == nil {
		notifs = []model.Notification{}
	}

	unread, _ := h.notifs.UnreadCount(r.Context(), userID)
	jsonOK(w, map[string]any{
		"notifications": notifs,
		"total":         total,
		"unread":        unread,
		"limit":         limit,
		"offset":        offset,
	})
}

// POST /notifications/{id}/read
func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	userID := middleware.UserIDFromCtx(r.Context())
	if err := h.notifs.MarkRead(r.Context(), id, userID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "mark read failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /notifications/read-all
func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	if err := h.notifs.MarkAllRead(r.Context(), userID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "mark all read failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
