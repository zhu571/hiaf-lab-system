package common

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	ctx := SetRequestID(context.Background(), "test-uuid")
	r = r.WithContext(ctx)

	WriteSuccess(w, r, map[string]string{"key": "value"})

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.RequestID != "test-uuid" {
		t.Errorf("expected request_id test-uuid, got %s", body.RequestID)
	}
	if body.Data == nil {
		t.Error("expected non-nil data")
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	ctx := SetRequestID(context.Background(), "test-uuid")
	r = r.WithContext(ctx)

	WriteError(w, r, 401, "unauthorized", "bad token", map[string]any{"field": "token"})

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}

	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Error.Code != "unauthorized" {
		t.Errorf("expected code unauthorized, got %s", body.Error.Code)
	}
	if body.RequestID != "test-uuid" {
		t.Errorf("expected request_id test-uuid, got %s", body.RequestID)
	}
}
