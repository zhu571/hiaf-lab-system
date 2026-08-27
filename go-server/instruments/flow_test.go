package instruments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

func TestFrequencyGridAndObjective(t *testing.T) {
	req := CreateFlowRequest{Objective: "在 1 kHz 到 100 kHz 取 21 个对数频点"}
	parseSweepObjective(&req)
	if req.StartHz != 1000 || req.StopHz != 100000 || req.Points != 21 || req.Spacing != "log" {
		t.Fatalf("parsed request: %+v", req)
	}
	grid, err := frequencyGrid(req.StartHz, req.StopHz, req.Points, req.Spacing)
	if err != nil {
		t.Fatal(err)
	}
	if len(grid) != 21 || grid[0] != 1000 || grid[20] != 100000 {
		t.Fatalf("grid endpoints: %#v", grid)
	}
	for i := 1; i < len(grid); i++ {
		if grid[i] <= grid[i-1] {
			t.Fatalf("grid is not increasing: %#v", grid)
		}
	}
	if _, err = frequencyGrid(1000, 200000, 21, "log"); err == nil {
		t.Fatal("unsafe span accepted")
	}
}

func TestNextFlowDecisionRejectsMultiCommandAndRed(t *testing.T) {
	for _, body := range []string{
		`{"decision":"next_command","command":"set_frequency","params":{"hz":1000},"commands":[{"command":"measure_single"}],"reason":"x"}`,
		`{"decision":"next_command","command":"reset","params":{},"reason":"x"}`,
	} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(json.RawMessage(body)) }))
		svc := NewServiceWithGateway(ts.URL)
		svc.ConfigureInterpreter(ts.URL, "token")
		flow := &FlowSession{ID: "f", InstrumentID: "hioki_im3536", FlowKind: FlowKindImpedanceSweep, Limits: FlowLimits{AllowedCommands: []string{"set_frequency", "measure_single"}}, FrequencyGrid: []float64{1000, 2000}}
		if _, err := svc.NextFlowDecision(context.Background(), flow, nil, nil); err == nil {
			t.Fatalf("unsafe decision accepted: %s", body)
		}
		ts.Close()
	}
}

func TestWorkerSessionOwnerBlocksInsertionAndEmergencyPreempts(t *testing.T) {
	inst := startFakeTCPInstrument(t, "Hioki,IM3536\n")
	w := NewInstrumentWorker(WorkerConfig{InstrumentID: "hioki_im3536", Addr: inst.addr, Terminator: "\n"})
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	if err := w.AcquireSession("flow-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	ordinary := &QueueCommand{Name: "identify", Risk: "green", ResponseCh: make(chan CommandResult, 1)}
	if err := w.Submit(ordinary); err == nil || !strings.Contains(err.Error(), "instrument_busy") {
		t.Fatalf("ordinary insertion: %v", err)
	}
	owned := &QueueCommand{Name: "identify", Risk: "green", SessionID: "flow-1", ResponseCh: make(chan CommandResult, 1)}
	if err := w.Submit(owned); err != nil {
		t.Fatal(err)
	}
	<-owned.ResponseCh
	if err := w.EmergencyStop(); err != nil {
		t.Fatal(err)
	}
	if w.SessionOwner() != "" {
		t.Fatal("emergency stop did not release session owner")
	}
}

func TestRetryClassification(t *testing.T) {
	// 只有超时与显式标记为 communication_error 的瞬时硬件通信错误可重试。
	if !retryableCommandError(context.DeadlineExceeded) ||
		!retryableCommandError(newCommandError("communication_error", errors.New("write tcp: connection reset by peer"))) ||
		!retryableCommandError(newCommandError("timeout", errors.New("command timeout"))) {
		t.Fatal("retryable failure rejected")
	}
	// M5 回归：中文 NormalizeParams 校验错误绝不重试（旧子串分类器会把它误判为 communication_error）。
	if _, err := NormalizeParams("hioki_im3536", "set_frequency", map[string]any{"hz": 9.9e9}); err == nil {
		t.Fatal("out-of-range hz must fail validation")
	} else {
		if retryableCommandError(err) {
			t.Fatalf("validation error must not retry: %v", err)
		}
		if wrapped := newCommandError("validation_error", err); retryableCommandError(wrapped) || commandErrorCode(wrapped) != "validation_error" {
			t.Fatalf("wrapped validation error misclassified: %v", wrapped)
		}
	}
	for _, err := range []error{
		errors.New("参数 hz 超过安全上限"), // 未标记的未知错误 → fail-closed 不可重试
		newCommandError("validation_error", errors.New("instrument is locked until manual check")),
		newCommandError("rate_limited", errors.New("instrument command rate limit exceeded")),
		errors.New("instrument is locked until manual check"),
		errors.New("instrument session is not acquired"),
		assertError("parse_failed"),
	} {
		if retryableCommandError(err) {
			t.Fatalf("non-retryable failure accepted: %v", err)
		}
	}
	if commandErrorCode(assertError("mystery")) != "validation_error" {
		t.Fatal("unknown errors must classify as validation_error (fail-closed)")
	}
	if !canRetryMeasure("measure_single", 0, 0, context.DeadlineExceeded, WorkerStateRunning) {
		t.Fatal("first trusted timeout should retry")
	}
	for _, tc := range []struct {
		command          string
		pending, retries int
		err              error
		state            WorkerState
	}{{"measure_single", 0, 1, context.DeadlineExceeded, WorkerStateRunning}, {"measure_single", 0, 0, context.DeadlineExceeded, WorkerStateNeedsReconnect}, {"set_frequency", 0, 0, context.DeadlineExceeded, WorkerStateRunning}, {"measure_single", -1, 0, context.DeadlineExceeded, WorkerStateRunning}, {"measure_single", 0, 0, newCommandError("validation_error", errors.New("参数 hz 超过安全上限")), WorkerStateRunning}} {
		if canRetryMeasure(tc.command, tc.pending, tc.retries, tc.err, tc.state) {
			t.Fatalf("unsafe retry accepted: %+v", tc)
		}
	}
}

func TestFlowFeatureFlag(t *testing.T) {
	if FlowEnabled() {
		t.Fatal("flow flag must default to off")
	}
	for _, on := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Setenv("INSTRUMENT_FLOW_ENABLED", on)
		if !FlowEnabled() {
			t.Fatalf("flag value %q must enable flows", on)
		}
	}
	for _, off := range []string{"", "0", "false", "off", "nonsense"} {
		t.Setenv("INSTRUMENT_FLOW_ENABLED", off)
		if FlowEnabled() {
			t.Fatalf("flag value %q must disable flows", off)
		}
	}
}

// M6 双态覆盖：默认关时 flow 写端点 404 flow_disabled；开启后进入正常校验路径。
func TestFlowHandlersGatedByFeatureFlag(t *testing.T) {
	middleware.SetJWTSecret([]byte(insHandlerSecret))
	h := NewHandler(NewServiceWithGateway("http://unused"), nil, map[string]*InstrumentWorker{})
	router := chi.NewRouter()
	router.Route("/api/v1/instruments", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Post("/{id}/flows", h.CreateFlow)
		r.Post("/{id}/flows/{flow_id}/approve", h.ApproveFlow)
		r.Post("/{id}/flows/{flow_id}/stop", h.StopFlow)
	})
	maintainer := insToken(t, insDBUserID, "flowflag", auth.RoleMaintainer)
	for _, path := range []string{
		"/api/v1/instruments/hioki_im3536/flows",
		"/api/v1/instruments/hioki_im3536/flows/f1/approve",
		"/api/v1/instruments/hioki_im3536/flows/f1/stop",
	} {
		rec := insRequest(t, router, http.MethodPost, path, maintainer, "flag-key", `{"objective":"x"}`)
		if rec.Code != http.StatusNotFound || insErrorCode(t, rec) != "flow_disabled" {
			t.Fatalf("flag off: %s -> %d %s", path, rec.Code, rec.Body.String())
		}
	}
	t.Setenv("INSTRUMENT_FLOW_ENABLED", "true")
	rec := insRequest(t, router, http.MethodPost, "/api/v1/instruments/hioki_im3536/flows", maintainer, "flag-key-2", `{"objective":"x"}`)
	if rec.Code == http.StatusNotFound && insErrorCode(t, rec) == "flow_disabled" {
		t.Fatalf("flag on must open flow endpoints, got %s", rec.Body.String())
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
