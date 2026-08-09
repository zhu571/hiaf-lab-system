package middleware

import (
	"net/http"
	"os"
	"strings"
)

// CORSAllowedOrigins 返回 CORS 白名单：读环境变量 CORS_ALLOWED_ORIGINS（逗号分隔），
// 默认 vite dev 两个回环变体。生产同源（前端经 go:embed 同端口托管）无需配置；
// 独立域名/端口前端在 .env 追加。
func CORSAllowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		raw = "http://127.0.0.1:5173,http://localhost:5173"
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func originAllowed(origin string) bool {
	for _, o := range CORSAllowedOrigins() {
		if o == origin {
			return true
		}
	}
	return false
}

// CORS 白名单中间件：Origin 命中白名单 → 回显该 Origin + 凭据头；
// 未命中 / 无 Origin → 不设置 Access-Control-Allow-Origin（浏览器侧拦截响应，
// 服务端照常处理，不影响 curl / 内部工具）。OPTIONS 预检路径与 Max-Age 保持现状。
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-Id, X-CSRF-Token")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
