package testdata

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

// 批量端点容量与并发常量（方案定稿值）。
const (
	batchMaxRows         = 100
	batchRunCheckWorkers = 4
)

// batchRowFieldOrder 定义行内字段稳定排序（data_type→…→body），
// 与 api-contract 的 errors 排序契约一致；source 紧随 quality（不在契约字段表内，但允许行级报错）。
var batchRowFieldOrder = map[string]int{
	"data_type": 0, "measurement": 1, "value": 2, "unit": 3,
	"quality": 4, "source": 5, "measured_at": 6, "run_id": 7, "notes": 8, "body": 9,
}

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
	CreateBatch(items []*TestData) error
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

// CreateBatch 单事务原子批量插入：逐行复用单条校验规则，收集全部错误一次返回；
// decodeFailed 由 handler 传入（解码失败行下标集合，占位零值，跳过语义校验）；
// decodeErrors 为对应行的 JSON 结构层错误（unknown_field / invalid_row），与语义错误在同一排序点合并。
func (s *Service) CreateBatch(projectID, userID, userRole string, headers http.Header, rows []CreateBatchRow, decodeFailed map[int]bool, decodeErrors []RowError) (*BatchCreateResult, error) {
	projectID = strings.TrimSpace(projectID)
	if err := s.requireProject(projectID); err != nil {
		return nil, err
	}
	if err := s.requireAccess(projectID, userID, userRole, projects.RoleMember); err != nil {
		return nil, err
	}
	// 行数上限与空批（防御，handler 已拦）。
	if len(rows) == 0 {
		return nil, ErrEmptyBatch
	}
	if len(rows) > batchMaxRows {
		return nil, ErrBatchTooLarge
	}
	// 逐行语义校验，收集全部错误（不遇错即停）。
	semanticErrors := make([]RowError, 0, len(rows)*2)
	for i := range rows {
		if decodeFailed[i] {
			continue
		}
		s.validateBatchRow(rows[i], i, &semanticErrors)
	}
	batchErr := mergeRowErrors(semanticErrors, decodeErrors)
	if len(batchErr) > 0 {
		return nil, &BatchValidationError{Errors: batchErr}
	}
	// run_id 存在性校验（去重 + 有界并发）；不存在的 run_id 为引用它的每一行追加 run_not_found。
	if err := s.validateBatchRuns(rows, headers, &batchErr); err != nil {
		return nil, err
	}
	if len(batchErr) > 0 {
		return nil, &BatchValidationError{Errors: batchErr}
	}
	items := make([]*TestData, 0, len(rows))
	for i := range rows {
		items = append(items, s.buildBatchTestData(projectID, userID, rows[i]))
	}
	if err := s.repo.CreateBatch(items); err != nil {
		// 插入期 FK 竞态兜底：run 在「校验 → 插入」窗口内被删 → 转为行级 422，任何路径都不产生部分成功。
		var rowErr *RowError
		if errors.As(err, &rowErr) {
			return nil, &BatchValidationError{Errors: []RowError{*rowErr}}
		}
		return nil, err
	}
	return &BatchCreateResult{Items: items, Count: len(items)}, nil
}

// validateBatchRow 逐行校验，规则与单条 Create（service.go:68-76）逐条对应。
func (s *Service) validateBatchRow(row CreateBatchRow, index int, errors *[]RowError) {
	dataType := strings.TrimSpace(row.DataType)
	if dataType == "" {
		*errors = append(*errors, RowError{Index: index, Field: "data_type", Code: "required", Message: "数据类型必填"})
	} else if !validDataType(dataType) {
		*errors = append(*errors, RowError{Index: index, Field: "data_type", Code: "invalid_enum", Message: "数据类型不在允许枚举内"})
	}
	measurement := strings.TrimSpace(row.Measurement)
	if measurement == "" {
		*errors = append(*errors, RowError{Index: index, Field: "measurement", Code: "required", Message: "测量项必填"})
	} else if len(measurement) > 128 {
		*errors = append(*errors, RowError{Index: index, Field: "measurement", Code: "too_long", Message: "测量项不能超过 128 字符"})
	}
	if row.Value == nil {
		*errors = append(*errors, RowError{Index: index, Field: "value", Code: "required", Message: "数值必填"})
	} else if math.IsNaN(*row.Value) || math.IsInf(*row.Value, 0) {
		*errors = append(*errors, RowError{Index: index, Field: "value", Code: "not_a_number", Message: "数值必须是有限数字"})
	}
	if len(strings.TrimSpace(row.Unit)) > 16 {
		*errors = append(*errors, RowError{Index: index, Field: "unit", Code: "too_long", Message: "单位不能超过 16 字符"})
	}
	if row.Quality != nil {
		if quality := strings.TrimSpace(*row.Quality); !validQuality(quality) {
			*errors = append(*errors, RowError{Index: index, Field: "quality", Code: "invalid_enum", Message: "质量不在允许枚举内"})
		}
	}
	if row.Source != nil {
		if source := strings.TrimSpace(*row.Source); !validSource(source) {
			*errors = append(*errors, RowError{Index: index, Field: "source", Code: "invalid_enum", Message: "来源不在允许枚举内"})
		}
	}
	if row.RunID != nil {
		if runID := strings.TrimSpace(*row.RunID); runID != "" && uuid.Validate(runID) != nil {
			*errors = append(*errors, RowError{Index: index, Field: "run_id", Code: "invalid_uuid", Message: "实验批次 ID 必须是合法 UUID"})
		}
	}
}

// mergeRowErrors 合并语义错误与 handler 传入的解码错误，
// 统一按 index 升序、同 index 内字段序稳定排序，保证「收集全部错误一次返回」无实现歧义。
func mergeRowErrors(semantic, decode []RowError) []RowError {
	merged := make([]RowError, 0, len(semantic)+len(decode))
	merged = append(merged, semantic...)
	merged = append(merged, decode...)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Index != merged[j].Index {
			return merged[i].Index < merged[j].Index
		}
		return batchRowFieldOrder[merged[i].Field] < batchRowFieldOrder[merged[j].Field]
	})
	return merged
}

// validateBatchRuns 对全部通过 uuid 校验的 run_id 去重后做存在性校验：
// 有界 worker 池（并发度 batchRunCheckWorkers），结果写回同步 map；
// 不存在的 run_id → 为引用它的每一行追加 run_not_found（行级，继续收集）；
// validator 返回基础设施错误 → 整体返回 err（handler 落 500）。
func (s *Service) validateBatchRuns(rows []CreateBatchRow, headers http.Header, errors *[]RowError) error {
	if s.runs == nil {
		return nil
	}
	unique := make(map[string]bool)
	for i := range rows {
		if rows[i].RunID != nil {
			if id := strings.TrimSpace(*rows[i].RunID); id != "" {
				unique[id] = true
			}
		}
	}
	if len(unique) == 0 {
		return nil
	}
	runIDs := make([]string, 0, len(unique))
	for id := range unique {
		runIDs = append(runIDs, id)
	}
	var (
		mu       sync.Mutex
		missing  = make(map[string]bool)
		firstErr error
	)
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < batchRunCheckWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				exists, err := s.runs.Exists(id, headers)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					continue
				}
				if !exists {
					mu.Lock()
					missing[id] = true
					mu.Unlock()
				}
			}
		}()
	}
	for _, id := range runIDs {
		jobs <- id
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	for i := range rows {
		if rows[i].RunID != nil && missing[strings.TrimSpace(*rows[i].RunID)] {
			*errors = append(*errors, RowError{Index: i, Field: "run_id", Code: "run_not_found", Message: "实验批次不存在"})
		}
	}
	return nil
}

// buildBatchTestData 组装入库对象：默认 quality/source 与全字段 trim 规范化同单条 Create。
func (s *Service) buildBatchTestData(projectID, userID string, row CreateBatchRow) *TestData {
	quality, source := QualityNormal, SourceManual
	if row.Quality != nil {
		quality = strings.TrimSpace(*row.Quality)
	}
	if row.Source != nil {
		source = strings.TrimSpace(*row.Source)
	}
	td := &TestData{
		ProjectID: projectID, DataType: strings.TrimSpace(row.DataType),
		Measurement: strings.TrimSpace(row.Measurement), Value: *row.Value, Unit: strings.TrimSpace(row.Unit),
		Quality: quality, Source: source, MeasuredAt: row.MeasuredAt, Notes: strings.TrimSpace(row.Notes),
		RecordedBy: &userID,
	}
	if row.RunID != nil {
		if runID := strings.TrimSpace(*row.RunID); runID != "" {
			td.RunID = &runID
		}
	}
	return td
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
