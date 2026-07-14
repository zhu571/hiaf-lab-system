package issues

import (
	"errors"
	"strings"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
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
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	ProjectStatus(projectID string) (string, error)
	CanAccessProject(projectID, userID, userRole, minRole string) (bool, error)
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
	ok, err := s.access.CanAccessProject(projectID, userID, userRole, projects.RoleMember)
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
	ok, err := s.access.CanAccessProject(projectID, userID, userRole, projects.RoleViewer)
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
	ok, err := s.access.CanAccessProject(issue.ProjectID, userID, userRole, projects.RoleViewer)
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
	if !s.canUpdateIssue(*issue, userID, userRole) {
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
	if !s.canTransition(*issue, userID, userRole, req.TargetStatus) {
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
	ok, err := s.access.CanAccessProject(issue.ProjectID, userID, userRole, projects.RoleViewer)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
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

func (s *Service) canUpdateIssue(issue Issue, userID, userRole string) bool {
	if issue.AuthorID == userID {
		return true
	}
	ok, err := s.access.CanAccessProject(issue.ProjectID, userID, userRole, projects.RoleMaintainer)
	return err == nil && ok
}

func (s *Service) canTransition(issue Issue, userID, userRole, target string) bool {
	isAssignee := issue.AssigneeID != nil && *issue.AssigneeID == userID
	isAuthor := issue.AuthorID == userID
	isOwner := s.canAccess(issue.ProjectID, userID, userRole, projects.RoleOwner)
	isMaintainer := s.canAccess(issue.ProjectID, userID, userRole, projects.RoleMaintainer)
	isAdmin := userRole == auth.RoleAdmin

	if target == StatusClosed && (isOwner || isAdmin) {
		return true
	}
	switch {
	case issue.Status == StatusOpen && target == StatusInProgress:
		return isAssignee || isMaintainer || isOwner || isAdmin
	case issue.Status == StatusInProgress && target == StatusResolved:
		return isAssignee || isOwner || isAdmin
	case issue.Status == StatusResolved && target == StatusClosed:
		return isAuthor || isOwner || isAdmin
	case (issue.Status == StatusResolved || issue.Status == StatusClosed) && target == StatusOpen:
		return isOwner || isAdmin
	default:
		return false
	}
}

func (s *Service) canAccess(projectID, userID, userRole, minRole string) bool {
	ok, err := s.access.CanAccessProject(projectID, userID, userRole, minRole)
	return err == nil && ok
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
	Repo interface {
		GetByID(id string) (*projects.Project, error)
		GetMember(projectID, userID string) (*projects.ProjectMember, error)
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

func (a ProjectAccessAdapter) CanAccessProject(projectID, userID, userRole, minRole string) (bool, error) {
	if userRole == auth.RoleAdmin {
		return true, nil
	}
	member, err := a.Repo.GetMember(projectID, userID)
	if err != nil {
		return false, err
	}
	return member != nil && member.Status == projects.MemberStatusActive && middleware.ProjectRoleRank(member.Role) >= middleware.ProjectRoleRank(minRole), nil
}
