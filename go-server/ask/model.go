package ask

import "time"

// AskHistory 是 ask_history 一行记录（方案 §1，字段与迁移 033 对齐）。
// Rows 为 JSONB 快照（总字节 ≤256KB，见 §5），列表接口按约定不含该大字段。
type AskHistory struct {
	ID         string           `json:"id"`
	UserID     string           `json:"user_id"`
	RequestID  string           `json:"request_id"`
	Question   string           `json:"question"`
	Answer     string           `json:"answer"`
	SQLText    string           `json:"sql_text"`
	TableName  string           `json:"table_name"`
	Columns    []string         `json:"columns"`
	Rows       []map[string]any `json:"rows,omitempty"`
	RowCount   int              `json:"row_count"`
	DurationMS int              `json:"duration_ms"`
	Model      string           `json:"model"`
	CreatedAt  time.Time        `json:"created_at"`
}

// ChatRequest POST /api/v1/ask/chat 入参。
type ChatRequest struct {
	Question string `json:"question"`
}

// ChatResponse 返回给前端（rows 快照与 ask_history 存同一份，见方案 §5）。
type ChatResponse struct {
	ID         string           `json:"id"`
	Question   string           `json:"question"`
	Answer     string           `json:"answer"`
	SQL        string           `json:"sql"`
	TableName  string           `json:"table_name"`
	Columns    []string         `json:"columns"`
	Rows       []map[string]any `json:"rows"`
	RowCount   int              `json:"row_count"`
	Truncated  bool             `json:"truncated"`
	DurationMS int              `json:"duration_ms"`
	CreatedAt  time.Time        `json:"created_at"`
}

// ExecuteRequest POST /api/v1/ask/execute 入参（py-agent 内部调用）。
type ExecuteRequest struct {
	SQL string `json:"sql"`
}

// ExecuteResponse 只读执行结果（与 ask_history.rows 快照同源同封顶）。
type ExecuteResponse struct {
	SQL       string           `json:"sql"`
	TableName string           `json:"table_name"`
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RowCount  int              `json:"row_count"`
	Truncated bool             `json:"truncated"`
}

// agentAskResponse 是 py-agent /v1/ask 的返回结构（方案 §2）。
type agentAskResponse struct {
	Answer    string           `json:"answer"`
	SQL       string           `json:"sql"`
	Rows      []map[string]any `json:"rows"`
	Columns   []string         `json:"columns"`
	Table     string           `json:"table"`
	RowCount  int              `json:"row_count"`
	Truncated bool             `json:"truncated"`
}
