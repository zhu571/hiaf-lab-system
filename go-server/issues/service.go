package issues

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrIssueNotFound             = errors.New("Issue 不存在")
	ErrInvalidTransition         = errors.New("Issue 状态流转不合法")
	ErrTransitionForbidden       = errors.New("当前用户无权执行该状态流转")
	ErrReasonRequired            = errors.New("重新打开 Issue 必须填写原因")
	ErrRelatedLogNotFound        = errors.New("关联日志不存在")
	ErrRelatedLogProjectMismatch = errors.New("关联日志不属于当前项目")
	ErrInvalidInput              = errors.New("请求参数无效")
	ErrProjectNotFound           = errors.New("项目不存在")
	ErrForbidden                 = errors.New("当前用户无权访问该项目")
	ErrProjectLifecycleBlocked   = errors.New("项目当前状态不允许创建 Issue")
	ErrIssueClosed               = errors.New("已关闭 Issue 不允许修改")
	ErrCommentsDisabled          = errors.New("项目已关闭评论")
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	ProjectStatus(projectID string) (string, error)
	ProjectCommentPolicy(projectID string) (string, error)
	HasProjectPermission(projectID, userID string, perm middleware.Permission) (bool, error)
}

type issueRepository interface {
	Create(projectID, authorID string, req CreateIssueRequest, occurredAt time.Time, reportDate string) (*Issue, error)
	GetByID(id string) (*Issue, error)
	List(projectID string, params IssueListParams) ([]Issue, int, error)
	Update(id string, req UpdateIssueRequest) (*Issue, error)
	TransitionStatus(id, targetStatus, userID, comment string, addComment bool) (*Issue, error)
	AddComment(issueID, authorID, content string) (*Comment, error)
	GetComments(issueID string, page, perPage int) ([]Comment, error)
	CountRelatedLogs(projectID string, logIDs []string) (int, error)
	CountLogsByIDs(logIDs []string) (int, error)
}

type Service struct {
	repo   issueRepository
	access ProjectAccessChecker
}

func NewService(repo issueRepository, access ProjectAccessChecker) *Service {
	return &Service{repo: repo, access: access}
}

func (s *Service) Create(projectID, userID, userRole string, req CreateIssueRequest) (*Issue, error) {
	req.Title = strings.TrimSpace(req.Title)
	req.Severity = defaultSeverity(req.Severity)
	if req.Title == "" || len(req.Title) > 256 || !validSeverity(req.Severity) {
		return nil, ErrInvalidInput
	}
	if req.AssigneeID != nil {
		trimmed := strings.TrimSpace(*req.AssigneeID)
		req.AssigneeID = &trimmed
	}

	exists, err := s.access.ProjectExists(projectID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrProjectNotFound
	}
	status, err := s.access.ProjectStatus(projectID)
	if err != nil {
		return nil, err
	}
	if status != projects.StatusActive {
		return nil, ErrProjectLifecycleBlocked
	}
	ok, err := s.access.HasProjectPermission(projectID, userID, middleware.PermCreateIssue)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}

	relatedLogIDs := dedupeNonEmpty(req.RelatedLogIDs)
	if len(relatedLogIDs) > 0 {
		anyCount, err := s.repo.CountLogsByIDs(relatedLogIDs)
		if err != nil {
			return nil, err
		}
		if anyCount != len(relatedLogIDs) {
			return nil, ErrRelatedLogNotFound
		}
		projectCount, err := s.repo.CountRelatedLogs(projectID, relatedLogIDs)
		if err != nil {
			return nil, err
		}
		if projectCount != len(relatedLogIDs) {
			return nil, ErrRelatedLogProjectMismatch
		}
	}
	req.RelatedLogIDs = relatedLogIDs

	occurredAt := time.Now()
	if req.OccurredAt != nil && strings.TrimSpace(*req.OccurredAt) != "" {
		occurredAt, err = time.Parse(time.RFC3339, strings.TrimSpace(*req.OccurredAt))
		if err != nil {
			return nil, ErrInvalidInput
		}
	}
	reportDate := time.Now().Format(time.DateOnly)
	if req.ReportDate != nil && strings.TrimSpace(*req.ReportDate) != "" {
		reportDate = strings.TrimSpace(*req.ReportDate)
		if _, err := time.Parse(time.DateOnly, reportDate); err != nil {
			return nil, ErrInvalidInput
		}
	}
	return s.repo.Create(projectID, userID, req, occurredAt, reportDate)
}

func (s *Service) List(projectID, userID, userRole string, params IssueListParams) (*IssueListResult, error) {
	if params.PerPage > 100 {
		params.PerPage = 100
	}
	if params.Status == "" {
		params.Status = StatusOpen
	}
	if !validStatus(params.Status) || !validOptionalSeverity(params.Severity) || !validSort(params.Sort) || !validOrder(params.Order) {
		return nil, ErrInvalidInput
	}
	ok, err := s.access.HasProjectPermission(projectID, userID, middleware.PermRead)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	items, total, err := s.repo.List(projectID, params)
	if err != nil {
		return nil, err
	}
	page := params.Page
	if page < 1 {
		page = 1
	}
	return &IssueListResult{Items: items, Total: total, Page: page}, nil
}

func (s *Service) GetByID(id, userID, userRole string) (*Issue, error) {
	issue, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, ErrIssueNotFound
	}
	ok, err := s.access.HasProjectPermission(issue.ProjectID, userID, middleware.PermRead)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	return issue, nil
}

func (s *Service) Update(id, userID, userRole string, req UpdateIssueRequest) (*Issue, error) {
	issue, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, ErrIssueNotFound
	}
	if issue.Status == StatusClosed {
		return nil, ErrIssueClosed
	}
	ok, err := s.access.HasProjectPermission(issue.ProjectID, userID, middleware.PermUpdateIssue)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	if req.Title != nil {
		trimmed := strings.TrimSpace(*req.Title)
		if trimmed == "" || len(trimmed) > 256 {
			return nil, ErrInvalidInput
		}
		req.Title = &trimmed
	}
	if req.Severity != nil {
		trimmed := strings.TrimSpace(*req.Severity)
		if !validSeverity(trimmed) {
			return nil, ErrInvalidInput
		}
		req.Severity = &trimmed
	}
	if req.AssigneeID != nil {
		trimmed := strings.TrimSpace(*req.AssigneeID)
		req.AssigneeID = &trimmed
	}
	updated, err := s.repo.Update(id, req)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrIssueNotFound
	}
	return updated, nil
}

func (s *Service) Transition(id, userID, userRole string, req TransitionRequest) (*Issue, error) {
	req.TargetStatus = strings.TrimSpace(req.TargetStatus)
	req.Reason = strings.TrimSpace(req.Reason)
	if !validStatus(req.TargetStatus) {
		return nil, ErrInvalidInput
	}
	issue, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, ErrIssueNotFound
	}
	if issue.Status == req.TargetStatus {
		return nil, ErrInvalidTransition
	}
	if (issue.Status == StatusResolved || issue.Status == StatusClosed) && req.TargetStatus == StatusOpen && req.Reason == "" {
		return nil, ErrReasonRequired
	}
	if req.AddComment && req.Reason == "" {
		return nil, ErrInvalidInput
	}
	projectStatus, err := s.access.ProjectStatus(issue.ProjectID)
	if err != nil {
		return nil, err
	}
	if projectStatus != projects.StatusActive {
		return nil, ErrProjectLifecycleBlocked
	}
	ok, err := s.access.HasProjectPermission(issue.ProjectID, userID, middleware.PermUpdateIssue)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrTransitionForbidden
	}
	updated, err := s.repo.TransitionStatus(id, req.TargetStatus, userID, req.Reason, req.AddComment)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrIssueNotFound
	}
	return updated, nil
}

func (s *Service) AddComment(id, userID, userRole string, req AddCommentRequest) (*Comment, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, ErrInvalidInput
	}
	issue, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, ErrIssueNotFound
	}
	policy, err := s.access.ProjectCommentPolicy(issue.ProjectID)
	if err != nil {
		return nil, err
	}
	switch policy {
	case projects.CommentPolicyEveryone:
	case projects.CommentPolicyMembers:
		ok, err := s.access.HasProjectPermission(issue.ProjectID, userID, middleware.PermRead)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrForbidden
		}
	case projects.CommentPolicyDisabled:
		return nil, ErrCommentsDisabled
	default:
		return nil, ErrInvalidInput
	}
	return s.repo.AddComment(id, userID, content)
}

func (s *Service) GetComments(id, userID, userRole string, page, perPage int) ([]Comment, error) {
	issue, err := s.GetByID(id, userID, userRole)
	if err != nil {
		return nil, err
	}
	return s.repo.GetComments(issue.ID, page, perPage)
}

func validSeverity(v string) bool {
	switch strings.TrimSpace(v) {
	case SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	default:
		return false
	}
}

func validOptionalSeverity(v string) bool {
	return strings.TrimSpace(v) == "" || validSeverity(v)
}

func validStatus(v string) bool {
	switch strings.TrimSpace(v) {
	case StatusOpen, StatusInProgress, StatusResolved, StatusClosed:
		return true
	default:
		return false
	}
}

func validSort(v string) bool {
	switch strings.TrimSpace(v) {
	case "", "severity", "created", "updated":
		return true
	default:
		return false
	}
}

func validOrder(v string) bool {
	switch strings.TrimSpace(v) {
	case "", "asc", "desc":
		return true
	default:
		return false
	}
}

func dedupeNonEmpty(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

type ProjectAccessAdapter struct {
	DB   *sql.DB
	Repo interface {
		GetByID(id string) (*projects.Project, error)
	}
}

func (a ProjectAccessAdapter) ProjectExists(projectID string) (bool, error) {
	project, err := a.Repo.GetByID(projectID)
	return project != nil, err
}

func (a ProjectAccessAdapter) ProjectStatus(projectID string) (string, error) {
	project, err := a.Repo.GetByID(projectID)
	if err != nil || project == nil {
		return "", err
	}
	return project.Status, nil
}

func (a ProjectAccessAdapter) ProjectCommentPolicy(projectID string) (string, error) {
	project, err := a.Repo.GetByID(projectID)
	if err != nil || project == nil {
		return "", err
	}
	return project.CommentPolicy, nil
}

func (a ProjectAccessAdapter) HasProjectPermission(projectID, userID string, perm middleware.Permission) (bool, error) {
	return middleware.HasPermission(a.DB, projectID, userID, perm)
}
