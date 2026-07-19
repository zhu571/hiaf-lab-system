package instruments

import (
	"net"
	"time"
)

// PiezoStatus is the full piezo instrument state returned by the status endpoint.
type PiezoStatus struct {
	A1      float64 `json:"a1"`
	ValveSP float64 `json:"valve_sp"`
	Running bool    `json:"running"`
	Error   string  `json:"error,omitempty"`
}

// SetpointRequest is the body for POST /piezo/setpoint.
type SetpointRequest struct {
	Value float64 `json:"value"`
}

// SCPIConnection is a TCP connection to a SCPI instrument.
type SCPIConnection struct {
	addr       string
	terminator string
	timeout    time.Duration
	conn       net.Conn
}

// CommandDef is a command loaded from the instrument whitelist.
type CommandDef struct {
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description" json:"description"`
	Risk        string         `yaml:"risk" json:"risk"`
	SCPI        string         `yaml:"scpi,omitempty" json:"scpi,omitempty"`
	Build       string         `yaml:"build,omitempty" json:"build,omitempty"`
	TimeoutMS   int            `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	Params      map[string]any `yaml:"params,omitempty" json:"params,omitempty"`
	Returns     any            `yaml:"returns,omitempty" json:"returns,omitempty"`
}

// CommandResult records one SCPI command execution.
type CommandResult struct {
	Command  string        `json:"command"`
	Response string        `json:"response,omitempty"`
	Duration time.Duration `json:"duration"`
	Error    error         `json:"-"`
}

// WorkerConfig configures one instrument's serial command worker.
type WorkerConfig struct {
	InstrumentID string
	Addr         string
	Terminator   string
	RateLimit    int
	RateWindow   time.Duration
}

// QueueCommand is a structured whitelist command waiting for execution.
type QueueCommand struct {
	Name       string
	Params     map[string]any
	Risk       string
	Priority   int
	ResponseCh chan CommandResult
}

// WorkerState is the current state of an InstrumentWorker.
type WorkerState string

const (
	WorkerStateRunning        WorkerState = "running"
	WorkerStateRateLimited    WorkerState = "rate_limited"
	WorkerStateNeedsReconnect WorkerState = "needs_reconnect"
	WorkerStateError          WorkerState = "error"
)
