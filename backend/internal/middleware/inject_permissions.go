package middleware

import (
	"context"
	"net/http"

	"github.com/dbre-maestro/maestro/internal/repository"
)

// InjectPermissions loads user's effective permissions from DB and puts them in context.
// Must run after RequireAuth.
func InjectPermissions(userRepo *repository.UserRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := UserIDFromCtx(r.Context())
			if userID == 0 {
				next.ServeHTTP(w, r)
				return
			}

			permissions, err := userRepo.GetEffectivePermissionKeys(r.Context(), userID)
			if err != nil {
				http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
				return
			}
			if permissions == nil {
				permissions = []string{}
			}

			ctx := context.WithValue(r.Context(), CtxPermissions, permissions)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
