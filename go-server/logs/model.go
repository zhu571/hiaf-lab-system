package logs

import "time"

const (
	ReportStatusDraft     = "draft"
	ReportStatusSubmitted = "submitted"
	ReportStatusConfirmed = "confirmed"
	ReportStatusLocked    = "locked"

	QualityUnchecked = "unchecked"
	QualityPassed    = "passed"
	QualityWarnings  = "warnings"

	LogStatusDraft     = "draft"
	LogStatusConfirmed = "confirmed"
	LogStatusLocked    = "locked"
	LogStatusVoided    = "voided"

	CategoryGeneral      = "general"
	CategoryAssembly     = "assembly"
	CategoryTest         = "test"
	CategoryCryo         = "cryo"
	CategoryRF           = "rf"
	CategoryVacuum       = "vacuum"
	CategoryBeam         = "beam"
	CategoryDataAnalysis = "data_analysis"

	SourceManual = "manual"
	SourceAgent  = "agent"
	SourceImport = "import"
	SourceWechat = "wechat"
)

type DailyReport struct {
	ID            string    `json:"id"`
	ReportDate    string    `json:"report_date"`
	AuthorID      string    `json:"author_id"`
	AuthorName    string    `json:"author_name,omitempty"`
	RawText       string    `json:"raw_text"`
	Summary       string    `json:"summary"`
	ContentStatus string    `json:"content_status"`
	QualityStatus string    `json:"quality_status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Logs          []Log     `json:"logs,omitempty"`
}

type Log struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	AuthorID      string    `json:"author_id"`
	OccurredAt    time.Time `json:"occurred_at"`
	Category      string    `json:"category"`
	Content       string    `json:"content"`
	Source        string    `json:"source"`
	ContentStatus string    `json:"content_status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	RunID         *string   `json:"run_id,omitempty"`
}

// WeeklyReportEntry 是周报取数的日报条目（AI-1）：供 main.go 注入的 weekly 窄接口
// 读取，字段对齐 weekly.ReportEntry；SQL 在 logs 包内只访问 daily_reports + users
// 展示字段（见 AGENTS.md §5 轻量只读登记）。
type WeeklyReportEntry struct {
	ReportDate string
	AuthorName string
	RawText    string
	Summary    string
}

type CreateDailyReportRequest struct {
	ReportDate string `json:"report_date,omitempty"`
	RawText    string `json:"raw_text,omitempty"`
}

type UpdateDailyReportRequest struct {
	RawText *string `json:"raw_text,omitempty"`
	Summary *string `json:"summary,omitempty"`
}

type CreateLogRequest struct {
	DailyReportID *string `json:"daily_report_id,omitempty"`
	Category      string  `json:"category"`
	Content       string  `json:"content"`
	OccurredAt    *string `json:"occurred_at,omitempty"`
	Source        string  `json:"source,omitempty"`
}

type UpdateLogRequest struct {
	Category      *string `json:"category,omitempty"`
	Content       *string `json:"content,omitempty"`
	OccurredAt    *string `json:"occurred_at,omitempty"`
	ContentStatus *string `json:"content_status,omitempty"`
}

type SubmitReportRequest struct {
	Force bool `json:"force"`
}

type SubmitResult struct {
	Report   DailyReport     `json:"report"`
	Warnings []SubmitWarning `json:"warnings"`
	Blocked  bool            `json:"blocked"`
}

type SubmitWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	LogID   string `json:"log_id,omitempty"`
}

type LogListParams struct {
	Page     int    `json:"page"`
	PerPage  int    `json:"per_page"`
	Category string `json:"category,omitempty"`
	DateFrom string `json:"date_from,omitempty"`
	DateTo   string `json:"date_to,omitempty"`
	Status   string `json:"status,omitempty"`
}

type LogListResult struct {
	Items []Log `json:"items"`
	Total int   `json:"total"`
	Page  int   `json:"page"`
}

type ReportListParams struct {
	AuthorID string `json:"author_id,omitempty"`
	Status   string `json:"status,omitempty"`
	Keyword  string `json:"keyword,omitempty"`
	Date     string `json:"date,omitempty"`
	Page     int    `json:"page"`
	PerPage  int    `json:"per_page"`
}

// AiParseLogEntry 是 AI 整理产出的一条候选日志（草稿，未入库）。
type AiParseLogEntry struct {
	Category   string `json:"category"`
	ProjectID  string `json:"project_id"`
	Content    string `json:"content"`
	OccurredAt string `json:"occurred_at"`
}

// AiParseResult 对齐 py-agent /v1/daily-parse 的三态契约。
type AiParseResult struct {
	Status        string            `json:"status"`
	Logs          []AiParseLogEntry `json:"logs"`
	Summary       *string           `json:"summary"`
	Question      *string           `json:"question"`
	Reason        *string           `json:"reason"`
	Model         string            `json:"model,omitempty"`
	PromptVersion string            `json:"prompt_version,omitempty"`
}
