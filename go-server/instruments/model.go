package instruments

import (
	"encoding/json"
	"net"
	"time"
)

// CommandLogEntry is an audited instrument command execution.
type CommandLogEntry struct {
	ID               string          `json:"id"`
	InstrumentID     string          `json:"instrument_id"`
	CommandName      string          `json:"command_name"`
	RiskLevel        string          `json:"risk_level"`
	ParamsRaw        json.RawMessage `json:"params_raw"`
	ParamsNormalized json.RawMessage `json:"params_normalized"`
	UserID           string          `json:"user_id"`
	ActingUserID     *string         `json:"acting_user_id"`
	LeaseID          *string         `json:"lease_id"`
	ApprovalID       *string         `json:"approval_id"`
	WhitelistVersion string          `json:"whitelist_version"`
	BeforeSnapshot   json.RawMessage `json:"before_snapshot"`
	ResultSummary    *string         `json:"result_summary"`
	ErrorCode        *string         `json:"error_code"`
	DurationMS       *int            `json:"duration_ms"`
	RequestID        string          `json:"request_id"`
	CreatedAt        time.Time       `json:"created_at"`
}

// Lease is an exclusive instrument usage lease.
type Lease struct {
	ID           string     `json:"id"`
	InstrumentID string     `json:"instrument_id"`
	UserID       string     `json:"user_id"`
	Purpose      string     `json:"purpose"`
	Status       string     `json:"status"`
	ExpiresAt    time.Time  `json:"expires_at"`
	CreatedAt    time.Time  `json:"created_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	RevokedBy    *string    `json:"revoked_by"`
}

// Approval authorizes one command and parameter hash for a lease.
type Approval struct {
	ID          string     `json:"id"`
	LeaseID     *string    `json:"lease_id"`
	CommandName string     `json:"command_name"`
	ParamsHash  string     `json:"params_hash"`
	RequestedBy string     `json:"requested_by"`
	ApprovedBy  string     `json:"approved_by"`
	Status      string     `json:"status"`
	ApprovedAt  *time.Time `json:"approved_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

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
