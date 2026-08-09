package automation

import (
	"encoding/json"
	"time"
)

// 一期白名单（与迁移 032 的 CHECK 一致）：防规则引擎扩张。
const (
	TriggerDailyReportSubmitted = "daily_report.submitted"
	ActionEnqueueAgentTask      = "enqueue_agent_task"
)

// Rule 是 automation_rules 一行记录。
type Rule struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	TriggerEvent string          `json:"trigger_event"`
	Action       json.RawMessage `json:"action"`
	Enabled      bool            `json:"enabled"`
	CreatedBy    *string         `json:"created_by,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// CreateRuleRequest 是 POST /rules 的请求体。
type CreateRuleRequest struct {
	Name         string          `json:"name"`
	TriggerEvent string          `json:"trigger_event"`
	Action       json.RawMessage `json:"action"`
}

// UpdateRuleRequest 是 PATCH /rules/{id} 的请求体；一期仅允许切换 enabled。
type UpdateRuleRequest struct {
	Enabled *bool `json:"enabled"`
}
