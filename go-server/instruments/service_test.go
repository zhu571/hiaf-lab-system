package instruments

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseResultSweepXY(t *testing.T) {
	svc := NewServiceWithGateway("http://unused")
	def, err := GetCommand("e5063a", "trigger_single")
	if err != nil || def.ResultParser == nil {
		t.Fatalf("trigger_single missing result_parser: def=%+v err=%v", def, err)
	}
	parsed, err := svc.ParseResult(def, "noise 1000000,-10.5;2000000,-20.25;3000000,-15.0; tail")
	if err != nil {
		t.Fatalf("ParseResult returned error: %v", err)
	}
	if parsed.Type != "sweep_xy" || len(parsed.Points) != 3 || parsed.XLabel == "" || parsed.YLabel == "" {
		t.Fatalf("unexpected parsed result: %+v", parsed)
	}
	if parsed.Points[0] != (Point{X: 1000000, Y: -10.5}) || parsed.Points[2] != (Point{X: 3000000, Y: -15.0}) {
		t.Fatalf("unexpected points: %+v", parsed.Points)
	}
}

func TestParseResultSingleValue(t *testing.T) {
	svc := NewServiceWithGateway("http://unused")
	def, err := GetCommand("hioki_im3536", "measure_single")
	if err != nil || def.ResultParser == nil {
		t.Fatalf("measure_single missing result_parser: def=%+v err=%v", def, err)
	}
	parsed, err := svc.ParseResult(def, "1.0234E+2,-89.5,0,0")
	if err != nil {
		t.Fatalf("ParseResult returned error: %v", err)
	}
	if parsed.Type != "single_value" || parsed.Value == nil || *parsed.Value != 102.34 {
		t.Fatalf("unexpected parsed result: %+v", parsed)
	}
}

func TestParseResultWithoutParser(t *testing.T) {
	svc := NewServiceWithGateway("http://unused")
	def, err := GetCommand("e5063a", "identify")
	if err != nil {
		t.Fatalf("GetCommand: %v", err)
	}
	parsed, err := svc.ParseResult(def, "Keysight,E5063A")
	if err != nil || parsed != nil {
		t.Fatalf("expected (nil, nil), got parsed=%+v err=%v", parsed, err)
	}
}

func TestParseResultRejectsMalformedSweep(t *testing.T) {
	svc := NewServiceWithGateway("http://unused")
	def, err := GetCommand("e5063a", "trigger_single")
	if err != nil {
		t.Fatalf("GetCommand: %v", err)
	}
	if _, err := svc.ParseResult(def, "no numeric data here"); err == nil {
		t.Fatal("expected error for unparseable sweep response")
	}
}

func TestInterpretValidatesAgentCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer internal" {
			t.Fatal("missing internal authorization")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "command": "set_power",
			"params": map[string]any{"power_dbm": -35}, "confidence": 0.9,
			"explanation": "设置保守功率", "question": "", "reason": "",
			"prompt_version": "1.0", "model": "test",
		})
	}))
	defer server.Close()
	svc := NewServiceWithGateway(server.URL)
	svc.ConfigureInterpreter(server.URL, "internal")

	candidate, err := svc.Interpret(context.Background(), "e5063a", NLCommandRequest{Input: "设置功率 -35 dBm"})
	if err != nil || !candidate.Validation.OK || candidate.SCPI != "SOUR1:POW -35" {
		t.Fatalf("unexpected candidate=%+v err=%v", candidate, err)
	}
}

func TestPiezoStatusReadsGatewayPVs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/batch" {
			t.Fatalf("unexpected gateway call path=%q", r.URL.Path)
		}
		writeBatch(t, map[string]any{
			"GasCell:Piezo:A1": 1.25, "GasCell:Piezo:ValveSP": 2.5,
			"GasCell:Piezo:Running": 1, "GasCell:Piezo:Error": "",
		})(w, r)
	}))
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/batch" {
			t.Fatalf("unexpected gateway call path=%q", r.URL.Path)
		}
		writeBatch(t, map[string]any{
			"GasCell:Piezo:A1": 1.25, "GasCell:Piezo:ValveSP": 2.5,
			"GasCell:Piezo:Running": 0, "GasCell:Piezo:Error": nil,
		})(w, r)
	}))
	defer server.Close()

	status, err := NewServiceWithGateway(server.URL).PiezoStatus()
	if err != nil {
		t.Fatalf("PiezoStatus returned error: %v", err)
	}
	if status.Running || status.Error != "" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestGasCellStatusKeepsPartialSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/batch" {
			t.Fatalf("unexpected gateway call path=%q", r.URL.Path)
		}
		writeBatch(t, map[string]any{
			"GasCell:Piezo:A1":       4.2,
			"GasCell:Piezo:Setpoint": nil,
		})(w, r)
	}))
	defer server.Close()

	snapshot := NewServiceWithGateway(server.URL).GasCellStatus()
	if value, ok := pointNumber(snapshot.Data["GasCell:Piezo:A1"]); !ok || value != 4.2 {
		t.Fatalf("unexpected A1 point: %+v", snapshot.Data["GasCell:Piezo:A1"])
	}
	if snapshot.Data["GasCell:Piezo:Setpoint"].Quality != "disconnected" {
		t.Fatalf("expected disconnected setpoint: %+v", snapshot.Data["GasCell:Piezo:Setpoint"])
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

func TestGasCellWriteValidatesRoleAndReadback(t *testing.T) {
	value := 0.0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Value float64 `json:"value"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			value = body.Value
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writePV(t, value)(w, r)
	}))
	defer server.Close()
	svc := NewServiceWithGateway(server.URL)

	if _, err := svc.WriteGasCellPV("viewer", "GasCell:Piezo:Setpoint", 4.5); err != ErrGasCellPermission {
		t.Fatalf("expected permission error, got %v", err)
	}
	result, err := svc.WriteGasCellPV("maintainer", "GasCell:Piezo:Setpoint", 4.5)
	if err != nil || result.Warning != "" || result.Readback != 4.5 {
		t.Fatalf("unexpected checked write result=%+v err=%v", result, err)
	}
}

func TestGasCellWriteReturnsReadbackWarning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writePV(t, 9.0)(w, r)
	}))
	defer server.Close()

	result, err := NewServiceWithGateway(server.URL).WriteGasCellPV("admin", "GasCell:Piezo:Setpoint", 4.5)
	if err != nil || result.Warning == "" {
		t.Fatalf("expected warning, result=%+v err=%v", result, err)
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

func writeBatch(t *testing.T, values map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{"values": values}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}
}
