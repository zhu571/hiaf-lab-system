package sensors

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newHandlerWithServer(t *testing.T, csv string) (*Handler, *httptest.Server) {
	t.Helper()
	server := mockInflux(t, 200, csv, nil)
	svc := newMockSvc(t, server.URL)
	return NewHandler(svc), server
}

func TestHandlerLatest(t *testing.T) {
	h, server := newHandlerWithServer(t, "_time,tag,_value\n2026-01-01T00:00:00Z,pressure,1.5\n")
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sensors/latest?tags=pressure", nil)
	rec := httptest.NewRecorder()
	h.Latest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("latest = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Points []SensorPoint `json:"points"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Points) != 1 || envelope.Data.Points[0].Value != 1.5 {
		t.Fatalf("points: %+v", envelope.Data.Points)
	}
}

func TestHandlerLatestError(t *testing.T) {
	svc := NewServiceWithConfig(Config{
		Addr:         "http://127.0.0.1:1", // 连接失败 → 503
		Token:        "tok",
		Org:          "lab",
		Bucket:       "sensors",
		Measurements: defaultMeasurements,
	})
	h := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sensors/latest?tags=pressure", nil)
	rec := httptest.NewRecorder()
	h.Latest(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("latest error = %d, want 503, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "sensor_error" {
		t.Fatalf("error code = %q", envelope.Error.Code)
	}
}

func TestHandlerHistory(t *testing.T) {
	h, server := newHandlerWithServer(t, "_time,tag,_value\n2026-01-01T00:00:00Z,pressure,2.0\n")
	defer server.Close()

	// 缺 tag → 400
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sensors/history", nil)
	rec := httptest.NewRecorder()
	h.History(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing tag = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}

	// 正常 → 200
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sensors/history?tag=pressure&from=-2h&interval=5m", nil)
	rec = httptest.NewRecorder()
	h.History(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("history = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Points []SensorPoint `json:"points"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Points) != 1 || envelope.Data.Points[0].Value != 2.0 {
		t.Fatalf("points: %+v", envelope.Data.Points)
	}
}

func TestHandlerHistoryError(t *testing.T) {
	svc := NewServiceWithConfig(Config{
		Addr:         "http://127.0.0.1:1",
		Token:        "tok",
		Org:          "lab",
		Bucket:       "sensors",
		Measurements: defaultMeasurements,
	})
	h := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sensors/history?tag=pressure", nil)
	rec := httptest.NewRecorder()
	h.History(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("history error = %d, want 503", rec.Code)
	}
}
