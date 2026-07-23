package assembly

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrStepNotFound      = errors.New("装配步骤不存在")
	ErrProjectNotFound   = errors.New("项目不存在")
	ErrForbidden         = errors.New("当前用户无权访问该项目")
	ErrInvalidInput      = errors.New("请求参数无效")
	ErrInvalidTransition = errors.New("不允许的状态转移")
	ErrDependencyPending = errors.New("依赖步骤尚未完成")
	ErrDependencyCycle   = errors.New("装配步骤依赖形成环")
	ErrStepConflict      = errors.New("装配步骤状态已变化")
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	CanAccessProject(projectID, userID, userRole, minRole string) (bool, error)
}

type stepRepository interface {
	Create(step *AssemblyStep) error
	GetByID(id string) (*AssemblyStep, error)
	GetByProject(projectID string) ([]AssemblyStep, error)
	Update(id string, req UpdateStepRequest) error
	UpdateStatus(id, fromStatus, toStatus string, startedAt, completedAt *time.Time) error
	SoftDelete(id string) error
	Reorder(projectID string, items []ReorderItem) error
	GetDependencyChain(id string) ([]string, error)
	MaxStepOrder(projectID string) (int, error)
}

type Service struct {
	repo   stepRepository
	access ProjectAccessChecker
	now    func() time.Time
}

func NewService(repo stepRepository, access ProjectAccessChecker) *Service {
	return &Service{repo: repo, access: access, now: time.Now}
}

func (s *Service) Create(projectID, userID, userRole string, req CreateStepRequest) (*AssemblyStep, error) {
	projectID = strings.TrimSpace(projectID)
	if userRole == auth.RoleAgent {
		return nil, ErrForbidden
	}
	if err := s.requireProject(projectID); err != nil {
		return nil, err
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleMember); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 256 || req.StepOrder < 0 {
		return nil, ErrInvalidInput
	}
	if req.AssignedTo != nil && uuid.Validate(strings.TrimSpace(*req.AssignedTo)) != nil {
		return nil, ErrInvalidInput
	}
	if req.DependsOn != nil {
		dependencyID := strings.TrimSpace(*req.DependsOn)
		if err := s.validateDependency("", projectID, dependencyID); err != nil {
			return nil, err
		}
		req.DependsOn = &dependencyID
	}
	order := req.StepOrder
	if order == 0 {
		max, err := s.repo.MaxStepOrder(projectID)
		if err != nil {
			return nil, err
		}
		order = max + 1
	}
	creator := userID
	step := &AssemblyStep{ProjectID: projectID, Name: name, Description: strings.TrimSpace(req.Description),
		DependsOn: req.DependsOn, Status: StatusPlanned, AssignedTo: req.AssignedTo, StepOrder: order, CreatedBy: &creator}
	if err := s.repo.Create(step); err != nil {
		return nil, err
	}
	return step, nil
}

func (s *Service) List(projectID, userID, userRole string, params ListParams) (*ListResult, error) {
	projectID = strings.TrimSpace(projectID)
	if err := s.requireProject(projectID); err != nil {
		return nil, err
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleViewer); err != nil {
		return nil, err
	}
	status := strings.TrimSpace(params.Status)
	if status != "" && !validStatus(status) {
		return nil, ErrInvalidInput
	}
	items, err := s.repo.GetByProject(projectID)
	if err != nil {
		return nil, err
	}
	if status != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.Status == status {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	page, perPage := normalizePage(params.Page, params.PerPage)
	total, start := len(items), (page-1)*perPage
	if start >= total {
		items = []AssemblyStep{}
	} else {
		end := min(start+perPage, total)
		items = items[start:end]
	}
	return &ListResult{Items: items, Total: total, Page: page, PerPage: perPage}, nil
}

func (s *Service) GetByID(id, userID, userRole string) (*AssemblyStep, error) {
	step, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.requireAccess(step.ProjectID, userID, userRole, projects.RoleViewer); err != nil {
		return nil, err
	}
	return step, nil
}

func (s *Service) Update(id, userID, userRole string, req UpdateStepRequest) (*AssemblyStep, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrForbidden
	}
	step, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if req.Transition == nil {
		if req.OverrideReason != nil {
			return nil, ErrInvalidInput
		}
		if req.Name == nil && req.Description == nil && req.AssignedTo == nil {
			return nil, ErrInvalidInput
		}
		if err := s.requireAccess(step.ProjectID, userID, userRole, projects.RoleMaintainer); err != nil {
			return nil, err
		}
		if err := normalizeUpdate(&req); err != nil {
			return nil, err
		}
		if err := s.repo.Update(step.ID, req); err != nil {
			return nil, err
		}
	} else {
		if req.Name != nil || req.Description != nil || req.AssignedTo != nil {
			return nil, ErrInvalidInput
		}
		if err := s.requireAccess(step.ProjectID, userID, userRole, projects.RoleMember); err != nil {
			return nil, err
		}
		transition := strings.TrimSpace(*req.Transition)
		toStatus, ok := transitionTarget(step.Status, transition)
		if !ok {
			return nil, ErrInvalidTransition
		}
		if (transition == TransitionStart || transition == TransitionResume) && step.DependsOn != nil {
			if err := s.requireCompletedDependency(*step.DependsOn, req.OverrideReason); err != nil {
				return nil, err
			}
		}
		startedAt, completedAt := transitionTimes(step, transition, s.now())
		if err := s.repo.UpdateStatus(step.ID, step.Status, toStatus, startedAt, completedAt); err != nil {
			return nil, err
		}
	}
	return s.get(step.ID)
}

func (s *Service) Reorder(projectID, userID, userRole string, items []ReorderItem) error {
	projectID = strings.TrimSpace(projectID)
	if userRole == auth.RoleAgent {
		return ErrForbidden
	}
	if err := s.requireProject(projectID); err != nil {
		return err
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleMaintainer); err != nil {
		return err
	}
	if len(items) == 0 {
		return ErrInvalidInput
	}
	ids, orders := map[string]bool{}, map[int]bool{}
	for _, item := range items {
		if uuid.Validate(item.ID) != nil || item.StepOrder <= 0 || ids[item.ID] || orders[item.StepOrder] {
			return ErrInvalidInput
		}
		step, err := s.repo.GetByID(item.ID)
		if err != nil {
			return err
		}
		if step == nil {
			return ErrStepNotFound
		}
		if step.ProjectID != projectID {
			return ErrInvalidInput
		}
		ids[item.ID], orders[item.StepOrder] = true, true
	}
	return s.repo.Reorder(projectID, items)
}

func (s *Service) SoftDelete(id, userID, userRole string) error {
	if userRole == auth.RoleAgent {
		return ErrForbidden
	}
	step, err := s.get(id)
	if err != nil {
		return err
	}
	if step.CreatedBy == nil || *step.CreatedBy != userID {
		if err := s.requireAccess(step.ProjectID, userID, userRole, projects.RoleMaintainer); err != nil {
			return err
		}
	}
	return s.repo.SoftDelete(step.ID)
}

func (s *Service) validateDependency(stepID, projectID, dependencyID string) error {
	if uuid.Validate(dependencyID) != nil || dependencyID == stepID {
		return ErrInvalidInput
	}
	dependency, err := s.repo.GetByID(dependencyID)
	if err != nil {
		return err
	}
	if dependency == nil {
		return ErrStepNotFound
	}
	if dependency.ProjectID != projectID {
		return ErrInvalidInput
	}
	chain, err := s.repo.GetDependencyChain(dependencyID)
	if err != nil {
		return err
	}
	for _, id := range chain {
		if id == stepID {
			return ErrDependencyCycle
		}
	}
	return nil
}

func (s *Service) requireCompletedDependency(id string, overrideReason *string) error {
	dependency, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if dependency == nil {
		return ErrStepNotFound
	}
	if dependency.Status == StatusCompleted {
		return nil
	}
	if dependency.Status == StatusCancelled && overrideReason != nil && strings.TrimSpace(*overrideReason) != "" {
		return nil
	}
	return ErrDependencyPending
}

func (s *Service) get(id string) (*AssemblyStep, error) {
	id = strings.TrimSpace(id)
	if uuid.Validate(id) != nil {
		return nil, ErrInvalidInput
	}
	step, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if step == nil {
		return nil, ErrStepNotFound
	}
	return step, nil
}

func (s *Service) requireProject(projectID string) error {
	if uuid.Validate(projectID) != nil {
		return ErrInvalidInput
	}
	exists, err := s.access.ProjectExists(projectID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrProjectNotFound
	}
	return nil
}

func (s *Service) requireAccess(projectID, userID, userRole, minRole string) error {
	ok, err := s.access.CanAccessProject(projectID, userID, userRole, minRole)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func transitionTarget(status, transition string) (string, bool) {
	for _, allowed := range AllowedTransitions[status] {
		if transition == allowed {
			switch transition {
			case TransitionStart, TransitionResume:
				return StatusInProgress, true
			case TransitionPause:
				return StatusPaused, true
			case TransitionComplete:
				return StatusCompleted, true
			case TransitionSkip:
				return StatusSkipped, true
			case TransitionCancel:
				return StatusCancelled, true
			}
		}
	}
	return "", false
}

func transitionTimes(step *AssemblyStep, transition string, now time.Time) (*time.Time, *time.Time) {
	startedAt, completedAt := step.StartedAt, step.CompletedAt
	switch transition {
	case TransitionStart:
		startedAt, completedAt = &now, nil
	case TransitionPause, TransitionResume:
		completedAt = nil
	case TransitionComplete, TransitionSkip:
		if startedAt == nil {
			startedAt = &now
		}
		completedAt = &now
	case TransitionCancel:
		completedAt = &now
	}
	return startedAt, completedAt
}

func normalizeUpdate(req *UpdateStepRequest) error {
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 256 {
			return ErrInvalidInput
		}
		req.Name = &name
	}
	if req.Description != nil {
		description := strings.TrimSpace(*req.Description)
		req.Description = &description
	}
	if req.AssignedTo != nil {
		assignedTo := strings.TrimSpace(*req.AssignedTo)
		if uuid.Validate(assignedTo) != nil {
			return ErrInvalidInput
		}
		req.AssignedTo = &assignedTo
	}
	return nil
}

func validStatus(status string) bool {
	switch status {
	case StatusPlanned, StatusInProgress, StatusPaused, StatusCompleted, StatusSkipped, StatusCancelled:
		return true
	default:
		return false
	}
}

func normalizePage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
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
	rank := map[string]int{projects.RoleViewer: 1, projects.RoleMember: 2, projects.RoleMaintainer: 3, projects.RoleOwner: 4}
	return member != nil && member.Status == projects.MemberStatusActive && rank[member.Role] >= rank[minRole], nil
}
