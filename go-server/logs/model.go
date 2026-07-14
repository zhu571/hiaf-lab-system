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
}

type CreateDailyReportRequest struct {
	ReportDate string `json:"report_date,omitempty"`
	RawText    string `json:"raw_text,omitempty"`
}

type CreateLogRequest struct {
	DailyReportID *string `json:"daily_report_id,omitempty"`
	Category      string  `json:"category"`
	Content       string  `json:"content"`
	OccurredAt    *string `json:"occurred_at,omitempty"`
	Source        string  `json:"source,omitempty"`
}

type UpdateLogRequest struct {
	Category   *string `json:"category,omitempty"`
	Content    *string `json:"content,omitempty"`
	OccurredAt *string `json:"occurred_at,omitempty"`
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
