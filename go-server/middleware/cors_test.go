package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// CORS 白名单行为（P1-4）：无 Origin / 白名单内 / 白名单外 / OPTIONS 预检四路径。
// t.Setenv 固定白名单，避免受运行环境 CORS_ALLOWED_ORIGINS 影响。

func corsServe(origin, method string) *httptest.ResponseRecorder {
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/api/v1/projects", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	handler.ServeHTTP(w, r)
	return w
}

func TestCORSNoOriginNoACAO(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://127.0.0.1:5173,http://localhost:5173")
	w := corsServe("", http.MethodGet)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("no Origin: expected no ACAO header, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("no Origin: expected no credentials header, got %q", got)
	}
}

func TestCORSAllowedOriginEchoes(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://127.0.0.1:5173,http://localhost:5173")
	for _, origin := range []string{"http://127.0.0.1:5173", "http://localhost:5173"} {
		w := corsServe(origin, http.MethodGet)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("origin %q: ACAO = %q, want echo", origin, got)
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Fatalf("origin %q: expected credentials true, got %q", origin, got)
		}
	}
}

func TestCORSDisallowedOriginNoACAO(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://127.0.0.1:5173,http://localhost:5173")
	w := corsServe("http://evil.example", http.MethodGet)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("evil origin: expected no ACAO header, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("evil origin: expected no credentials header, got %q", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://127.0.0.1:5173,http://localhost:5173")
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("preflight must not reach handler")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/api/v1/projects", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("preflight ACAO = %q, want echo", got)
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got == "" {
		t.Fatal("preflight must keep Access-Control-Max-Age")
	}
}

func TestCORSAllowedOriginsFromEnv(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://lab.example.com, http://192.168.1.10:3000")
	w := corsServe("https://lab.example.com", http.MethodGet)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://lab.example.com" {
		t.Fatalf("env-configured origin: ACAO = %q, want echo", got)
	}
	w = corsServe("http://localhost:5173", http.MethodGet)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("env 覆盖默认白名单后 localhost:5173 不应放行，got %q", got)
	}
}
