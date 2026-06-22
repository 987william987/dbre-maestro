package handler

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dbre-maestro/maestro/internal/auth"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	users               *repository.UserRepo
	sessions            *repository.SessionRepo
	audit               *repository.AuditRepo
	jwtSecret           []byte
	refreshCookieSecure bool
	loginRateLimiter    requestRateLimiter
	refreshRateLimiter  requestRateLimiter
}

const (
	refreshCookiePath       = "/api/auth/refresh"
	refreshReuseGraceWindow = 30 * time.Second
	sessionListLimit        = 20
)

func NewAuthHandler(users *repository.UserRepo, sessions *repository.SessionRepo, audit *repository.AuditRepo, jwtSecret []byte, refreshCookieSecure ...bool) *AuthHandler {
	secure := false
	if len(refreshCookieSecure) > 0 {
		secure = refreshCookieSecure[0]
	}
	return &AuthHandler{
		users:               users,
		sessions:            sessions,
		audit:               audit,
		jwtSecret:           jwtSecret,
		refreshCookieSecure: secure,
		loginRateLimiter:    newRequestRateLimiter(5, time.Minute),
		refreshRateLimiter:  newRequestRateLimiter(30, time.Minute),
	}
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
	username := strings.TrimSpace(req.Username)
	if !h.allowAuthAttempt(w, r, h.loginRateLimiter, "login", strings.ToLower(username)) {
		return
	}

	user, err := h.users.GetByUsername(r.Context(), username)
	if err != nil || user == nil {
		h.logLoginFailed(r, nil, username, "invalid_credentials")
		jsonErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !user.IsActive {
		h.logLoginFailed(r, &user.ID, user.Username, "disabled_user")
		jsonErr(w, http.StatusForbidden, "user is disabled")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		h.logLoginFailed(r, &user.ID, user.Username, "invalid_credentials")
		jsonErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	requiresMFA, err := h.users.RequiresMFA(r.Context(), user)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "mfa policy check failed")
		return
	}
	if requiresMFA {
		if !user.MFAEnabled || len(user.MFASecret) == 0 {
			h.startMFASetup(w, r, user)
			return
		}
		mfaToken, err := auth.NewMFAChallengeToken(user.ID, user.Username, false, h.jwtSecret)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "token error")
			return
		}
		jsonOK(w, map[string]any{
			"mfa_required": true,
			"mfa_token":    mfaToken,
		})
		return
	}

	h.completeLogin(w, r, user)
}

func (h *AuthHandler) completeLogin(w http.ResponseWriter, r *http.Request, user *model.User) {
	rawRefresh, hashRefresh, err := auth.NewRefreshToken()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}

	expiresAt := time.Now().Add(auth.RefreshTokenTTL)
	session, err := h.sessions.Create(r.Context(), user.ID, hashRefresh,
		r.Header.Get("User-Agent"), clientIP(r), expiresAt)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "session error")
		return
	}

	accessToken, err := auth.NewAccessToken(user.ID, user.Username, session.ID, h.jwtSecret)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}

	http.SetCookie(w, h.refreshCookie(r, rawRefresh, expiresAt))

	h.logAudit(r, repository.AuditEntry{
		ActorID:    &user.ID,
		ActorName:  user.Username,
		ActionType: "login",
	})

	jsonOK(w, map[string]string{"access_token": accessToken})
}

func (h *AuthHandler) startMFASetup(w http.ResponseWriter, r *http.Request, user *model.User) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "DBRE Maestro",
		AccountName: user.Username,
	})
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "mfa setup failed")
		return
	}
	if err := h.users.StoreMFASecret(r.Context(), user.ID, key.Secret()); err != nil {
		jsonErr(w, http.StatusInternalServerError, "mfa setup failed")
		return
	}
	mfaToken, err := auth.NewMFAChallengeToken(user.ID, user.Username, true, h.jwtSecret)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}
	qrDataURL, err := totpQRCodeDataURL(key)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "mfa setup failed")
		return
	}
	jsonOK(w, map[string]any{
		"mfa_setup_required": true,
		"mfa_token":          mfaToken,
		"otp_auth_url":       key.URL(),
		"mfa_secret":         key.Secret(),
		"qr_data_url":        qrDataURL,
	})
}

// POST /auth/mfa/verify
func (h *AuthHandler) VerifyMFA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MFAToken string `json:"mfa_token"`
		Code     string `json:"code"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	claims, err := auth.ParseMFAChallengeToken(strings.TrimSpace(req.MFAToken), h.jwtSecret)
	if err != nil {
		jsonErr(w, http.StatusUnauthorized, "invalid mfa token")
		return
	}
	user, err := h.users.GetByID(r.Context(), claims.UserID)
	if err != nil || user == nil {
		jsonErr(w, http.StatusUnauthorized, "user not found")
		return
	}
	if !user.IsActive {
		jsonErr(w, http.StatusUnauthorized, "user is disabled")
		return
	}
	requiresMFA, err := h.users.RequiresMFA(r.Context(), user)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "mfa policy check failed")
		return
	}
	if !requiresMFA {
		jsonErr(w, http.StatusBadRequest, "mfa is not required")
		return
	}
	secret, err := h.users.DecryptMFASecret(user)
	if err != nil || secret == "" {
		jsonErr(w, http.StatusUnauthorized, "mfa is not configured")
		return
	}
	if !totp.Validate(strings.TrimSpace(req.Code), secret) {
		h.logAudit(r, repository.AuditEntry{
			ActorID:      &user.ID,
			ActorName:    user.Username,
			ActionType:   "mfa_failed",
			ResourceType: "auth",
		})
		jsonErr(w, http.StatusUnauthorized, "invalid mfa code")
		return
	}
	if claims.Setup || !user.MFAEnabled {
		if err := h.users.EnableMFA(r.Context(), user.ID); err != nil {
			jsonErr(w, http.StatusInternalServerError, "enable mfa failed")
			return
		}
		h.logAudit(r, repository.AuditEntry{
			ActorID:      &user.ID,
			ActorName:    user.Username,
			ActionType:   "mfa_enable",
			ResourceType: "auth",
		})
	}
	h.completeLogin(w, r, user)
}

// POST /auth/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if !h.allowAuthAttempt(w, r, h.refreshRateLimiter, "refresh", "") {
		return
	}
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
	if session.RevokedAt != nil {
		if time.Since(*session.RevokedAt) <= refreshReuseGraceWindow {
			http.SetCookie(w, h.clearRefreshCookie(r))
			jsonErr(w, http.StatusUnauthorized, "stale refresh token")
			return
		}
		h.sessions.RevokeAllForUser(r.Context(), session.UserID)
		http.SetCookie(w, h.clearRefreshCookie(r))
		h.logAudit(r, repository.AuditEntry{
			ActorID:      &session.UserID,
			ActionType:   "refresh_token_reuse_detected",
			ResourceType: "session",
			ResourceID:   &session.ID,
			Details: map[string]any{
				"session_id": session.ID,
				"revoked_at": session.RevokedAt,
			},
		})
		jsonErr(w, http.StatusUnauthorized, "refresh token reuse detected")
		return
	}
	if time.Now().After(session.ExpiresAt) {
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
	requiresMFA, err := h.users.RequiresMFA(r.Context(), user)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "mfa policy check failed")
		return
	}
	if requiresMFA && (!user.MFAEnabled || len(user.MFASecret) == 0) {
		h.sessions.RevokeAllForUser(r.Context(), user.ID)
		jsonErr(w, http.StatusUnauthorized, "mfa required")
		return
	}

	rawRefresh, hashRefresh, err := auth.NewRefreshToken()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}

	expiresAt := time.Now().Add(auth.RefreshTokenTTL)
	newSession, err := h.sessions.Create(r.Context(), user.ID, hashRefresh,
		r.Header.Get("User-Agent"), clientIP(r), expiresAt)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "session error")
		return
	}

	accessToken, err := auth.NewAccessToken(user.ID, user.Username, newSession.ID, h.jwtSecret)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}

	http.SetCookie(w, h.refreshCookie(r, rawRefresh, expiresAt))

	jsonOK(w, map[string]string{"access_token": accessToken})
}

// POST /auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("refresh_token")
	if err == nil {
		hash := auth.HashRefreshToken(cookie.Value)
		h.sessions.Revoke(r.Context(), hash)
	}

	http.SetCookie(w, h.clearRefreshCookie(r))

	userID := middleware.UserIDFromCtx(r.Context())
	username := middleware.UsernameFromCtx(r.Context())
	if userID != 0 {
		h.logAudit(r, repository.AuditEntry{
			ActorID:    &userID,
			ActorName:  username,
			ActionType: "logout",
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	sessions, err := h.sessions.ListForUserLimit(r.Context(), userID, sessionListLimit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "load sessions failed")
		return
	}
	if sessions == nil {
		sessions = []model.Session{}
	}
	jsonOK(w, map[string]any{
		"sessions":           sessions,
		"current_session_id": middleware.SessionIDFromCtx(r.Context()),
	})
}

func (h *AuthHandler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	sessionID, ok := parseAuthUintParam(w, r, "id")
	if !ok {
		return
	}
	revoked, err := h.sessions.RevokeByIDForUser(r.Context(), sessionID, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "revoke session failed")
		return
	}
	if !revoked {
		jsonErr(w, http.StatusNotFound, "session not found")
		return
	}
	h.logAudit(r, repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "session_revoke",
		ResourceType: "session",
		ResourceID:   &sessionID,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) RevokeSessions(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromCtx(r.Context())
	if err := h.sessions.RevokeAllForUser(r.Context(), userID); err != nil {
		jsonErr(w, http.StatusInternalServerError, "revoke sessions failed")
		return
	}
	http.SetCookie(w, h.clearRefreshCookie(r))
	h.logAudit(r, repository.AuditEntry{
		ActorID:      &userID,
		ActorName:    middleware.UsernameFromCtx(r.Context()),
		ActionType:   "session_revoke_all",
		ResourceType: "session",
	})
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

func (h *AuthHandler) allowAuthAttempt(w http.ResponseWriter, r *http.Request, limiter requestRateLimiter, action string, subject string) bool {
	key := clientIP(r)
	if subject != "" {
		key += ":" + subject
	}
	if limiter == nil || limiter.Allow(key, time.Now()) {
		return true
	}
	h.logAudit(r, repository.AuditEntry{
		ActionType:   "auth_rate_limited",
		ResourceType: "auth",
		Details: map[string]any{
			"action":  action,
			"subject": subject,
		},
	})
	jsonErr(w, http.StatusTooManyRequests, fmt.Sprintf("%s rate limit exceeded", action))
	return false
}

func (h *AuthHandler) refreshCookie(r *http.Request, value string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     "refresh_token",
		Value:    value,
		Path:     refreshCookiePath,
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.refreshCookieSecure || r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	}
}

func (h *AuthHandler) clearRefreshCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.refreshCookieSecure || r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	}
}

func (h *AuthHandler) logAudit(r *http.Request, entry repository.AuditEntry) {
	if h.audit == nil {
		return
	}
	if entry.IPAddress == "" {
		entry.IPAddress = clientIP(r)
	}
	_ = h.audit.Log(r.Context(), entry)
}

func (h *AuthHandler) logLoginFailed(r *http.Request, actorID *uint64, username string, reason string) {
	h.logAudit(r, repository.AuditEntry{
		ActorID:      actorID,
		ActorName:    username,
		ActionType:   "login_failed",
		ResourceType: "auth",
		Details: map[string]any{
			"reason": reason,
		},
	})
}

func totpQRCodeDataURL(key *otp.Key) (string, error) {
	img, err := key.Image(220, 220)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func parseAuthUintParam(w http.ResponseWriter, r *http.Request, name string) (uint64, bool) {
	value := strings.TrimSpace(chi.URLParam(r, name))
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		jsonErr(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
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
