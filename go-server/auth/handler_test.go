package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginRouteUsesAuditMiddleware(t *testing.T) {
	audited := false
	audit := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			audited = true
			next.ServeHTTP(w, r)
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	NewHandler(nil).Routes(audit).ServeHTTP(httptest.NewRecorder(), req)
	if !audited {
		t.Fatal("login route did not use audit middleware")
	}
}

// CSRF cookie 必须 Path=/ 且非 HttpOnly，否则前端页面（路径不在 /api 下）
// 无法通过 document.cookie 读取它来恢复 X-CSRF-Token header。
func TestSetCSRFCookieIsReadableFromPages(t *testing.T) {
	w := httptest.NewRecorder()
	token := setCSRFCookie(w, false)
	if token == "" {
		t.Fatal("expected non-empty csrf token")
	}

	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "csrf_token" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("csrf_token cookie not set")
	}
	if cookie.Value != token {
		t.Errorf("cookie value %q does not match returned token %q", cookie.Value, token)
	}
	if cookie.Path != "/" {
		t.Errorf("expected cookie Path=/, got %q", cookie.Path)
	}
	if cookie.HttpOnly {
		t.Error("csrf_token cookie must not be HttpOnly")
	}
}

// 未携带 access token 的请求必须被 AuthRequired 拦截（svc 为 nil 也不会被触达）。
func TestUpdateProfileRequiresAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/profile", nil)
	rec := httptest.NewRecorder()
	NewHandler(nil).Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated /profile, got %d", rec.Code)
	}
}

// --- 公网开放安全加固（S1/S2）---

// S2：ALLOW_REGISTER 默认关闭 → 注册入口 403 registration_disabled。
func TestRegisterDisabledByDefault(t *testing.T) {
	t.Setenv("ALLOW_REGISTER", "false")
	req := httptest.NewRequest(http.MethodPost, "/register", nil)
	rr := httptest.NewRecorder()
	NewHandler(nil).Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("register disabled: got %d, want 403", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "registration_disabled") {
		t.Errorf("expected registration_disabled, body: %s", rr.Body.String())
	}
}

// S2：注册限流收紧为 5 次/小时/IP。
func TestAllowRegisterIPFivePerHour(t *testing.T) {
	h := NewHandler(nil)
	ip := "10.0.0.9"
	for i := 0; i < 5; i++ {
		if !h.allowRegisterIP(ip) {
			t.Fatalf("register call %d should pass", i+1)
		}
	}
	if h.allowRegisterIP(ip) {
		t.Fatal("6th register within window should be rejected")
	}
	if !h.allowRegisterIP("10.0.0.10") {
		t.Fatal("other IP should pass")
	}
}

// S1：IP 级滑动窗口——计数、窗口滑动、不同 IP 独立。
func TestAllowLoginIPSlidingWindow(t *testing.T) {
	t.Setenv("LOGIN_RATE_LIMIT_IP_MAX", "3")
	t.Setenv("LOGIN_RATE_LIMIT_IP_WINDOW", "15m")
	now := time.Now()
	h := NewHandler(nil)
	h.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !h.allowLoginIP("10.0.0.1") {
			t.Fatalf("login call %d should pass", i+1)
		}
	}
	if h.allowLoginIP("10.0.0.1") {
		t.Fatal("4th login within window should be rejected")
	}
	if !h.allowLoginIP("10.0.0.2") {
		t.Fatal("different IP should pass")
	}
	now = now.Add(16 * time.Minute)
	if !h.allowLoginIP("10.0.0.1") {
		t.Fatal("login after window should pass (old calls expired)")
	}
}

// S1：LOGIN_RATE_LIMIT_IP_MAX=0 关闭 IP 级限流。
func TestAllowLoginIPDisabledWhenMaxZero(t *testing.T) {
	t.Setenv("LOGIN_RATE_LIMIT_IP_MAX", "0")
	h := NewHandler(nil)
	for i := 0; i < 30; i++ {
		if !h.allowLoginIP("10.0.0.1") {
			t.Fatalf("call %d should pass when disabled", i+1)
		}
	}
}

// S1：IP 级限流键仅 IP（跨用户名聚合）——同 IP 不同用户名共享计数。
func TestAllowLoginIPAggregatesAcrossUsernames(t *testing.T) {
	t.Setenv("LOGIN_RATE_LIMIT_IP_MAX", "2")
	h := NewHandler(nil)
	if !h.allowLoginIP("10.0.0.1") || !h.allowLoginIP("10.0.0.1") {
		t.Fatal("first two login calls should pass")
	}
	// 换用户名但同 IP：计数仍共享（防公网跨用户名刷号绕过）。
	if h.allowLoginIP("10.0.0.1") {
		t.Fatal("3rd login from same IP must be rejected regardless of username")
	}
}
