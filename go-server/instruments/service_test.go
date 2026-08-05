package instruments

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseResultSweepXY(t *testing.T) {
	svc := NewServiceWithGateway("http://unused")
	// 旧版 "x,y;x,y" 单行格式仍需兼容；regex 须支持科学计数法
	def := &CommandDef{ResultParser: &ResultParserConfig{
		Type: "sweep_xy", XLabel: "频率 (Hz)", YLabel: "S11 (dB)",
		Regex: `(?P<points>(?:[+-]?[\d.]+(?:[eE][+-]?\d+)?,[+-]?[\d.]+(?:[eE][+-]?\d+)?(?:;|$))+)`,
	}}
	parsed, err := svc.ParseResult(def, "noise 1.0E+06,-1.05E+01;+2.0E+06,-2.025E+01;3.0E+06,-1.5E+01; tail")
	if err != nil {
		t.Fatalf("ParseResult returned error: %v", err)
	}
	if parsed.Type != "sweep_xy" || len(parsed.Points) != 3 || parsed.XLabel == "" || parsed.YLabel == "" {
		t.Fatalf("unexpected parsed result: %+v", parsed)
	}
	if parsed.Points[0] != (Point{X: 1e6, Y: -10.5}) || parsed.Points[2] != (Point{X: 3e6, Y: -15.0}) {
		t.Fatalf("unexpected points: %+v", parsed.Points)
	}
}

func TestParseResultSweepXYComplex(t *testing.T) {
	svc := NewServiceWithGateway("http://unused")
	def, err := GetCommand("e5063a", "trigger_single")
	if err != nil || def.ResultParser == nil {
		t.Fatalf("trigger_single missing result_parser: def=%+v err=%v", def, err)
	}
	// E5063A 实际响应：第一行 SDATA 复数数组（re,im 对），第二行 FREQ 频率轴，均为科学计数法
	response := "+1.00000000E-01,+0.00000000E+00,+7.07106781E-01,-7.07106781E-01\n" +
		"+1.00000000E+06,+2.00000000E+06"
	parsed, err := svc.ParseResult(def, response)
	if err != nil {
		t.Fatalf("ParseResult returned error: %v", err)
	}
	if parsed.Type != "sweep_xy" || len(parsed.Points) != 2 || parsed.XLabel == "" || parsed.YLabel == "" {
		t.Fatalf("unexpected parsed result: %+v", parsed)
	}
	// 点 1: |0.1| = 0.1 → -20 dB；点 2: |0.7071+j0.7071| ≈ 1 → ≈0 dB
	// 两个 dB 断言统一使用同一容差常量
	const epsilon = 1e-6
	if parsed.Points[0].X != 1e6 || math.Abs(parsed.Points[0].Y-(-20)) > epsilon {
		t.Fatalf("unexpected point 0: %+v", parsed.Points[0])
	}
	if parsed.Points[1].X != 2e6 || math.Abs(parsed.Points[1].Y) > epsilon {
		t.Fatalf("unexpected point 1: %+v", parsed.Points[1])
	}
}

func TestParseResultSweepXYComplexMismatch(t *testing.T) {
	svc := NewServiceWithGateway("http://unused")
	def, err := GetCommand("e5063a", "trigger_single")
	if err != nil {
		t.Fatalf("GetCommand: %v", err)
	}
	// 复数数据 2 点，频率轴 3 点 → 必须报错
	if _, err := svc.ParseResult(def, "1.0E-01,0,2.0E-01,0\n1.0E+06,2.0E+06,3.0E+06"); err == nil {
		t.Fatal("expected error for mismatched complex/frequency lengths")
	}
	// 单行响应（缺频率轴）→ 必须报错
	if _, err := svc.ParseResult(def, "1.0E-01,0,2.0E-01,0"); err == nil {
		t.Fatal("expected error for single-section response")
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
	// 两行响应但内容非数字 → splitFloatList 失败，应报"无法解析复数数据段"
	if _, err := svc.ParseResult(def, "abc,def\nghi,jkl"); err == nil ||
		!strings.Contains(err.Error(), "无法解析复数数据段") {
		t.Fatalf("expected splitFloatList parse error, got %v", err)
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
