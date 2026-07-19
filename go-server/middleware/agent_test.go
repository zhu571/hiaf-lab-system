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
