package projects

import "time"

const (
	StatusDraft     = "draft"
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusArchived  = "archived"

	VisibilityRestricted = "restricted"
	VisibilityWorkspace  = "workspace"

	CommentPolicyEveryone = "everyone"
	CommentPolicyMembers  = "members"
	CommentPolicyDisabled = "disabled"

	RoleOwner      = "owner"
	RoleMaintainer = "maintainer"
	RoleMember     = "member"
	RoleViewer     = "viewer"

	MemberStatusActive    = "active"
	MemberStatusSuspended = "suspended"
)

type Project struct {
	ID              string     `json:"id"`
	Code            string     `json:"code"`
	Name            string     `json:"name"`
	ShortName       string     `json:"short_name"`
	Description     string     `json:"description"`
	Status          string     `json:"status"`
	Visibility      string     `json:"visibility"`
	CommentPolicy   string     `json:"comment_policy"`
	OwnerUserID     string     `json:"owner_user_id"`
	StartDate       *string    `json:"start_date"`
	TargetEndDate   *string    `json:"target_end_date"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
	DefaultCategory string     `json:"default_category"`
	TagsJSON        []byte     `json:"-"`
	Tags            []string   `json:"tags"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type ProjectMember struct {
	ProjectID string    `json:"project_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	Overrides []byte    `json:"-"`
	Muted     bool      `json:"muted"`
	JoinedAt  time.Time `json:"joined_at"`
	AddedBy   string    `json:"added_by"`
}

type CreateProjectRequest struct {
	Code            string   `json:"code"`
	Name            string   `json:"name"`
	ShortName       string   `json:"short_name,omitempty"`
	Description     string   `json:"description,omitempty"`
	Visibility      string   `json:"visibility,omitempty"`
	StartDate       *string  `json:"start_date,omitempty"`
	TargetEndDate   *string  `json:"target_end_date,omitempty"`
	DefaultCategory string   `json:"default_category,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

type UpdateProjectRequest struct {
	Name            *string  `json:"name,omitempty"`
	ShortName       *string  `json:"short_name,omitempty"`
	Description     *string  `json:"description,omitempty"`
	Visibility      *string  `json:"visibility,omitempty"`
	CommentPolicy   *string  `json:"comment_policy,omitempty"`
	StartDate       *string  `json:"start_date,omitempty"`
	TargetEndDate   *string  `json:"target_end_date,omitempty"`
	DefaultCategory *string  `json:"default_category,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

type AddMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type UpdateMemberRequest struct {
	Role string `json:"role"`
}

type ProjectWithStats struct {
	Project
	MemberCount    int `json:"member_count"`
	OpenIssueCount int `json:"open_issue_count"`
	LogCount       int `json:"log_count"`
}

type StatusTransitionRequest struct {
	Action         string `json:"action"`
	IgnoreWarnings bool   `json:"ignore_warnings,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type TransitionWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}
