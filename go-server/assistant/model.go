package assistant

import "time"

// Session 是 assistant_sessions 的对外模型（第一阶段只支持私有会话）。
type Session struct {
	ID            string     `json:"id"`
	UserID        string     `json:"user_id"`
	ProjectID     *string    `json:"project_id,omitempty"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	Summary       string     `json:"summary"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Message 是 assistant_messages 的对外模型。
type Message struct {
	ID          string         `json:"id"`
	SessionID   string         `json:"session_id"`
	RunID       *string        `json:"run_id,omitempty"`
	UserID      *string        `json:"user_id,omitempty"`
	Role        string         `json:"role"`
	Content     string         `json:"content"`
	ContentJSON map[string]any `json:"content_json,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

type CreateSessionRequest struct {
	Title     string `json:"title"`
	ProjectID string `json:"project_id"`
}

type UpdateSessionRequest struct {
	Title  *string `json:"title"`
	Status *string `json:"status"`
}

type CreateMessageRequest struct {
	Content string `json:"content"`
}

// Run 是 assistant_runs 的对外模型（第一阶段只落占位回复，不调用 LLM）。
type Run struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id"`
	UserID        string     `json:"user_id"`
	Status        string     `json:"status"`
	Model         string     `json:"model"`
	PromptVersion string     `json:"prompt_version"`
	InputTokens   int        `json:"input_tokens"`
	OutputTokens  int        `json:"output_tokens"`
	Error         *string    `json:"error,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

type CreateRunRequest struct {
	Content string `json:"content"`
}

type RunResult struct {
	Run      Run       `json:"run"`
	Messages []Message `json:"messages"`
}

// Usage 是 assistant_usage 的当日用量。
type Usage struct {
	Date         string `json:"date"`
	Runs         int    `json:"runs"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	ToolCalls    int    `json:"tool_calls"`
	RunLimit     int    `json:"run_limit"`
}
