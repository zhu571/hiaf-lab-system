package ask

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// 非法 UUID 直接 404，不送 PG（防 500）。
func TestHistoryByID_InvalidUUID(t *testing.T) {
	middleware.SetJWTSecret([]byte("ask-test-secret"))
	token, err := middleware.GenerateToken("u1", "alice", "member", 0, []byte("ask-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(NewService(NewRepository(nil), nil)) // 非法 UUID 路径不触碰 DB

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ask/history/not-a-uuid", nil)
	req = req.WithContext(common.SetRequestID(req.Context(), "req-test"))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-a-uuid")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
		middleware.AuthRequired(http.HandlerFunc(h.HistoryByID)).ServeHTTP(w, r)
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid uuid, got %d: %s", rr.Code, rr.Body.String())
	}
}
