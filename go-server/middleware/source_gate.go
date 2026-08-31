package middleware

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

type sourceIPKeyType string

const sourceIPKey sourceIPKeyType = "source_ip"

type sourceKindKeyType string

const sourceKindKey sourceKindKeyType = "source_kind"

// 网段 / Caddy 虚拟 IP 常量（可 env 覆盖，见 SourceGate）。
const (
	defaultSourceGateLANCIDR   = "10.144.144.0/24"
	defaultSourceGateProxyHost = "10.144.144.100"
)

type sourceKind int

const (
	sourceKindProxy      sourceKind = iota // Caddy 反代入口（秘密头匹配，信任 XFF）
	sourceKindSuspicious                   // Caddy peer 但秘密头不匹配 → 一律 403
	sourceKindInternal                     // 内网直连
	sourceKindPublic                       // 其余（公网直连）
)

// SourceGate 判定请求来源并规范化来源 IP（全局唯一读取点 GetSourceIP）。
//
// 判定状态机（peer = TCP 对端 r.RemoteAddr 的 host，在任何 XFF 读取之前取数）：
//   - peer == Caddy 且 X-Lab-Proxy == LAB_PROXY_SHARED_SECRET → 公网入口，信任 XFF
//   - peer == Caddy 且秘密头不匹配 → 可疑入口，403 source_gate_rejected
//   - 网段内且 ≠ Caddy → 内网直连，剥除 XFF（防内网伪造），全量写权限
//   - 其余 → 公网，忽略 XFF，仅路径白名单可写
//
// 必须注册在 chi RealIP 之前：RealIP 会改写 r.RemoteAddr，先于它取数才能拿到真实 TCP 对端。
// SOURCE_GATE_ENABLED=false 完全放行（回滚开关），仅保留来源 IP 规范化与内网 XFF 剥除；
// 默认 true 且未配置 LAB_PROXY_SHARED_SECRET 时，除内网外全部拒绝（安全默认）。
// parseLanCIDRs 解析逗号分隔的 LAN CIDR 列表（服务间 docker 网段等可并列内网）。
func parseLanCIDRs(raw string) []*net.IPNet {
	var out []*net.IPNet
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(part); err == nil {
			out = append(out, n)
		}
	}
	return out // 配置错误项跳过；全空 = 无内网（安全默认：其余全按公网白名单约束）
}

func SourceGate() func(http.Handler) http.Handler {
	lanNets := parseLanCIDRs(envString("SOURCE_GATE_LAN_CIDR", defaultSourceGateLANCIDR))
	proxyIP := net.ParseIP(envString("SOURCE_GATE_PROXY_HOST", defaultSourceGateProxyHost))
	secret := os.Getenv("LAB_PROXY_SHARED_SECRET")
	enabled := os.Getenv("SOURCE_GATE_ENABLED") != "false"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := hostOnly(r.RemoteAddr)
			kind, srcIP := classifySource(peer, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Lab-Proxy"), proxyIP, lanNets, secret)
			if kind == sourceKindInternal {
				// 内网直连剥除代理头：chi RealIP 只能看到真实 RemoteAddr。
				r.Header.Del("X-Forwarded-For")
				r.Header.Del("X-Real-IP")
				r.Header.Del("Cf-Connecting-Ip")
				r.Header.Del("True-Client-IP")
				r.Header.Del("X-Client-IP")
			}
			ctx := context.WithValue(r.Context(), sourceIPKey, srcIP)
			ctx = context.WithValue(ctx, sourceKindKey, kind.String())
			if !enabled {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			switch kind {
			case sourceKindSuspicious:
				common.WriteError(w, r, http.StatusForbidden, "source_gate_rejected", "来源校验失败，请求被拒绝", nil)
				return
			case sourceKindProxy, sourceKindPublic:
				if secret == "" {
					// 未配置共享秘密：除内网外全部拒绝（安全默认，防止误放公网写）。
					common.WriteError(w, r, http.StatusForbidden, "source_gate_rejected", "来源门未配置共享秘密，非内网请求被拒绝", nil)
					return
				}
				if !sourceWriteAllowed(r.Method, r.URL.Path) {
					common.WriteError(w, r, http.StatusForbidden, "source_gate_denied", "公网来源禁止该写操作", nil)
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// classifySource 按状态机判定来源并返回规范化来源 IP。
func classifySource(peer, xff, secretHeader string, proxyIP net.IP, lanNets []*net.IPNet, secret string) (sourceKind, string) {
	peerIP := net.ParseIP(peer)
	if proxyIP != nil && peerIP != nil && proxyIP.Equal(peerIP) {
		if secret != "" && subtle.ConstantTimeCompare([]byte(secretHeader), []byte(secret)) == 1 {
			// 信任 XFF（Caddy 已删旧值并覆写为唯一真实值）；空 XFF 回退 peer。
			if ip := firstXFF(xff); ip != "" {
				return sourceKindProxy, ip
			}
			return sourceKindProxy, peer
		}
		return sourceKindSuspicious, peer
	}
	if inAnyLAN(peerIP, lanNets) {
		return sourceKindInternal, peer
	}
	return sourceKindPublic, peer
}

// inAnyLAN 判定 peerIP 是否落在任一内网网段。
func inAnyLAN(peerIP net.IP, lanNets []*net.IPNet) bool {
	if peerIP == nil {
		return false
	}
	for _, n := range lanNets {
		if n.Contains(peerIP) {
			return true
		}
	}
	return false
}

// String 返回来源分类的日志字段值（与 GetSourceKind 约定一致）。
func (k sourceKind) String() string {
	switch k {
	case sourceKindProxy:
		return "proxy"
	case sourceKindSuspicious:
		return "suspicious"
	case sourceKindInternal:
		return "internal"
	case sourceKindPublic:
		return "public"
	}
	return "unknown"
}

// sourceWriteAllowed 公网写路径白名单（不按 HTTP 方法一刀切）：
// AI 只读辅助端点 + 认证端点 + 急停显式例外（D7）放行，其余写一律拒。
func sourceWriteAllowed(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	switch {
	case path == "/api/v1/auth/login",
		path == "/api/v1/auth/refresh",
		path == "/api/v1/auth/register",
		path == "/api/v1/auth/logout",
		path == "/api/v1/auth/change-password",
		path == "/api/v1/auth/profile",
		path == "/api/v1/ask/chat",
		path == "/api/v1/attachments":
		return true
	case strings.HasPrefix(path, "/api/v1/instruments/") && strings.HasSuffix(path, "/nl-commands"),
		strings.HasPrefix(path, "/api/v1/instruments/") && strings.HasSuffix(path, "/parse-result"),
		strings.HasPrefix(path, "/api/v1/instruments/") && strings.HasSuffix(path, "/emergency-stop"),
		strings.HasPrefix(path, "/api/v1/logs/") && strings.HasSuffix(path, "/ai-parse"):
		return true
	}
	return false
}

func firstXFF(v string) string {
	if v == "" {
		return ""
	}
	if first := strings.TrimSpace(strings.Split(v, ",")[0]); first != "" {
		return first
	}
	return ""
}

// GetSourceIP 返回来源门规范化的来源 IP（全局唯一读取点；未经过来源门返回空串）。
func GetSourceIP(ctx context.Context) string {
	ip, _ := ctx.Value(sourceIPKey).(string)
	return ip
}

// GetSourceKind 返回来源门判定的来源分类（proxy/suspicious/internal/public；未经过来源门返回空串）。
func GetSourceKind(ctx context.Context) string {
	kind, _ := ctx.Value(sourceKindKey).(string)
	return kind
}

// requestSourceIP 供 middleware 内部读取：优先来源门 IP，缺失（未挂载/单测）回退真实 TCP 对端。
func requestSourceIP(r *http.Request) string {
	if ip := GetSourceIP(r.Context()); ip != "" {
		return ip
	}
	return hostOnly(r.RemoteAddr)
}

func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
