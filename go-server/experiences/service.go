package experiences

import (
	"errors"
	"strings"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrExperienceNotFound        = errors.New("经验不存在")
	ErrNotCandidate              = errors.New("只有候选经验可以修改或发布")
	ErrNotPublished              = errors.New("只有已发布经验可以归档")
	ErrPublishForbidden          = errors.New("当前用户无权发布经验")
	ErrGlobalExperienceAdminOnly = errors.New("全局经验仅管理员可创建、发布或归档")
	ErrInvalidInput              = errors.New("请求参数无效")
	ErrProjectNotFound           = errors.New("项目不存在")
	ErrForbidden                 = errors.New("当前用户无权访问该项目")
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	CanAccessProject(projectID, userID, userRole, minRole string) (bool, error)
	ProjectRole(projectID, userID, userRole string) (string, error)
}

type AgentTaskValidator interface {
	ValidateAgentTask(taskID, actingUserID string) (bool, error)
}

type experienceRepository interface {
	Create(authorID string, req CreateExperienceRequest) (*Experience, error)
	GetByID(id string) (*Experience, error)
	List(params ExperienceListParams) ([]Experience, int, error)
	Update(id string, req UpdateExperienceRequest) (*Experience, error)
	Publish(id, reviewerID string) (*Experience, error)
	Archive(id string) (*Experience, error)
}

type Service struct {
	repo      experienceRepository
	access    ProjectAccessChecker
	validator AgentTaskValidator
}

func NewService(repo experienceRepository, access ProjectAccessChecker, validators ...AgentTaskValidator) *Service {
	s := &Service{repo: repo, access: access}
	if len(validators) > 0 {
		s.validator = validators[0]
	}
	return s
}

func (s *Service) Create(userID, userRole string, req CreateExperienceRequest) (*Experience, error) {
	if err := s.validateAgentFields(userID, userRole, req.AiGenerated, req.AgentTaskID); err != nil {
		return nil, ErrInvalidInput
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	if req.Title == "" || len(req.Title) > 256 || req.Content == "" {
		return nil, ErrInvalidInput
	}
	req.Tags = normalizeTags(req.Tags)
	links, err := s.normalizeLinks(req.LinkedProjects)
	if err != nil {
		return nil, err
	}
	req.LinkedProjects = links
	if req.ProjectID != nil {
		projectID := strings.TrimSpace(*req.ProjectID)
		if projectID == "" {
			req.ProjectID = nil
		} else {
			req.ProjectID = &projectID
		}
	}

	if req.ProjectID == nil {
		if userRole != auth.RoleAdmin {
			return nil, ErrGlobalExperienceAdminOnly
		}
		return s.repo.Create(userID, req)
	}

	if err := s.requireProject(*req.ProjectID); err != nil {
		return nil, err
	}
	ok, err := s.access.CanAccessProject(*req.ProjectID, userID, userRole, projects.RoleMember)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	return s.repo.Create(userID, req)
}

func (s *Service) validateAgentFields(userID, userRole string, aiGenerated bool, taskID *string) error {
	if userRole != auth.RoleAgent {
		if aiGenerated || taskID != nil {
			return ErrInvalidInput
		}
		return nil
	}
	if !aiGenerated || taskID == nil || strings.TrimSpace(*taskID) == "" || s.validator == nil {
		return ErrInvalidInput
	}
	valid, err := s.validator.ValidateAgentTask(strings.TrimSpace(*taskID), userID)
	if err != nil {
		return err
	}
	if !valid {
		return ErrInvalidInput
	}
	return nil
}

func (s *Service) List(userID, userRole string, params ExperienceListParams) (*ExperienceListResult, error) {
	params.Status = strings.TrimSpace(params.Status)
	if params.Status == "" {
		params.Status = StatusPublished
	}
	if !validStatus(params.Status) {
		return nil, ErrInvalidInput
	}
	params.Tags = normalizeTags(params.Tags)
	params.Keyword = strings.TrimSpace(params.Keyword)
	params.UserRole = userRole
	if params.PerPage > 100 {
		params.PerPage = 100
	}
	if strings.TrimSpace(params.ProjectID) != "" {
		params.ProjectID = strings.TrimSpace(params.ProjectID)
		ok, err := s.access.CanAccessProject(params.ProjectID, userID, userRole, projects.RoleViewer)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrForbidden
		}
		role, err := s.access.ProjectRole(params.ProjectID, userID, userRole)
		if err != nil {
			return nil, err
		}
		params.ProjectRole = role
	}
	if params.Status == StatusCandidate && userRole != auth.RoleAdmin && !canReviewProjectRole(params.ProjectRole) {
		params.CandidateAuthorID = userID
	}

	items, total, err := s.repo.List(params)
	if err != nil {
		return nil, err
	}
	page, perPage := normalizePage(params.Page, params.PerPage)
	if perPage > 100 {
		perPage = 100
	}
	return &ExperienceListResult{Items: items, Total: total, Page: page, PerPage: perPage}, nil
}

func (s *Service) GetByID(id, userID, userRole string) (*Experience, error) {
	exp, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if exp == nil {
		return nil, ErrExperienceNotFound
	}
	if !s.canRead(*exp, userID, userRole) {
		return nil, ErrForbidden
	}
	return exp, nil
}

func (s *Service) Update(id, userID, userRole string, req UpdateExperienceRequest) (*Experience, error) {
	exp, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if exp == nil {
		return nil, ErrExperienceNotFound
	}
	if exp.Status != StatusCandidate {
		return nil, ErrNotCandidate
	}
	if !s.canUpdate(*exp, userID, userRole) {
		return nil, ErrForbidden
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" || len(title) > 256 {
			return nil, ErrInvalidInput
		}
		req.Title = &title
	}
	if req.Content != nil {
		content := strings.TrimSpace(*req.Content)
		if content == "" {
			return nil, ErrInvalidInput
		}
		req.Content = &content
	}
	if req.Tags != nil {
		req.Tags = normalizeTags(req.Tags)
	}
	if req.LinkedProjects != nil {
		links, err := s.normalizeLinks(req.LinkedProjects)
		if err != nil {
			return nil, err
		}
		req.LinkedProjects = links
	}
	updated, err := s.repo.Update(id, req)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) Publish(id, userID, userRole string) (*Experience, error) {
	exp, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if exp == nil {
		return nil, ErrExperienceNotFound
	}
	if exp.Status != StatusCandidate {
		return nil, ErrNotCandidate
	}
	if exp.ProjectID == nil {
		if userRole != auth.RoleAdmin {
			return nil, ErrGlobalExperienceAdminOnly
		}
		return s.repo.Publish(id, userID)
	}
	ok, err := s.access.CanAccessProject(*exp.ProjectID, userID, userRole, projects.RoleMaintainer)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrPublishForbidden
	}
	return s.repo.Publish(id, userID)
}

func (s *Service) Archive(id, userID, userRole string) (*Experience, error) {
	exp, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if exp == nil {
		return nil, ErrExperienceNotFound
	}
	if exp.Status != StatusPublished {
		return nil, ErrNotPublished
	}
	if exp.ProjectID == nil {
		if userRole != auth.RoleAdmin {
			return nil, ErrGlobalExperienceAdminOnly
		}
		return s.repo.Archive(id)
	}
	ok, err := s.access.CanAccessProject(*exp.ProjectID, userID, userRole, projects.RoleOwner)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	return s.repo.Archive(id)
}

func (s *Service) canRead(exp Experience, userID, userRole string) bool {
	if userRole == auth.RoleAdmin {
		return true
	}
	if exp.Status == StatusCandidate {
		if exp.AuthorID == userID {
			return true
		}
		if exp.ProjectID == nil {
			return false
		}
		return s.canAccess(*exp.ProjectID, userID, userRole, projects.RoleMaintainer)
	}
	if exp.ProjectID == nil {
		return true
	}
	return s.canAccess(*exp.ProjectID, userID, userRole, projects.RoleViewer)
}

func (s *Service) canUpdate(exp Experience, userID, userRole string) bool {
	if userRole == auth.RoleAdmin || exp.AuthorID == userID {
		return true
	}
	if exp.ProjectID == nil {
		return false
	}
	return s.canAccess(*exp.ProjectID, userID, userRole, projects.RoleMaintainer)
}

func (s *Service) canAccess(projectID, userID, userRole, minRole string) bool {
	ok, err := s.access.CanAccessProject(projectID, userID, userRole, minRole)
	return err == nil && ok
}

func (s *Service) requireProject(projectID string) error {
	exists, err := s.access.ProjectExists(projectID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrProjectNotFound
	}
	return nil
}

func (s *Service) normalizeLinks(in []ExperienceProjectLink) ([]ExperienceProjectLink, error) {
	seen := map[string]bool{}
	out := make([]ExperienceProjectLink, 0, len(in))
	for _, link := range in {
		projectID := strings.TrimSpace(link.ProjectID)
		if projectID == "" || seen[projectID] {
			continue
		}
		if err := s.requireProject(projectID); err != nil {
			return nil, err
		}
		relation := strings.TrimSpace(link.Relation)
		if relation == "" {
			relation = RelationApplicable
		}
		if !validRelation(relation) {
			return nil, ErrInvalidInput
		}
		seen[projectID] = true
		out = append(out, ExperienceProjectLink{ProjectID: projectID, Relation: relation})
	}
	return out, nil
}

func normalizeTags(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, tag := range in {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

func validStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case StatusCandidate, StatusPublished, StatusArchived:
		return true
	default:
		return false
	}
}

func validRelation(relation string) bool {
	switch relation {
	case RelationPrimary, RelationApplicable, RelationDerivedFrom:
		return true
	default:
		return false
	}
}

func canReviewProjectRole(role string) bool {
	switch role {
	case "maintainer", "owner":
		return true
	default:
		return false
	}
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

func (a ProjectAccessAdapter) CanAccessProject(projectID, userID, userRole, minRole string) (bool, error) {
	if userRole == auth.RoleAdmin {
		return true, nil
	}
	member, err := a.Repo.GetMember(projectID, userID)
	if err != nil {
		return false, err
	}
	roleRank := map[string]int{"viewer": 1, "member": 2, "maintainer": 3, "owner": 4}
	return member != nil && member.Status == projects.MemberStatusActive && roleRank[member.Role] >= roleRank[minRole], nil
}

func (a ProjectAccessAdapter) ProjectRole(projectID, userID, userRole string) (string, error) {
	if userRole == auth.RoleAdmin {
		return projects.RoleOwner, nil
	}
	member, err := a.Repo.GetMember(projectID, userID)
	if err != nil || member == nil || member.Status != projects.MemberStatusActive {
		return "", err
	}
	return member.Role, nil
}
