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
