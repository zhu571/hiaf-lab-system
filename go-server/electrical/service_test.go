package electrical

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type writeCountingConn struct {
	net.Conn
	writes *atomic.Int64
}

func (c *writeCountingConn) Write(data []byte) (int, error) {
	c.writes.Add(1)
	return c.Conn.Write(data)
}

func passiveTestDial(cfg Config, mestekWrites, keithleyWrites *atomic.Int64) dialContextFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		client, server := net.Pipe()
		writes := mestekWrites
		if address == cfg.KeithleyAddr {
			writes = keithleyWrites
			go serveKeithleyTest(server)
		} else {
			go func() {
				defer server.Close()
				if address == cfg.MESTEKAddr {
					_, _ = server.Write(testResponse(0x14, testRegisters()))
				}
			}()
		}
		return &writeCountingConn{Conn: client, writes: writes}, nil
	}
}

func serveKeithleyTest(server net.Conn) {
	defer server.Close()
	reader := bufio.NewReader(server)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		switch strings.TrimSpace(line) {
		case ":READ?":
			_, _ = server.Write([]byte("+7.919254E-10A,+1.011508E+06,+2.000000E+00\n"))
		}
	}
}

func TestSingleShotMESTEKPassiveAnd6485Active(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MESTEKActiveRead = false
	cfg.Enabled = false
	cfg.Devices = []DeviceConfig{{Source: SourceRIGOL, Addr: "rigol:5555"}}
	var mestekWrites, keithleyWrites atomic.Int64
	svc := NewService(cfg, nil)
	svc.dial = passiveTestDial(cfg, &mestekWrites, &keithleyWrites)
	for _, device := range svc.devices {
		device.query = func(ctx context.Context, addr, command string) (string, error) {
			return "1.25", nil
		}
	}
	result, err := svc.SingleShot(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Samples) != 11 { // four RIGOL + five MESTEK values/raw + one 6485
		t.Fatalf("single-shot samples = %d, want 11", len(result.Samples))
	}
	if mestekWrites.Load() != 0 {
		t.Fatalf("passive MESTEK socket wrote %d times", mestekWrites.Load())
	}
	if keithleyWrites.Load() == 0 {
		t.Fatal("active 6485 poll did not send any command")
	}
	if _, err := svc.SingleShot(context.Background(), "user-1"); err == nil {
		t.Fatal("per-user rate limit not applied")
	} else {
		var rateErr *RateLimitError
		if !errors.As(err, &rateErr) {
			t.Fatalf("wrong rate-limit error: %T %v", err, err)
		}
	}
	svc.singleRunning.Store(true)
	if _, err := svc.SingleShot(context.Background(), "user-2"); !errors.Is(err, ErrSingleShotRunning) {
		t.Fatalf("single-flight not applied: %v", err)
	}
	svc.singleRunning.Store(false)
}

func TestContinuousPassiveMESTEKRun(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MESTEKActiveRead = false
	cfg.Enabled = true
	cfg.Devices = nil
	cfg.ReconnectDelay = 10 * time.Millisecond
	cfg.KeithleyReadInterval = 10 * time.Millisecond
	var mestekWrites, keithleyWrites atomic.Int64
	svc := NewService(cfg, nil)
	svc.dial = passiveTestDial(cfg, &mestekWrites, &keithleyWrites)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		latest := svc.Latest()
		if len(latest.Samples) >= 6 && latest.ContinuousEnabled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("continuous samples did not arrive: %+v", latest)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("continuous workers did not stop")
	}
	if mestekWrites.Load() != 0 {
		t.Fatalf("continuous passive MESTEK socket wrote %d times", mestekWrites.Load())
	}
	if keithleyWrites.Load() == 0 {
		t.Fatal("continuous 6485 poll did not send any command")
	}
}

func TestSingleShotTimeoutRemovesPassiveWaiters(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MESTEKActiveRead = false
	cfg.Enabled = true
	cfg.Devices = nil
	cfg.SingleShotTimeout = 20 * time.Millisecond
	svc := NewService(cfg, nil)
	svc.running = true
	svc.runCtx = context.Background()
	if _, err := svc.SingleShot(context.Background(), "user-timeout"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("single-shot error = %v, want context.DeadlineExceeded", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		svc.mu.RLock()
		remaining := len(svc.waiters)
		svc.mu.RUnlock()
		if remaining == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed-out passive waiters were not removed: %+v", svc.waiters)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBacklogBoundAndWriteFailureHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "influx unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cfg := DefaultConfig()
	cfg.Devices = nil
	cfg.BacklogCapacity = 2
	cfg.WriteBatchSize = 2
	influx := NewInfluxClient(InfluxConfig{Addr: server.URL, Token: "token", Org: "lab", Bucket: "bucket"})
	svc := NewService(cfg, influx)
	value := 1.0
	makeSample := func() Sample {
		return Sample{Time: time.Now(), Source: SourceRIGOL, Channel: "CH1", Metric: "vpp_V", Mapping: MappingDirect, Value: &value, Quality: "good"}
	}
	svc.record([]Sample{makeSample(), makeSample()})
	svc.record([]Sample{makeSample()})
	latest := svc.Latest()
	if latest.WriteBacklog != 2 || latest.Sources[2].StorageError == "" {
		t.Fatalf("bounded backlog/health mismatch: %+v", latest)
	}
	svc.flushBacklog(context.Background())
	latest = svc.Latest()
	if latest.WriteBacklog != 2 || latest.Sources[2].WriteErrors == 0 || latest.Sources[2].StorageError == "" {
		t.Fatalf("write failure not reflected in source health: %+v", latest.Sources[2])
	}
}

func TestDisabledMESTEKDoesNotStartWorker(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MESTEKEnabled = false
	cfg.Devices = nil
	cfg.ReconnectDelay = 10 * time.Millisecond
	cfg.KeithleyReadInterval = 10 * time.Millisecond
	var mestekDials atomic.Int64
	svc := NewService(cfg, nil)
	svc.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == cfg.MESTEKAddr {
			mestekDials.Add(1)
		}
		return nil, errors.New("offline")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("disabled MESTEK service did not stop")
	}
	if mestekDials.Load() != 0 {
		t.Fatalf("disabled MESTEK dialed %d times", mestekDials.Load())
	}
	for _, health := range svc.Latest().Sources {
		if health.Source == SourceMESTEK && health.Status != "disabled" {
			t.Fatalf("MESTEK health = %+v, want disabled", health)
		}
	}
}

func TestSingleShotSkipsDisabledMESTEK(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MESTEKEnabled = false
	cfg.MESTEKActiveRead = false
	cfg.Devices = nil
	var mestekWrites, keithleyWrites atomic.Int64
	svc := NewService(cfg, nil)
	svc.dial = passiveTestDial(cfg, &mestekWrites, &keithleyWrites)
	result, err := svc.SingleShot(context.Background(), "user-disabled")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Samples) != 1 || result.Samples[0].Source != Source6485 {
		t.Fatalf("single-shot samples = %+v, want only the 6485 reading", result.Samples)
	}
	if _, failed := result.Errors[SourceMESTEK]; failed {
		t.Fatalf("disabled MESTEK reported an acquisition error: %+v", result.Errors)
	}
	if mestekWrites.Load() != 0 {
		t.Fatalf("disabled MESTEK socket wrote %d times", mestekWrites.Load())
	}
}

func TestHandlers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Devices = nil
	svc := NewService(cfg, nil)
	handler := NewHandler(svc)

	recorder := httptest.NewRecorder()
	handler.Latest(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/electrical/latest", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("latest status = %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	handler.History(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/electrical/history?limit=nope", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid history status = %d", recorder.Code)
	}
	svc.singleRunning.Store(true)
	recorder = httptest.NewRecorder()
	handler.SingleShot(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/electrical/single-shot", nil))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("single-flight status = %d", recorder.Code)
	}
	svc.singleRunning.Store(false)
}
