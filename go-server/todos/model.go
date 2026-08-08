package todos

import "time"

const (
	StatusPending   = "pending"
	StatusDone      = "done"
	StatusDeferred  = "deferred"
	StatusCancelled = "cancelled"

	PriorityHigh   = "high"
	PriorityMedium = "medium"
	PriorityLow    = "low"

	SourceManual   = "manual"
	SourceLLM      = "llm"
	SourceIssue    = "issue"
	SourceDailyLLM = "daily_llm"

	ScopeAll    = "all"
	ScopeMine   = "mine"
	ScopeShared = "shared"

	// 生成/推送 job 审计动作名。
	ActionGenerate  = "todos.generate"
	ActionRollover  = "todos.rollover"
	ActionIssueSync = "todos.issue_sync"
	ActionCleanup   = "todos.cleanup"
	ActionPush      = "todos.push"
)

// Todo 是待办领域模型（对应 todos 表，owner_display_name 供推送/列表来源标注）。
type Todo struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	Priority         string     `json:"priority"`
	Status           string     `json:"status"`
	Source           string     `json:"source"`
	CreatedBy        string     `json:"created_by"`
	CreatedFor       string     `json:"created_for"`
	ProjectID        *string    `json:"project_id,omitempty"`
	IssueID          *string    `json:"issue_id,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	CompletedBy      *string    `json:"completed_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	OwnerDisplayName string     `json:"owner_display_name"`
}

type CreateRequest struct {
	Title     string  `json:"title"`
	Priority  string  `json:"priority,omitempty"`
	ProjectID *string `json:"project_id,omitempty"`
}

type LLMParseRequest struct {
	RawText string `json:"raw_text"`
}

// LLMParseResponse 是 llm-parse 的草稿响应（不落库）。status=ok 时 reason 可为降级提示。
type LLMParseResponse struct {
	Status   string  `json:"status"`
	Title    string  `json:"title"`
	Priority string  `json:"priority"`
	Reason   *string `json:"reason,omitempty"`
}

type LLMAddRequest struct {
	DraftID  *string `json:"draft_id,omitempty"`
	Title    string  `json:"title"`
	Priority string  `json:"priority,omitempty"`
}

// UpdateRequest 是 PATCH /todos/{id} 的编辑请求：updated_at 为乐观锁版本。
type UpdateRequest struct {
	UpdatedAt *time.Time `json:"updated_at"`
	Title     *string    `json:"title,omitempty"`
	Priority  *string    `json:"priority,omitempty"`
	ProjectID *string    `json:"project_id,omitempty"`
}

type ListParams struct {
	Date   string
	Scope  string
	Status string
	Limit  int
}

type NotificationTopic struct {
	Topic        string `json:"topic"`
	SubscribeURL string `json:"subscribe_url"`
}

type ProvisionResponse struct {
	ProvisionToken string    `json:"provision_token"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type RedeemResponse struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Topic    string `json:"topic"`
}

// CleanupHint 是推送模板里的清理预警分组（剩余天数 → 条数）。
type CleanupHint struct {
	DaysLeft int `json:"days_left"`
	Count    int `json:"count"`
}

// LLM 客户端返回的单条候选。
type LLMItem struct {
	Title    string `json:"title"`
	Priority string `json:"priority"`
}
