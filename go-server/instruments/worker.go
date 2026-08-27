package instruments

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/alert"
)

const (
	defaultRateLimit  = 10
	defaultRateWindow = 10 * time.Second
	commandQueueSize  = 10
	emergencyCommand  = "emergency-stop"
)

// InstrumentWorker serializes commands sent to one instrument.
type InstrumentWorker struct {
	cfg         WorkerConfig
	conn        *SCPIConnection
	cmdQueue    chan *QueueCommand
	emergencyCh chan *QueueCommand
	stopCh      chan struct{}
	doneCh      chan struct{}
	state       WorkerState
	mu          sync.RWMutex

	lastCmdTimes  []time.Time
	rateLimited   bool
	rateLimitedAt time.Time
	started       bool
	stopped       bool
	sessionOwner  string
	sessionUntil  time.Time
	queueEpoch    uint64
}

// NewInstrumentWorker creates an idle worker with a bounded command queue.
func NewInstrumentWorker(cfg WorkerConfig) *InstrumentWorker {
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = defaultRateLimit
	}
	if cfg.RateWindow <= 0 {
		cfg.RateWindow = defaultRateWindow
	}
	return &InstrumentWorker{
		cfg:         cfg,
		cmdQueue:    make(chan *QueueCommand, commandQueueSize),
		emergencyCh: make(chan *QueueCommand, 1),
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
}

// Start connects to the instrument and starts consuming commands.
func (w *InstrumentWorker) Start() error {
	w.mu.Lock()
	if w.started || w.stopped {
		w.mu.Unlock()
		return fmt.Errorf("instrument worker cannot be started")
	}
	w.mu.Unlock()

	if err := w.reconnect(); err != nil {
		w.setState(WorkerStateNeedsReconnect)
		return err
	}
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		w.closeConnection()
		return fmt.Errorf("instrument worker cannot be started")
	}
	w.started = true
	w.state = WorkerStateRunning
	go w.run()
	w.mu.Unlock()
	return nil
}

// Submit queues a normal command without blocking when the queue is full.
func (w *InstrumentWorker) Submit(cmd *QueueCommand) error {
	if cmd == nil || cmd.Name == "" || cmd.ResponseCh == nil {
		return fmt.Errorf("command name and response channel are required")
	}
	w.mu.Lock()
	if w.sessionOwner != "" && time.Now().After(w.sessionUntil) {
		w.sessionOwner, w.sessionUntil = "", time.Time{}
		w.queueEpoch++
	}
	running := w.started && !w.stopped
	owner := w.sessionOwner
	if !running {
		w.mu.Unlock()
		return fmt.Errorf("instrument worker is not running")
	}
	if w.state == WorkerStateLockedManualCheck {
		w.mu.Unlock()
		return fmt.Errorf("instrument is locked until manual check")
	}
	if owner != "" && cmd.SessionID != owner {
		w.mu.Unlock()
		return fmt.Errorf("instrument_busy: owned by flow session")
	}
	if owner == "" && cmd.SessionID != "" {
		w.mu.Unlock()
		return fmt.Errorf("instrument session is not acquired")
	}
	cmd.queueEpoch = w.queueEpoch
	w.mu.Unlock()
	select {
	case w.cmdQueue <- cmd:
		return nil
	default:
		return fmt.Errorf("instrument command queue is full")
	}
}

// AcquireSession reserves the worker across multiple Submit calls. EmergencyStop bypasses it.
func (w *InstrumentWorker) AcquireSession(sessionID string, until time.Time) error {
	if sessionID == "" || !until.After(time.Now()) {
		return fmt.Errorf("invalid instrument session")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started || w.stopped {
		return fmt.Errorf("instrument worker is not running")
	}
	if w.sessionOwner != "" && time.Now().Before(w.sessionUntil) && w.sessionOwner != sessionID {
		return fmt.Errorf("instrument_busy: owned by flow session")
	}
	if w.state == WorkerStateLockedManualCheck {
		return fmt.Errorf("instrument is locked until manual check")
	}
	w.queueEpoch++
	w.sessionOwner, w.sessionUntil = sessionID, until
	return nil
}

func (w *InstrumentWorker) ReleaseSession(sessionID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sessionOwner == sessionID {
		w.queueEpoch++
		w.sessionOwner, w.sessionUntil = "", time.Time{}
	}
}

func (w *InstrumentWorker) SessionOwner() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.sessionOwner
}

// WaitFlowYellowSlot enforces the conservative flow share of seven yellow sends per rolling window.
func (w *InstrumentWorker) WaitFlowYellowSlot(ctx context.Context, deadline time.Time) error {
	for {
		now := time.Now()
		w.mu.Lock()
		cutoff := now.Add(-w.cfg.RateWindow)
		first := 0
		for first < len(w.lastCmdTimes) && w.lastCmdTimes[first].Before(cutoff) {
			first++
		}
		w.lastCmdTimes = w.lastCmdTimes[first:]
		if len(w.lastCmdTimes) < 7 {
			w.mu.Unlock()
			return nil
		}
		wait := w.lastCmdTimes[0].Add(w.cfg.RateWindow).Sub(now)
		w.mu.Unlock()
		if now.Add(wait).After(deadline) {
			return newCommandError("rate_limited", fmt.Errorf("flow yellow budget unavailable before deadline"))
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Stop closes the connection after stopping the worker loop.
func (w *InstrumentWorker) Stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	started := w.started
	close(w.stopCh)
	w.mu.Unlock()
	if started {
		<-w.doneCh
	}
	w.closeConnection()
}

// EmergencyStop queues the instrument-specific safe-stop sequence ahead of normal work.
func (w *InstrumentWorker) EmergencyStop() error {
	cmd := &QueueCommand{
		Name:       emergencyCommand,
		Risk:       "red",
		Priority:   1,
		ResponseCh: make(chan CommandResult, 1),
	}
	w.mu.Lock()
	running := w.started && !w.stopped
	if !running {
		w.mu.Unlock()
		return fmt.Errorf("instrument worker is not running")
	}
	select {
	case w.emergencyCh <- cmd:
		w.queueEpoch++
		w.sessionOwner, w.sessionUntil = "", time.Time{}
		w.state = WorkerStateLockedManualCheck
		w.mu.Unlock()
		return nil
	default:
		w.mu.Unlock()
		return fmt.Errorf("emergency stop is already queued")
	}
}

// State returns a concurrency-safe snapshot of the worker state.
func (w *InstrumentWorker) State() WorkerState {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state
}

func (w *InstrumentWorker) run() {
	defer close(w.doneCh)
	for {
		var cmd *QueueCommand
		select {
		case cmd = <-w.emergencyCh:
		default:
			select {
			case cmd = <-w.emergencyCh:
			case cmd = <-w.cmdQueue:
			case <-w.stopCh:
				return
			}
		}
		w.execute(cmd)
	}
}

func (w *InstrumentWorker) execute(cmd *QueueCommand) {
	started := time.Now()
	if cmd.Name != emergencyCommand {
		w.mu.Lock()
		if w.sessionOwner != "" && started.After(w.sessionUntil) {
			w.sessionOwner, w.sessionUntil = "", time.Time{}
			w.queueEpoch++
		}
		valid := w.state != WorkerStateLockedManualCheck && cmd.queueEpoch == w.queueEpoch && cmd.SessionID == w.sessionOwner
		w.mu.Unlock()
		if !valid {
			w.respond(cmd, CommandResult{Command: cmd.Name, Duration: time.Since(started), Error: newCommandError("cancelled", fmt.Errorf("command invalidated by instrument ownership boundary"))})
			return
		}
	}
	if cmd.Name != emergencyCommand && cmd.Risk == "yellow" && w.rateLimitExceeded(started) {
		w.respond(cmd, CommandResult{Command: cmd.Name, Duration: time.Since(started), Error: newCommandError("rate_limited", fmt.Errorf("instrument command rate limit exceeded"))})
		return
	}
	if cmd.Name != emergencyCommand && cmd.Risk == "yellow" {
		w.recordCmdTime(started)
	}

	scpi, err := w.buildSCPI(cmd)
	if err != nil {
		w.setStateUnlessLocked(WorkerStateError)
		w.respond(cmd, CommandResult{Command: cmd.Name, Duration: time.Since(started), Error: newCommandError("validation_error", err)})
		return
	}
	if w.connection() == nil {
		if err = w.reconnect(); err != nil {
			w.reportAlert("warning", "仪器断开: "+w.cfg.InstrumentID, err.Error())
			w.setStateUnlessLocked(WorkerStateNeedsReconnect)
			w.respond(cmd, CommandResult{Command: cmd.Name, Duration: time.Since(started), Error: newCommandError("communication_error", err)})
			return
		}
		// 重连成功 → 解除「仪器断开」告警（幂等：无 active 行时 no-op）。
		w.resolveAlert("仪器断开: " + w.cfg.InstrumentID)
	}
	response, err := w.connection().Send(scpi)
	if err != nil {
		w.closeConnection()
		w.reportAlert("error", "仪器恢复失败: "+w.cfg.InstrumentID, err.Error())
		w.setStateUnlessLocked(WorkerStateNeedsReconnect)
		err = newCommandError("communication_error", err)
	} else if cmd.Name != emergencyCommand {
		w.setStateUnlessLocked(WorkerStateRunning)
	}
	if cmd.Name == emergencyCommand {
		w.setState(WorkerStateLockedManualCheck)
	}
	w.respond(cmd, CommandResult{Command: cmd.Name, Response: response, Duration: time.Since(started), Error: err})
}

func (w *InstrumentWorker) ConfirmManualCheck() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != WorkerStateLockedManualCheck {
		return fmt.Errorf("instrument is not awaiting manual check")
	}
	w.state = WorkerStateRunning
	return nil
}

func (w *InstrumentWorker) requireManualCheck(reason error) {
	w.mu.Lock()
	w.queueEpoch++
	w.sessionOwner, w.sessionUntil = "", time.Time{}
	w.state = WorkerStateLockedManualCheck
	w.mu.Unlock()
	w.reportAlert("error", "仪器审计失败: "+w.cfg.InstrumentID, reason.Error())
}

func (w *InstrumentWorker) buildSCPI(cmd *QueueCommand) (string, error) {
	if cmd.Name == emergencyCommand {
		switch w.cfg.InstrumentID {
		case "e5063a":
			return "ABOR;INIT1:CONT OFF;SOUR1:POW -45", nil
		case "hioki_im3536":
			return "ABOR;DCBias OFF;LEVel:VOLTage 0.01", nil
		default:
			return "", fmt.Errorf("emergency stop is not defined for instrument %q", w.cfg.InstrumentID)
		}
	}

	scpi, normalized, err := RenderSCPI(w.cfg.InstrumentID, cmd.Name, cmd.Params)
	cmd.Params = normalized
	return scpi, err
}

func (w *InstrumentWorker) rateLimitExceeded(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	cutoff := now.Add(-w.cfg.RateWindow)
	first := 0
	for first < len(w.lastCmdTimes) && w.lastCmdTimes[first].Before(cutoff) {
		first++
	}
	w.lastCmdTimes = w.lastCmdTimes[first:]
	if len(w.lastCmdTimes) < w.cfg.RateLimit {
		w.rateLimited = false
		return false
	}
	if !w.rateLimited {
		w.reportAlert("warning", "仪器限流: "+w.cfg.InstrumentID, "命令频率过高")
	}
	w.rateLimited = true
	w.rateLimitedAt = now
	if w.state != WorkerStateLockedManualCheck {
		w.state = WorkerStateRateLimited
	}
	return true
}

func (w *InstrumentWorker) setStateUnlessLocked(state WorkerState) {
	w.mu.Lock()
	if w.state != WorkerStateLockedManualCheck {
		w.state = state
	}
	w.mu.Unlock()
}

func (w *InstrumentWorker) recordCmdTime(now time.Time) {
	w.mu.Lock()
	w.lastCmdTimes = append(w.lastCmdTimes, now)
	w.mu.Unlock()
}

func (w *InstrumentWorker) reconnect() error {
	conn, err := NewSCPIConnection(w.cfg.Addr, w.cfg.Terminator)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()
	return nil
}

func (w *InstrumentWorker) connection() *SCPIConnection {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.conn
}

func (w *InstrumentWorker) closeConnection() {
	w.mu.Lock()
	conn := w.conn
	w.conn = nil
	w.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (w *InstrumentWorker) setState(state WorkerState) {
	w.mu.Lock()
	w.state = state
	w.mu.Unlock()
}

// reportAlert 异步上报告警中心（source=instruments；未注入 Reporter 时静默，
// 保持 worker 单测无注入可运行）。
func (w *InstrumentWorker) reportAlert(level, title, detail string) {
	if w.cfg.Reporter == nil {
		return
	}
	go func() {
		if _, err := w.cfg.Reporter.Report(context.Background(), level, "instruments", title, detail); err != nil {
			slog.Error("instrument alert report failed", "error", err, "instrument", w.cfg.InstrumentID)
		}
	}()
}

// resolveAlert 异步解除「仪器断开」类告警（幂等：无 active 行时 no-op）。
func (w *InstrumentWorker) resolveAlert(title string) {
	if w.cfg.Reporter == nil {
		return
	}
	go func() {
		if err := w.cfg.Reporter.ResolveBySource(context.Background(), "instruments", title, alert.ResolvedBySystem); err != nil {
			slog.Error("instrument alert resolve failed", "error", err, "instrument", w.cfg.InstrumentID)
		}
	}()
}

func (w *InstrumentWorker) respond(cmd *QueueCommand, result CommandResult) {
	select {
	case cmd.ResponseCh <- result:
	case <-w.stopCh:
	}
}
