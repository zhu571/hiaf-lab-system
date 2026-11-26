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
