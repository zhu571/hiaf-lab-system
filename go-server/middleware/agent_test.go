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
