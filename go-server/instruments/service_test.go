package instruments

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPiezoStatusReadsGatewayPVs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/GasCell:Piezo:A1", writePV(t, 1.25))
	mux.HandleFunc("/GasCell:Piezo:ValveSP", writePV(t, 2.5))
	mux.HandleFunc("/GasCell:Piezo:Running", writePV(t, 1))
	mux.HandleFunc("/GasCell:Piezo:Error", writePV(t, ""))
	server := httptest.NewServer(mux)
	defer server.Close()

	status, err := NewServiceWithGateway(server.URL).PiezoStatus()
	if err != nil {
		t.Fatalf("PiezoStatus returned error: %v", err)
	}
	if status.A1 != 1.25 || status.ValveSP != 2.5 || !status.Running || status.Error != "" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestPiezoStatusIgnoresMissingErrorPV(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/GasCell:Piezo:A1", writePV(t, 1.25))
	mux.HandleFunc("/GasCell:Piezo:ValveSP", writePV(t, 2.5))
	mux.HandleFunc("/GasCell:Piezo:Running", writePV(t, 0))
	server := httptest.NewServer(mux)
	defer server.Close()

	status, err := NewServiceWithGateway(server.URL).PiezoStatus()
	if err != nil {
		t.Fatalf("PiezoStatus returned error: %v", err)
	}
	if status.Running || status.Error != "" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestPiezoWriteCallsGateway(t *testing.T) {
	var gotPath string
	var gotValue float64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body struct {
			Value float64 `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		gotValue = body.Value
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := NewServiceWithGateway(server.URL).PiezoSetpoint(3.75); err != nil {
		t.Fatalf("PiezoSetpoint returned error: %v", err)
	}
	if gotPath != "/GasCell:Piezo:Setpoint" || gotValue != 3.75 {
		t.Fatalf("unexpected gateway call path=%q value=%v", gotPath, gotValue)
	}
}

func writePV(t *testing.T, value any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{"pv": "test", "value": value}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}
}
