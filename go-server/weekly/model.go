package weekly

import "time"

// 周报模块（AI-1）：手动端点 + 每周日 20:00 定时调度，AI 两步生成周报，
// 落库 experiences（global/published/tags=weekly_summary），推 ntfy。
// 跨模块只读（daily_reports/issues）与落库（experiences）全部经 main.go 注入窄接口。

const (
	// DefaultTopic 是周报 ntfy 主题（main_bridges 适配器固定；模块内仅供日志）。
	DefaultTopic = "lab-weekly"
)

type SummaryRequest struct {
	// WeekStart 是周一开始日期（YYYY-MM-DD，必须为周一）；空 = 本周一（APP_TIMEZONE）。
	WeekStart string `json:"week_start,omitempty"`
	// Notify 是否推 ntfy；缺省 true。
	Notify *bool `json:"notify,omitempty"`
}

type SummaryResult struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Markdown   string   `json:"markdown"`
	Highlights []string `json:"highlights"`
	Problems   []string `json:"problems"`
	DataPoints []string `json:"data_points"`
	WeekStart  string   `json:"week_start"`
	WeekEnd    string   `json:"week_end"`
	Reused     bool     `json:"reused"`
}

// ReportEntry 是周报 LLM 输入的日报条目（经 main.go 桥接自 logs 模块）。
type ReportEntry struct {
	ReportDate string
	AuthorName string
	RawText    string
	Summary    string
}

// IssueStats 是周报 LLM 输入的 issue 统计（经 main.go 桥接自 issues 模块）。
type IssueStats struct {
	Created          int
	Resolved         int
	OpenHighCritical int
}

// SavedSummary 是落库后的周报（经 main.go 桥接自 experiences 模块）。
// 注：experiences 仅存 title + content（markdown 正文）；一句话 summary 仅用于
// 即时推送，复用历史周报时 Summary 为空（正文在 Markdown 字段）。
type SavedSummary struct {
	ID        string
	Title     string
	Markdown  string
	CreatedAt time.Time
}

// LLMRequest / LLMResponse 是 Go → py-agent /v1/weekly-summary 的契约。
type LLMRequest struct {
	WeekStart  string        `json:"week_start"`
	WeekEnd    string        `json:"week_end"`
	Reports    []ReportEntry `json:"reports"`
	IssueStats IssueStats    `json:"issue_stats"`
}

type LLMResponse struct {
	Status     string   `json:"status"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Markdown   string   `json:"markdown"`
	Highlights []string `json:"highlights"`
	Problems   []string `json:"problems"`
	DataPoints []string `json:"data_points"`
}
