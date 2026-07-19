package middleware

import (
	"net/http"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/refresh" || r.URL.Path == "/api/v1/auth/register" || r.URL.Path == "/api/v1/auth/logout" {
			next.ServeHTTP(w, r)
			return
		}
		// Agent 服务账号 API 无需 CSRF（有 JWT + acting-user 认证链）
		if len(r.URL.Path) >= 15 && r.URL.Path[:15] == "/api/v1/agent/" {
			next.ServeHTTP(w, r)
			return
		}

		header := r.Header.Get("X-CSRF-Token")
		cookie, err := r.Cookie("csrf_token")
		if err != nil || header == "" || header != cookie.Value {
			common.WriteError(w, r, http.StatusForbidden, "csrf_failed", "CSRF token 无效", nil)
			return
		}

		next.ServeHTTP(w, r)
	})
}
