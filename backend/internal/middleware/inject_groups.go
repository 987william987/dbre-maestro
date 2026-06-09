package middleware

import (
	"context"
	"net/http"

	"github.com/dbre-maestro/maestro/internal/repository"
)

// InjectAuthGroups loads user's auth groups from DB and puts them in context.
// Must run after RequireAuth.
func InjectAuthGroups(userRepo *repository.UserRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := UserIDFromCtx(r.Context())
			if userID == 0 {
				next.ServeHTTP(w, r)
				return
			}
			groups, err := userRepo.GetAuthGroups(r.Context(), userID)
			if err != nil {
				http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
				return
			}
			ctx := context.WithValue(r.Context(), CtxAuthGroups, groups)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
