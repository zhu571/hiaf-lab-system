package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func TestRecovererReturnsUnified500(t *testing.T) {
	before := panicsTotal.Value()
	h := Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/panic", nil)
	req = req.WithContext(common.SetRequestID(req.Context(), "req-panic"))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	if payload.Error.Code != "internal_error" || payload.RequestID != "req-panic" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if panicsTotal.Value() != before+1 {
		t.Fatalf("panicsTotal = %d, want %d", panicsTotal.Value(), before+1)
	}
}

func TestRecovererPreservesWrittenResponse(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("partial"))
		panic("late boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/panic", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (must not rewrite after header)", rec.Code)
	}
	if rec.Body.String() != "partial" {
		t.Fatalf("body = %q, want partial", rec.Body.String())
	}
}
