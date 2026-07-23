package rfmatch

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrRecordNotFound  = errors.New("RF 匹配记录不存在")
	ErrProjectNotFound = errors.New("项目不存在")
	ErrForbidden       = errors.New("当前用户无权访问该项目")
	ErrInvalidInput    = errors.New("请求参数无效")
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	CanAccessProject(projectID, userID, userRole, minRole string) (bool, error)
	ProjectRole(projectID, userID, userRole string) (string, error)
}

type recordRepository interface {
	Create(record *RFMatchingRecord) error
	GetByID(id string) (*RFMatchingRecord, error)
	List(params ListParams) ([]RFMatchingRecord, int, error)
	Update(id string, req UpdateRFMatchingRequest) error
	MarkVoid(id, voidedBy, reason string) error
}

type Service struct {
	repo   recordRepository
	access ProjectAccessChecker
}

func NewService(repo recordRepository, access ProjectAccessChecker) *Service {
	return &Service{repo: repo, access: access}
}

func (s *Service) Create(projectID, userID, userRole string, req CreateRFMatchingRequest) (*RFMatchingRecord, error) {
	projectID = strings.TrimSpace(projectID)
	if err := s.requireProject(projectID); err != nil {
		return nil, err
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleMember); err != nil {
		return nil, err
	}
	device := strings.TrimSpace(req.Device)
	if !validDevice(device) || req.FrequencyMHz <= 0 || req.Status == nil {
		return nil, ErrInvalidInput
	}
	status := strings.TrimSpace(*req.Status)
	if !validStatus(status) {
		return nil, ErrInvalidInput
	}
	measuredAt := time.Now()
	if req.MeasuredAt != nil {
		measuredAt = *req.MeasuredAt
	}
	record := &RFMatchingRecord{
		ProjectID: projectID, Device: device, FrequencyMHz: req.FrequencyMHz, S11: req.S11,
		InputFreq: req.InputFreq, InputVoltage: req.InputVoltage, InputPower: req.InputPower,
		InputDesc: strings.TrimSpace(req.InputDesc), OutputFreq: req.OutputFreq,
		OutputVoltage: req.OutputVoltage, OutputPower: req.OutputPower, OutputDesc: strings.TrimSpace(req.OutputDesc),
		TransformerTurns: strings.TrimSpace(req.TransformerTurns), CapacitanceText: strings.TrimSpace(req.CapacitanceText),
		TransformerMaterial: strings.TrimSpace(req.TransformerMaterial), ShuntInductance: strings.TrimSpace(req.ShuntInductance),
		SeriesCapacitor: strings.TrimSpace(req.SeriesCapacitor), Status: &status, Notes: strings.TrimSpace(req.Notes),
		MeasuredAt: measuredAt, MeasuredBy: &userID,
	}
	if err := s.repo.Create(record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Service) GetByID(id, userID, userRole string) (*RFMatchingRecord, error) {
	record, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.requireAccess(record.ProjectID, userID, userRole, projects.RoleViewer); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Service) List(projectID, userID, userRole string, params ListParams) (*ListResult, error) {
	projectID = strings.TrimSpace(projectID)
	if uuid.Validate(projectID) != nil {
		return nil, ErrInvalidInput
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleViewer); err != nil {
		return nil, err
	}
	params.ProjectID = projectID
	params.Device = strings.TrimSpace(params.Device)
	params.Status = strings.TrimSpace(params.Status)
	if (params.Device != "" && !validDevice(params.Device)) || (params.Status != "" && !validStatus(params.Status)) {
		return nil, ErrInvalidInput
	}
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	items, total, err := s.repo.List(params)
	if err != nil {
		return nil, err
	}
	return &ListResult{Items: items, Total: total, Page: params.Page, PerPage: params.PerPage}, nil
}

func (s *Service) Update(id, userID, userRole string, req UpdateRFMatchingRequest) (*RFMatchingRecord, error) {
	record, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.requireAccess(record.ProjectID, userID, userRole, projects.RoleMember); err != nil {
		return nil, err
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !validStatus(status) {
			return nil, ErrInvalidInput
		}
		req.Status = &status
	}
	trimUpdateStrings(&req)
	if err := s.repo.Update(record.ID, req); err != nil {
		return nil, err
	}
	return s.get(record.ID)
}

func (s *Service) MarkVoid(id, userID, userRole, reason string) error {
	if userRole == auth.RoleAgent {
		return ErrForbidden
	}
	record, err := s.get(id)
	if err != nil {
		return err
	}
	if record.MeasuredBy == nil || *record.MeasuredBy != userID {
		role, err := s.access.ProjectRole(record.ProjectID, userID, userRole)
		if err != nil {
			return err
		}
		if role != projects.RoleOwner {
			return ErrForbidden
		}
	}
	return s.repo.MarkVoid(record.ID, userID, strings.TrimSpace(reason))
}

func (s *Service) get(id string) (*RFMatchingRecord, error) {
	id = strings.TrimSpace(id)
	if uuid.Validate(id) != nil {
		return nil, ErrInvalidInput
	}
	record, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrRecordNotFound
	}
	return record, nil
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

func validDevice(value string) bool {
	switch value {
	case DeviceRFCarpet, DeviceRFQ, DeviceQPIG:
		return true
	default:
		return false
	}
}

func validStatus(value string) bool {
	switch value {
	case StatusPass, StatusAdjust, StatusFail:
		return true
	default:
		return false
	}
}

func trimUpdateStrings(req *UpdateRFMatchingRequest) {
	for _, value := range []**string{&req.InputDesc, &req.OutputDesc, &req.TransformerTurns, &req.CapacitanceText,
		&req.TransformerMaterial, &req.ShuntInductance, &req.SeriesCapacitor, &req.Notes} {
		if *value != nil {
			trimmed := strings.TrimSpace(**value)
			*value = &trimmed
		}
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
