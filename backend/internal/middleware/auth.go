package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/dbre-maestro/maestro/internal/auth"
	"github.com/dbre-maestro/maestro/internal/model"
)

type contextKey string

const (
	CtxUserID     contextKey = "user_id"
	CtxUsername   contextKey = "username"
	CtxAuthGroups contextKey = "auth_groups"
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

func RequireGroup(groups ...model.AuthGroup) func(http.Handler) http.Handler {
	allowed := make(map[model.AuthGroup]bool, len(groups))
	for _, g := range groups {
		allowed[g] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userGroups, _ := r.Context().Value(CtxAuthGroups).([]model.AuthGroup)
			for _, g := range userGroups {
				if allowed[g] {
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

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return after
	}
	return ""
}
