package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// gateTest 构造带来源门中间件的处理器，返回捕获的规范化来源 IP、来源分类与 XFF 是否保留。
func gateTest(t *testing.T, secret, enabled, remoteAddr, proxyHeader, xff, method, path string) (*httptest.ResponseRecorder, string, string, string) {
	t.Helper()
	t.Setenv("LAB_PROXY_SHARED_SECRET", secret)
	t.Setenv("SOURCE_GATE_ENABLED", enabled)
	var gotIP, gotKind, gotXFF string
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remoteAddr
	if proxyHeader != "" {
		req.Header.Set("X-Lab-Proxy", proxyHeader)
	}
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rr := httptest.NewRecorder()
	SourceGate()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP = GetSourceIP(r.Context())
		gotKind = GetSourceKind(r.Context())
		gotXFF = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)
	return rr, gotIP, gotKind, gotXFF
}

// 三场景之一：Caddy peer + 秘密头匹配 = 公网入口（proxy），信任 XFF，白名单写放行。
func TestSourceGateCaddyPeerSecretMatchTrustsXFF(t *testing.T) {
	rr, ip, kind, xff := gateTest(t, "s3cret-value", "true", "10.144.144.100:50000", "s3cret-value", "203.0.113.7", http.MethodPost, "/api/v1/logs/l1/ai-parse")
	if rr.Code != http.StatusOK {
		t.Fatalf("whitelisted POST via trusted proxy: got %d", rr.Code)
	}
	if ip != "203.0.113.7" {
		t.Errorf("source IP = %q, want XFF 203.0.113.7", ip)
	}
	if kind != "proxy" {
		t.Errorf("source kind = %q, want proxy", kind)
	}
	if xff != "203.0.113.7" {
		t.Errorf("XFF must be preserved for trusted proxy, got %q", xff)
	}
}

// 三场景之二：Caddy peer + 秘密头不匹配 = 可疑入口，一律 403 source_gate_rejected；
// 且 next 不被调用（来源门在最外层，请求在到达 RequestLogger 前即终止，无日志条目不落）。
func TestSourceGateCaddyPeerSecretMismatchRejected(t *testing.T) {
	rr, _, gotKind, _ := gateTest(t, "s3cret-value", "true", "10.144.144.100:50000", "wrong", "203.0.113.7", http.MethodGet, "/api/v1/logs")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("mismatched secret: got %d, want 403", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "source_gate_rejected") {
		t.Errorf("expected source_gate_rejected, body: %s", rr.Body.String())
	}
	if gotKind != "" {
		t.Errorf("rejected request must not reach inner handler (source_kind not propagated), got %q", gotKind)
	}
}

// 三场景之三：内网 peer 剥除伪造 XFF，写操作全量放行。
func TestSourceGateInternalPeerStripsXFF(t *testing.T) {
	rr, ip, kind, xff := gateTest(t, "s3cret-value", "true", "10.144.144.12:8000", "", "10.144.144.100", http.MethodPost, "/api/v1/instruments/i1/commands")
	if rr.Code != http.StatusOK {
		t.Fatalf("internal write: got %d, want 200", rr.Code)
	}
	if ip != "10.144.144.12" {
		t.Errorf("source IP = %q, want peer 10.144.144.12", ip)
	}
	if kind != "internal" {
		t.Errorf("source kind = %q, want internal", kind)
	}
	if xff != "" {
		t.Errorf("internal peer XFF must be stripped, got %q", xff)
	}
}

// XFF 伪造：公网直连带伪造内网 XFF，来源 IP 仍为真实 peer，且非白名单写被拒。
func TestSourceGatePublicForgedXFFCannotBypass(t *testing.T) {
	// 非白名单写被拒（伪造内网 XFF 不能放行）。
	rr, _, _, _ := gateTest(t, "s3cret-value", "true", "203.0.113.7:40000", "", "10.144.144.100", http.MethodPost, "/api/v1/instruments/i1/commands")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("public non-whitelisted write: got %d, want 403", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "source_gate_denied") {
		t.Errorf("expected source_gate_denied, body: %s", rr.Body.String())
	}

	// 白名单放行时来源 IP 必须是真实 peer（伪造 XFF 被忽略，不能冒充内网）。
	rr2, ip2, kind2, _ := gateTest(t, "s3cret-value", "true", "203.0.113.7:40000", "", "10.144.144.100", http.MethodPost, "/api/v1/auth/login")
	if rr2.Code != http.StatusOK {
		t.Fatalf("public whitelisted POST login: got %d, want 200", rr2.Code)
	}
	if ip2 != "203.0.113.7" {
		t.Errorf("login source IP = %q, want real peer (XFF ignored)", ip2)
	}
	if kind2 != "public" {
		t.Errorf("source kind = %q, want public", kind2)
	}
}

// SOURCE_GATE_ENABLED=false：完全放行（回滚开关），仍保留来源 IP 规范化与分类写入 ctx。
func TestSourceGateDisabledRollsBack(t *testing.T) {
	rr, ip, kind, _ := gateTest(t, "s3cret-value", "false", "203.0.113.7:40000", "", "1.2.3.4", http.MethodPost, "/api/v1/instruments/i1/commands")
	if rr.Code != http.StatusOK {
		t.Fatalf("disabled gate must fully pass, got %d", rr.Code)
	}
	if ip != "203.0.113.7" {
		t.Errorf("source IP = %q, want peer (XFF ignored)", ip)
	}
	if kind != "public" {
		t.Errorf("source kind = %q, want public", kind)
	}
}

// 默认 true 且缺 secret：除内网外全部拒绝。
func TestSourceGateMissingSecretRejectsNonInternal(t *testing.T) {
	rr, _, _, _ := gateTest(t, "", "true", "203.0.113.7:40000", "", "", http.MethodPost, "/api/v1/auth/login")
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "source_gate_rejected") {
		t.Fatalf("public without secret: got %d %s, want 403 source_gate_rejected", rr.Code, rr.Body.String())
	}
	rr2, _, kind2, _ := gateTest(t, "", "true", "10.144.144.12:8000", "", "1.2.3.4", http.MethodPost, "/api/v1/instruments/i1/commands")
	if rr2.Code != http.StatusOK {
		t.Fatalf("internal without secret must pass, got %d", rr2.Code)
	}
	if kind2 != "internal" {
		t.Errorf("source kind = %q, want internal", kind2)
	}
}

// GetSourceKind 未经过来源门时返回空串（与 GetSourceIP 约定一致）。
func TestGetSourceKindMissing(t *testing.T) {
	if kind := GetSourceKind(context.Background()); kind != "" {
		t.Errorf("GetSourceKind without gate = %q, want empty", kind)
	}
}

// sourceKind.String 覆盖全部枚举并兜底 unknown（日志字段值稳定）。
func TestSourceKindString(t *testing.T) {
	cases := map[sourceKind]string{
		sourceKindProxy:      "proxy",
		sourceKindSuspicious: "suspicious",
		sourceKindInternal:   "internal",
		sourceKindPublic:     "public",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("String(%d) = %q, want %q", k, got, want)
		}
	}
	if got := (sourceKind)(99).String(); got != "unknown" {
		t.Errorf("String(unknown) = %q, want unknown", got)
	}
}

// 公网写路径白名单矩阵（含急停显式例外与 GET 放行）。
func TestSourceWriteAllowedMatrix(t *testing.T) {
	allowed := []string{
		http.MethodGet, http.MethodHead, http.MethodOptions,
		"/api/v1/auth/login", "/api/v1/auth/refresh", "/api/v1/auth/register",
		"/api/v1/ask/chat",
		"/api/v1/instruments/i1/nl-commands",
		"/api/v1/instruments/i1/parse-result",
		"/api/v1/instruments/i1/emergency-stop",
		"/api/v1/logs/l1/ai-parse",
	}
	for _, m := range allowed[:3] {
		if !sourceWriteAllowed(m, "/api/v1/whatever") {
			t.Errorf("method %s must always pass", m)
		}
	}
	for _, p := range allowed[3:] {
		if !sourceWriteAllowed(http.MethodPost, p) {
			t.Errorf("whitelisted POST %s must pass", p)
		}
	}
	denied := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/instruments/i1/commands"},
		{http.MethodPost, "/api/v1/instruments/i1/nl-execute"},
		{http.MethodPost, "/api/v1/logs/l1/ai-parse-x"},
		{http.MethodPost, "/api/v1/projects"},
		{http.MethodPatch, "/api/v1/projects/p1"},
		{http.MethodPut, "/api/v1/instruments/i1/safety/a5-max"},
		{http.MethodDelete, "/api/v1/attachments/a1"},
		{http.MethodPost, "/api/v1/instruments/i1/emergency-stop/extra"},
	}
	for _, d := range denied {
		if sourceWriteAllowed(d.method, d.path) {
			t.Errorf("%s %s must be denied from public", d.method, d.path)
		}
	}
}
