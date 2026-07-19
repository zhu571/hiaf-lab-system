package testdata

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrTestDataNotFound = errors.New("测试数据不存在")
	ErrRunNotFound      = errors.New("实验批次不存在")
	ErrProjectNotFound  = errors.New("项目不存在")
	ErrForbidden        = errors.New("当前用户无权访问该项目")
	ErrInvalidInput     = errors.New("请求参数无效")
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	CanAccessProject(projectID, userID, userRole, minRole string) (bool, error)
	ProjectRole(projectID, userID, userRole string) (string, error)
}

type RunValidator interface {
	Exists(runID string, headers http.Header) (bool, error)
}

type testDataRepository interface {
	Create(td *TestData) error
	GetByID(id string) (*TestData, error)
	List(params ListParams) ([]TestData, int, error)
	Update(id string, req UpdateTestDataRequest) error
	MarkInvalid(id, recordedBy string) error
}

type Service struct {
	repo   testDataRepository
	access ProjectAccessChecker
	runs   RunValidator
}

func NewService(repo testDataRepository, access ProjectAccessChecker, runs RunValidator) *Service {
	return &Service{repo: repo, access: access, runs: runs}
}

func (s *Service) Create(projectID, userID, userRole string, headers http.Header, req CreateTestDataRequest) (*TestData, error) {
	projectID = strings.TrimSpace(projectID)
	if err := s.requireProject(projectID); err != nil {
		return nil, err
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleMember); err != nil {
		return nil, err
	}
	quality, source := QualityNormal, SourceManual
	if req.Quality != nil {
		quality = strings.TrimSpace(*req.Quality)
	}
	if req.Source != nil {
		source = strings.TrimSpace(*req.Source)
	}
	td := &TestData{
		ProjectID: projectID, DataType: strings.TrimSpace(req.DataType),
		Measurement: strings.TrimSpace(req.Measurement), Value: req.Value, Unit: strings.TrimSpace(req.Unit),
		Quality: quality, Source: source, MeasuredAt: req.MeasuredAt, Notes: strings.TrimSpace(req.Notes), RecordedBy: &userID,
	}
	if !validDataType(td.DataType) || td.Measurement == "" || len(td.Measurement) > 128 ||
		len(td.Unit) > 16 || !validQuality(td.Quality) || !validSource(td.Source) {
		return nil, ErrInvalidInput
	}
	if req.RunID != nil {
		runID := strings.TrimSpace(*req.RunID)
		if uuid.Validate(runID) != nil || s.runs == nil {
			return nil, ErrInvalidInput
		}
		exists, err := s.runs.Exists(runID, headers)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrRunNotFound
		}
		td.RunID = &runID
	}
	if err := s.repo.Create(td); err != nil {
		return nil, err
	}
	return td, nil
}

func (s *Service) GetByID(id, userID, userRole string) (*TestData, error) {
	td, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.requireAccess(td.ProjectID, userID, userRole, projects.RoleViewer); err != nil {
		return nil, err
	}
	return td, nil
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
	params.RunID = strings.TrimSpace(params.RunID)
	params.DataType = strings.TrimSpace(params.DataType)
	params.Quality = strings.TrimSpace(params.Quality)
	if (params.RunID != "" && uuid.Validate(params.RunID) != nil) ||
		(params.DataType != "" && !validDataType(params.DataType)) ||
		(params.Quality != "" && !validQuality(params.Quality)) {
		return nil, ErrInvalidInput
	}
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	items, total, err := s.repo.List(params)
	if err != nil {
		return nil, err
	}
	return &ListResult{Items: items, Total: total, Page: params.Page, PerPage: params.PerPage}, nil
}

func (s *Service) Update(id, userID, userRole string, req UpdateTestDataRequest) (*TestData, error) {
	td, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if err := s.requireAccess(td.ProjectID, userID, userRole, projects.RoleMember); err != nil {
		return nil, err
	}
	if req.DataType != nil {
		return nil, ErrInvalidInput
	}
	if req.Measurement != nil {
		measurement := strings.TrimSpace(*req.Measurement)
		if measurement == "" || len(measurement) > 128 {
			return nil, ErrInvalidInput
		}
		req.Measurement = &measurement
	}
	if req.Unit != nil {
		unit := strings.TrimSpace(*req.Unit)
		if len(unit) > 16 {
			return nil, ErrInvalidInput
		}
		req.Unit = &unit
	}
	if req.Quality != nil {
		quality := strings.TrimSpace(*req.Quality)
		if !validQuality(quality) {
			return nil, ErrInvalidInput
		}
		req.Quality = &quality
	}
	if req.Notes != nil {
		notes := strings.TrimSpace(*req.Notes)
		req.Notes = &notes
	}
	if err := s.repo.Update(td.ID, req); err != nil {
		return nil, err
	}
	return s.get(td.ID)
}

func (s *Service) MarkInvalid(id, userID, userRole string) error {
	if userRole == auth.RoleAgent {
		return ErrForbidden
	}
	td, err := s.get(id)
	if err != nil {
		return err
	}
	if userRole != auth.RoleAdmin && (td.RecordedBy == nil || *td.RecordedBy != userID) {
		role, err := s.access.ProjectRole(td.ProjectID, userID, userRole)
		if err != nil {
			return err
		}
		if role != projects.RoleOwner {
			return ErrForbidden
		}
	}
	return s.repo.MarkInvalid(td.ID, userID)
}

func (s *Service) get(id string) (*TestData, error) {
	id = strings.TrimSpace(id)
	if uuid.Validate(id) != nil {
		return nil, ErrInvalidInput
	}
	td, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if td == nil {
		return nil, ErrTestDataNotFound
	}
	return td, nil
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

func validDataType(value string) bool {
	switch value {
	case DataTypeCryo, DataTypePressure, DataTypeVoltage, DataTypeRFVoltage, DataTypeEfficiency:
		return true
	default:
		return false
	}
}

func validQuality(value string) bool {
	switch value {
	case QualityNormal, QualityOutlier, QualitySuspect, QualityInvalid:
		return true
	default:
		return false
	}
}

func validSource(value string) bool {
	switch value {
	case SourceManual, SourceInstrument, SourceImport, SourceAgent, SourceBackfill:
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

type HTTPRunValidator struct {
	baseURL string
	client  *http.Client
}

func NewHTTPRunValidator(baseURL string) *HTTPRunValidator {
	return &HTTPRunValidator{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 3 * time.Second},
	}
}

func (v *HTTPRunValidator) Exists(runID string, headers http.Header) (bool, error) {
	endpoint := v.baseURL + "/api/v1/experiment-runs/" + url.PathEscape(runID)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("create experiment run request: %w", err)
	}
	for _, name := range []string{"Authorization", "X-Acting-User-ID", "X-Agent-Task-ID"} {
		req.Header.Set(name, headers.Get(name))
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("validate experiment run: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("experiment run validation returned status %d", resp.StatusCode)
	}
	return true, nil
}
