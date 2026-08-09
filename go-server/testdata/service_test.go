package testdata

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

const (
	projectUUID = "b0000000-0000-4000-8000-000000000001"
	runUUID     = "70000000-0000-4000-8000-000000000001"
	dataUUID    = "d0000000-0000-4000-8000-000000000001"
)

func TestServiceCreateAndMarkInvalid(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, &fakeRuns{exists: true})
	runID := runUUID
	td, err := svc.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypeCryo, RunID: &runID, Measurement: " temperature ", Value: 79.6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if td.Source != SourceManual || td.Quality != QualityNormal || td.Measurement != "temperature" {
		t.Fatalf("defaults/normalization = %#v", td)
	}

	if err := svc.MarkInvalid(td.ID, "other", auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("MarkInvalid other error = %v", err)
	}
	ownerService := NewService(repo, fakeAccess{role: projects.RoleOwner}, &fakeRuns{exists: true})
	if err := ownerService.MarkInvalid(td.ID, "owner", auth.RoleMember); err != nil {
		t.Fatalf("MarkInvalid owner error = %v", err)
	}
	if err := svc.MarkInvalid(td.ID, "creator", auth.RoleMember); err != nil || repo.item.Quality != QualityInvalid {
		t.Fatalf("MarkInvalid creator error = %v, quality = %q", err, repo.item.Quality)
	}
}

func TestCreateRejectsMissingRunAndUpdateRejectsDataType(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleOwner}, &fakeRuns{})
	runID := runUUID
	_, err := svc.Create(projectUUID, "owner", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypePressure, RunID: &runID, Measurement: "pressure",
	})
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("Create missing run error = %v", err)
	}
	repo.item = &TestData{ID: dataUUID, ProjectID: projectUUID, DataType: DataTypePressure, RecordedBy: stringPointer("creator")}
	dataType := DataTypeCryo
	_, err = svc.Update(dataUUID, "owner", auth.RoleMember, UpdateTestDataRequest{DataType: &dataType})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Update data_type error = %v", err)
	}
}

func TestHTTPRunValidator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/experiment-runs/"+runUUID || r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("request = %s, authorization = %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	headers := http.Header{"Authorization": {"Bearer token"}}
	exists, err := NewHTTPRunValidator(server.URL).Exists(runUUID, headers)
	if err != nil || !exists {
		t.Fatalf("Exists = %v, %v", exists, err)
	}
}

func batchRow(dataType, measurement string, value *float64, runID *string) CreateBatchRow {
	return CreateBatchRow{DataType: dataType, Measurement: measurement, Value: value, RunID: runID}
}

func TestBatchCreateCollectsAllErrors(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	badType := "quantum"
	badRun := "not-a-uuid"
	_, err := svc.CreateBatch(projectUUID, "creator", auth.RoleMember, nil, []CreateBatchRow{
		batchRow(badType, "t", float64Pointer(1), nil),
		batchRow(DataTypeCryo, "t", nil, nil),
		batchRow(DataTypeCryo, "t", float64Pointer(1), &badRun),
	}, nil)
	var batchErr *BatchValidationError
	if !errors.As(err, &batchErr) {
		t.Fatalf("error = %v, want BatchValidationError", err)
	}
	if len(batchErr.Errors) != 3 {
		t.Fatalf("errors = %v, want 3", batchErr.Errors)
	}
	// 排序断言：index 升序、同 index 内字段序（data_type→measurement→value→…→run_id）。
	for i, want := range []RowError{
		{Index: 0, Field: "data_type", Code: "invalid_enum"},
		{Index: 1, Field: "value", Code: "required"},
		{Index: 2, Field: "run_id", Code: "invalid_uuid"},
	} {
		if batchErr.Errors[i].Index != want.Index || batchErr.Errors[i].Field != want.Field || batchErr.Errors[i].Code != want.Code {
			t.Fatalf("errors[%d] = %v, want %v", i, batchErr.Errors[i], want)
		}
	}
	if repo.batchCalls != 0 {
		t.Fatalf("CreateBatch called %d times, want 0（校验失败不得触碰 DB）", repo.batchCalls)
	}
	if !strings.Contains(batchErr.Error(), "3 行校验失败") {
		t.Fatalf("Error() = %q", batchErr.Error())
	}
}

func TestBatchCreateEmptyRejected(t *testing.T) {
	svc := NewService(&fakeRepository{}, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	if _, err := svc.CreateBatch(projectUUID, "creator", auth.RoleMember, nil, nil, nil); !errors.Is(err, ErrEmptyBatch) {
		t.Fatalf("empty batch error = %v", err)
	}
}

func TestBatchCreateTooLarge(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	rows := make([]CreateBatchRow, 0, batchMaxRows+1)
	for i := 0; i < batchMaxRows+1; i++ {
		rows = append(rows, batchRow(DataTypeCryo, "t", float64Pointer(1), nil))
	}
	if _, err := svc.CreateBatch(projectUUID, "creator", auth.RoleMember, nil, rows, nil); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("too large error = %v", err)
	}
	if repo.batchCalls != 0 {
		t.Fatalf("CreateBatch called, want 0")
	}
}

func TestBatchCreateRunDedupe(t *testing.T) {
	repo := &fakeRepository{}
	runs := &fakeRuns{exists: true}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, runs)
	runID := runUUID
	result, err := svc.CreateBatch(projectUUID, "creator", auth.RoleMember, nil, []CreateBatchRow{
		batchRow(DataTypeCryo, "t1", float64Pointer(1), &runID),
		batchRow(DataTypePressure, "t2", float64Pointer(2), &runID),
		batchRow(DataTypeCryo, "t3", float64Pointer(3), nil),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if runs.calls != 1 {
		t.Fatalf("runs.Exists calls = %d, want 1（同 run_id 去重）", runs.calls)
	}
	if result.Count != 3 || len(result.Items) != 3 {
		t.Fatalf("result = %+v", result)
	}
	if result.Items[0].RunID == nil || *result.Items[0].RunID != runID || result.Items[2].RunID != nil {
		t.Fatalf("run_id 回填错误：%v / %v", result.Items[0].RunID, result.Items[2].RunID)
	}
}

func TestBatchCreateRunNotFound(t *testing.T) {
	repo := &fakeRepository{}
	missingRun := "70000000-0000-4000-8000-000000000099"
	otherRun := "70000000-0000-4000-8000-000000000098"
	svc := NewService(repo, fakeAccess{role: projects.RoleMember},
		&fakeRuns{exists: true, missing: map[string]bool{missingRun: true}})
	_, err := svc.CreateBatch(projectUUID, "creator", auth.RoleMember, nil, []CreateBatchRow{
		batchRow(DataTypeCryo, "t1", float64Pointer(1), &missingRun),
		batchRow(DataTypeCryo, "t2", float64Pointer(2), &missingRun),
		batchRow(DataTypeCryo, "t3", float64Pointer(3), &otherRun),
		batchRow(DataTypeCryo, "t4", float64Pointer(4), nil),
	}, nil)
	var batchErr *BatchValidationError
	if !errors.As(err, &batchErr) {
		t.Fatalf("error = %v, want BatchValidationError", err)
	}
	if len(batchErr.Errors) != 2 {
		t.Fatalf("errors = %v, want 2（引用缺失 run 的每行一条）", batchErr.Errors)
	}
	for i, wantIndex := range []int{0, 1} {
		if batchErr.Errors[i].Index != wantIndex || batchErr.Errors[i].Code != "run_not_found" || batchErr.Errors[i].Field != "run_id" {
			t.Fatalf("errors[%d] = %v", i, batchErr.Errors[i])
		}
	}
	if repo.batchCalls != 0 {
		t.Fatalf("CreateBatch called, want 0")
	}
}

func TestBatchCreateSuccess(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, &fakeRuns{exists: true})
	runID := runUUID
	result, err := svc.CreateBatch(projectUUID, "creator", auth.RoleMember, nil, []CreateBatchRow{
		{DataType: " cryo ", Measurement: " target_temp ", Value: float64Pointer(4.2), RunID: &runID,
			Unit: "K", Quality: stringPointer("suspect"), Notes: " 稳定后读数 "},
		{DataType: DataTypePressure, Measurement: "cell_pressure", Value: float64Pointer(0.013)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 2 {
		t.Fatalf("count = %d, want 2", result.Count)
	}
	first := result.Items[0]
	if first.DataType != DataTypeCryo || first.Measurement != "target_temp" || first.Unit != "K" || first.Notes != "稳定后读数" {
		t.Fatalf("trim 失败：%+v", first)
	}
	if first.Quality != QualitySuspect || first.Source != SourceManual {
		t.Fatalf("quality/source = %q/%q", first.Quality, first.Source)
	}
	if first.ProjectID != projectUUID || first.RecordedBy == nil || *first.RecordedBy != "creator" {
		t.Fatalf("project/recorded_by = %+v", first)
	}
	if first.RunID == nil || *first.RunID != runID {
		t.Fatalf("run_id = %v", first.RunID)
	}
	second := result.Items[1]
	if second.Quality != QualityNormal || second.Source != SourceManual || second.Unit != "" {
		t.Fatalf("第二行默认值 = %+v", second)
	}
	if repo.batchCalls != 1 {
		t.Fatalf("CreateBatch calls = %d, want 1", repo.batchCalls)
	}
}

func TestBatchCreateForbidden(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleViewer}, &fakeRuns{})
	_, err := svc.CreateBatch(projectUUID, "viewer", auth.RoleViewer, nil, []CreateBatchRow{
		batchRow(DataTypeCryo, "t", float64Pointer(1), nil),
	}, nil)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer error = %v, want ErrForbidden（权限前置，先于行校验）", err)
	}
	if repo.batchCalls != 0 {
		t.Fatalf("CreateBatch called, want 0")
	}
}

func TestBatchCreateFKFallback(t *testing.T) {
	repo := &fakeRepository{batchErr: &RowError{Index: 0, Field: "run_id", Code: "run_not_found", Message: "实验批次不存在"}}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, &fakeRuns{exists: true})
	runID := runUUID
	_, err := svc.CreateBatch(projectUUID, "creator", auth.RoleMember, nil, []CreateBatchRow{
		batchRow(DataTypeCryo, "t1", float64Pointer(1), &runID),
		batchRow(DataTypeCryo, "t2", float64Pointer(2), nil),
	}, nil)
	var batchErr *BatchValidationError
	if !errors.As(err, &batchErr) {
		t.Fatalf("error = %v, want BatchValidationError（FK 竞态转行级 422）", err)
	}
	if len(batchErr.Errors) != 1 || batchErr.Errors[0].Index != 0 ||
		batchErr.Errors[0].Code != "run_not_found" || batchErr.Errors[0].Field != "run_id" {
		t.Fatalf("errors = %v", batchErr.Errors)
	}
}

func TestBatchMergeDecodeAndSemanticErrors(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	rows := []CreateBatchRow{
		batchRow(DataTypeCryo, "t0", float64Pointer(1), nil),
		batchRow("bad_type", "t1", float64Pointer(2), nil),
		batchRow(DataTypeCryo, "t2", nil, nil),
	}
	decodeErrors := []RowError{
		{Index: 2, Field: "extra_field", Code: "unknown_field", Message: "未知字段 extra_field"},
	}
	_, err := svc.CreateBatch(projectUUID, "creator", auth.RoleMember, nil, rows, decodeErrors)
	var batchErr *BatchValidationError
	if !errors.As(err, &batchErr) {
		t.Fatalf("error = %v, want BatchValidationError", err)
	}
	// 合并后 3 条：index1 语义错误、index2 语义错误（value 必填）、index2 解码错误（unknown_field）；
	// 排序断言：index 升序、同 index 内字段序稳定。
	if len(batchErr.Errors) != 3 {
		t.Fatalf("errors = %v, want 3", batchErr.Errors)
	}
	wants := []RowError{
		{Index: 1, Field: "data_type", Code: "invalid_enum"},
		{Index: 2, Field: "extra_field", Code: "unknown_field"},
		{Index: 2, Field: "value", Code: "required"},
	}
	for i, want := range wants {
		got := batchErr.Errors[i]
		if got.Index != want.Index || got.Field != want.Field || got.Code != want.Code {
			t.Fatalf("errors[%d] = %v, want %v（排序断言：index 升序、同 index 字段序）", i, got, want)
		}
	}
	if repo.batchCalls != 0 {
		t.Fatalf("CreateBatch called, want 0")
	}
}

func TestBatchCreateServiceTooLarge(t *testing.T) {
	svc := NewService(&fakeRepository{}, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	rows := make([]CreateBatchRow, 0, batchMaxRows+10)
	for i := 0; i < batchMaxRows+10; i++ {
		rows = append(rows, batchRow(DataTypeCryo, "t", float64Pointer(1), nil))
	}
	// 直接调 service（绕过 handler 层拦截），纵深防线仍应生效。
	if _, err := svc.CreateBatch(projectUUID, "creator", auth.RoleMember, nil, rows, nil); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("error = %v, want ErrBatchTooLarge", err)
	}
}

func float64Pointer(v float64) *float64 { return &v }

type fakeRepository struct {
	item *TestData
	// batchCalls 记录 CreateBatch 调用次数；batchErr 控制 CreateBatch 返回值（FK 兜底测试用）。
	batchCalls int
	batchErr   error
}

func (f *fakeRepository) Create(td *TestData) error {
	td.ID, td.CreatedAt, td.UpdatedAt = dataUUID, time.Now(), time.Now()
	f.item = td
	return nil
}
func (f *fakeRepository) CreateBatch(items []*TestData) error {
	f.batchCalls++
	if f.batchErr != nil {
		return f.batchErr
	}
	for _, td := range items {
		td.ID, td.CreatedAt, td.UpdatedAt = dataUUID, time.Now(), time.Now()
	}
	return nil
}
func (f *fakeRepository) GetByID(id string) (*TestData, error) { return f.item, nil }
func (f *fakeRepository) List(ListParams) ([]TestData, int, error) {
	return nil, 0, nil
}
func (f *fakeRepository) Update(string, UpdateTestDataRequest) error { return nil }
func (f *fakeRepository) MarkInvalid(string, string) error {
	f.item.Quality = QualityInvalid
	return nil
}

type fakeAccess struct{ role string }

func (f fakeAccess) ProjectExists(string) (bool, error) { return true, nil }
func (f fakeAccess) CanAccessProject(_, _, _ string, minRole string) (bool, error) {
	rank := map[string]int{projects.RoleViewer: 1, projects.RoleMember: 2, projects.RoleMaintainer: 3, projects.RoleOwner: 4}
	return rank[f.role] >= rank[minRole], nil
}
func (f fakeAccess) ProjectRole(_, _, _ string) (string, error) { return f.role, nil }

type fakeRuns struct {
	exists  bool
	missing map[string]bool // 命中该集合的 run_id 视为不存在
	calls   int
}

func (f *fakeRuns) Exists(runID string, _ http.Header) (bool, error) {
	f.calls++
	if f.missing[runID] {
		return false, nil
	}
	return f.exists, nil
}

func stringPointer(value string) *string { return &value }
