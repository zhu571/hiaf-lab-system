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

// RequireRoleOrService 双通道鉴权：service token 调用（claims==nil 且 IsServiceCall）
// → 放行（告警 resolve 内部恢复上报，先例：ask/execute 的 AuthRequired 放行）；
// 否则按 RequireRole 校验用户角色（前端手动 resolve 仅 admin/maintainer）。
// 现状 RequireRole 对 service call 直接 403，不能用于双通道端点。
func RequireRoleOrService(roles ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserClaims(r.Context())
			if claims == nil && IsServiceCall(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			if claims == nil || !allowed[claims.Role] {
				common.WriteError(w, r, http.StatusForbidden, "permission_denied", "权限不足", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
