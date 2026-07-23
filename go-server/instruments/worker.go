package instruments

import (
	"fmt"
	"sync"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/notify"
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
	w.started = true
	w.mu.Unlock()

	if err := w.reconnect(); err != nil {
		w.setState(WorkerStateNeedsReconnect)
		w.mu.Lock()
		w.started = false
		w.mu.Unlock()
		return err
	}
	w.setState(WorkerStateRunning)
	go w.run()
	return nil
}

// Submit queues a normal command without blocking when the queue is full.
func (w *InstrumentWorker) Submit(cmd *QueueCommand) error {
	if cmd == nil || cmd.Name == "" || cmd.ResponseCh == nil {
		return fmt.Errorf("command name and response channel are required")
	}
	w.mu.RLock()
	running := w.started && !w.stopped
	w.mu.RUnlock()
	if !running {
		return fmt.Errorf("instrument worker is not running")
	}
	select {
	case w.cmdQueue <- cmd:
		return nil
	default:
		return fmt.Errorf("instrument command queue is full")
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
	w.mu.RLock()
	running := w.started && !w.stopped
	w.mu.RUnlock()
	if !running {
		return fmt.Errorf("instrument worker is not running")
	}
	select {
	case w.emergencyCh <- cmd:
		return nil
	default:
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
	if cmd.Name != emergencyCommand && cmd.Risk == "yellow" && w.rateLimitExceeded(started) {
		w.respond(cmd, CommandResult{Command: cmd.Name, Duration: time.Since(started), Error: fmt.Errorf("instrument command rate limit exceeded")})
		return
	}
	if cmd.Name != emergencyCommand {
		w.recordCmdTime(started)
	}

	scpi, err := w.buildSCPI(cmd)
	if err != nil {
		w.setState(WorkerStateError)
		w.respond(cmd, CommandResult{Command: cmd.Name, Duration: time.Since(started), Error: err})
		return
	}
	if w.connection() == nil {
		if err = w.reconnect(); err != nil {
			go notify.Send("lab-instruments", "仪器断开: "+w.cfg.InstrumentID, err.Error(), notify.WebURL, "default", []string{"warning"})
			w.setState(WorkerStateNeedsReconnect)
			w.respond(cmd, CommandResult{Command: cmd.Name, Duration: time.Since(started), Error: err})
			return
		}
	}
	response, err := w.connection().Send(scpi)
	if err != nil {
		w.closeConnection()
		go notify.InstrumentRestoreFailed(w.cfg.InstrumentID, err.Error())
		w.setState(WorkerStateNeedsReconnect)
	} else {
		w.setState(WorkerStateRunning)
	}
	w.respond(cmd, CommandResult{Command: cmd.Name, Response: response, Duration: time.Since(started), Error: err})
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
		go notify.Send("lab-instruments", "仪器限流: "+w.cfg.InstrumentID, "命令频率过高", notify.WebURL, "default", []string{"warning"})
	}
	w.rateLimited = true
	w.rateLimitedAt = now
	w.state = WorkerStateRateLimited
	return true
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

func (w *InstrumentWorker) respond(cmd *QueueCommand, result CommandResult) {
	select {
	case cmd.ResponseCh <- result:
	case <-w.stopCh:
	}
}
