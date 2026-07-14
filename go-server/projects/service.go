package projects

import (
	"errors"
	"strings"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
)

var (
	ErrProjectNotFound   = errors.New("项目不存在")
	ErrCodeTaken         = errors.New("项目代码已存在")
	ErrInvalidTransition = errors.New("项目状态流转不合法")
	ErrForbidden         = errors.New("当前用户无权执行该操作")
	ErrLastOwner         = errors.New("无法移除：项目至少需要一个 owner")
	ErrInvalidInput      = errors.New("请求参数无效")
	ErrUserNotFound      = errors.New("用户不存在")
	ErrTransitionWarning = errors.New("状态流转存在警告")
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(req CreateProjectRequest, userID string) (*Project, error) {
	code := strings.TrimSpace(req.Code)
	name := strings.TrimSpace(req.Name)
	if code == "" || name == "" {
		return nil, ErrInvalidInput
	}
	if err := validateDate(req.StartDate); err != nil {
		return nil, err
	}
	if err := validateDate(req.TargetEndDate); err != nil {
		return nil, err
	}
	taken, err := s.repo.IsCodeTaken(code)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrCodeTaken
	}

	project := &Project{
		Code:            code,
		Name:            name,
		ShortName:       strings.TrimSpace(req.ShortName),
		Description:     req.Description,
		Status:          StatusDraft,
		Visibility:      defaultString(req.Visibility, VisibilityRestricted),
		OwnerUserID:     userID,
		StartDate:       req.StartDate,
		TargetEndDate:   req.TargetEndDate,
		DefaultCategory: strings.TrimSpace(req.DefaultCategory),
		Tags:            req.Tags,
		CreatedBy:       userID,
	}
	if !validVisibility(project.Visibility) {
		return nil, ErrInvalidInput
	}

	created, err := s.repo.Create(project)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.AddMember(created.ID, userID, RoleOwner, userID); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Service) GetByID(id string) (*ProjectWithStats, error) {
	project, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, ErrProjectNotFound
	}
	return s.withStats(project)
}

func (s *Service) List(userID, userRole, status string) ([]ProjectWithStats, error) {
	if status != "" && !validStatus(status) {
		return nil, ErrInvalidInput
	}
	projects, err := s.repo.List(userID, status, userRole == auth.RoleAdmin)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectWithStats, 0, len(projects))
	for i := range projects {
		p := projects[i]
		item, err := s.withStats(&p)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) Update(id string, req UpdateProjectRequest) (*Project, error) {
	project, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, ErrProjectNotFound
	}
	if err := validateDate(req.StartDate); err != nil {
		return nil, err
	}
	if err := validateDate(req.TargetEndDate); err != nil {
		return nil, err
	}
	if req.Name != nil {
		project.Name = strings.TrimSpace(*req.Name)
		if project.Name == "" {
			return nil, ErrInvalidInput
		}
	}
	if req.ShortName != nil {
		project.ShortName = strings.TrimSpace(*req.ShortName)
	}
	if req.Description != nil {
		project.Description = *req.Description
	}
	if req.Visibility != nil {
		project.Visibility = strings.TrimSpace(*req.Visibility)
		if !validVisibility(project.Visibility) {
			return nil, ErrInvalidInput
		}
	}
	if req.StartDate != nil {
		project.StartDate = req.StartDate
	}
	if req.TargetEndDate != nil {
		project.TargetEndDate = req.TargetEndDate
	}
	if req.DefaultCategory != nil {
		project.DefaultCategory = strings.TrimSpace(*req.DefaultCategory)
	}
	if req.Tags != nil {
		project.Tags = req.Tags
	}

	updated, err := s.repo.Update(project)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrProjectNotFound
	}
	return updated, nil
}

func (s *Service) TransitionStatus(id string, req StatusTransitionRequest, userID, userRole string) (*Project, []TransitionWarning, error) {
	project, err := s.repo.GetByID(id)
	if err != nil {
		return nil, nil, err
	}
	if project == nil {
		return nil, nil, ErrProjectNotFound
	}

	target, err := targetStatus(project.Status, req.Action)
	if err != nil {
		return project, nil, err
	}
	if err := s.requireOwnerOrAdmin(project.ID, userID, userRole); err != nil {
		return project, nil, err
	}
	if project.Status == StatusArchived && target == StatusActive {
		if strings.TrimSpace(req.Reason) == "" {
			return project, nil, ErrInvalidInput
		}
	}

	var warnings []TransitionWarning
	if project.Status == StatusActive && target == StatusCompleted {
		_, openIssues, _, err := s.repo.GetStats(project.ID)
		if err != nil {
			return nil, nil, err
		}
		if openIssues > 0 {
			warnings = append(warnings, TransitionWarning{
				Code:    "open_issues",
				Message: "项目仍有未解决 Issue",
				Count:   openIssues,
			})
			if !req.IgnoreWarnings {
				return project, warnings, ErrTransitionWarning
			}
		}
	}

	updated, err := s.repo.UpdateStatus(project.ID, target)
	if err != nil {
		return nil, nil, err
	}
	if updated == nil {
		return nil, nil, ErrProjectNotFound
	}
	return updated, warnings, nil
}

func (s *Service) AddMember(projectID string, req AddMemberRequest, addedBy string) (*ProjectMember, error) {
	if strings.TrimSpace(req.UserID) == "" || !validProjectRole(req.Role) {
		return nil, ErrInvalidInput
	}
	if _, err := s.mustGetProject(projectID); err != nil {
		return nil, err
	}
	exists, err := s.repo.UserExists(req.UserID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrUserNotFound
	}
	return s.repo.AddMember(projectID, req.UserID, req.Role, addedBy)
}

func (s *Service) RemoveMember(projectID, userID string) error {
	member, err := s.repo.GetMember(projectID, userID)
	if err != nil {
		return err
	}
	if member == nil {
		return ErrProjectNotFound
	}
	if member.Role == RoleOwner {
		count, err := s.repo.CountOwners(projectID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastOwner
		}
	}
	return s.repo.RemoveMember(projectID, userID)
}

func (s *Service) UpdateMemberRole(projectID, userID string, req UpdateMemberRequest) (*ProjectMember, error) {
	if !validProjectRole(req.Role) {
		return nil, ErrInvalidInput
	}
	member, err := s.repo.GetMember(projectID, userID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, ErrProjectNotFound
	}
	if member.Role == RoleOwner && req.Role != RoleOwner {
		count, err := s.repo.CountOwners(projectID)
		if err != nil {
			return nil, err
		}
		if count <= 1 {
			return nil, ErrLastOwner
		}
	}
	updated, err := s.repo.UpdateMemberRole(projectID, userID, req.Role)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrProjectNotFound
	}
	return updated, nil
}

func (s *Service) ListMembers(projectID string) ([]ProjectMember, error) {
	if _, err := s.mustGetProject(projectID); err != nil {
		return nil, err
	}
	return s.repo.ListMembers(projectID)
}

func (s *Service) mustGetProject(id string) (*Project, error) {
	project, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, ErrProjectNotFound
	}
	return project, nil
}

func (s *Service) withStats(project *Project) (*ProjectWithStats, error) {
	memberCount, openIssueCount, logCount, err := s.repo.GetStats(project.ID)
	if err != nil {
		return nil, err
	}
	return &ProjectWithStats{
		Project:        *project,
		MemberCount:    memberCount,
		OpenIssueCount: openIssueCount,
		LogCount:       logCount,
	}, nil
}

func (s *Service) requireOwnerOrAdmin(projectID, userID, userRole string) error {
	if userRole == auth.RoleAdmin {
		return nil
	}
	member, err := s.repo.GetMember(projectID, userID)
	if err != nil {
		return err
	}
	if member == nil || member.Status != MemberStatusActive || member.Role != RoleOwner {
		return ErrForbidden
	}
	return nil
}

func targetStatus(current, action string) (string, error) {
	switch strings.TrimSpace(action) {
	case "activate":
		if current == StatusDraft {
			return StatusActive, nil
		}
	case "complete":
		if current == StatusActive {
			return StatusCompleted, nil
		}
	case "archive":
		if current == StatusCompleted {
			return StatusArchived, nil
		}
	case "reactivate":
		if current == StatusArchived {
			return StatusActive, nil
		}
	}
	return "", ErrInvalidTransition
}

func validateDate(s *string) error {
	if s == nil || *s == "" {
		return nil
	}
	if _, err := time.Parse(time.DateOnly, *s); err != nil {
		return ErrInvalidInput
	}
	return nil
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func validStatus(status string) bool {
	switch status {
	case StatusDraft, StatusActive, StatusCompleted, StatusArchived:
		return true
	default:
		return false
	}
}

func validVisibility(v string) bool {
	switch v {
	case VisibilityRestricted, VisibilityWorkspace:
		return true
	default:
		return false
	}
}

func validProjectRole(role string) bool {
	switch role {
	case RoleOwner, RoleMaintainer, RoleMember, RoleViewer:
		return true
	default:
		return false
	}
}
