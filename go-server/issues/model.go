package issues

import "time"

const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"

	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

type Issue struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Severity    string     `json:"severity"`
	AuthorID    string     `json:"author_id"`
	AssigneeID  *string    `json:"assignee_id,omitempty"`
	AiGenerated bool       `json:"ai_generated"`
	AgentTaskID *string    `json:"agent_task_id,omitempty"`
	ReportDate  string     `json:"report_date"`
	OccurredAt  time.Time  `json:"occurred_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Comments    []Comment  `json:"comments,omitempty"`
}

type Comment struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	AuthorID  string    `json:"author_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateIssueRequest struct {
	Title         string   `json:"title"`
	Description   string   `json:"description,omitempty"`
	Severity      string   `json:"severity,omitempty"`
	AssigneeID    *string  `json:"assignee_id,omitempty"`
	OccurredAt    *string  `json:"occurred_at,omitempty"`
	ReportDate    *string  `json:"report_date,omitempty"`
	RelatedLogIDs []string `json:"related_log_ids,omitempty"`
	AiGenerated   bool     `json:"ai_generated"`
	AgentTaskID   *string  `json:"agent_task_id,omitempty"`
}

type UpdateIssueRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Severity    *string `json:"severity,omitempty"`
	AssigneeID  *string `json:"assignee_id,omitempty"`
}

type TransitionRequest struct {
	TargetStatus string `json:"target_status"`
	Reason       string `json:"reason"`
	AddComment   bool   `json:"add_comment"`
}

type AddCommentRequest struct {
	Content string `json:"content"`
}

type IssueListParams struct {
	Status   string `json:"status,omitempty"`
	Severity string `json:"severity,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	Author   string `json:"author,omitempty"`
	Search   string `json:"search,omitempty"`
	Page     int    `json:"page"`
	PerPage  int    `json:"per_page"`
	Sort     string `json:"sort,omitempty"`
	Order    string `json:"order,omitempty"`
}

type IssueListResult struct {
	Items []Issue `json:"items"`
	Total int     `json:"total"`
	Page  int     `json:"page"`
}
