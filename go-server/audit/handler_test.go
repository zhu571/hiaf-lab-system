package audit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

func setupJWT(t *testing.T) {
	t.Helper()
	middleware.JWTSecret = []byte("audit-handler-test-secret")
	middleware.TokenVersionValidator = nil
}

func tokenFor(t *testing.T, role string) string {
	t.Helper()
	tok, err := middleware.GenerateToken("00000000-0000-0000-0000-000000000001", "tester", role, 1, middleware.JWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// newTestRouter 复刻 main.go 的注册顺序与中间件：/verify、/events 先于 /{request_id}。
func newTestRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Route("/api/v1/audit", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Get("/verify", h.VerifyChain)
		r.Get("/events", h.ListEvents)
		r.Get("/{request_id}", h.GetByRequestID)
	})
	return r
}

func doGet(t *testing.T, router http.Handler, path, role string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if role != "" {
		req.Header.Set("Authorization", "Bearer "+tokenFor(t, role))
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAuditRoutesRequireAdminOrMaintainer(t *testing.T) {
	setupJWT(t)
	// svc 为 nil：鉴权拒绝必须发生在触达 service 之前。
	router := newTestRouter(NewHandler(nil))
	for _, path := range []string{"/api/v1/audit/verify", "/api/v1/audit/events", "/api/v1/audit/req_x"} {
		if rec := doGet(t, router, path, ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s 无 token: code = %d, want 401", path, rec.Code)
		}
		if rec := doGet(t, router, path, "viewer"); rec.Code != http.StatusForbidden {
			t.Fatalf("%s viewer: code = %d, want 403", path, rec.Code)
		}
	}
}

func TestVerifyChainParamValidation(t *testing.T) {
	setupJWT(t)
	router := newTestRouter(NewHandler(nil))
	for _, path := range []string{
		"/api/v1/audit/verify?from_id=abc",
		"/api/v1/audit/verify?to_id=-1",
		"/api/v1/audit/verify?from_id=9&to_id=3",
	} {
		if rec := doGet(t, router, path, "admin"); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: code = %d, want 400", path, rec.Code)
		}
	}
}

func TestListEventsParamValidation(t *testing.T) {
	setupJWT(t)
	router := newTestRouter(NewHandler(nil))
	for _, path := range []string{
		"/api/v1/audit/events?from=not-a-time",
		"/api/v1/audit/events?to=2026-13-99T00:00:00Z",
		"/api/v1/audit/events?user_id=not-a-uuid",
	} {
		if rec := doGet(t, router, path, "maintainer"); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: code = %d, want 400", path, rec.Code)
		}
	}
}

// TestStaticRoutesNotSwallowedByRequestID 路由顺序回归：/verify、/events 若被
// /{request_id} 吞掉，会以 "verify"/"events" 为 request_id 调用 nil service 直接 panic；
// 命中正确 handler 时非法参数返回 400。
func TestStaticRoutesNotSwallowedByRequestID(t *testing.T) {
	setupJWT(t)
	router := newTestRouter(NewHandler(nil))
	if rec := doGet(t, router, "/api/v1/audit/verify?from_id=abc", "admin"); rec.Code != http.StatusBadRequest {
		t.Fatalf("/verify?from_id=abc: code = %d, want 400", rec.Code)
	}
	if rec := doGet(t, router, "/api/v1/audit/events?from=bad", "admin"); rec.Code != http.StatusBadRequest {
		t.Fatalf("/events?from=bad: code = %d, want 400", rec.Code)
	}
}
