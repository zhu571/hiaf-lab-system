package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentContextRejectsDelegationHeadersFromUser(t *testing.T) {
	ctx := context.WithValue(context.Background(), userClaimsKey, &UserClaims{UserID: "user-1", Role: "member"})
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	req.Header.Set("X-Acting-User-ID", "user-2")
	rr := httptest.NewRecorder()

	AgentContext(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("request should have been rejected")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAgentBusinessPathAllowlist(t *testing.T) {
	allowed := agentBusinessPathAllowed(http.MethodPost, "/api/v1/projects/p1/issues")
	blocked := agentBusinessPathAllowed(http.MethodPost, "/api/v1/experiences/e1/publish")
	if !allowed || blocked {
		t.Fatalf("allowlist mismatch: allowed=%v blocked=%v", allowed, blocked)
	}
}

func TestAgentCannotAiParseDailyReport(t *testing.T) {
	if agentBusinessPathAllowed(http.MethodPost, "/api/v1/daily-reports/report_1/ai-parse") {
		t.Fatal("agent must not be allowed to POST /api/v1/daily-reports/{id}/ai-parse")
	}
}

// C10 递归防护：agent 写日报（PATCH /api/v1/daily-reports/{id}）必须被显式拒绝，
// 防止未来白名单放开形成 agent → daily_reports → 触发器 → agent 的自触发回路。
func TestAgentCannotPatchDailyReport(t *testing.T) {
	const reportURL = "/api/v1/daily-reports/00000000-0000-0000-0000-000000000001"
	if agentBusinessPathAllowed(http.MethodPatch, reportURL) {
		t.Fatal("agent must not be allowed to PATCH /api/v1/daily-reports/{id}")
	}
	// 中间件级验证：路径拒绝发生在任务校验（需 DB）之前，AgentContext(nil) 即可。
	ctx := context.WithValue(context.Background(), userClaimsKey, &UserClaims{UserID: "agent-1", Role: "agent"})
	req := httptest.NewRequest(http.MethodPatch, reportURL, nil).WithContext(ctx)
	req.Header.Set("X-Acting-User-ID", "user-1")
	req.Header.Set("X-Agent-Task-ID", "task-1")
	rr := httptest.NewRecorder()

	AgentContext(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("agent PATCH daily-reports should have been rejected")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAgentContextSkipsServiceCall(t *testing.T) {
	// service token 调用（by-date 白名单）无 JWT claims，AgentContext 必须放行，
	// 否则生产链路 AuthRequired→AgentContext 会把 scheduler 的日报拉取挡成 401。
	old := serviceToken
	defer func() { serviceToken = old }()
	SetServiceToken("svc-secret")

	stack := ServiceToken()(AgentContext(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsServiceCall(r.Context()) {
			t.Fatal("expected service call marker to survive AgentContext")
		}
		w.WriteHeader(http.StatusOK)
	})))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date?user_id=u1", nil)
	r.Header.Set("Authorization", "Bearer svc-secret")
	stack.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("service call must pass AgentContext, got %d", w.Code)
	}
}
