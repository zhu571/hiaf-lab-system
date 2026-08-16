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
	ClaimToken      *string         `json:"claim_token,omitempty"`
	ClaimedAt       *time.Time      `json:"claimed_at,omitempty"`
	LeaseExpiresAt  *time.Time      `json:"lease_expires_at,omitempty"`
	NextAttemptAt   *time.Time      `json:"next_attempt_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	LastError       *string         `json:"last_error,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Model           *string         `json:"model,omitempty"`
	PromptVersion   *string         `json:"prompt_version,omitempty"`
	AgentConfidence *float64        `json:"agent_confidence,omitempty"`
	// 快照仅经 trace 端点输出（030），不进 Claim/Complete/Fail 的任务响应体。
	RawTextSnapshot *string   `json:"-"`
	RawTextSHA256   *string   `json:"-"`
	ReportDate      *string   `json:"report_date,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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
	ClaimToken      string           `json:"claim_token"`
	// AI 看到的原始输入快照与报告日期（030），由 worker 随结果回传；
	// 缺省（旧 worker/旧任务）时保持 NULL，trace 端点降级显示。
	RawTextSnapshot string `json:"raw_text_snapshot"`
	ReportDate      string `json:"report_date"`
}

type ClaimTaskRequest struct {
	LeaseSeconds int `json:"lease_seconds,omitempty"`
}

type FailTaskRequest struct {
	Error      string `json:"error"`
	ClaimToken string `json:"claim_token"`
}

// RenewTaskRequest 是 POST /api/v1/agent/tasks/{id}/renew 的请求体（R8）：
// worker 在 LLM 调用前与执行期间周期续约，防止前置 HTTP 链 + LLM 最坏耗时
// 超出租约导致任务被重领、双跑。
type RenewTaskRequest struct {
	LeaseSeconds int    `json:"lease_seconds,omitempty"`
	ClaimToken   string `json:"claim_token"`
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

// CandidateTrace 是 GET /api/v1/agent/candidates/{id}/trace 的响应（030 审计链）：
// 谁看到什么（task 快照）、谁批准（audit）、产出什么（result）一条可查。
type CandidateTrace struct {
	Candidate *AgentCandidateAction `json:"candidate"`
	Task      TraceTask             `json:"task"`
	Report    *TraceReport          `json:"report"`
	Result    *TraceResult          `json:"result"`
	Audit     []AuditEvent          `json:"audit"`
}

// TraceTask 是任务的审计视角字段；快照三字段对 030 迁移前完成的存量任务为 null（降级）。
type TraceTask struct {
	ID              string   `json:"id"`
	Status          string   `json:"status"`
	Model           *string  `json:"model,omitempty"`
	PromptVersion   *string  `json:"prompt_version,omitempty"`
	AgentConfidence *float64 `json:"agent_confidence,omitempty"`
	RawTextSnapshot *string  `json:"raw_text_snapshot"`
	RawTextSHA256   *string  `json:"raw_text_sha256"`
	ReportDate      *string  `json:"report_date"`
}

// TraceReport 是日报当前值（经 logs 模块注入读取，可为 null：无权限或异常降级）。
type TraceReport struct {
	ID         string `json:"id"`
	ReportDate string `json:"report_date"`
	RawText    string `json:"raw_text"`
}

// TraceResult 是候选的执行产物（经 issues/experiences 注入按 candidate_id 反查）。
type TraceResult struct {
	IssueID      *string `json:"issue_id,omitempty"`
	ExperienceID *string `json:"experience_id,omitempty"`
	Title        string  `json:"title"`
	URL          string  `json:"url"`
}

// AuditEvent 是 trace 内嵌的审计行（经 audit 模块注入读取，agent 不直接查 audit_log）。
type AuditEvent struct {
	ID          int64          `json:"id"`
	RequestID   string         `json:"request_id"`
	Username    string         `json:"username"`
	Method      string         `json:"method"`
	Path        string         `json:"path"`
	Action      string         `json:"action"`
	StatusCode  int            `json:"status_code"`
	ActorType   string         `json:"actor_type"`
	AgentTaskID *string        `json:"agent_task_id,omitempty"`
	Detail      map[string]any `json:"detail,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}
