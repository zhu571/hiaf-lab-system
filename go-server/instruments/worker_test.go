package instruments

import (
	"bufio"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTCPInstrument：本地 TCP 模拟 SCPI 仪器；收到以 ? 结尾的命令返回应答，
// 其余命令仅读取；lines 记录收到的全部命令（并发安全），供急停序列断言。
type fakeTCPInstrument struct {
	listener  net.Listener
	addr      string
	response  string
	responder func(string) string
	mu        sync.Mutex
	lines     []string
}

func startFakeTCPInstrument(t *testing.T, response string) *fakeTCPInstrument {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	inst := &fakeTCPInstrument{listener: listener, addr: listener.Addr().String(), response: response}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				reader := bufio.NewReader(c)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimSpace(line)
					inst.mu.Lock()
					inst.lines = append(inst.lines, line)
					inst.mu.Unlock()
					if strings.HasSuffix(line, "?") {
						response := inst.response
						if inst.responder != nil {
							response = inst.responder(line)
						}
						if response != "" {
							_, _ = c.Write([]byte(response))
						}
					}
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return inst
}

// waitLine 轮询等待仪器收到指定命令。
func (f *fakeTCPInstrument) waitLine(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, line := range f.lines {
			if line == want {
				f.mu.Unlock()
				return
			}
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("instrument did not receive %q; got %v", want, f.lines)
}

func TestWorkerDefaultsAndValidation(t *testing.T) {
	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: "1.2.3.4:1", Terminator: "\n"})
	if worker.cfg.RateLimit != defaultRateLimit || worker.cfg.RateWindow != defaultRateWindow {
		t.Fatalf("defaults: limit=%d window=%v", worker.cfg.RateLimit, worker.cfg.RateWindow)
	}
	if worker.State() != "" {
		t.Fatalf("initial state = %q", worker.State())
	}
	// Submit 参数校验
	if err := worker.Submit(nil); err == nil {
		t.Fatal("nil command accepted")
	}
	if err := worker.Submit(&QueueCommand{Name: "", ResponseCh: make(chan CommandResult, 1)}); err == nil {
		t.Fatal("empty name accepted")
	}
	if err := worker.Submit(&QueueCommand{Name: "identify"}); err == nil {
		t.Fatal("nil response channel accepted")
	}
	// 未运行
	if err := worker.Submit(&QueueCommand{Name: "identify", ResponseCh: make(chan CommandResult, 1)}); err == nil ||
		!strings.Contains(err.Error(), "not running") {
		t.Fatalf("submit when not running: %v", err)
	}
	if err := worker.EmergencyStop(); err == nil {
		t.Fatal("emergency stop when not running should fail")
	}
	// Stop 幂等（未启动时不阻塞）
	worker.Stop()
	worker.Stop()
}

func TestWorkerStartFailsWhenUnreachable(t *testing.T) {
	// 借用已关闭的 listener 端口 → 连接拒绝
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()

	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: addr, Terminator: "\n"})
	if err := worker.Start(); err == nil {
		t.Fatal("expected start failure")
	}
	if worker.State() != WorkerStateNeedsReconnect {
		t.Fatalf("state = %q, want needs_reconnect", worker.State())
	}
	// started 已复位：可再次 Start
	if err := worker.Start(); err == nil {
		t.Fatal("expected second start failure")
	}
	// Start 成功后不可重复 Start（跑满验证由 fake instrument 测试覆盖）
	worker.Stop()
}

func TestWorkerFullLoopIdentifyAndEmergency(t *testing.T) {
	inst := startFakeTCPInstrument(t, "Keysight,E5063A\n")
	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: inst.addr, Terminator: "\n"})
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	if worker.State() != WorkerStateRunning {
		t.Fatalf("state = %q", worker.State())
	}

	// 绿色只读命令：identify → 返回仪器应答
	resultCh := make(chan CommandResult, 1)
	if err := worker.Submit(&QueueCommand{Name: "identify", Risk: "green", ResponseCh: resultCh}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-resultCh:
		if result.Error != nil || !strings.Contains(result.Response, "Keysight") {
			t.Fatalf("identify result: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("identify timeout")
	}

	// 急停：优先通道 + 内建安全序列（仪器应收到 ABOR 序列）
	if err := worker.EmergencyStop(); err != nil {
		t.Fatal(err)
	}
	inst.waitLine(t, "ABOR")
	inst.waitLine(t, "SOUR1:POW -45")
}

func TestWorkerRateLimit(t *testing.T) {
	inst := startFakeTCPInstrument(t, "")
	worker := NewInstrumentWorker(WorkerConfig{
		InstrumentID: "e5063a", Addr: inst.addr, Terminator: "\n",
		RateLimit: 2, RateWindow: time.Minute,
	})
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()

	submitYellow := func() error {
		ch := make(chan CommandResult, 1)
		if err := worker.Submit(&QueueCommand{Name: "set_power", Risk: "yellow", Params: map[string]any{"power_dbm": -30.0}, ResponseCh: ch}); err != nil {
			return err
		}
		select {
		case result := <-ch:
			return result.Error
		case <-time.After(5 * time.Second):
			return errors.New("timeout")
		}
	}

	if err := submitYellow(); err != nil {
		t.Fatalf("first yellow: %v", err)
	}
	if err := submitYellow(); err != nil {
		t.Fatalf("second yellow: %v", err)
	}
	err := submitYellow()
	if err == nil || !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Fatalf("third yellow: got %v, want rate limit", err)
	}
	if worker.State() != WorkerStateRateLimited {
		t.Fatalf("state = %q, want rate_limited", worker.State())
	}
	// 急停不受限流影响：ABOR 序列仍被下发，且执行成功后状态恢复 running
	if err := worker.EmergencyStop(); err != nil {
		t.Fatalf("emergency under rate limit: %v", err)
	}
	inst.waitLine(t, "ABOR")
	inst.waitLine(t, "SOUR1:POW -45")
	// 急停命令执行完毕后锁定至人工检查；轮询避免与
	// execute 内的 setState 竞争（waitLine 只保证仪器已收到命令）。
	deadline := time.Now().Add(5 * time.Second)
	for worker.State() != WorkerStateLockedManualCheck {
		if time.Now().After(deadline) {
			t.Fatalf("state = %q, want locked manual check after emergency", worker.State())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := worker.ConfirmManualCheck(); err != nil || worker.State() != WorkerStateRunning {
		t.Fatalf("manual check: state=%q err=%v", worker.State(), err)
	}
}

func TestWorkerBuildSCPIErrors(t *testing.T) {
	// 非法命令 → RenderSCPI 报错
	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: "1.2.3.4:1", Terminator: "\n"})
	if _, err := worker.buildSCPI(&QueueCommand{Name: "nope", Params: map[string]any{}}); err == nil {
		t.Fatal("expected unknown command error")
	}
	// 未定义急停序列的仪器
	other := NewInstrumentWorker(WorkerConfig{InstrumentID: "nope", Addr: "1.2.3.4:1", Terminator: "\n"})
	if _, err := other.buildSCPI(&QueueCommand{Name: emergencyCommand}); err == nil ||
		!strings.Contains(err.Error(), "not defined") {
		t.Fatalf("emergency for unknown instrument: %v", err)
	}
}

// 执行路径：buildSCPI 失败 → state=error + 响应带回错误（不触碰网络）。
func TestWorkerExecuteBuildFailure(t *testing.T) {
	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: "1.2.3.4:1", Terminator: "\n"})
	ch := make(chan CommandResult, 1)
	worker.execute(&QueueCommand{Name: "nope", Risk: "green", ResponseCh: ch})
	result := <-ch
	if result.Error == nil {
		t.Fatal("expected error result")
	}
	if worker.State() != WorkerStateError {
		t.Fatalf("state = %q, want error", worker.State())
	}
}
