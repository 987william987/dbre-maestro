package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dbre-maestro/maestro/internal/auth"
	"github.com/dbre-maestro/maestro/internal/config"
	"github.com/dbre-maestro/maestro/internal/larkoauth"
	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	users                *repository.UserRepo
	sessions             *repository.SessionRepo
	audit                *repository.AuditRepo
	settings             *repository.SettingsRepo
	mfaChallenges        *repository.MFAChallengeRepo
	larkLogins           *repository.LarkLoginRepo
	jwtSecret            []byte
	larkOAuth            config.LarkOAuthConfig
	larkOAuthClient      larkoauth.Client
	refreshCookieSecure  bool
	loginRateLimiter     requestRateLimiter
	refreshRateLimiter   requestRateLimiter
	mfaVerifyRateLimiter requestRateLimiter
	larkLoginRateLimiter requestRateLimiter
	mfaEnforcement       MFAEnforcement
}

type MFAEnforcement string

const (
	MFAEnforcementDisabled          MFAEnforcement = "disabled"
	MFAEnforcementRequiredForAdmins MFAEnforcement = "required_for_admins"
)

const (
	refreshCookiePath       = "/api/auth/refresh"
	refreshReuseGraceWindow = 30 * time.Second
	sessionListLimit        = 20
	mfaMaxAttempts          = 5
	larkLoginStateTTL       = 10 * time.Minute
	larkLoginTicketTTL      = 2 * time.Minute
)

func NewAuthHandler(users *repository.UserRepo, sessions *repository.SessionRepo, audit *repository.AuditRepo, jwtSecret []byte, options ...any) *AuthHandler {
	secure := false
	enforcement := MFAEnforcementDisabled
	var mfaChallenges *repository.MFAChallengeRepo
	var settings *repository.SettingsRepo
	var larkLogins *repository.LarkLoginRepo
	var larkOAuth config.LarkOAuthConfig
	var larkOAuthClient larkoauth.Client
	for _, option := range options {
		switch value := option.(type) {
		case bool:
			secure = value
		case string:
			enforcement = normalizeMFAEnforcement(value)
		case MFAEnforcement:
			enforcement = value
		case *repository.MFAChallengeRepo:
			mfaChallenges = value
		case *repository.SettingsRepo:
			settings = value
		case *repository.LarkLoginRepo:
			larkLogins = value
		case config.LarkOAuthConfig:
			larkOAuth = value
		case larkoauth.Client:
			larkOAuthClient = value
		}
	}
	h := &AuthHandler{
		users:                users,
		sessions:             sessions,
		audit:                audit,
		settings:             settings,
		mfaChallenges:        mfaChallenges,
		larkLogins:           larkLogins,
		jwtSecret:            jwtSecret,
		larkOAuth:            larkOAuth,
		larkOAuthClient:      larkOAuthClient,
		refreshCookieSecure:  secure,
		loginRateLimiter:     newRequestRateLimiter(5, time.Minute),
		refreshRateLimiter:   newRequestRateLimiter(30, time.Minute),
		mfaVerifyRateLimiter: newRequestRateLimiter(10, time.Minute),
		larkLoginRateLimiter: newRequestRateLimiter(20, time.Minute),
		mfaEnforcement:       enforcement,
	}
	return h
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
	if user.PasswordLoginDisabled {
		h.logLoginFailed(r, &user.ID, user.Username, "password_login_disabled")
		jsonErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		h.logLoginFailed(r, &user.ID, user.Username, "invalid_credentials")
		jsonErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	requiresMFA, err := h.requiresMFA(r.Context(), user)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "mfa policy check failed")
		return
	}
	if requiresMFA {
		if !user.MFAEnabled || len(user.MFASecret) == 0 {
			h.startMFASetup(w, r, user)
			return
		}
		mfaToken, err := h.newMFAChallenge(r.Context(), r, user, false)
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

// GET /auth/lark/login/start
func (h *AuthHandler) StartLarkLogin(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.resolveLarkOAuthConfig(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "lark login config error")
		return
	}
	if !cfg.Configured() || h.larkLogins == nil {
		jsonErr(w, http.StatusServiceUnavailable, "lark login is not configured")
		return
	}
	if !h.allowAuthAttempt(w, r, h.larkLoginRateLimiter, "lark_login_start", clientIP(r)) {
		return
	}
	state, err := auth.NewTokenID()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}
	returnTo := sanitizeReturnTo(r.URL.Query().Get("returnTo"))
	if err := h.larkLogins.Create(r.Context(), state, returnTo, time.Now().Add(larkLoginStateTTL)); err != nil {
		jsonErr(w, http.StatusInternalServerError, "lark login state error")
		return
	}
	jsonOK(w, map[string]string{"url": larkoauth.AuthorizeURL(cfg, state)})
}

// GET /auth/lark/login/callback
func (h *AuthHandler) CompleteLarkLogin(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.resolveLarkOAuthConfig(r.Context())
	if err != nil {
		http.Redirect(w, r, larkLoginRedirect("", "login_failed"), http.StatusFound)
		return
	}
	if !cfg.Configured() || h.larkLogins == nil {
		http.Redirect(w, r, larkLoginRedirect("", "login_failed"), http.StatusFound)
		return
	}
	now := time.Now()
	state, ok, err := h.larkLogins.ClaimState(r.Context(), strings.TrimSpace(r.URL.Query().Get("state")), now)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "lark login state error")
		return
	}
	if !ok || state == nil {
		http.Redirect(w, r, larkLoginRedirect("", "login_failed"), http.StatusFound)
		return
	}
	if code := strings.TrimSpace(r.URL.Query().Get("error")); code != "" {
		errCode := larkLoginOAuthErrorCode(code)
		_ = h.larkLogins.MarkFailed(r.Context(), state.ID, errCode)
		http.Redirect(w, r, larkLoginRedirect("", errCode), http.StatusFound)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		_ = h.larkLogins.MarkFailed(r.Context(), state.ID, "login_failed")
		http.Redirect(w, r, larkLoginRedirect("", "login_failed"), http.StatusFound)
		return
	}
	client := h.larkOAuthClient
	if client == nil {
		defaultClient := larkoauth.NewHTTPClient()
		client = defaultClient
	}
	identity, err := client.ExchangeCode(r.Context(), cfg, code)
	if err != nil || strings.TrimSpace(identity.OpenID) == "" {
		_ = h.larkLogins.MarkFailed(r.Context(), state.ID, "invalid_lark_identity")
		h.logLoginFailed(r, nil, "", "lark_oauth_failed")
		http.Redirect(w, r, larkLoginRedirect("", "login_failed"), http.StatusFound)
		return
	}
	user, err := h.findOrCreateLarkUser(r.Context(), identity)
	if err != nil {
		_ = h.larkLogins.MarkFailed(r.Context(), state.ID, "resolve_user_failed")
		h.logLoginFailed(r, nil, firstNonEmptyString(identity.EnterpriseEmail, identity.Email), "lark_user_resolve_failed")
		http.Redirect(w, r, larkLoginRedirect("", "login_failed"), http.StatusFound)
		return
	}
	if !user.IsActive {
		h.logLoginFailed(r, &user.ID, user.Username, "disabled_user")
		http.Redirect(w, r, larkLoginRedirect("", "user_disabled"), http.StatusFound)
		return
	}
	ticket, err := auth.NewTokenID()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "token error")
		return
	}
	if err := h.larkLogins.StoreTicket(r.Context(), state.ID, user.ID, ticket, time.Now().Add(larkLoginTicketTTL)); err != nil {
		jsonErr(w, http.StatusInternalServerError, "lark login ticket error")
		return
	}
	http.Redirect(w, r, larkLoginRedirect(ticket, ""), http.StatusFound)
}

// POST /auth/lark/login/result/consume
func (h *AuthHandler) ConsumeLarkLoginResult(w http.ResponseWriter, r *http.Request) {
	if h.larkLogins == nil {
		jsonErr(w, http.StatusServiceUnavailable, "lark login is not configured")
		return
	}
	var req struct {
		Ticket string `json:"ticket"`
	}
	if err := bindJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	state, ok, err := h.larkLogins.ConsumeTicket(r.Context(), strings.TrimSpace(req.Ticket), time.Now())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "lark login ticket error")
		return
	}
	if !ok || state == nil {
		jsonErr(w, http.StatusUnauthorized, "invalid lark login ticket")
		return
	}
	userID, err := state.RequiredUserID()
	if err != nil {
		jsonErr(w, http.StatusUnauthorized, "invalid lark login ticket")
		return
	}
	user, err := h.users.GetByID(r.Context(), userID)
	if err != nil || user == nil {
		jsonErr(w, http.StatusUnauthorized, "invalid lark login ticket")
		return
	}
	if !user.IsActive {
		jsonErr(w, http.StatusForbidden, "user is disabled")
		return
	}
	requiresMFA, err := h.requiresMFA(r.Context(), user)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "mfa policy check failed")
		return
	}
	if requiresMFA {
		if !user.MFAEnabled || len(user.MFASecret) == 0 {
			h.startMFASetup(w, r, user)
			return
		}
		mfaToken, err := h.newMFAChallenge(r.Context(), r, user, false)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "token error")
			return
		}
		jsonOK(w, map[string]any{"mfa_required": true, "mfa_token": mfaToken})
		return
	}
	h.completeLoginWithPayload(w, r, user, map[string]string{"return_to": sanitizeReturnTo(state.ReturnTo)})
}

func (h *AuthHandler) findOrCreateLarkUser(ctx context.Context, identity larkoauth.Identity) (*model.User, error) {
	enterpriseEmail := strings.TrimSpace(identity.EnterpriseEmail)
	if h.larkOAuth.RequireEnterpriseEmail && enterpriseEmail == "" {
		return nil, fmt.Errorf("lark enterprise_email is required")
	}
	if enterpriseEmail != "" && !larkEnterpriseEmailAllowed(enterpriseEmail, h.larkOAuth.EnterpriseEmailDomains) {
		return nil, fmt.Errorf("lark enterprise_email domain is not allowed")
	}

	input := repository.LarkIdentityInput{
		OpenID:      strings.TrimSpace(identity.OpenID),
		UnionID:     strings.TrimSpace(identity.UnionID),
		Email:       enterpriseEmail,
		DisplayName: strings.TrimSpace(identity.DisplayName),
		AvatarURL:   strings.TrimSpace(identity.AvatarURL),
	}
	if input.OpenID == "" {
		return nil, fmt.Errorf("lark identity missing open_id")
	}

	user, err := h.users.GetByLarkLoginIdentity(ctx, input.OpenID, input.UnionID)
	if err != nil {
		return nil, err
	}
	if user != nil {
		if err := h.users.BindLarkIdentity(ctx, user.ID, input); err != nil {
			return nil, err
		}
		return h.users.GetByID(ctx, user.ID)
	}

	if input.Email != "" {
		user, err = h.users.GetByEmail(ctx, input.Email)
		if err != nil {
			return nil, err
		}
		if user != nil {
			if user.IsProtected {
				return nil, fmt.Errorf("protected user cannot be auto-bound to lark")
			}
			if err := h.users.BindLarkIdentity(ctx, user.ID, input); err != nil {
				return nil, err
			}
			return h.users.GetByID(ctx, user.ID)
		}
	}

	passwordSeed, err := auth.NewTokenID()
	if err != nil {
		return nil, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(passwordSeed), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	input.PasswordHash = string(passwordHash)
	return h.users.CreateLarkDeveloper(ctx, h.uniqueLarkUsername(ctx, identity), input)
}

func larkEnterpriseEmailAllowed(email string, allowedDomains []string) bool {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(normalizedEmail, "@")
	if at < 0 || at == len(normalizedEmail)-1 {
		return false
	}
	if len(allowedDomains) == 0 {
		return true
	}
	domain := normalizedEmail[at+1:]
	for _, allowed := range allowedDomains {
		normalizedAllowed := strings.ToLower(strings.TrimSpace(allowed))
		if normalizedAllowed == "" {
			continue
		}
		if domain == normalizedAllowed || strings.HasSuffix(domain, "."+normalizedAllowed) {
			return true
		}
	}
	return false
}

func (h *AuthHandler) resolveLarkOAuthConfig(ctx context.Context) (config.LarkOAuthConfig, error) {
	cfg := h.larkOAuth
	if h.settings == nil {
		return cfg, nil
	}

	settings, err := h.settings.Get(ctx)
	if err != nil {
		return config.LarkOAuthConfig{}, err
	}
	cfg.Enabled = settings.LarkOAuthEnabled
	if strings.TrimSpace(settings.LarkOAuthSite) != "" {
		cfg.Site = strings.TrimSpace(settings.LarkOAuthSite)
	}
	if strings.TrimSpace(settings.LarkOAuthRedirectURL) != "" {
		cfg.RedirectURL = strings.TrimSpace(settings.LarkOAuthRedirectURL)
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		cfg.AppID = strings.TrimSpace(settings.LarkAppID)
	}
	if strings.TrimSpace(cfg.AppSecret) == "" {
		secret, err := h.settings.GetLarkAppSecret(ctx)
		if err != nil {
			return config.LarkOAuthConfig{}, err
		}
		cfg.AppSecret = strings.TrimSpace(secret)
	}
	return cfg, nil
}

func (h *AuthHandler) uniqueLarkUsername(ctx context.Context, identity larkoauth.Identity) string {
	base := larkUsernameBase(identity)
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		existing, err := h.users.GetByUsername(ctx, candidate)
		if err != nil || existing == nil {
			return candidate
		}
	}
}

func larkUsernameBase(identity larkoauth.Identity) string {
	source := strings.TrimSpace(identity.Email)
	if at := strings.Index(source, "@"); at > 0 {
		source = source[:at]
	}
	if source == "" {
		source = strings.TrimSpace(identity.DisplayName)
	}
	if source == "" {
		source = firstNonEmptyString(identity.UnionID, identity.OpenID, "lark-user")
	}

	var builder strings.Builder
	for _, char := range source {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '-' || char == '_' || char == '.' {
			builder.WriteRune(char)
		} else if unicode.IsSpace(char) {
			builder.WriteRune('-')
		}
	}
	base := strings.Trim(builder.String(), "-_.")
	if base == "" {
		return "lark-user"
	}
	return strings.ToLower(base)
}

func sanitizeReturnTo(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "://") {
		return "/"
	}
	return value
}

func larkLoginRedirect(ticket string, errorCode string) string {
	values := url.Values{}
	if strings.TrimSpace(ticket) != "" {
		values.Set("lark_ticket", strings.TrimSpace(ticket))
	} else if strings.TrimSpace(errorCode) != "" {
		values.Set("lark_error", strings.TrimSpace(errorCode))
	}
	if encoded := values.Encode(); encoded != "" {
		return "/login?" + encoded
	}
	return "/login"
}

func larkLoginOAuthErrorCode(value string) string {
	if strings.TrimSpace(value) == "access_denied" {
		return "access_denied"
	}
	return "login_failed"
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (h *AuthHandler) completeLogin(w http.ResponseWriter, r *http.Request, user *model.User) {
	h.completeLoginWithPayload(w, r, user, nil)
}

func (h *AuthHandler) completeLoginWithPayload(w http.ResponseWriter, r *http.Request, user *model.User, extra map[string]string) {
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

	payload := map[string]string{"access_token": accessToken}
	for key, value := range extra {
		payload[key] = value
	}
	jsonOK(w, payload)
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
	mfaToken, err := h.newMFAChallenge(r.Context(), r, user, true)
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

func (h *AuthHandler) newMFAChallenge(ctx context.Context, r *http.Request, user *model.User, setup bool) (string, error) {
	if h.mfaChallenges == nil {
		return "", fmt.Errorf("mfa challenge repository is not configured")
	}
	tokenID, err := auth.NewTokenID()
	if err != nil {
		return "", err
	}
	createdIP := clientIP(r)
	expiresAt := time.Now().Add(auth.MFAChallengeTTL)
	if err := h.mfaChallenges.Create(ctx, model.MFAChallenge{
		TokenID:   tokenID,
		UserID:    user.ID,
		Setup:     setup,
		ExpiresAt: expiresAt,
		CreatedIP: &createdIP,
	}); err != nil {
		return "", err
	}
	return auth.NewMFAChallengeTokenWithID(user.ID, user.Username, setup, tokenID, h.jwtSecret)
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
	if claims.ID == "" || h.mfaChallenges == nil {
		jsonErr(w, http.StatusUnauthorized, "invalid mfa challenge")
		return
	}
	if !h.allowMFAVerifyAttempt(w, r, claims) {
		return
	}
	challenge, err := h.mfaChallenges.GetByTokenID(r.Context(), claims.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "mfa challenge check failed")
		return
	}
	if !validMFAChallenge(challenge, claims) {
		jsonErr(w, http.StatusUnauthorized, "invalid mfa challenge")
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
	requiresMFA, err := h.requiresMFA(r.Context(), user)
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
		if err := h.mfaChallenges.RecordFailedAttempt(r.Context(), challenge.ID, mfaMaxAttempts); err != nil {
			jsonErr(w, http.StatusInternalServerError, "mfa challenge update failed")
			return
		}
		h.logAudit(r, repository.AuditEntry{
			ActorID:      &user.ID,
			ActorName:    user.Username,
			ActionType:   "mfa_failed",
			ResourceType: "auth",
		})
		jsonErr(w, http.StatusUnauthorized, "invalid mfa code")
		return
	}
	used, err := h.mfaChallenges.MarkUsed(r.Context(), challenge.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "mfa challenge update failed")
		return
	}
	if !used {
		jsonErr(w, http.StatusUnauthorized, "invalid mfa challenge")
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

func validMFAChallenge(challenge *model.MFAChallenge, claims *auth.MFAChallengeClaims) bool {
	if challenge == nil || claims == nil {
		return false
	}
	if challenge.UserID != claims.UserID || challenge.Setup != claims.Setup {
		return false
	}
	if challenge.UsedAt != nil || challenge.RevokedAt != nil {
		return false
	}
	return time.Now().Before(challenge.ExpiresAt)
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
	requiresMFA, err := h.requiresMFA(r.Context(), user)
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

func (h *AuthHandler) allowMFAVerifyAttempt(w http.ResponseWriter, r *http.Request, claims *auth.MFAChallengeClaims) bool {
	if h.mfaVerifyRateLimiter == nil || claims == nil {
		return true
	}
	now := time.Now()
	keys := []string{
		"ip:" + clientIP(r),
		fmt.Sprintf("user:%d", claims.UserID),
		"token:" + claims.ID,
	}
	for _, key := range keys {
		if h.mfaVerifyRateLimiter.Allow(key, now) {
			continue
		}
		h.logAudit(r, repository.AuditEntry{
			ActorID:      &claims.UserID,
			ActorName:    claims.Username,
			ActionType:   "auth_rate_limited",
			ResourceType: "auth",
			Details: map[string]any{
				"action": "mfa_verify",
			},
		})
		jsonErr(w, http.StatusTooManyRequests, "mfa verify rate limit exceeded")
		return false
	}
	return true
}

func (h *AuthHandler) requiresMFA(ctx context.Context, user *model.User) (bool, error) {
	if h.mfaEnforcement == MFAEnforcementDisabled {
		return false, nil
	}
	if h.mfaEnforcement == MFAEnforcementRequiredForAdmins {
		return h.users.RequiresMFA(ctx, user)
	}
	return false, nil
}

func normalizeMFAEnforcement(value string) MFAEnforcement {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(MFAEnforcementRequiredForAdmins):
		return MFAEnforcementRequiredForAdmins
	default:
		return MFAEnforcementDisabled
	}
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
