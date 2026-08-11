package runs

import (
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
	"github.com/zhu571/hiaf-lab-system/go-server/steptemplates"
)

// 集成测试：需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移）。
// runs.Service 的 repo 是具体 *Repository，无法注入 mock，故 service 层 CRUD/流转/步骤
// 走真实 PostgreSQL（与 projects/service_db_test.go 同模式）；无 TEST_DATABASE_URL 时跳过。
// 种子用户/项目/模板为固定 UUID（ON CONFLICT DO NOTHING）+ t.Cleanup 清理，CI 以 -p 1 串行。

const (
	runAdminUserID    = "00000000-0000-0000-0000-00000000f101"
	runOwnerUserID    = "00000000-0000-0000-0000-00000000f102"
	runMemberUserID   = "00000000-0000-0000-0000-00000000f103"
	runOutsiderUserID = "00000000-0000-0000-0000-00000000f104"
	runViewerUserID   = "00000000-0000-0000-0000-00000000f105"

	runTestProjectID  = "c0000000-0000-4000-8000-000000000301"
	runTestTemplateID = "c0000000-0000-4000-8000-000000000302"
	runTemplateItemID = "c0000000-0000-4000-8000-000000000303"
	runTestReportID   = "c0000000-0000-4000-8000-000000000304"
)

func openRunsTestDB(t *testing.T) *sql.DB {
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

	// 种子用户：admin / owner / member / 无成员关系的 outsider / viewer 成员
	for _, u := range []struct {
		id       string
		username string
		role     string
	}{
		{runAdminUserID, "runs_dbtest_admin", "admin"},
		{runOwnerUserID, "runs_dbtest_owner", "member"},
		{runMemberUserID, "runs_dbtest_member", "member"},
		{runOutsiderUserID, "runs_dbtest_outsider", "member"},
		{runViewerUserID, "runs_dbtest_viewer", "member"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Runs DB Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}

	// 种子项目（owner 为 f102，成员：owner/member/viewer）
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'PRJ_RUNS_DBTEST', 'Runs DB 种子项目', 'draft', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, runTestProjectID, runOwnerUserID); err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		userID string
		role   string
	}{
		{runOwnerUserID, "owner"},
		{runMemberUserID, "member"},
		{runViewerUserID, "viewer"},
	} {
		if _, err := db.Exec(
			`INSERT INTO project_members (project_id, user_id, role, status, added_by)
			 VALUES ($1, $2, $3, 'active', $2)
			 ON CONFLICT (project_id, user_id) DO NOTHING`, runTestProjectID, m.userID, m.role); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_run_links WHERE run_id IN (SELECT id FROM experiment_runs WHERE project_id = $1)`, runTestProjectID)
		db.Exec(`DELETE FROM run_steps WHERE run_id IN (SELECT id FROM experiment_runs WHERE project_id = $1)`, runTestProjectID)
		db.Exec(`DELETE FROM experiment_runs WHERE project_id = $1`, runTestProjectID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, runTestReportID)
		db.Exec(`DELETE FROM step_template_items WHERE template_id = $1`, runTestTemplateID)
		db.Exec(`DELETE FROM step_templates WHERE id = $1`, runTestTemplateID)
		db.Exec(`DELETE FROM project_members WHERE project_id = $1`, runTestProjectID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, runTestProjectID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4,$5)`, runAdminUserID, runOwnerUserID, runMemberUserID, runOutsiderUserID, runViewerUserID)
	})
	return db
}

func newRunsDBService(db *sql.DB) *Service {
	projectsRepo := projects.NewRepository(db)
	return NewService(NewRepository(db), ProjectAccessAdapter{Repo: projectsRepo})
}

func seedRunTemplate(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO step_templates (id, name, kind) VALUES ($1, '降温流程', 'experiment')
		 ON CONFLICT (id) DO NOTHING`, runTestTemplateID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO step_template_items (id, template_id, name, step_order) VALUES ($1, $2, '预冷', 1)
		 ON CONFLICT (id) DO NOTHING`, runTemplateItemID, runTestTemplateID); err != nil {
		t.Fatal(err)
	}
}

type dbTemplateReader struct{ repo *steptemplates.Repository }

func (b dbTemplateReader) GetTemplateWithItems(id string) (*SteptemplatesTemplate, []SteptemplatesItem, error) {
	tmpl, items, err := b.repo.GetTemplateWithItems(id)
	if err != nil || tmpl == nil {
		return nil, nil, err
	}
	t := &SteptemplatesTemplate{ID: tmpl.ID, Name: tmpl.Name, Kind: tmpl.Kind}
	out := make([]SteptemplatesItem, len(items))
	for i, item := range items {
		out[i] = SteptemplatesItem{
			ID: item.ID, Name: item.Name, Description: item.Description,
			StepOrder: item.StepOrder, DependsOnOrder: item.DependsOnOrder,
		}
	}
	return t, out, nil
}

func createDBRun(t *testing.T, db *sql.DB, name string) *ExperimentRun {
	t.Helper()
	svc := newRunsDBService(db)
	run, err := svc.Create(runTestProjectID, runOwnerUserID, auth.RoleMember, CreateRunRequest{
		Name: name, Campaign: strPtr("camp-1"), TargetTemp: ptrFloat(10), MinTemp: ptrFloat(5),
		PressureMin: ptrFloat(1), PressureMax: ptrFloat(2), Devices: []string{DeviceRFCarpet, DeviceRFQ},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiment_runs WHERE id = $1`, run.ID) })
	return run
}

func ptrFloat(v float64) *float64 { return &v }

func TestDBCreateRun(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)

	// 项目不存在 → ErrProjectNotFound
	if _, err := svc.Create("b0000000-0000-4000-8000-000000009999", runOwnerUserID, auth.RoleMember, CreateRunRequest{Name: "x"}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing project: got %v, want ErrProjectNotFound", err)
	}
	// 项目 ID 为空 → ErrInvalidInput
	if _, err := svc.Create("  ", runOwnerUserID, auth.RoleMember, CreateRunRequest{Name: "x"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty project: got %v, want ErrInvalidInput", err)
	}
	// outsider / viewer 无创建权限
	if _, err := svc.Create(runTestProjectID, runOutsiderUserID, auth.RoleMember, CreateRunRequest{Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider create: got %v, want ErrForbidden", err)
	}
	if _, err := svc.Create(runTestProjectID, runViewerUserID, auth.RoleMember, CreateRunRequest{Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer create: got %v, want ErrForbidden", err)
	}

	// 非法输入
	for name, req := range map[string]CreateRunRequest{
		"empty name":   {Name: "  "},
		"bad run_type": {Name: "x", RunType: strPtr("overnight")},
		"long gas":     {Name: "x", GasType: strPtr("Helium-16-chars-plus")},
		"bad pressure": {Name: "x", PressureUnit: strPtr("mbar-extra-long")},
		"min>max":      {Name: "x", PressureMin: ptrFloat(3), PressureMax: ptrFloat(2)},
		"bad device":   {Name: "x", Devices: []string{"laser"}},
		"long name":    {Name: string(make([]byte, 300))},
	} {
		if _, err := svc.Create(runTestProjectID, runOwnerUserID, auth.RoleMember, req); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: got %v, want ErrInvalidInput", name, err)
		}
	}

	// 成功：默认 run_type=test / status=planned / gas=He / pressure_unit=mbar；设备去重
	run, err := svc.Create(runTestProjectID, runOwnerUserID, auth.RoleMember, CreateRunRequest{
		Name: "  正常运行  ", Devices: []string{DeviceRFQ, DeviceRFQ, DeviceRFCarpet},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiment_runs WHERE id = $1`, run.ID) })
	if run.Name != "正常运行" || run.RunType != RunTypeTest || run.Status != StatusPlanned ||
		run.GasType != GasTypeHe || run.PressureUnit != "mbar" || len(run.Devices) != 2 {
		t.Fatalf("defaults: %+v", run)
	}
	if run.CreatedBy == nil || *run.CreatedBy != runOwnerUserID {
		t.Fatalf("created_by: %+v", run)
	}

	// admin 无成员关系也可创建
	adminRun, err := svc.Create(runTestProjectID, runAdminUserID, auth.RoleAdmin, CreateRunRequest{Name: "admin-run"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiment_runs WHERE id = $1`, adminRun.ID) })
	if adminRun.Status != StatusPlanned {
		t.Fatalf("admin run: %+v", adminRun)
	}
}

func TestDBListRuns(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)
	createDBRun(t, db, "list-a")
	createDBRun(t, db, "list-b")
	createDBRun(t, db, "list-c")

	// outsider → ErrForbidden
	if _, err := svc.List(runTestProjectID, runOutsiderUserID, auth.RoleMember, RunListParams{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider list: got %v, want ErrForbidden", err)
	}
	// 项目不存在 → ErrProjectNotFound
	if _, err := svc.List("b0000000-0000-4000-8000-000000009999", runOwnerUserID, auth.RoleMember, RunListParams{}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing project list: got %v", err)
	}
	// 非法 status / run_type 过滤 → ErrInvalidInput
	if _, err := svc.List(runTestProjectID, runOwnerUserID, auth.RoleMember, RunListParams{Status: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad status: got %v", err)
	}
	if _, err := svc.List(runTestProjectID, runOwnerUserID, auth.RoleMember, RunListParams{RunType: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad run_type: got %v", err)
	}

	// viewer 可读；campaign/status/run_type 过滤 + 分页
	result, err := svc.List(runTestProjectID, runViewerUserID, auth.RoleMember, RunListParams{Campaign: "camp-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || len(result.Items) != 3 || result.Page != 1 || result.PerPage != 20 {
		t.Fatalf("campaign filter: %+v", result)
	}
	paged, err := svc.List(runTestProjectID, runOwnerUserID, auth.RoleMember, RunListParams{PerPage: 2, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if paged.Total != 3 || len(paged.Items) != 2 {
		t.Fatalf("page1: total=%d items=%d", paged.Total, len(paged.Items))
	}
	page2, err := svc.List(runTestProjectID, runOwnerUserID, auth.RoleMember, RunListParams{PerPage: 2, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) != 1 {
		t.Fatalf("page2: %+v", page2)
	}
	// 分页参数归一化
	norm, err := svc.List(runTestProjectID, runOwnerUserID, auth.RoleMember, RunListParams{Page: 0, PerPage: 500})
	if err != nil {
		t.Fatal(err)
	}
	if norm.PerPage != 100 {
		t.Fatalf("per_page clamp: %d", norm.PerPage)
	}
}

func TestDBRunGetByID(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)
	run := createDBRun(t, db, "get-me")

	got, err := svc.GetByID(run.ID, runMemberUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != run.ID || got.Status != StatusPlanned {
		t.Fatalf("get: %+v", got)
	}
	if _, err := svc.GetByID("b0000000-0000-4000-8000-000000009999", runMemberUserID, auth.RoleMember); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing run: got %v, want ErrRunNotFound", err)
	}
	if _, err := svc.GetByID(run.ID, runOutsiderUserID, auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider get: got %v, want ErrForbidden", err)
	}
}

func TestDBRunUpdate(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)
	run := createDBRun(t, db, "update-me")

	// 非创建者 member 无 maintainer 权限 → ErrForbidden（creator 豁免只认创建者）
	if _, err := svc.Update(run.ID, runMemberUserID, auth.RoleMember, UpdateRunRequest{Name: strPtr("x")}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member update: got %v, want ErrForbidden", err)
	}
	// 不存在的 run → ErrRunNotFound
	if _, err := svc.Update("b0000000-0000-4000-8000-000000009999", runOwnerUserID, auth.RoleMember, UpdateRunRequest{Name: strPtr("x")}); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing update: got %v, want ErrRunNotFound", err)
	}
	// 非法输入：空 name / 坏 run_type / min>max
	if _, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Name: strPtr("  ")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty name: got %v", err)
	}
	if _, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{RunType: strPtr("bogus")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad run type: got %v", err)
	}
	if _, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{PressureMin: ptrFloat(9), PressureMax: ptrFloat(1)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("min>max: got %v", err)
	}

	// 元数据更新：改名 + 设备去重 + 清空 campaign
	campaign := ""
	updated, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{
		Name: strPtr("  改名后  "), Devices: []string{DeviceQPIG, DeviceQPIG}, Campaign: &campaign,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "改名后" || len(updated.Devices) != 1 || updated.Devices[0] != DeviceQPIG ||
		updated.Campaign != nil {
		t.Fatalf("updated: %+v", updated)
	}

	// 流转 + 元数据混用 → ErrInvalidInput
	start := "start"
	if _, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: &start, Name: strPtr("x")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("transition+metadata: got %v, want ErrInvalidInput", err)
	}
	// 全路径：planned → active → paused → resume → completed
	toActive, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: &start})
	if err != nil {
		t.Fatal(err)
	}
	if toActive.Status != StatusActive || toActive.StartedAt == nil {
		t.Fatalf("started: %+v", toActive)
	}
	pause := "pause"
	toPaused, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: &pause})
	if err != nil {
		t.Fatal(err)
	}
	if toPaused.Status != StatusPaused {
		t.Fatalf("paused: %+v", toPaused)
	}
	resume := "resume"
	toResumed, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: &resume})
	if err != nil {
		t.Fatal(err)
	}
	if toResumed.Status != StatusActive {
		t.Fatalf("resumed: %+v", toResumed)
	}
	complete := "complete"
	toCompleted, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: &complete})
	if err != nil {
		t.Fatal(err)
	}
	if toCompleted.Status != StatusCompleted || toCompleted.StartedAt == nil || toCompleted.EndedAt == nil {
		t.Fatalf("completed: %+v", toCompleted)
	}
	// completed 上再 start → ErrInvalidTransition
	if _, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: &start}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid transition: got %v", err)
	}
}

func TestDBRunAbortPaths(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)

	// planned → aborted（started 不落，ended 落）
	run := createDBRun(t, db, "abort-planned")
	abort := "abort"
	aborted, err := svc.Update(run.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: &abort})
	if err != nil {
		t.Fatal(err)
	}
	if aborted.Status != StatusAborted || aborted.StartedAt != nil || aborted.EndedAt == nil {
		t.Fatalf("abort planned: %+v", aborted)
	}

	// active → aborted（started + ended 都落）
	run2 := createDBRun(t, db, "abort-active")
	if _, err := svc.Update(run2.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: strPtr("start")}); err != nil {
		t.Fatal(err)
	}
	aborted2, err := svc.Update(run2.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: &abort})
	if err != nil {
		t.Fatal(err)
	}
	if aborted2.Status != StatusAborted || aborted2.StartedAt == nil || aborted2.EndedAt == nil {
		t.Fatalf("abort active: %+v", aborted2)
	}

	// paused → aborted
	run3 := createDBRun(t, db, "abort-paused")
	if _, err := svc.Update(run3.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: strPtr("start")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(run3.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: strPtr("pause")}); err != nil {
		t.Fatal(err)
	}
	aborted3, err := svc.Update(run3.ID, runOwnerUserID, auth.RoleMember, UpdateRunRequest{Transition: &abort})
	if err != nil {
		t.Fatal(err)
	}
	if aborted3.Status != StatusAborted {
		t.Fatalf("abort paused: %+v", aborted3)
	}

	// 状态冲突：repo 层 UpdateStatus 带错 fromStatus → ErrRunConflict
	repo := NewRepository(db)
	if err := repo.UpdateStatus(run3.ID, StatusPlanned, StatusActive, true, false); !errors.Is(err, ErrRunConflict) {
		t.Fatalf("status conflict: got %v, want ErrRunConflict", err)
	}
}

func TestDBRunDelete(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)
	run := createDBRun(t, db, "delete-me")

	// 加两个步骤后软删除，验证级联
	step1, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "s1", StepOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	step2, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "s2", StepOrder: 2})
	if err != nil {
		t.Fatal(err)
	}

	// 非创建者 member 删除 → ErrForbidden
	if err := svc.SoftDelete(run.ID, runMemberUserID, auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member delete: got %v, want ErrForbidden", err)
	}
	// 创建者删除成功
	if err := svc.SoftDelete(run.ID, runOwnerUserID, auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	// 再删 → ErrRunNotFound（软删后不可见）
	if err := svc.SoftDelete(run.ID, runOwnerUserID, auth.RoleMember); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("double delete: got %v, want ErrRunNotFound", err)
	}
	if _, err := svc.GetByID(run.ID, runOwnerUserID, auth.RoleMember); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("get deleted: got %v", err)
	}
	// 级联：步骤软删
	if got, err := svc.GetStepByIDForTest(step1.ID); err != nil || got != nil {
		t.Fatalf("step1 after delete: %v err=%v", got, err)
	}
	if got, err := svc.GetStepByIDForTest(step2.ID); err != nil || got != nil {
		t.Fatalf("step2 after delete: %v err=%v", got, err)
	}
}

func TestDBReportLinks(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)
	if _, err := db.Exec(
		`INSERT INTO daily_reports (id, report_date, author_id) VALUES ($1, '2026-08-01', $2)
		 ON CONFLICT (id) DO NOTHING`, runTestReportID, runOwnerUserID); err != nil {
		t.Fatal(err)
	}
	run := createDBRun(t, db, "link-me")

	// 非创建者 member → ErrForbidden（maintainer 语义）
	if _, err := svc.AddReportLink(run.ID, runTestReportID, runMemberUserID, auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member add link: got %v, want ErrForbidden", err)
	}
	// 空 reportID → ErrInvalidInput
	if _, err := svc.AddReportLink(run.ID, "  ", runOwnerUserID, auth.RoleMember); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty report id: got %v, want ErrInvalidInput", err)
	}
	// 添加成功 + 幂等（ON CONFLICT DO NOTHING）
	links, err := svc.AddReportLink(run.ID, runTestReportID, runOwnerUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0] != runTestReportID {
		t.Fatalf("links after add: %v", links)
	}
	if _, err := svc.AddReportLink(run.ID, runTestReportID, runOwnerUserID, auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	// 移除
	links, err = svc.RemoveReportLink(run.ID, runTestReportID, runOwnerUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("links after remove: %v", links)
	}
	// 移除不存在的关联 → ErrReportLinkNotFound
	if _, err := svc.RemoveReportLink(run.ID, runTestReportID, runOwnerUserID, auth.RoleMember); !errors.Is(err, ErrReportLinkNotFound) {
		t.Fatalf("remove missing link: got %v, want ErrReportLinkNotFound", err)
	}
}

func TestDBRunStepsCRUD(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)
	run := createDBRun(t, db, "steps-crud")

	// 非法输入：空名 / 超长 / 负 order
	if _, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty step name: got %v", err)
	}
	if _, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: string(make([]byte, 300)), StepOrder: 1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("long step name: got %v", err)
	}
	if _, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "x", StepOrder: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative order: got %v", err)
	}
	// outsider → ErrForbidden
	if _, err := svc.CreateStep(run.ID, runOutsiderUserID, auth.RoleMember, CreateStepRequest{Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider step: got %v", err)
	}

	// order=0 自动 max+1；两个步骤 + 依赖
	step1, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "step-1", StepOrder: 0})
	if err != nil {
		t.Fatal(err)
	}
	if step1.StepOrder != 1 {
		t.Fatalf("auto order: %+v", step1)
	}
	dep := step1.ID
	step2, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "step-2", StepOrder: 2, DependsOn: &dep})
	if err != nil {
		t.Fatal(err)
	}
	if step2.DependsOn == nil || *step2.DependsOn != step1.ID || step2.StepOrder != 2 {
		t.Fatalf("depends: %+v", step2)
	}
	// 依赖不存在的步骤 → ErrStepNotFound
	if _, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "x", StepOrder: 3, DependsOn: strPtr("b0000000-0000-4000-8000-000000009999")}); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("missing dependency: got %v", err)
	}
	// 非 UUID 依赖 → ErrInvalidInput
	if _, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "x", StepOrder: 3, DependsOn: strPtr("not-a-uuid")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad dependency uuid: got %v", err)
	}

	// ListSteps：2 步按 step_order 升序
	list, err := svc.ListSteps(run.ID, runMemberUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 || len(list.Items) != 2 || list.Items[0].Name != "step-1" || list.Items[1].Name != "step-2" {
		t.Fatalf("list steps: %+v", list)
	}

	// 更新步骤元数据：非创建者 member 无 maintainer 权限
	if _, err := svc.UpdateStep(step1.ID, runMemberUserID, auth.RoleMember, UpdateStepRequest{Name: strPtr("x")}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member update step: got %v, want ErrForbidden", err)
	}
	// 空更新（无字段）→ ErrInvalidInput
	if _, err := svc.UpdateStep(step1.ID, runOwnerUserID, auth.RoleMember, UpdateStepRequest{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty step update: got %v", err)
	}
	// 正常更新名称；空依赖串视为非法输入（生产代码无清空依赖路径）
	renamed, err := svc.UpdateStep(step1.ID, runOwnerUserID, auth.RoleMember, UpdateStepRequest{Name: strPtr(" 改名步骤 ")})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "改名步骤" {
		t.Fatalf("renamed: %+v", renamed)
	}
	if _, err := svc.UpdateStep(step1.ID, runOwnerUserID, auth.RoleMember, UpdateStepRequest{DependsOn: strPtr("")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty depends_on: got %v, want ErrInvalidInput", err)
	}
	// 更新为不存在的步骤 → ErrStepNotFound
	if _, err := svc.UpdateStep("b0000000-0000-4000-8000-000000009999", runOwnerUserID, auth.RoleMember, UpdateStepRequest{Name: strPtr("x")}); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("update missing step: got %v", err)
	}

	// 删除步骤：创建者 member 可删；非创建者 member 不可删
	if err := svc.DeleteStep(step1.ID, runMemberUserID, auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member delete step: got %v, want ErrForbidden", err)
	}
	// 非创建者 maintainer 可删（owner 用户 + maintainer 角色）
	if err := svc.DeleteStep(step1.ID, runOwnerUserID, auth.RoleMaintainer); err != nil {
		t.Fatal(err)
	}
	// 删不存在的 → ErrStepNotFound
	if err := svc.DeleteStep("b0000000-0000-4000-8000-000000009999", runOwnerUserID, auth.RoleMaintainer); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("delete missing step: got %v", err)
	}
	// 非法 id → ErrInvalidInput
	if err := svc.DeleteStep("nope", runOwnerUserID, auth.RoleMaintainer); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("delete bad id: got %v", err)
	}

	// 创建者本人（member 角色）删自己的步骤 → 允许
	own, err := svc.CreateStep(run.ID, runMemberUserID, auth.RoleMember, CreateStepRequest{Name: "own-step", StepOrder: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteStep(own.ID, runMemberUserID, auth.RoleMember); err != nil {
		t.Fatalf("creator delete own step: %v", err)
	}
}

func TestDBRunStepTransitions(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)
	run := createDBRun(t, db, "step-transitions")

	step, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "t-step", StepOrder: 1})
	if err != nil {
		t.Fatal(err)
	}

	// 非创建者 member 可做流转（member 权限）；outsider 不可
	if _, err := svc.UpdateStep(step.ID, runOutsiderUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionStart)}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider step transition: got %v", err)
	}
	// 流转 + 元数据混用 → ErrInvalidInput
	if _, err := svc.UpdateStep(step.ID, runOwnerUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionStart), Name: strPtr("x")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("transition+meta: got %v", err)
	}
	// 非法流转：planned → resume
	if _, err := svc.UpdateStep(step.ID, runOwnerUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionResume)}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid step transition: got %v", err)
	}

	// 全路径：start → pause → resume → complete（由非创建者 member 操作）
	started, err := svc.UpdateStep(step.ID, runMemberUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionStart)})
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != StepStatusInProgress || started.StartedAt == nil || started.CompletedAt != nil {
		t.Fatalf("started: %+v", started)
	}
	paused, err := svc.UpdateStep(step.ID, runMemberUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionPause)})
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != StepStatusPaused {
		t.Fatalf("paused: %+v", paused)
	}
	resumed, err := svc.UpdateStep(step.ID, runMemberUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionResume)})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != StepStatusInProgress {
		t.Fatalf("resumed: %+v", resumed)
	}
	completed, err := svc.UpdateStep(step.ID, runMemberUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionComplete)})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StepStatusCompleted || completed.StartedAt == nil || completed.CompletedAt == nil {
		t.Fatalf("completed: %+v", completed)
	}

	// 新步骤：start 后 skip（skip 仅 allowed 于 in_progress）
	skipStep, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "skip-me", StepOrder: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateStep(skipStep.ID, runOwnerUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionStart)}); err != nil {
		t.Fatal(err)
	}
	skipped, err := svc.UpdateStep(skipStep.ID, runOwnerUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionSkip)})
	if err != nil {
		t.Fatal(err)
	}
	if skipped.Status != StepStatusSkipped || skipped.StartedAt == nil || skipped.CompletedAt == nil {
		t.Fatalf("skipped: %+v", skipped)
	}
	// skipped → start（允许重新开始）
	reStarted, err := svc.UpdateStep(skipStep.ID, runOwnerUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionStart)})
	if err != nil {
		t.Fatal(err)
	}
	if reStarted.Status != StepStatusInProgress {
		t.Fatalf("re-start skipped: %+v", reStarted)
	}
	// cancel 路径
	cancelStep, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "cancel-me", StepOrder: 3})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := svc.UpdateStep(cancelStep.ID, runOwnerUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionCancel)})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StepStatusCancelled || cancelled.CompletedAt == nil {
		t.Fatalf("cancelled: %+v", cancelled)
	}
	// cancelled 再流转 → ErrInvalidTransition
	if _, err := svc.UpdateStep(cancelStep.ID, runOwnerUserID, auth.RoleMember, UpdateStepRequest{Transition: strPtr(StepTransitionResume)}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("transition cancelled: got %v", err)
	}

	// repo 层状态冲突：fromStatus 不匹配 → ErrStepConflict
	repo := NewRepository(db)
	if err := repo.UpdateStepStatus(step.ID, StepStatusPlanned, StepStatusInProgress, nil, nil); !errors.Is(err, ErrStepConflict) {
		t.Fatalf("step conflict: got %v, want ErrStepConflict", err)
	}
}

func TestDBRunStepReorder(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)
	run := createDBRun(t, db, "reorder-me")
	s1, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "r1", StepOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "r2", StepOrder: 2})
	if err != nil {
		t.Fatal(err)
	}
	s3, err := svc.CreateStep(run.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "r3", StepOrder: 3})
	if err != nil {
		t.Fatal(err)
	}

	// 空列表 → ErrInvalidInput
	if err := svc.ReorderSteps(run.ID, runOwnerUserID, auth.RoleMember, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty reorder: got %v", err)
	}
	// 重复 id / 重复 order / 非法 order → ErrInvalidInput
	if err := svc.ReorderSteps(run.ID, runOwnerUserID, auth.RoleMember, []ReorderItem{{ID: s1.ID, StepOrder: 1}, {ID: s1.ID, StepOrder: 2}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("dup id: got %v", err)
	}
	if err := svc.ReorderSteps(run.ID, runOwnerUserID, auth.RoleMember, []ReorderItem{{ID: s1.ID, StepOrder: 1}, {ID: s2.ID, StepOrder: 1}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("dup order: got %v", err)
	}
	if err := svc.ReorderSteps(run.ID, runOwnerUserID, auth.RoleMember, []ReorderItem{{ID: s1.ID, StepOrder: 0}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero order: got %v", err)
	}
	// 不属于本 run 的步骤 → ErrInvalidInput
	otherRun := createDBRun(t, db, "other-run")
	otherStep, err := svc.CreateStep(otherRun.ID, runOwnerUserID, auth.RoleMember, CreateStepRequest{Name: "other", StepOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReorderSteps(run.ID, runOwnerUserID, auth.RoleMember, []ReorderItem{{ID: otherStep.ID, StepOrder: 1}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("foreign step: got %v", err)
	}
	// 不存在的步骤 → ErrStepNotFound
	if err := svc.ReorderSteps(run.ID, runOwnerUserID, auth.RoleMember, []ReorderItem{{ID: "b0000000-0000-4000-8000-000000009999", StepOrder: 1}}); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("missing step reorder: got %v", err)
	}
	// 成功重排：1→3, 2→1, 3→2
	if err := svc.ReorderSteps(run.ID, runOwnerUserID, auth.RoleMember, []ReorderItem{{ID: s1.ID, StepOrder: 3}, {ID: s2.ID, StepOrder: 1}, {ID: s3.ID, StepOrder: 2}}); err != nil {
		t.Fatal(err)
	}
	items, err := svc.repo.(*Repository).ListSteps(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	order := map[string]int{}
	for _, item := range items {
		order[item.ID] = item.StepOrder
	}
	if order[s1.ID] != 3 || order[s2.ID] != 1 || order[s3.ID] != 2 {
		t.Fatalf("reordered: %v", order)
	}
}

func TestDBApplyTemplate(t *testing.T) {
	db := openRunsTestDB(t)
	svc := newRunsDBService(db)
	run := createDBRun(t, db, "template-me")

	// 两者都给 / 都不给 → ErrInvalidInput
	if _, err := svc.ApplyTemplate(run.ID, runOwnerUserID, auth.RoleMember, ApplyTemplateRequest{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no source: got %v", err)
	}
	if _, err := svc.ApplyTemplate(run.ID, runOwnerUserID, auth.RoleMember, ApplyTemplateRequest{
		TemplateID: strPtr(runTestTemplateID), Steps: []StepDef{{Name: "x", StepOrder: 1}},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("both sources: got %v", err)
	}
	// tmplReader 未配置 → 报错
	if _, err := svc.ApplyTemplate(run.ID, runOwnerUserID, auth.RoleMember, ApplyTemplateRequest{TemplateID: strPtr(runTestTemplateID)}); err == nil {
		t.Fatal("want error when template reader not configured")
	}
	// outsider → ErrForbidden
	if _, err := svc.ApplyTemplate(run.ID, runOutsiderUserID, auth.RoleMember, ApplyTemplateRequest{TemplateID: strPtr(runTestTemplateID)}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider apply: got %v", err)
	}

	// 模板不存在 → 报错
	svc.ConfigureTemplates(dbTemplateReader{repo: steptemplates.NewRepository(db)})
	if _, err := svc.ApplyTemplate(run.ID, runOwnerUserID, auth.RoleMember, ApplyTemplateRequest{TemplateID: strPtr("b0000000-0000-4000-8000-000000009999")}); err == nil {
		t.Fatal("want error for missing template")
	}
	// 错误 kind（assembly）→ common.Error bad_request
	seedRunTemplate(t, db)
	if _, err := db.Exec(`UPDATE step_templates SET kind = 'assembly' WHERE id = $1`, runTestTemplateID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyTemplate(run.ID, runOwnerUserID, auth.RoleMember, ApplyTemplateRequest{TemplateID: strPtr(runTestTemplateID)}); err == nil {
		t.Fatal("want error for wrong kind")
	} else {
		var commonErr *common.Error
		if !errors.As(err, &commonErr) || commonErr.Code != "bad_request" {
			t.Fatalf("wrong kind error: %v", err)
		}
	}
	// 恢复 kind=experiment 后成功应用：模板 item 展开 + source_template_id 落库
	if _, err := db.Exec(`UPDATE step_templates SET kind = 'experiment' WHERE id = $1`, runTestTemplateID); err != nil {
		t.Fatal(err)
	}
	steps, err := svc.ApplyTemplate(run.ID, runOwnerUserID, auth.RoleMember, ApplyTemplateRequest{TemplateID: strPtr(runTestTemplateID)})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Name != "预冷" || steps[0].StepOrder != 1 {
		t.Fatalf("template steps: %+v", steps)
	}
	// source_template_id 由 SetSourceTemplateID 落库，需重读确认
	repo := NewRepository(db)
	persisted, err := repo.GetStepByID(steps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted == nil || persisted.SourceTemplateID == nil || *persisted.SourceTemplateID != runTestTemplateID {
		t.Fatalf("source template id: %+v", persisted)
	}

	// 内联步骤（含 depends_on_order 映射）+ 超限拒绝
	run2 := createDBRun(t, db, "template-inline")
	inline, err := svc.ApplyTemplate(run2.ID, runOwnerUserID, auth.RoleMember, ApplyTemplateRequest{
		Steps: []StepDef{
			{Name: "a", StepOrder: 2},
			{Name: "b", StepOrder: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inline) != 2 || inline[0].Name != "b" || inline[0].StepOrder != 1 || inline[1].Name != "a" || inline[1].StepOrder != 2 {
		t.Fatalf("inline steps: %+v", inline)
	}
	tooMany := make([]StepDef, 31)
	for i := range tooMany {
		tooMany[i] = StepDef{Name: "s", StepOrder: i + 1}
	}
	if _, err := svc.ApplyTemplate(run2.ID, runOwnerUserID, auth.RoleMember, ApplyTemplateRequest{Steps: tooMany}); err == nil {
		t.Fatal("want error for >30 steps")
	}
	// 带 depends_on_order 的批量插入（CreateStepsMany 依赖映射分支）
	run3 := createDBRun(t, db, "template-depends")
	withDep, err := svc.ApplyTemplate(run3.ID, runOwnerUserID, auth.RoleMember, ApplyTemplateRequest{
		Steps: []StepDef{
			{Name: "d1", StepOrder: 1},
			{Name: "d2", StepOrder: 2, DependsOnOrder: intPtr(1)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(withDep) != 2 || withDep[1].DependsOn == nil || *withDep[1].DependsOn != withDep[0].ID {
		t.Fatalf("depends mapping: %+v", withDep)
	}
}

func intPtr(v int) *int { return &v }

// GetStepByIDForTest 供测试断言软删后的步骤不可见。
func (s *Service) GetStepByIDForTest(id string) (*RunStep, error) {
	return s.repo.GetStepByID(id)
}
