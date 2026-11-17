package electrical

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSingleShotCompleteTimeoutMapsToGatewayTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Devices = nil
	cfg.SingleShotTimeout = 20 * time.Millisecond
	svc := NewService(cfg, nil)
	svc.running = true
	svc.runCtx = context.Background()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/electrical/single-shot", nil)
	NewHandler(svc).SingleShot(recorder, request)
	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("single-shot timeout status = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode timeout response: %v", err)
	}
	if body.Error.Code != "acquisition_timeout" {
		t.Fatalf("single-shot timeout code = %q, want acquisition_timeout", body.Error.Code)
	}
}
