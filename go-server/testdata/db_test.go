package testdata

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

// 第三轮补缺口：单条 Create 的 handler 全链路、GetByID/List/Update/MarkInvalid、
// source='instrument' 路径、*float64 零值语义（batch value=0 显式通过）、
// HTTPRunValidator 错误路径、repository 真实 DB 回查（含批量原子性）。
// 固定 UUID 段 d4xx（用户）+ d0000000-…d4xx（项目/run）。

const (
	tdDBOwnerID  = "00000000-0000-0000-0000-00000000d401"
	tdDBMemberID = "00000000-0000-0000-0000-00000000d402"

	tdDBProjectID = "d0000000-0000-4000-8000-00000000d401"
	tdDBRunID     = "d0000000-0000-4000-8000-00000000d402"
)

func openTestDataDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	for _, u := range []struct {
		id       string
		username string
		role     string
	}{
		{tdDBOwnerID, "td_dbtest_owner", "member"},
		{tdDBMemberID, "td_dbtest_member", "member"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'TestData DB Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'PRJ_TD_DBTEST', '测试数据集成项目', 'draft', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, tdDBProjectID, tdDBOwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 VALUES ($1, $2, 'owner', 'active', $2), ($1, $3, 'member', 'active', $2)
		 ON CONFLICT (project_id, user_id) DO NOTHING`, tdDBProjectID, tdDBOwnerID, tdDBMemberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO experiment_runs (id, project_id, name, status)
		 VALUES ($1, $2, 'TD 集成批次', 'completed')
		 ON CONFLICT (id) DO NOTHING`, tdDBRunID, tdDBProjectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM test_data WHERE project_id = $1`, tdDBProjectID)
		db.Exec(`DELETE FROM experiment_runs WHERE id = $1`, tdDBRunID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, tdDBProjectID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, tdDBOwnerID, tdDBMemberID)
	})
	return db
}

func tdUniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("td-h-%d", time.Now().UnixNano())
}

// ---------- repository 层（真实 DB） ----------

func TestDBRepositoryRoundTrip(t *testing.T) {
	db := openTestDataDB(t)
	repo := NewRepository(db)

	measuredAt := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	runID := tdDBRunID
	recordedBy := tdDBMemberID
	td := &TestData{
		ProjectID: tdDBProjectID, RunID: &runID, DataType: DataTypeCryo,
		Measurement: "target_temp", Value: 4.2, Unit: "K", Quality: QualityNormal,
		Source: SourceInstrument, MeasuredAt: &measuredAt, Notes: "稳定后读数", RecordedBy: &recordedBy,
	}
	if err := repo.Create(td); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM test_data WHERE id = $1`, td.ID) })
	if td.ID == "" || td.CreatedAt.IsZero() {
		t.Fatalf("created: %+v", td)
	}

	got, err := repo.GetByID(td.ID)
	if err != nil || got == nil {
		t.Fatalf("GetByID: %+v err=%v", got, err)
	}
	if got.Value != 4.2 || got.Unit != "K" || got.Source != SourceInstrument || got.Quality != QualityNormal ||
		got.RunID == nil || *got.RunID != tdDBRunID || got.MeasuredAt == nil || !got.MeasuredAt.Equal(measuredAt) ||
		got.Notes != "稳定后读数" || got.RecordedBy == nil || *got.RecordedBy != tdDBMemberID {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	// 缺失 id → nil（非错误）
	if missing, err := repo.GetByID("d0000000-0000-4000-8000-000000009999"); err != nil || missing != nil {
		t.Fatalf("missing GetByID: %+v err=%v", missing, err)
	}

	// Update：测量项/数值/质量/备注
	unit := "mK"
	newValue := 3.9
	note := "修正"
	if err := repo.Update(td.ID, UpdateTestDataRequest{Value: &newValue, Unit: &unit, Notes: &note}); err != nil {
		t.Fatal(err)
	}
	after, err := repo.GetByID(td.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Value != 3.9 || after.Unit != "mK" || after.Notes != "修正" {
		t.Fatalf("after update: %+v", after)
	}
	// 空请求 → no-op
	if err := repo.Update(td.ID, UpdateTestDataRequest{}); err != nil {
		t.Fatal(err)
	}
	// 更新不存在 → ErrTestDataNotFound
	if err := repo.Update("d0000000-0000-4000-8000-000000009999", UpdateTestDataRequest{Value: &newValue}); err != ErrTestDataNotFound {
		t.Fatalf("update missing: got %v", err)
	}

	// MarkInvalid：quality → invalid；重复标记幂等；不存在 → ErrTestDataNotFound
	if err := repo.MarkInvalid(td.ID, tdDBMemberID); err != nil {
		t.Fatal(err)
	}
	invalid, err := repo.GetByID(td.ID)
	if err != nil || invalid.Quality != QualityInvalid {
		t.Fatalf("mark invalid: %+v err=%v", invalid, err)
	}
	if err := repo.MarkInvalid(td.ID, tdDBMemberID); err != nil {
		t.Fatalf("re-mark invalid: %v", err)
	}
	if err := repo.MarkInvalid("d0000000-0000-4000-8000-000000009999", tdDBMemberID); err != ErrTestDataNotFound {
		t.Fatalf("mark invalid missing: got %v", err)
	}
}

func TestDBRepositoryListFiltersAndPagination(t *testing.T) {
	db := openTestDataDB(t)
	repo := NewRepository(db)

	seed := func(dataType, measurement string, value float64, quality string) *TestData {
		recordedBy := tdDBMemberID
		td := &TestData{
			ProjectID: tdDBProjectID, DataType: dataType, Measurement: measurement, Value: value,
			Unit: "K", Quality: quality, Source: SourceManual, RecordedBy: &recordedBy,
		}
		if err := repo.Create(td); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM test_data WHERE id = $1`, td.ID) })
		return td
	}
	cryo := seed(DataTypeCryo, "t1", 4.2, QualityNormal)
	seed(DataTypePressure, "p1", 0.013, QualityNormal)
	seed(DataTypeCryo, "t2", 5.0, QualityInvalid) // invalid 默认被过滤

	// 全量（project）：invalid 排除
	list, total, err := repo.List(ListParams{ProjectID: tdDBProjectID})
	if err != nil || total != 2 || len(list) != 2 {
		t.Fatalf("plain list: %d items, total=%d err=%v", len(list), total, err)
	}
	// data_type 过滤
	byType, total, err := repo.List(ListParams{ProjectID: tdDBProjectID, DataType: DataTypePressure})
	if err != nil || total != 1 || byType[0].Measurement != "p1" {
		t.Fatalf("type filter: %+v total=%d err=%v", byType, total, err)
	}
	// quality=invalid 显式查询 → 1 条
	_, total, err = repo.List(ListParams{ProjectID: tdDBProjectID, Quality: QualityInvalid})
	if err != nil || total != 1 {
		t.Fatalf("invalid filter: total=%d err=%v", total, err)
	}
	// run_id 过滤（有 run 的数据）
	if _, err := db.Exec(`UPDATE test_data SET run_id = $1 WHERE id = $2`, tdDBRunID, cryo.ID); err != nil {
		t.Fatal(err)
	}
	byRun, total, err := repo.List(ListParams{ProjectID: tdDBProjectID, RunID: tdDBRunID})
	if err != nil || total != 1 || byRun[0].ID != cryo.ID {
		t.Fatalf("run filter: %+v total=%d err=%v", byRun, total, err)
	}
	// 分页：per_page=1 → 第 1 页 1 条，total 仍为 2
	paged, total, err := repo.List(ListParams{ProjectID: tdDBProjectID, Page: 1, PerPage: 1})
	if err != nil || total != 2 || len(paged) != 1 {
		t.Fatalf("paged: %d items total=%d err=%v", len(paged), total, err)
	}
}

func TestDBRepositoryBatchAtomicity(t *testing.T) {
	db := openTestDataDB(t)
	repo := NewRepository(db)

	// 全合法两行 → 全部入库
	recordedBy := tdDBMemberID
	items := []*TestData{
		{ProjectID: tdDBProjectID, DataType: DataTypeCryo, Measurement: "m1", Value: 1, Quality: QualityNormal, Source: SourceManual, RecordedBy: &recordedBy},
		{ProjectID: tdDBProjectID, DataType: DataTypePressure, Measurement: "m2", Value: 2, Quality: QualityNormal, Source: SourceManual, RecordedBy: &recordedBy},
	}
	if err := repo.CreateBatch(items); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == "" {
			t.Fatalf("batch item missing id: %+v", item)
		}
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM test_data WHERE id = $1 OR id = $2`, items[0].ID, items[1].ID) })

	// 第二行 run_id 指向不存在 run → 整批回滚 + RowError（index=1，FK 竞态兜底语义）
	badRun := "d0000000-0000-4000-8000-000000009999"
	rollbackItems := []*TestData{
		{ProjectID: tdDBProjectID, DataType: DataTypeCryo, Measurement: "r1", Value: 1, Quality: QualityNormal, Source: SourceManual, RecordedBy: &recordedBy},
		{ProjectID: tdDBProjectID, RunID: &badRun, DataType: DataTypeCryo, Measurement: "r2", Value: 2, Quality: QualityNormal, Source: SourceManual, RecordedBy: &recordedBy},
	}
	err := repo.CreateBatch(rollbackItems)
	var rowErr *RowError
	if !errors.As(err, &rowErr) || rowErr.Index != 1 || rowErr.Field != "run_id" || rowErr.Code != "run_not_found" {
		t.Fatalf("batch FK error: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM test_data WHERE project_id = $1 AND measurement IN ('r1','r2')`,
		tdDBProjectID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("batch not atomic: %d rows persisted", count)
	}
}

// ---------- service 层（fake repo 补缺口） ----------

func TestServiceCreateInstrumentSourceAndValidation(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, &fakeRuns{exists: true})

	// source='instrument' 显式路径
	instrument := SourceInstrument
	quality := QualitySuspect
	runID := runUUID
	td, err := svc.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypeRFVoltage, Measurement: "rf_peak", Value: 1.5, Unit: "kV",
		Source: &instrument, Quality: &quality, RunID: &runID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if td.Source != SourceInstrument || td.Quality != QualitySuspect || td.RunID == nil || *td.RunID != runUUID {
		t.Fatalf("instrument source: %+v", td)
	}
	// 非法 data_type / quality / source / 超长 measurement / 超长 unit
	badType := "quantum"
	if _, err := svc.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: badType, Measurement: "m",
	}); err != ErrInvalidInput {
		t.Fatalf("bad data type: got %v", err)
	}
	badQuality := "bogus"
	if _, err := svc.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypeCryo, Measurement: "m", Quality: &badQuality,
	}); err != ErrInvalidInput {
		t.Fatalf("bad quality: got %v", err)
	}
	badSource := "bogus"
	if _, err := svc.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypeCryo, Measurement: "m", Source: &badSource,
	}); err != ErrInvalidInput {
		t.Fatalf("bad source: got %v", err)
	}
	if _, err := svc.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypeCryo, Measurement: strings.Repeat("m", 129),
	}); err != ErrInvalidInput {
		t.Fatalf("long measurement: got %v", err)
	}
	if _, err := svc.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypeCryo, Measurement: "m", Unit: strings.Repeat("u", 17),
	}); err != ErrInvalidInput {
		t.Fatalf("long unit: got %v", err)
	}
	// run_id 非法 UUID / run validator 报错透传 / run 不存在
	badRun := "not-a-uuid"
	if _, err := svc.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypeCryo, Measurement: "m", RunID: &badRun,
	}); err != ErrInvalidInput {
		t.Fatalf("bad run uuid: got %v", err)
	}
	svcErr := NewService(repo, fakeAccess{role: projects.RoleMember}, &fakeRunsErr{})
	if _, err := svcErr.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypeCryo, Measurement: "m", RunID: &runID,
	}); err == nil {
		t.Fatalf("run validator error: expected error, got nil")
	}
	// viewer 无写权限 → ErrForbidden；项目不存在 → ErrProjectNotFound
	svcViewer := NewService(repo, fakeAccess{role: projects.RoleViewer}, &fakeRuns{exists: true})
	if _, err := svcViewer.Create(projectUUID, "viewer", auth.RoleViewer, nil, CreateTestDataRequest{
		DataType: DataTypeCryo, Measurement: "m",
	}); err != ErrForbidden {
		t.Fatalf("viewer create: got %v", err)
	}
	svcMissing := NewService(repo, fakeMissingProject{}, &fakeRuns{})
	if _, err := svcMissing.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypeCryo, Measurement: "m",
	}); err != ErrProjectNotFound {
		t.Fatalf("missing project: got %v", err)
	}
}

// fakeMissingProject：ProjectExists=false。
type fakeMissingProject struct{}

func (f fakeMissingProject) ProjectExists(string) (bool, error) { return false, nil }
func (f fakeMissingProject) CanAccessProject(_, _, _ string, minRole string) (bool, error) {
	return true, nil
}
func (f fakeMissingProject) ProjectRole(_, _, _ string) (string, error) { return "", nil }

// fakeRunsErr：Exists 恒报错（模拟 runs 服务不可用）。
type fakeRunsErr struct{}

func (f *fakeRunsErr) Exists(runID string, _ http.Header) (bool, error) {
	return false, errors.New("runs service down")
}

// fakeAccessDeny：无任何项目权限。
type fakeAccessDeny struct{}

func (f fakeAccessDeny) ProjectExists(string) (bool, error) { return true, nil }
func (f fakeAccessDeny) CanAccessProject(_, _, _ string, minRole string) (bool, error) {
	return false, nil
}
func (f fakeAccessDeny) ProjectRole(_, _, _ string) (string, error) { return "", nil }

// fakeUpdatingRepo：fakeRepository 的 Update 实现为空操作（不修改 item），
// 这里补一个真正应用字段更新的版本，用于断言 trim/回读语义。
type fakeUpdatingRepo struct {
	*fakeRepository
}

func (f *fakeUpdatingRepo) Update(id string, req UpdateTestDataRequest) error {
	if f.item == nil || f.item.ID != id {
		return ErrTestDataNotFound
	}
	if req.Measurement != nil {
		f.item.Measurement = *req.Measurement
	}
	if req.Value != nil {
		f.item.Value = *req.Value
	}
	if req.Unit != nil {
		f.item.Unit = *req.Unit
	}
	if req.Quality != nil {
		f.item.Quality = *req.Quality
	}
	if req.MeasuredAt != nil {
		f.item.MeasuredAt = req.MeasuredAt
	}
	if req.Notes != nil {
		f.item.Notes = *req.Notes
	}
	return nil
}

func TestServiceGetListUpdate(t *testing.T) {
	repo := &fakeRepository{}
	repo.item = &TestData{ID: dataUUID, ProjectID: projectUUID, DataType: DataTypeCryo,
		Measurement: "t", Quality: QualityNormal, Source: SourceManual, RecordedBy: stringPointer("creator")}
	svc := NewService(repo, fakeAccess{role: projects.RoleViewer}, &fakeRuns{})

	// GetByID：非法 id / 不存在 / 无权限
	if _, err := svc.GetByID("bad", "viewer", auth.RoleViewer); err != ErrInvalidInput {
		t.Fatalf("bad id: got %v", err)
	}
	repo.item = nil
	if _, err := svc.GetByID(dataUUID, "viewer", auth.RoleViewer); err != ErrTestDataNotFound {
		t.Fatalf("missing: got %v", err)
	}
	repo.item = &TestData{ID: dataUUID, ProjectID: projectUUID, DataType: DataTypeCryo, Measurement: "t"}
	svcDenied := NewService(repo, fakeAccessDeny{}, &fakeRuns{})
	if _, err := svcDenied.GetByID(dataUUID, "nobody", auth.RoleViewer); err != ErrForbidden {
		t.Fatalf("outsider get: got %v", err)
	}

	// List：非法 project id / run_id / data_type / quality 过滤
	if _, err := svc.List("bad", "viewer", auth.RoleViewer, ListParams{}); err != ErrInvalidInput {
		t.Fatalf("bad project: got %v", err)
	}
	if _, err := svc.List(projectUUID, "viewer", auth.RoleViewer, ListParams{RunID: "not-a-uuid"}); err != ErrInvalidInput {
		t.Fatalf("bad run filter: got %v", err)
	}
	if _, err := svc.List(projectUUID, "viewer", auth.RoleViewer, ListParams{DataType: "quantum"}); err != ErrInvalidInput {
		t.Fatalf("bad type filter: got %v", err)
	}
	if _, err := svc.List(projectUUID, "viewer", auth.RoleViewer, ListParams{Quality: "bogus"}); err != ErrInvalidInput {
		t.Fatalf("bad quality filter: got %v", err)
	}
	// List 正常：空结果
	result, err := svc.List(projectUUID, "viewer", auth.RoleViewer, ListParams{Page: 1, PerPage: 20})
	if err != nil || result.Total != 0 || result.Page != 1 || result.PerPage != 20 {
		t.Fatalf("list: %+v err=%v", result, err)
	}

	// Update：非法 measurement/unit/quality；成功路径（member 级权限）
	updatingRepo := &fakeUpdatingRepo{fakeRepository: &fakeRepository{}}
	updatingRepo.item = &TestData{ID: dataUUID, ProjectID: projectUUID, DataType: DataTypeCryo,
		Measurement: "t", Quality: QualityNormal, Source: SourceManual, RecordedBy: stringPointer("creator")}
	svcUpdate := NewService(updatingRepo, fakeAccess{role: projects.RoleMember}, &fakeRuns{})
	empty := "  "
	if _, err := svcUpdate.Update(dataUUID, "creator", auth.RoleMember, UpdateTestDataRequest{Measurement: &empty}); err != ErrInvalidInput {
		t.Fatalf("empty measurement: got %v", err)
	}
	longUnit := strings.Repeat("u", 17)
	if _, err := svcUpdate.Update(dataUUID, "creator", auth.RoleMember, UpdateTestDataRequest{Unit: &longUnit}); err != ErrInvalidInput {
		t.Fatalf("long unit: got %v", err)
	}
	badQuality := "bogus"
	if _, err := svcUpdate.Update(dataUUID, "creator", auth.RoleMember, UpdateTestDataRequest{Quality: &badQuality}); err != ErrInvalidInput {
		t.Fatalf("bad quality: got %v", err)
	}
	newMeasurement := "  target_temp  "
	updated, err := svcUpdate.Update(dataUUID, "creator", auth.RoleMember, UpdateTestDataRequest{Measurement: &newMeasurement})
	if err != nil || updated.Measurement != "target_temp" {
		t.Fatalf("update: %+v err=%v", updated, err)
	}
	// Update：非成员 → ErrForbidden；不存在 → ErrTestDataNotFound
	svcDeniedUpdate := NewService(updatingRepo, fakeAccessDeny{}, &fakeRuns{})
	if _, err := svcDeniedUpdate.Update(dataUUID, "nobody", auth.RoleMember, UpdateTestDataRequest{Notes: stringPointer("x")}); err != ErrForbidden {
		t.Fatalf("outsider update: got %v", err)
	}
	updatingRepo.item = nil
	if _, err := svcUpdate.Update(dataUUID, "creator", auth.RoleMember, UpdateTestDataRequest{Notes: stringPointer("x")}); err != ErrTestDataNotFound {
		t.Fatalf("missing update: got %v", err)
	}

	// MarkInvalid：agent 禁止；owner 兜底
	if err := svc.MarkInvalid(dataUUID, "agent", auth.RoleAgent); err != ErrForbidden {
		t.Fatalf("agent mark invalid: got %v", err)
	}
	repo.item = &TestData{ID: dataUUID, ProjectID: projectUUID, DataType: DataTypeCryo, Measurement: "t",
		Quality: QualityNormal, Source: SourceManual, RecordedBy: stringPointer("creator")}
	ownerSvc := NewService(repo, fakeAccess{role: projects.RoleOwner}, &fakeRuns{})
	if err := ownerSvc.MarkInvalid(dataUUID, "other", auth.RoleMember); err != nil {
		t.Fatalf("owner mark invalid: %v", err)
	}
}

func TestHTTPRunValidatorErrorPaths(t *testing.T) {
	// 404 → 不存在（false, nil）
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	exists, err := NewHTTPRunValidator(server.URL).Exists(runUUID, nil)
	if err != nil || exists {
		t.Fatalf("404 path: exists=%v err=%v", exists, err)
	}

	// 500 → 错误透传
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server2.Close()
	if _, err := NewHTTPRunValidator(server2.URL).Exists(runUUID, nil); err == nil {
		t.Fatal("500 path: expected error")
	}

	// 网络错误 → 错误透传
	if _, err := NewHTTPRunValidator("http://127.0.0.1:1").Exists(runUUID, nil); err == nil {
		t.Fatal("unreachable: expected error")
	}
}

// ---------- handler 层（fake repo + 真实 audit db） ----------

const tdHandlerTestSecret = "testdata-handler-test-secret"

func newTestDataHandlerRouter(t *testing.T, db *sql.DB, svc *Service) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(tdHandlerTestSecret))
	h := NewHandler(svc)
	router := chi.NewRouter()
	router.Route("/api/v1/projects/{id}", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/test-data", h.List)
		r.Post("/test-data", h.Create)
	})
	router.Route("/api/v1/test-data/{id}", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", h.GetByID)
		r.Patch("/", h.Update)
		r.Delete("/", h.MarkInvalid)
	})
	return router
}

func tdToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(tdHandlerTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func tdRequest(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func tdErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error body: %s, err=%v", rec.Body.String(), err)
	}
	return envelope.Error.Code
}

func assertTDAudit(t *testing.T, db *sql.DB, requestID, action string) {
	t.Helper()
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE request_id = $1 AND action = $2`, requestID, action,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit_log rows = %d, want 1 (request_id=%s action=%s)", count, requestID, action)
	}
}

func TestHandlerTestDataSingleFlow(t *testing.T) {
	db := openTestDataDB(t)
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleOwner}, &fakeRuns{exists: true})
	router := newTestDataHandlerRouter(t, db, svc)
	member := tdToken(t, tdDBMemberID, "member", auth.RoleMember)
	base := "/api/v1/projects/" + projectUUID

	// 401：无 token；400：缺 Idempotency-Key
	rec := tdRequest(t, router, http.MethodPost, base+"/test-data", "", tdUniqueKey(t),
		`{"data_type":"cryo","measurement":"t","value":1}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", rec.Code)
	}
	rec = tdRequest(t, router, http.MethodPost, base+"/test-data", member, "",
		`{"data_type":"cryo","measurement":"t","value":1}`)
	if rec.Code != http.StatusBadRequest || tdErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idempotency key = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：坏 JSON / 未知字段（DisallowUnknownFields）
	rec = tdRequest(t, router, http.MethodPost, base+"/test-data", member, tdUniqueKey(t), `{bad`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", rec.Code)
	}
	rec = tdRequest(t, router, http.MethodPost, base+"/test-data", member, tdUniqueKey(t),
		`{"data_type":"cryo","measurement":"t","value":1,"hack":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：语义校验失败（非法 data_type）
	rec = tdRequest(t, router, http.MethodPost, base+"/test-data", member, tdUniqueKey(t),
		`{"data_type":"quantum","measurement":"t","value":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad data type = %d", rec.Code)
	}
	// 400：run 不存在（api-contract：ErrRunNotFound 与 ErrInvalidInput 同归 400 bad_request）
	missingRun := "70000000-0000-4000-8000-000000000099"
	svcMissingRun := NewService(repo, fakeAccess{role: projects.RoleOwner}, &fakeRuns{exists: true, missing: map[string]bool{missingRun: true}})
	router = newTestDataHandlerRouter(t, db, svcMissingRun)
	rec = tdRequest(t, router, http.MethodPost, base+"/test-data", member, tdUniqueKey(t),
		`{"data_type":"cryo","measurement":"t","value":1,"run_id":"`+missingRun+`"}`)
	if rec.Code != http.StatusBadRequest || tdErrorCode(t, rec) != "bad_request" {
		t.Fatalf("missing run = %d, body=%s", rec.Code, rec.Body.String())
	}
	router = newTestDataHandlerRouter(t, db, svc)
	// 201：成功 + 审计 test_data.create
	rec = tdRequest(t, router, http.MethodPost, base+"/test-data", member, tdUniqueKey(t),
		`{"data_type":"cryo","measurement":"target_temp","value":4.2,"unit":"K","source":"instrument"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			ID     string `json:"id"`
			Source string `json:"source"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == "" || envelope.Data.Source != SourceInstrument {
		t.Fatalf("create response: %+v", envelope.Data)
	}
	assertTDAudit(t, db, envelope.RequestID, "test_data.create")
	tdPath := "/api/v1/test-data/" + envelope.Data.ID

	// GET 列表：200（fake repo 空）+ 400（非法过滤值）
	rec = tdRequest(t, router, http.MethodGet, base+"/test-data", member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = tdRequest(t, router, http.MethodGet, base+"/test-data?data_type=quantum", member, "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad filter = %d", rec.Code)
	}

	// GET by id：200；404（fake repo 按 id 不匹配返回 nil）
	rec = tdRequest(t, router, http.MethodGet, tdPath, member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, body=%s", rec.Code, rec.Body.String())
	}
	repo.item = nil
	rec = tdRequest(t, router, http.MethodGet, "/api/v1/test-data/d0000000-0000-4000-8000-000000009999", member, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get = %d", rec.Code)
	}

	// PATCH：200 + 审计 test_data.update；400（不可修改字段）
	repo.item = &TestData{ID: dataUUID, ProjectID: projectUUID, DataType: DataTypeCryo,
		Measurement: "t", Quality: QualityNormal, Source: SourceManual, RecordedBy: stringPointer("creator")}
	rec = tdRequest(t, router, http.MethodPatch, tdPath, member, tdUniqueKey(t),
		`{"measurement":"  new_name  ","value":5.5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	assertTDAudit(t, db, envelope.RequestID, "test_data.update")
	rec = tdRequest(t, router, http.MethodPatch, tdPath, member, tdUniqueKey(t), `{"data_type":"cryo"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("immutable field = %d, body=%s", rec.Code, rec.Body.String())
	}

	// DELETE：200 + 审计 test_data.delete；重复标记幂等（MarkInvalid 无状态机，200 两次）
	rec = tdRequest(t, router, http.MethodDelete, tdPath, member, tdUniqueKey(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("mark invalid = %d, body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	assertTDAudit(t, db, envelope.RequestID, "test_data.delete")
	rec = tdRequest(t, router, http.MethodDelete, tdPath, member, tdUniqueKey(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("double delete = %d（MarkInvalid 幂等）", rec.Code)
	}
}

// *float64 零值语义：批量行 value=0 显式通过（不得误判为缺失）。
func TestBatchValueZeroIsExplicitlyPresent(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, &fakeRuns{exists: true})
	zero := 0.0
	result, err := svc.CreateBatch(projectUUID, "creator", auth.RoleMember, nil, []CreateBatchRow{
		{DataType: DataTypeCryo, Measurement: "t", Value: &zero},
	}, nil, nil)
	if err != nil {
		t.Fatalf("value=0 batch: %v", err)
	}
	if result.Count != 1 || result.Items[0].Value != 0 {
		t.Fatalf("value=0 batch result: %+v", result)
	}
	// handler 层：value=0 行 + 空 value 行同时提交 → 只有空 value 行报 required
	router := newBatchRouter(svc)
	body := `[
		{"data_type":"cryo","measurement":"t1","value":0},
		{"data_type":"cryo","measurement":"t2"}
	]`
	rec := batchRequest(router, body, true)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	raw := envelope.Error.Details["errors"].([]any)
	if len(raw) != 1 {
		t.Fatalf("errors = %v, want 1（value=0 不应报 required）", raw)
	}
	first := raw[0].(map[string]any)
	if first["index"].(float64) != 1 || first["field"] != "value" || first["code"] != "required" {
		t.Fatalf("errors[0] = %v", first)
	}
}
