package middleware

import (
	"net/http"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserClaims(r.Context())
			if claims == nil || !allowed[claims.Role] {
				common.WriteError(w, r, http.StatusForbidden, "permission_denied", "权限不足", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
