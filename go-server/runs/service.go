package runs

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrRunNotFound        = errors.New("实验批次不存在")
	ErrProjectNotFound    = errors.New("项目不存在")
	ErrReportLinkNotFound = errors.New("日报关联不存在")
	ErrStepNotFound       = errors.New("实验步骤不存在")
	ErrForbidden          = errors.New("当前用户无权访问该项目")
	ErrInvalidInput       = errors.New("请求参数无效")
	ErrInvalidTransition  = errors.New("不允许的状态转移")
	ErrRunConflict        = errors.New("实验批次状态已变化")
	ErrStepConflict       = errors.New("实验步骤状态已变化")
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	CanAccessProject(projectID, userID, userRole, minRole string) (bool, error)
}

type runRepository interface {
	Create(run *ExperimentRun) error
	GetByID(id string) (*ExperimentRun, error)
	List(params RunListParams) ([]ExperimentRun, int, error)
	Update(id string, req UpdateRunRequest) error
	UpdateStatus(id, fromStatus, toStatus string, shouldHaveStartedAt, shouldHaveEndedAt bool) error
	SoftDelete(id string) error
	AddReportLink(runID, reportID string) error
	RemoveReportLink(runID, reportID string) error
	GetReportLinks(runID string) ([]string, error)
	ListSteps(runID string) ([]RunStep, error)
	CreateStep(step *RunStep) error
	CreateStepsMany(runID, userID string, steps []StepDef, startOrder int) ([]RunStep, error)
	GetStepByID(id string) (*RunStep, error)
	UpdateStep(id string, req UpdateStepRequest) error
	UpdateStepStatus(id, fromStatus, toStatus string, startedAt, completedAt *time.Time) error
	SoftDeleteStep(id string) error
	Reorder(runID string, items []ReorderItem) error
	MaxStepOrder(runID string) (int, error)
	SetSourceTemplateID(stepID, templateID string) error
}

type TemplateReader interface {
	GetTemplateWithItems(id string) (*SteptemplatesTemplate, []SteptemplatesItem, error)
}

type ReportReader interface {
	GetReportSummaries(ids []string, userID, userRole string) ([]LinkedDailyReport, error)
}

type SteptemplatesTemplate struct {
	ID   string
	Name string
	Kind string
}

type SteptemplatesItem struct {
	ID             string
	Name           string
	Description    string
	StepOrder      int
	DependsOnOrder *int
}

type Service struct {
	repo       runRepository
	access     ProjectAccessChecker
	tmplReader TemplateReader
	reports    ReportReader
}

func NewService(repo runRepository, access ProjectAccessChecker) *Service {
	return &Service{repo: repo, access: access}
}

func (s *Service) ConfigureTemplates(reader TemplateReader) {
	s.tmplReader = reader
}

func (s *Service) ConfigureReports(reader ReportReader) { s.reports = reader }

func (s *Service) Create(projectID, userID, userRole string, req CreateRunRequest) (*ExperimentRun, error) {
	projectID = strings.TrimSpace(projectID)
	if err := s.requireProject(projectID); err != nil {
		return nil, err
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleMember); err != nil {
		return nil, err
	}
	run := &ExperimentRun{
		ProjectID: projectID, Name: req.Name, Campaign: req.Campaign, RunType: RunTypeTest,
		Status: StatusPlanned, GasType: GasTypeHe, TargetTemp: req.TargetTemp, MinTemp: req.MinTemp,
		PressureMin: req.PressureMin, PressureMax: req.PressureMax, PressureUnit: "mbar",
		Devices: req.Devices, Description: strings.TrimSpace(req.Description), CreatedBy: &userID,
	}
	if req.RunType != nil {
		run.RunType = *req.RunType
	}
	if req.GasType != nil {
		run.GasType = *req.GasType
	}
	if req.PressureUnit != nil {
		run.PressureUnit = *req.PressureUnit
	}
	if req.HasBeam != nil {
		run.HasBeam = *req.HasBeam
	}
	if err := normalizeAndValidateRun(run); err != nil {
		return nil, err
	}
	if err := s.repo.Create(run); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Service) List(projectID, userID, userRole string, params RunListParams) (*RunListResult, error) {
	projectID = strings.TrimSpace(projectID)
	if err := s.requireProject(projectID); err != nil {
		return nil, err
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleViewer); err != nil {
		return nil, err
	}
	params.ProjectID = projectID
	params.Campaign = strings.TrimSpace(params.Campaign)
	params.Status = strings.TrimSpace(params.Status)
	params.RunType = strings.TrimSpace(params.RunType)
	if params.Status != "" && !validStatus(params.Status) || params.RunType != "" && !validRunType(params.RunType) {
		return nil, ErrInvalidInput
	}
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	items, total, err := s.repo.List(params)
	if err != nil {
		return nil, err
	}
	return &RunListResult{Items: items, Total: total, Page: params.Page, PerPage: params.PerPage}, nil
}

func (s *Service) GetByID(id, userID, userRole string) (*ExperimentRun, error) {
	run, err := s.getAccessible(id, userID, userRole, projects.RoleViewer, false)
	if err != nil {
		return nil, err
	}
	ids, err := s.repo.GetReportLinks(run.ID)
	if err != nil {
		return nil, err
	}
	run.DailyReports = make([]LinkedDailyReport, len(ids))
	for i, id := range ids {
		run.DailyReports[i].ID = id
	}
	if s.reports != nil && len(ids) > 0 {
		run.DailyReports, err = s.reports.GetReportSummaries(ids, userID, userRole)
		if err != nil {
			return nil, err
		}
	}
	return run, nil
}

func (s *Service) Update(id, userID, userRole string, req UpdateRunRequest) (*ExperimentRun, error) {
	run, err := s.getAccessible(id, userID, userRole, projects.RoleMaintainer, true)
	if err != nil {
		return nil, err
	}
	if req.Transition != nil {
		if hasMetadataUpdate(req) {
			return nil, ErrInvalidInput
		}
		toStatus, started, ended, err := targetTransition(run.Status, strings.TrimSpace(*req.Transition))
		if err != nil {
			return nil, err
		}
		if err := s.repo.UpdateStatus(id, run.Status, toStatus, started, ended); err != nil {
			return nil, err
		}
	} else {
		candidate := *run
		applyUpdate(&candidate, &req)
		if err := normalizeAndValidateRun(&candidate); err != nil {
			return nil, err
		}
		normalizeUpdateRequest(&req, &candidate)
		if err := s.repo.Update(id, req); err != nil {
			return nil, err
		}
	}
	updated, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrRunNotFound
	}
	return updated, nil
}

func (s *Service) SoftDelete(id, userID, userRole string) error {
	if _, err := s.getAccessible(id, userID, userRole, projects.RoleMaintainer, true); err != nil {
		return err
	}
	return s.repo.SoftDelete(id)
}

func (s *Service) AddReportLink(runID, reportID, userID, userRole string) ([]string, error) {
	if _, err := s.getAccessible(runID, userID, userRole, projects.RoleMaintainer, true); err != nil {
		return nil, err
	}
	if strings.TrimSpace(reportID) == "" {
		return nil, ErrInvalidInput
	}
	if err := s.repo.AddReportLink(runID, reportID); err != nil {
		return nil, err
	}
	return s.repo.GetReportLinks(runID)
}

func (s *Service) RemoveReportLink(runID, reportID, userID, userRole string) ([]string, error) {
	if _, err := s.getAccessible(runID, userID, userRole, projects.RoleMaintainer, true); err != nil {
		return nil, err
	}
	if err := s.repo.RemoveReportLink(runID, reportID); err != nil {
		return nil, err
	}
	return s.repo.GetReportLinks(runID)
}

func (s *Service) ListSteps(runID, userID, userRole string) (*StepListResult, error) {
	run, err := s.getAccessible(runID, userID, userRole, projects.RoleViewer, false)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListSteps(run.ID)
	if err != nil {
		return nil, err
	}
	return &StepListResult{Items: items, Total: len(items)}, nil
}

func (s *Service) CreateStep(runID, userID, userRole string, req CreateStepRequest) (*RunStep, error) {
	run, err := s.getAccessible(runID, userID, userRole, projects.RoleMember, false)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 256 || req.StepOrder < 0 {
		return nil, ErrInvalidInput
	}
	if req.DependsOn != nil {
		dependencyID := strings.TrimSpace(*req.DependsOn)
		if err := s.validateStepDependency("", run.ID, dependencyID); err != nil {
			return nil, err
		}
		req.DependsOn = &dependencyID
	}
	order := req.StepOrder
	if order == 0 {
		max, err := s.repo.MaxStepOrder(run.ID)
		if err != nil {
			return nil, err
		}
		order = max + 1
	}
	step := &RunStep{RunID: run.ID, Name: name, Description: strings.TrimSpace(req.Description),
		DependsOn: req.DependsOn, Status: StepStatusPlanned, StepOrder: order, CreatedBy: &userID}
	if err := s.repo.CreateStep(step); err != nil {
		return nil, err
	}
	return step, nil
}

func (s *Service) UpdateStep(stepID, userID, userRole string, req UpdateStepRequest) (*RunStep, error) {
	step, err := s.getStep(stepID)
	if err != nil {
		return nil, err
	}
	if req.Transition == nil {
		if req.Name == nil && req.Description == nil && req.DependsOn == nil {
			return nil, ErrInvalidInput
		}
		if err := s.requireStepAccess(step, userID, userRole, projects.RoleMaintainer); err != nil {
			return nil, err
		}
		if err := s.normalizeStepUpdate(step, &req); err != nil {
			return nil, err
		}
		if err := s.repo.UpdateStep(step.ID, req); err != nil {
			return nil, err
		}
	} else {
		if req.Name != nil || req.Description != nil || req.DependsOn != nil {
			return nil, ErrInvalidInput
		}
		if err := s.requireStepAccess(step, userID, userRole, projects.RoleMember); err != nil {
			return nil, err
		}
		transition := strings.TrimSpace(*req.Transition)
		toStatus, ok := stepTransitionTarget(step.Status, transition)
		if !ok {
			return nil, ErrInvalidTransition
		}
		startedAt, completedAt := stepTransitionTimes(step, transition, time.Now())
		if err := s.repo.UpdateStepStatus(step.ID, step.Status, toStatus, startedAt, completedAt); err != nil {
			return nil, err
		}
	}
	return s.getStep(step.ID)
}

func (s *Service) DeleteStep(stepID, userID, userRole string) error {
	step, err := s.getStep(stepID)
	if err != nil {
		return err
	}
	if step.CreatedBy == nil || *step.CreatedBy != userID {
		if err := s.requireStepAccess(step, userID, userRole, projects.RoleMaintainer); err != nil {
			return err
		}
	}
	return s.repo.SoftDeleteStep(step.ID)
}

func (s *Service) ReorderSteps(runID, userID, userRole string, items []ReorderItem) error {
	run, err := s.getAccessible(runID, userID, userRole, projects.RoleMaintainer, false)
	if err != nil {
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
		step, err := s.repo.GetStepByID(item.ID)
		if err != nil {
			return err
		}
		if step == nil {
			return ErrStepNotFound
		}
		if step.RunID != run.ID {
			return ErrInvalidInput
		}
		ids[item.ID], orders[item.StepOrder] = true, true
	}
	return s.repo.Reorder(run.ID, items)
}

func (s *Service) ApplyTemplate(runID, userID, userRole string, req ApplyTemplateRequest) ([]RunStep, error) {
	run, err := s.getAccessible(runID, userID, userRole, projects.RoleMember, false)
	if err != nil {
		return nil, err
	}
	hasTemplateID := req.TemplateID != nil && strings.TrimSpace(*req.TemplateID) != ""
	hasInlineSteps := len(req.Steps) > 0
	if hasTemplateID == hasInlineSteps {
		return nil, ErrInvalidInput
	}
	if hasTemplateID && s.tmplReader == nil {
		return nil, errors.New("模板读取服务未配置")
	}

	var stepDefs []StepDef
	var sourceTemplateID *string

	if hasTemplateID {
		tmpl, items, err := s.tmplReader.GetTemplateWithItems(strings.TrimSpace(*req.TemplateID))
		if err != nil {
			return nil, err
		}
		if tmpl == nil {
			return nil, errors.New("模板不存在")
		}
		if tmpl.Kind != "experiment" {
			return nil, common.NewError("bad_request", "模板类型不匹配，期望 experiment", nil)
		}
		sourceTemplateID = &tmpl.ID
		stepDefs = make([]StepDef, len(items))
		for i, item := range items {
			stepDefs[i] = StepDef{
				Name:           item.Name,
				Description:    item.Description,
				StepOrder:      item.StepOrder,
				DependsOnOrder: item.DependsOnOrder,
			}
		}
	} else {
		if len(req.Steps) > 30 {
			return nil, errors.New("步骤数不能超过 30")
		}
		stepDefs = req.Steps
	}

	maxOrder, err := s.repo.MaxStepOrder(run.ID)
	if err != nil {
		return nil, err
	}

	steps, err := s.repo.CreateStepsMany(run.ID, userID, stepDefs, maxOrder)
	if err != nil {
		return nil, err
	}

	if sourceTemplateID != nil {
		for i := range steps {
			if err := s.repo.SetSourceTemplateID(steps[i].ID, *sourceTemplateID); err != nil {
				return nil, err
			}
		}
	}

	return steps, nil
}

// validateStepDependency 校验依赖步骤存在且属于同一实验批次；依赖只存不强制。
func (s *Service) validateStepDependency(stepID, runID, dependencyID string) error {
	if uuid.Validate(dependencyID) != nil || dependencyID == stepID {
		return ErrInvalidInput
	}
	dependency, err := s.repo.GetStepByID(dependencyID)
	if err != nil {
		return err
	}
	if dependency == nil {
		return ErrStepNotFound
	}
	if dependency.RunID != runID {
		return ErrInvalidInput
	}
	return nil
}

func (s *Service) normalizeStepUpdate(step *RunStep, req *UpdateStepRequest) error {
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
	if req.DependsOn != nil {
		dependencyID := strings.TrimSpace(*req.DependsOn)
		if err := s.validateStepDependency(step.ID, step.RunID, dependencyID); err != nil {
			return err
		}
		req.DependsOn = &dependencyID
	}
	return nil
}

func (s *Service) getStep(id string) (*RunStep, error) {
	id = strings.TrimSpace(id)
	if uuid.Validate(id) != nil {
		return nil, ErrInvalidInput
	}
	step, err := s.repo.GetStepByID(id)
	if err != nil {
		return nil, err
	}
	if step == nil {
		return nil, ErrStepNotFound
	}
	return step, nil
}

func (s *Service) requireStepAccess(step *RunStep, userID, userRole, minRole string) error {
	run, err := s.repo.GetByID(step.RunID)
	if err != nil {
		return err
	}
	if run == nil {
		return ErrRunNotFound
	}
	return s.requireAccess(run.ProjectID, userID, userRole, minRole)
}

func stepTransitionTarget(status, transition string) (string, bool) {
	for _, allowed := range StepAllowedTransitions[status] {
		if transition == allowed {
			switch transition {
			case StepTransitionStart, StepTransitionResume:
				return StepStatusInProgress, true
			case StepTransitionPause:
				return StepStatusPaused, true
			case StepTransitionComplete:
				return StepStatusCompleted, true
			case StepTransitionSkip:
				return StepStatusSkipped, true
			case StepTransitionCancel:
				return StepStatusCancelled, true
			}
		}
	}
	return "", false
}

func stepTransitionTimes(step *RunStep, transition string, now time.Time) (*time.Time, *time.Time) {
	startedAt, completedAt := step.StartedAt, step.CompletedAt
	switch transition {
	case StepTransitionStart:
		startedAt, completedAt = &now, nil
	case StepTransitionPause, StepTransitionResume:
		completedAt = nil
	case StepTransitionComplete, StepTransitionSkip:
		if startedAt == nil {
			startedAt = &now
		}
		completedAt = &now
	case StepTransitionCancel:
		completedAt = &now
	}
	return startedAt, completedAt
}

func (s *Service) getAccessible(id, userID, userRole, minRole string, creatorAllowed bool) (*ExperimentRun, error) {
	run, err := s.repo.GetByID(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrRunNotFound
	}
	if creatorAllowed && run.CreatedBy != nil && *run.CreatedBy == userID {
		return run, nil
	}
	if err := s.requireAccess(run.ProjectID, userID, userRole, minRole); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Service) requireProject(projectID string) error {
	if projectID == "" {
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

func normalizeAndValidateRun(run *ExperimentRun) error {
	run.Name = strings.TrimSpace(run.Name)
	run.RunType = strings.TrimSpace(run.RunType)
	run.GasType = strings.TrimSpace(run.GasType)
	run.PressureUnit = strings.TrimSpace(run.PressureUnit)
	if run.Name == "" || len(run.Name) > 256 || !validRunType(run.RunType) ||
		run.GasType == "" || len(run.GasType) > 16 || run.PressureUnit == "" || len(run.PressureUnit) > 8 {
		return ErrInvalidInput
	}
	if run.Campaign != nil {
		campaign := strings.TrimSpace(*run.Campaign)
		if len(campaign) > 128 {
			return ErrInvalidInput
		}
		if campaign == "" {
			run.Campaign = nil
		} else {
			run.Campaign = &campaign
		}
	}
	if run.PressureMin != nil && run.PressureMax != nil && *run.PressureMin > *run.PressureMax {
		return ErrInvalidInput
	}
	devices, err := normalizeDevices(run.Devices)
	if err != nil {
		return err
	}
	run.Devices = devices
	return nil
}

func normalizeDevices(devices []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(devices))
	for _, device := range devices {
		device = strings.TrimSpace(device)
		if device != DeviceRFCarpet && device != DeviceRFQ && device != DeviceQPIG {
			return nil, ErrInvalidInput
		}
		if !seen[device] {
			seen[device] = true
			out = append(out, device)
		}
	}
	return out, nil
}

func validRunType(runType string) bool {
	switch runType {
	case RunTypeCooldown, RunTypeWarmup, RunTypeSteadyState, RunTypeTest:
		return true
	default:
		return false
	}
}

func validStatus(status string) bool {
	switch status {
	case StatusPlanned, StatusActive, StatusPaused, StatusCompleted, StatusAborted:
		return true
	default:
		return false
	}
}

func targetTransition(status, action string) (toStatus string, started, ended bool, err error) {
	switch status + ":" + action {
	case StatusPlanned + ":start":
		return StatusActive, true, false, nil
	case StatusPlanned + ":abort":
		return StatusAborted, false, true, nil
	case StatusActive + ":pause":
		return StatusPaused, true, false, nil
	case StatusActive + ":complete":
		return StatusCompleted, true, true, nil
	case StatusActive + ":abort":
		return StatusAborted, true, true, nil
	case StatusPaused + ":resume":
		return StatusActive, true, false, nil
	case StatusPaused + ":abort":
		return StatusAborted, true, true, nil
	default:
		return "", false, false, ErrInvalidTransition
	}
}

func hasMetadataUpdate(req UpdateRunRequest) bool {
	return req.Name != nil || req.Campaign != nil || req.RunType != nil || req.GasType != nil ||
		req.TargetTemp != nil || req.MinTemp != nil || req.PressureMin != nil || req.PressureMax != nil ||
		req.PressureUnit != nil || req.HasBeam != nil || req.Devices != nil || req.Description != nil
}

func applyUpdate(run *ExperimentRun, req *UpdateRunRequest) {
	if req.Name != nil {
		run.Name = *req.Name
	}
	if req.Campaign != nil {
		run.Campaign = req.Campaign
	}
	if req.RunType != nil {
		run.RunType = *req.RunType
	}
	if req.GasType != nil {
		run.GasType = *req.GasType
	}
	if req.TargetTemp != nil {
		run.TargetTemp = req.TargetTemp
	}
	if req.MinTemp != nil {
		run.MinTemp = req.MinTemp
	}
	if req.PressureMin != nil {
		run.PressureMin = req.PressureMin
	}
	if req.PressureMax != nil {
		run.PressureMax = req.PressureMax
	}
	if req.PressureUnit != nil {
		run.PressureUnit = *req.PressureUnit
	}
	if req.HasBeam != nil {
		run.HasBeam = *req.HasBeam
	}
	if req.Devices != nil {
		run.Devices = req.Devices
	}
	if req.Description != nil {
		run.Description = strings.TrimSpace(*req.Description)
	}
}

func normalizeUpdateRequest(req *UpdateRunRequest, run *ExperimentRun) {
	if req.Name != nil {
		req.Name = &run.Name
	}
	if req.Campaign != nil {
		if run.Campaign == nil {
			empty := ""
			req.Campaign = &empty
		} else {
			req.Campaign = run.Campaign
		}
	}
	if req.RunType != nil {
		req.RunType = &run.RunType
	}
	if req.GasType != nil {
		req.GasType = &run.GasType
	}
	if req.PressureUnit != nil {
		req.PressureUnit = &run.PressureUnit
	}
	if req.Devices != nil {
		req.Devices = run.Devices
	}
	if req.Description != nil {
		req.Description = &run.Description
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
	rank := map[string]int{projects.RoleViewer: 1, projects.RoleMember: 2, projects.RoleMaintainer: 3, projects.RoleOwner: 4}
	return member != nil && member.Status == projects.MemberStatusActive && rank[member.Role] >= rank[minRole], nil
}
