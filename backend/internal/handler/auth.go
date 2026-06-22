package handler

import (
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/dbre-maestro/maestro/internal/auth"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	users               *repository.UserRepo
	sessions            *repository.SessionRepo
	audit               *repository.AuditRepo
	jwtSecret           []byte
	refreshCookieSecure bool
}

const refreshCookiePath = "/api/auth/refresh"

func NewAuthHandler(users *repository.UserRepo, sessions *repository.SessionRepo, audit *repository.AuditRepo, jwtSecret []byte, refreshCookieSecure ...bool) *AuthHandler {
	secure := false
	if len(refreshCookieSecure) > 0 {
		secure = refreshCookieSecure[0]
	}
	return &AuthHandler{users: users, sessions: sessions, audit: audit, jwtSecret: jwtSecret, refreshCookieSecure: secure}
}

// GET /setup/status
func (h *AuthHandler) SetupStatus(w http.ResponseWriter, r *http.Request) {
	count, err := h.users.CountUsers(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	jsonOK(w, map[string]any{
		"setup_completed": count > 0,
	})
}

// POST /setup — first-time admin creation (no auth required, blocked after setup)
func (h *AuthHandler) Setup(w http.ResponseWriter, r *http.Request) {
	count, err := h.users.CountUsers(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if count > 0 {
		jsonErr(w, http.StatusConflict, "setup already completed")
		return
	}

	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validatePassword(req.Password); err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if req.Username == "" || req.Email == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "username and email are required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := h.users.Create(r.Context(), req.Username, req.Email, "", string(hash), true)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "create user failed")
		return
	}

	// Grant admin group
	if err := h.users.AddMembership(r.Context(), user.ID, model.AuthGroupAdmin, nil, nil); err != nil {
		jsonErr(w, http.StatusInternalServerError, "grant admin failed")
		return
	}

	jsonCreated(w, map[string]any{"id": user.ID, "username": user.Username})
}

// POST /auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.users.GetByUsername(r.Context(), req.Username)
	if err != nil || user == nil {
		jsonErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !user.IsActive {
		jsonErr(w, http.StatusForbidden, "user is disabled")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		jsonErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	accessToken, err := auth.NewAccessToken(user.ID, user.Username, h.jwtSecret)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}

	rawRefresh, hashRefresh, err := auth.NewRefreshToken()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}

	expiresAt := time.Now().Add(auth.RefreshTokenTTL)
	if _, err := h.sessions.Create(r.Context(), user.ID, hashRefresh,
		r.Header.Get("User-Agent"), clientIP(r), expiresAt); err != nil {
		jsonErr(w, http.StatusInternalServerError, "session error")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    rawRefresh,
		Path:     refreshCookiePath,
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.refreshCookieSecure || r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})

	h.audit.Log(r.Context(), repository.AuditEntry{
		ActorID:    &user.ID,
		ActorName:  user.Username,
		ActionType: "login",
		IPAddress:  clientIP(r),
	})

	jsonOK(w, map[string]string{"access_token": accessToken})
}

// POST /auth/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		jsonErr(w, http.StatusUnauthorized, "missing refresh token")
		return
	}

	hash := auth.HashRefreshToken(cookie.Value)
	session, err := h.sessions.GetByTokenHash(r.Context(), hash)
	if err != nil || session == nil {
		jsonErr(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	if session.RevokedAt != nil || time.Now().After(session.ExpiresAt) {
		jsonErr(w, http.StatusUnauthorized, "refresh token expired or revoked")
		return
	}

	// Rotate: revoke old, issue new
	h.sessions.Revoke(r.Context(), hash)

	user, err := h.users.GetByID(r.Context(), session.UserID)
	if err != nil || user == nil {
		jsonErr(w, http.StatusUnauthorized, "user not found")
		return
	}
	if !user.IsActive {
		h.sessions.RevokeAllForUser(r.Context(), user.ID)
		jsonErr(w, http.StatusUnauthorized, "user is disabled")
		return
	}

	accessToken, err := auth.NewAccessToken(user.ID, user.Username, h.jwtSecret)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}

	rawRefresh, hashRefresh, err := auth.NewRefreshToken()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}

	expiresAt := time.Now().Add(auth.RefreshTokenTTL)
	h.sessions.Create(r.Context(), user.ID, hashRefresh,
		r.Header.Get("User-Agent"), clientIP(r), expiresAt)

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    rawRefresh,
		Path:     refreshCookiePath,
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.refreshCookieSecure || r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})

	jsonOK(w, map[string]string{"access_token": accessToken})
}

// POST /auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("refresh_token")
	if err == nil {
		hash := auth.HashRefreshToken(cookie.Value)
		h.sessions.Revoke(r.Context(), hash)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.refreshCookieSecure || r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})

	userID := middleware.UserIDFromCtx(r.Context())
	username := middleware.UsernameFromCtx(r.Context())
	if userID != 0 {
		h.audit.Log(r.Context(), repository.AuditEntry{
			ActorID:    &userID,
			ActorName:  username,
			ActionType: "logout",
			IPAddress:  clientIP(r),
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /auth/me
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	if userID == 0 {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.users.GetByID(r.Context(), userID)
	if err != nil || user == nil {
		jsonErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	authGroupRecords, err := h.users.GetAuthGroupRecords(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load auth groups failed")
		return
	}

	permissions, err := h.users.GetEffectivePermissionKeys(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load permissions failed")
		return
	}
	if permissions == nil {
		permissions = []string{}
	}

	dbConnectionIDs, err := h.users.GetEffectiveDBConnectionIDs(r.Context(), userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load db scopes failed")
		return
	}
	if dbConnectionIDs == nil {
		dbConnectionIDs = []uint64{}
	}

	type authGroupView struct {
		ID          uint64 `json:"id"`
		GroupKey    string `json:"group_key"`
		Name        string `json:"name"`
		IsSystem    bool   `json:"is_system"`
		IsProtected bool   `json:"is_protected"`
	}

	authGroupViews := make([]authGroupView, 0, len(authGroupRecords))
	for _, record := range authGroupRecords {
		authGroupViews = append(authGroupViews, authGroupView{
			ID:          record.ID,
			GroupKey:    record.GroupKey,
			Name:        record.Name,
			IsSystem:    record.IsSystem,
			IsProtected: record.IsProtected,
		})
	}

	jsonOK(w, map[string]any{
		"id":                userID,
		"username":          middleware.UsernameFromCtx(r.Context()),
		"protected":         user.IsProtected,
		"is_active":         user.IsActive,
		"auth_groups":       authGroupViews,
		"permissions":       permissions,
		"db_connection_ids": dbConnectionIDs,
	})
}

func validatePassword(pw string) error {
	if len(pw) < 8 {
		return errStr("password must be at least 8 characters")
	}
	var hasUpper, hasLower, hasDigit bool
	for _, c := range pw {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsDigit(c):
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return errStr("password must contain uppercase, lowercase, and digit characters")
	}
	return nil
}

type errStr string

func (e errStr) Error() string { return string(e) }

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.SplitN(ip, ",", 2)[0]
	}
	return r.RemoteAddr
}
