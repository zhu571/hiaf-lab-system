package middleware

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

type serviceCallKeyType string

const serviceCallKey serviceCallKeyType = "service_call"

// serviceToken 在 main 启动时注入（SERVICE_TOKEN_FILE 优先，回退 SERVICE_TOKEN env）。
var serviceToken string

// SetServiceToken 设置内部服务 token（等价于 py-agent 的 PY_AGENT_INTERNAL_TOKEN_FILE 机制）。
func SetServiceToken(token string) {
	serviceToken = strings.TrimSpace(token)
}

// ServiceToken 校验白名单路径上的 SERVICE_TOKEN。白名单按方法分条件匹配：
// GET /api/v1/daily-reports/by-date（拉全量用户日报是敏感操作，scope 收敛）、
// POST /api/v1/ask/execute（AI 智能查询内部只读执行端点，见 ask 模块）与
// POST /api/v1/alerts/report、/api/v1/alerts/resolve（告警中心写端点，见 alert 模块）。
// 命中且正确 → ctx 标记 serviceCallKey 并跳过 JWT；非白名单路径直接放行（不消费 token）；
// 白名单上 token 错误 → 401 + 泄露告警。
func ServiceToken() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !((r.Method == http.MethodGet && r.URL.Path == "/api/v1/daily-reports/by-date") ||
				(r.Method == http.MethodPost && r.URL.Path == "/api/v1/ask/execute") ||
				(r.Method == http.MethodPost && r.URL.Path == "/api/v1/alerts/report") ||
				(r.Method == http.MethodPost && r.URL.Path == "/api/v1/alerts/resolve")) {
				next.ServeHTTP(w, r)
				return
			}
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}
			header = strings.TrimPrefix(header, "Bearer ")
			if header == "" {
				next.ServeHTTP(w, r)
				return
			}
			// 前端 by-date 也带普通 JWT（3 段点分）。形态区分：JWT 交给 AuthRequired
			// 正常鉴权（忽略 user_id 强制取自己），只有非 JWT 才按 service token 校验，
			// 避免误伤用户请求 / 对过期用户 JWT 误报安全告警。
			if looksLikeJWT(header) {
				next.ServeHTTP(w, r)
				return
			}
			if serviceToken == "" || subtle.ConstantTimeCompare([]byte(header), []byte(serviceToken)) != 1 {
				slog.Error("service token rejected", "ip", r.RemoteAddr, "path", r.URL.Path)
				// 校验失败属安全事件：收敛到告警中心（原 notify.SecurityAlert 直发，
				// 改走 alert 模块统一聚合去重，critical/security 双通道不变）。
				ReportAlert("critical", "security", "SERVICE_TOKEN 校验失败", "来源 IP: "+r.RemoteAddr)
				common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "service token 无效", nil)
				return
			}
			ctx := context.WithValue(r.Context(), serviceCallKey, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// looksLikeJWT 判断 Bearer 是否为 JWT 形态（3 段 base64url，点分）。
// service token 由 openssl rand -hex 32 生成（无点号），两者天然可区分。
func looksLikeJWT(s string) bool {
	parts := strings.Split(s, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// IsServiceCall 报告请求是否经由 service token 鉴权通过。
func IsServiceCall(ctx context.Context) bool {
	ok, _ := ctx.Value(serviceCallKey).(bool)
	return ok
}

// ReadServiceToken 读取 SERVICE_TOKEN（文件优先），与 main 的注入逻辑一致。
func ReadServiceToken() string {
	if v := serviceToken; v != "" {
		return v
	}
	if file := os.Getenv("SERVICE_TOKEN_FILE"); file != "" {
		if data, err := os.ReadFile(filepath.Clean(file)); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return strings.TrimSpace(os.Getenv("SERVICE_TOKEN"))
}
