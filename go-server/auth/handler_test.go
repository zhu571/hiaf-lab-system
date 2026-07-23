package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
