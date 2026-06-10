package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/dbre-maestro/maestro/internal/auth"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type contextKey string

const (
	CtxUserID      contextKey = "user_id"
	CtxUsername    contextKey = "username"
	CtxPermissions contextKey = "permissions"
)

func RequireAuth(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearer(r)
			if token == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			claims, err := auth.ParseAccessToken(token, secret)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), CtxUserID, claims.UserID)
			ctx = context.WithValue(ctx, CtxUsername, claims.Username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireActiveUser(users *repository.UserRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := UserIDFromCtx(r.Context())
			if userID == 0 {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			user, err := users.GetByID(r.Context(), userID)
			if err != nil {
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			if user == nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if !user.IsActive {
				http.Error(w, `{"error":"user is disabled"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RequirePermission(permissionKeys ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(permissionKeys))
	for _, key := range permissionKeys {
		allowed[key] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, key := range PermissionsFromCtx(r.Context()) {
				if allowed[key] {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		})
	}
}

func UserIDFromCtx(ctx context.Context) uint64 {
	v, _ := ctx.Value(CtxUserID).(uint64)
	return v
}

func UsernameFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(CtxUsername).(string)
	return v
}

func PermissionsFromCtx(ctx context.Context) []string {
	v, _ := ctx.Value(CtxPermissions).([]string)
	return v
}

func HasPermission(ctx context.Context, permissionKeys ...string) bool {
	allowed := make(map[string]bool, len(permissionKeys))
	for _, key := range permissionKeys {
		allowed[key] = true
	}
	for _, key := range PermissionsFromCtx(ctx) {
		if allowed[key] {
			return true
		}
	}
	return false
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return after
	}
	return ""
}
