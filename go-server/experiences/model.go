package experiences

import "time"

const (
	StatusCandidate = "candidate"
	StatusPublished = "published"
	StatusArchived  = "archived"

	RelationPrimary     = "primary"
	RelationApplicable  = "applicable"
	RelationDerivedFrom = "derived_from"
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
