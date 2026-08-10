package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// serviceCallRequest 构造被 ServiceToken 标记为 service call 的请求。
func serviceCallRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r = r.WithContext(common.SetRequestID(r.Context(), "test-id"))
	ctx := context.WithValue(r.Context(), serviceCallKey, true)
	return r.WithContext(ctx)
}

// ---------- CSRF：service call 豁免（方案 §4 改动点 2） ----------

func TestCSRF_ServiceCallExempt(t *testing.T) {
	handler := CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsServiceCall(r.Context()) {
			t.Fatal("expected service call context")
		}
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := serviceCallRequest(t, http.MethodPost, "/api/v1/alerts/report")
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("service call must bypass CSRF, got %d", w.Code)
	}
}

func TestCSRF_ServiceCallExemptOnlyMarkedPath(t *testing.T) {
	// 无标记（IsServiceCall=false）的用户 POST 仍强校验 CSRF。
	handler := CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/resolve", nil)
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("user POST must still require CSRF, got %d", w.Code)
	}
}

// ---------- 幂等：service call 豁免（方案 §4 改动点 3） ----------

func TestRequireIdempotencyKey_ServiceCallExempt(t *testing.T) {
	called := false
	handler := RequireIdempotencyKey(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := serviceCallRequest(t, http.MethodPost, "/api/v1/alerts/resolve")
	handler.ServeHTTP(w, r)
	if !called || w.Code != http.StatusOK {
		t.Fatalf("service call must bypass Idempotency-Key (no header), called=%v code=%d", called, w.Code)
	}
}

// ---------- RequireRoleOrService（方案 §4 改动点 4） ----------

func TestRequireRoleOrService(t *testing.T) {
	// service call → 放行（无 claims）。
	handler := RequireRoleOrService("admin", "maintainer")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := serviceCallRequest(t, http.MethodPost, "/api/v1/alerts/resolve")
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("service call must pass RequireRoleOrService, got %d", w.Code)
	}

	// 用户 member → 403（经 AuthRequired 注入 claims 后由 RequireRoleOrService 拒绝）。
	guard := func(next http.Handler) http.Handler {
		return RequireRoleOrService("admin", "maintainer")(next)
	}
	userStack := AuthRequired(guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	SetJWTSecret([]byte("guard-test-secret"))
	token, err := GenerateToken("u1", "alice", "member", 0, []byte("guard-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest(http.MethodPost, "/api/v1/alerts/resolve", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	userStack.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("member must be rejected, got %d", w.Code)
	}

	// 用户 admin → 200。
	token, err = GenerateToken("u2", "admin1", "admin", 0, []byte("guard-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest(http.MethodPost, "/api/v1/alerts/resolve", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	userStack.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("admin must pass RequireRoleOrService, got %d", w.Code)
	}
}

// ---------- ReportAlert 注入器（middleware → alert 不 import 环） ----------

func TestReportAlertInjection(t *testing.T) {
	var gotLevel, gotSource, gotTitle, gotDetail string
	var calls atomic.Int32
	SetAlertReporter(func(ctx context.Context, level, source, title, detail string) error {
		gotLevel, gotSource, gotTitle, gotDetail = level, source, title, detail
		calls.Add(1)
		return nil
	})
	defer SetAlertReporter(nil)

	ReportAlert("critical", "security", "SERVICE_TOKEN 校验失败", "来源 IP: 1.2.3.4")
	deadline := time.After(2 * time.Second)
	for calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("expected report within timeout")
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	if gotLevel != "critical" || gotSource != "security" || gotTitle == "" || gotDetail == "" {
		t.Fatalf("unexpected report args: %s/%s/%s/%s", gotLevel, gotSource, gotTitle, gotDetail)
	}

	// 未注入 → 静默。
	SetAlertReporter(nil)
	ReportAlert("critical", "security", "t", "d")
}
