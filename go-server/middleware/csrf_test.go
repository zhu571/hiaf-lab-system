package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRF_MatchingHeaderAndCookie(t *testing.T) {
	called := false
	handler := CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
	r.Header.Set("X-CSRF-Token", "abc123")
	handler.ServeHTTP(w, r)

	if !called {
		t.Fatal("expected request to pass CSRF check")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCSRF_MissingHeader(t *testing.T) {
	handler := CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called without CSRF header")
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestCSRF_MismatchedToken(t *testing.T) {
	handler := CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called with mismatched token")
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
	r.Header.Set("X-CSRF-Token", "different")
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestCSRF_SafeMethodsSkipCheck(t *testing.T) {
	handler := CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/v1/projects", nil)
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", method, w.Code)
		}
	}
}

func TestCSRF_AuthEndpointsSkipCheck(t *testing.T) {
	handler := CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	for _, path := range []string{
		"/api/v1/auth/login",
		"/api/v1/auth/refresh",
		"/api/v1/auth/register",
		"/api/v1/auth/logout",
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, nil)
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, w.Code)
		}
	}
}

// 回归：agent 豁免曾用 `len(path) >= 15 && path[:15] == "/api/v1/agent/"` 判断，
// 而 "/api/v1/agent/" 实际长度为 14，条件永假 → py-agent worker claim 持续 403 csrf_failed。
// R5 后豁免进一步收缩到 /api/v1/agent/tasks（服务账号任务 API）。
func TestCSRF_AgentPathsSkipCheck(t *testing.T) {
	handler := CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	for _, path := range []string{
		"/api/v1/agent/tasks/claim",
		"/api/v1/agent/tasks",
	} {
		w := httptest.NewRecorder()
		// 无 csrf_token cookie、无 X-CSRF-Token header，仍应放行。
		r := httptest.NewRequest(http.MethodPost, path, nil)
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, w.Code)
		}
	}
}

// R5：/api/v1/agent/candidates/{id}/approve|reject 是 admin/maintainer 人工端点
// （cookie 认证 + 会触发 AI 候选落库），不再随 /api/v1/agent/ 前缀整体豁免。
func TestCSRF_AgentCandidateEndpointsChecked(t *testing.T) {
	for _, path := range []string{
		"/api/v1/agent/candidates/11111111-1111-4111-8111-111111111111/approve",
		"/api/v1/agent/candidates/11111111-1111-4111-8111-111111111111/reject",
	} {
		t.Run(path, func(t *testing.T) {
			denied := CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("approve/reject without CSRF token must be rejected")
			}))
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, path, nil)
			r.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
			denied.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403 without CSRF header, got %d", w.Code)
			}

			allowed := CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			w = httptest.NewRecorder()
			r = httptest.NewRequest(http.MethodPost, path, nil)
			r.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
			r.Header.Set("X-CSRF-Token", "abc123")
			allowed.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 with valid CSRF header, got %d", w.Code)
			}
		})
	}
}
