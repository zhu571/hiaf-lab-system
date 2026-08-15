package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		// service token 调用（告警 report/resolve 等白名单端点）无 CSRF cookie，
		// 由 ServiceToken 中间件鉴权（先例：ask/execute）；IsServiceCall 只在
		// 白名单路径为 true，用户 JWT 通道仍强校验 CSRF。
		if IsServiceCall(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/refresh" || r.URL.Path == "/api/v1/auth/register" || r.URL.Path == "/api/v1/auth/logout" {
			next.ServeHTTP(w, r)
			return
		}
		// Agent 服务账号任务 API 无需 CSRF（有 JWT + acting-user 认证链，无 cookie 可用）。
		// 豁免只覆盖 /api/v1/agent/tasks：candidates approve/reject 是 admin/maintainer
		// 的人工端点（cookie 认证），必须纳入 CSRF 检查，防止跨站触发 AI 候选落库。
		if strings.HasPrefix(r.URL.Path, "/api/v1/agent/tasks/") || r.URL.Path == "/api/v1/agent/tasks" {
			next.ServeHTTP(w, r)
			return
		}
		// ask/execute 由 SERVICE_TOKEN 鉴权（无 CSRF cookie，见 service_token.go 白名单）
		if r.URL.Path == "/api/v1/ask/execute" {
			next.ServeHTTP(w, r)
			return
		}

		header := r.Header.Get("X-CSRF-Token")
		cookie, err := r.Cookie("csrf_token")
		// 恒定时间比较（对齐 service_token / ask 等仓库其他三处先例），防时序侧信道。
		if err != nil || header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
			common.WriteError(w, r, http.StatusForbidden, "csrf_failed", "CSRF token 无效", nil)
			return
		}

		next.ServeHTTP(w, r)
	})
}
