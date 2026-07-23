package agent

import (
	"encoding/json"
	"time"
)

const (
	TaskPending    = "pending"
	TaskProcessing = "processing"
	TaskDone       = "done"
	TaskFailed     = "failed"
	TaskDead       = "dead"

	CandidatePending         = "pending_review"
	CandidateApproved        = "approved"
	CandidateRejected        = "rejected"
	CandidateExecuted        = "executed"
	CandidateExecutionFailed = "execution_failed"
)

type PendingAgentTask struct {
	ID              string          `json:"id"`
	ReportID        string          `json:"report_id"`
	ActingUserID    string          `json:"acting_user_id"`
	Status          string          `json:"status"`
	Attempts        int             `json:"attempts"`
	ClaimedAt       *time.Time      `json:"claimed_at,omitempty"`
	LeaseExpiresAt  *time.Time      `json:"lease_expires_at,omitempty"`
	NextAttemptAt   *time.Time      `json:"next_attempt_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	LastError       *string         `json:"last_error,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Model           *string         `json:"model,omitempty"`
	PromptVersion   *string         `json:"prompt_version,omitempty"`
	AgentConfidence *float64        `json:"agent_confidence,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type AgentCandidateAction struct {
	ID              string          `json:"id"`
	TaskID          string          `json:"task_id"`
	ReportID        string          `json:"report_id,omitempty"`
	ActionType      string          `json:"action_type"`
	ProjectID       *string         `json:"project_id,omitempty"`
	PoolActionKey   string          `json:"pool_action_key"`
	Payload         json.RawMessage `json:"payload"`
	Status          string          `json:"status"`
	AgentConfidence *float64        `json:"agent_confidence,omitempty"`
	ReviewedBy      *string         `json:"reviewed_by,omitempty"`
	ReviewedAt      *time.Time      `json:"reviewed_at,omitempty"`
	ReviewReason    *string         `json:"review_reason,omitempty"`
	ExecutedAt      *time.Time      `json:"executed_at,omitempty"`
	ExecutionError  *string         `json:"execution_error,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

type CandidateInput struct {
	ActionType      string          `json:"action_type"`
	ProjectID       *string         `json:"project_id,omitempty"`
	Payload         json.RawMessage `json:"payload"`
	AgentConfidence *float64        `json:"agent_confidence,omitempty"`
}

type CompleteTaskRequest struct {
	Result          json.RawMessage  `json:"result"`
	Model           string           `json:"model"`
	PromptVersion   string           `json:"prompt_version"`
	AgentConfidence *float64         `json:"agent_confidence,omitempty"`
	Candidates      []CandidateInput `json:"candidates"`
}

type ClaimTaskRequest struct {
	LeaseSeconds int `json:"lease_seconds,omitempty"`
}

type FailTaskRequest struct {
	Error string `json:"error"`
}

type ReviewRequest struct {
	Reason string `json:"reason"`
}

type CandidateListResult struct {
	Items   []AgentCandidateAction `json:"items"`
	Total   int                    `json:"total"`
	Page    int                    `json:"page"`
	PerPage int                    `json:"per_page"`
}
