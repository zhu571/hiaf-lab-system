package automation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

func setupJWT(t *testing.T) {
	t.Helper()
	middleware.JWTSecret = []byte("automation-handler-test-secret")
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

// newTestRouter 复刻 main.go 挂载：AuthRequired + RequireRole(admin)（Audit/幂等需 DB，由 db 测试覆盖）。
func newTestRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Route("/api/v1/admin/automation", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.RequireRole(auth.RoleAdmin))
		r.Mount("/", h.Routes())
	})
	return r
}

func doReq(t *testing.T, router http.Handler, method, path, role, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if role != "" {
		req.Header.Set("Authorization", "Bearer "+tokenFor(t, role))
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// 全部端点仅 admin 可访问（032 规则表影响 agent 入队链路，必须收紧）。
func TestRoutesRequireAdmin(t *testing.T) {
	setupJWT(t)
	router := newTestRouter(NewHandler(nil))
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/admin/automation/rules"},
		{http.MethodPost, "/api/v1/admin/automation/rules"},
		{http.MethodPatch, "/api/v1/admin/automation/rules/00000000-0000-0000-0000-000000000001"},
		{http.MethodDelete, "/api/v1/admin/automation/rules/00000000-0000-0000-0000-000000000001"},
	} {
		if rec := doReq(t, router, tc.method, tc.path, "", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s 无 token: code = %d, want 401", tc.method, tc.path, rec.Code)
		}
		if rec := doReq(t, router, tc.method, tc.path, "maintainer", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s maintainer: code = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
}

// service 校验失败的 400 映射（svc 无 repository：校验必须先于 DB 访问返回）。
func TestWriteValidationErrors(t *testing.T) {
	setupJWT(t)
	router := newTestRouter(NewHandler(NewService(nil)))

	if rec := doReq(t, router, http.MethodPost, "/api/v1/admin/automation/rules", "admin", `{bad json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST 非法 JSON: code = %d, want 400", rec.Code)
	}
	if rec := doReq(t, router, http.MethodPost, "/api/v1/admin/automation/rules", "admin",
		`{"name":"r","trigger_event":"issue.created","action":{"type":"enqueue_agent_task"}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST 非白名单事件: code = %d, want 400", rec.Code)
	}
	if rec := doReq(t, router, http.MethodPatch, "/api/v1/admin/automation/rules/00000000-0000-0000-0000-000000000001", "admin",
		`{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH 空 body: code = %d, want 400", rec.Code)
	}
}
