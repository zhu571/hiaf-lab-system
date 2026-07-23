package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func TestRequestID_GeneratesUUID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := common.GetRequestID(r.Context())
		if id == "" {
			t.Error("expected non-empty request_id")
		}
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(w, r)

	if w.Header().Get("X-Request-Id") == "" {
		t.Error("expected X-Request-Id header")
	}
}

func TestRequestID_PassThrough(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := common.GetRequestID(r.Context())
		if id != "my-custom-id" {
			t.Errorf("expected 'my-custom-id', got %s", id)
		}
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-Id", "my-custom-id")
	handler.ServeHTTP(w, r)

	if w.Header().Get("X-Request-Id") != "my-custom-id" {
		t.Errorf("expected X-Request-Id 'my-custom-id', got %s", w.Header().Get("X-Request-Id"))
	}
}
