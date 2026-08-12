package experiences

import "time"

const (
	StatusCandidate = "candidate"
	StatusPublished = "published"
	StatusArchived  = "archived"

	RelationPrimary     = "primary"
	RelationApplicable  = "applicable"
	RelationDerivedFrom = "derived_from"

	// aiExtractedTag 标记 AI-2 经验候选提取落库的草稿经验（experiences 无 kind/scope 列，
	// 语义由 tags_json 承担：AI 提取 = ai_generated=true + candidate + 本 tag，
	// 对齐 weekly_summary tag 惯例）。
	aiExtractedTag = "ai_extracted"
)

type Experience struct {
	ID             string                  `json:"id"`
	ProjectID      *string                 `json:"project_id,omitempty"`
	Title          string                  `json:"title"`
	Content        string                  `json:"content"`
	Tags           []string                `json:"tags"`
	Status         string                  `json:"status"`
	AuthorID       string                  `json:"author_id"`
	ReviewerID     *string                 `json:"reviewer_id,omitempty"`
	AiGenerated    bool                    `json:"ai_generated"`
	AgentTaskID    *string                 `json:"agent_task_id,omitempty"`
	CandidateID    *string                 `json:"candidate_id,omitempty"`
	PublishedAt    *time.Time              `json:"published_at,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
	LinkedProjects []ExperienceProjectLink `json:"linked_projects,omitempty"`
}

type ExperienceProjectLink struct {
	ProjectID string `json:"project_id"`
	Relation  string `json:"relation"`
}

type CreateExperienceRequest struct {
	ProjectID      *string                 `json:"project_id,omitempty"`
	Title          string                  `json:"title"`
	Content        string                  `json:"content"`
	Tags           []string                `json:"tags,omitempty"`
	LinkedProjects []ExperienceProjectLink `json:"linked_projects,omitempty"`
	AiGenerated    bool                    `json:"ai_generated"`
	AgentTaskID    *string                 `json:"agent_task_id,omitempty"`
	CandidateID    *string                 `json:"candidate_id,omitempty"`
}

type UpdateExperienceRequest struct {
	Title          *string                 `json:"title,omitempty"`
	Content        *string                 `json:"content,omitempty"`
	Tags           []string                `json:"tags,omitempty"`
	LinkedProjects []ExperienceProjectLink `json:"linked_projects,omitempty"`
}

type ExperienceListParams struct {
	ProjectID         string
	Status            string
	Tags              []string
	Keyword           string
	Page              int
	PerPage           int
	CandidateAuthorID string
	ProjectRole       string
	UserRole          string
}

type ExperienceListResult struct {
	Items   []Experience `json:"items"`
	Total   int          `json:"total"`
	Page    int          `json:"page"`
	PerPage int          `json:"per_page"`
}

// ResolvedIssue 是经验提取（AI-2）的 issue 数据源（经 main.go 注入的 issues 窄接口
// 读取；experiences 不 SELECT issues 表，见 AGENTS.md §5）。
type ResolvedIssue struct {
	ID          string
	ProjectID   string
	Title       string
	Description string
	Comments    []string
	RunID       *string
}

// ExtractLLMRequest / ExtractLLMResponse 是 Go → py-agent /v1/experience-extract 的契约。
type ExtractLLMRequest struct {
	Issues []ExtractIssueInput `json:"issues"`
}

type ExtractIssueInput struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"project_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Comments    []string `json:"comments,omitempty"`
	RunID       string   `json:"run_id,omitempty"`
}

type ExtractLLMResponse struct {
	Status        string         `json:"status"`
	Entries       []ExtractEntry `json:"entries"`
	Model         string         `json:"model,omitempty"`
	PromptVersion string         `json:"prompt_version,omitempty"`
}

type ExtractEntry struct {
	IssueID    string   `json:"issue_id"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags,omitempty"`
	Confidence float64  `json:"confidence"`
}

type ExtractCandidatesResult struct {
	Items []ExtractedItem `json:"items"`
	Total int             `json:"total"`
}

// ExtractCandidatesRequest 是经验候选提取的手动触发请求。
type ExtractCandidatesRequest struct {
	// Days 是回溯天数（默认 7，限 1-30）。
	Days int `json:"days,omitempty"`
}

type ExtractedItem struct {
	Experience Experience `json:"experience"`
	IssueID    string     `json:"issue_id"`
	Confidence float64    `json:"confidence"`
}
