package assembly

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

// service 集成测试：需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移）。
// assembly.Service 的 repo 是具体 *Repository 但 access 是接口，故 service 层用真实 repo +
// fake access（允许/拒绝两种），覆盖步骤 CRUD、状态流转、依赖校验、reorder、软删、
// apply-template（模板/内联）。固定 UUID 种子 + t.Cleanup 清理，CI -p 1 串行。

const (
	asmDBOwnerID      = "00000000-0000-0000-0000-00000000bb01"
	asmDBMaintainerID = "00000000-0000-0000-0000-00000000bb02"
	asmDBMemberID     = "00000000-0000-0000-0000-00000000bb03"
	asmDBViewerID     = "00000000-0000-0000-0000-00000000bb04"
	asmDBOutsiderID   = "00000000-0000-0000-0000-00000000bb05"
	asmDBAdminID      = "00000000-0000-0000-0000-00000000bb06"

	asmDBProjectID = "c0000000-0000-4000-8000-00000000cb01"

	asmDBTemplateID = "c0000000-0000-4000-8000-00000000cb02"
)

func openAsmSvcDB(t *testing.T) *sql.DB {
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
		{asmDBOwnerID, "asm_db_owner", "member"},
		{asmDBMaintainerID, "asm_db_maintainer", "member"},
		{asmDBMemberID, "asm_db_member", "member"},
		{asmDBViewerID, "asm_db_viewer", "viewer"},
		{asmDBOutsiderID, "asm_db_outsider", "member"},
		{asmDBAdminID, "asm_db_admin", "admin"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Asm DB Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'ASM_DBTEST_ACTIVE', 'DB 种子项目', 'active', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, asmDBProjectID, asmDBOwnerID); err != nil {
		t.Fatal(err)
	}
	// 模板种子：ApplyTemplate 的 source_template_id FK
	if _, err := db.Exec(
		`INSERT INTO step_templates (id, name, kind)
		 VALUES ($1, 'DB 装配模板', 'assembly')
		 ON CONFLICT (id) DO NOTHING`, asmDBTemplateID); err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		userID string
		role   string
	}{
		{asmDBOwnerID, projects.RoleOwner},
		{asmDBMaintainerID, projects.RoleMaintainer},
		{asmDBMemberID, projects.RoleMember},
		{asmDBViewerID, projects.RoleViewer},
	} {
		if _, err := db.Exec(
			`INSERT INTO project_members (project_id, user_id, role, status, added_by)
			 VALUES ($1, $2, $3, 'active', $2)
			 ON CONFLICT (project_id, user_id) DO NOTHING`, asmDBProjectID, m.userID, m.role); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM assembly_steps WHERE project_id = $1`, asmDBProjectID)
		db.Exec(`DELETE FROM step_templates WHERE id = $1`, asmDBTemplateID)
		db.Exec(`DELETE FROM project_members WHERE project_id = $1`, asmDBProjectID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, asmDBProjectID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4,$5,$6)`,
			asmDBOwnerID, asmDBMaintainerID, asmDBMemberID, asmDBViewerID, asmDBOutsiderID, asmDBAdminID)
	})
	return db
}

type denyAccess struct{}

func (denyAccess) ProjectExists(string) (bool, error) { return true, nil }
func (denyAccess) CanAccessProject(string, string, string, string) (bool, error) {
	return false, nil
}

type missingProjectAccess struct{}

func (missingProjectAccess) ProjectExists(string) (bool, error) { return false, nil }
func (missingProjectAccess) CanAccessProject(string, string, string, string) (bool, error) {
	return true, nil
}

// fakeTemplateReader 模板读取器：可配置返回模板/条目或错误。
type fakeTemplateReader struct {
	tmpl  *SteptemplatesTemplate
	items []SteptemplatesItem
	err   error
}

func (f fakeTemplateReader) GetTemplateWithItems(id string) (*SteptemplatesTemplate, []SteptemplatesItem, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	if f.tmpl == nil {
		return nil, nil, nil
	}
	return f.tmpl, f.items, nil
}

func asmSvc(db *sql.DB, access ProjectAccessChecker) *Service {
	return NewService(NewRepository(db), access)
}

func uniqueAsmStepName() string {
	return fmt.Sprintf("装配步骤-%d", time.Now().UnixNano())
}

func TestDBCreateStep(t *testing.T) {
	db := openAsmSvcDB(t)
	svc := asmSvc(db, allowAccess{})

	// agent 拒绝 / 项目不存在 / 无权限
	if _, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleAgent, CreateStepRequest{Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent: got %v, want ErrForbidden", err)
	}
	missingSvc := asmSvc(db, missingProjectAccess{})
	if _, err := missingSvc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "x"}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing project: got %v, want ErrProjectNotFound", err)
	}
	denySvc := asmSvc(db, denyAccess{})
	if _, err := denySvc.Create(asmDBProjectID, asmDBMemberID, auth.RoleMember, CreateStepRequest{Name: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("no access: got %v, want ErrForbidden", err)
	}

	// 输入校验：空名 / 过长 / 负 order / 非法 assigned_to / 非法依赖 UUID
	tooLong := make([]rune, 257)
	for i := range tooLong {
		tooLong[i] = 'x'
	}
	if _, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty name: got %v", err)
	}
	if _, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: string(tooLong)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("long name: got %v", err)
	}
	if _, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "x", StepOrder: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative order: got %v", err)
	}
	badAssign := "not-a-uuid"
	if _, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "x", AssignedTo: &badAssign}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad assigned_to: got %v", err)
	}
	badDep := "not-a-uuid"
	if _, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "x", DependsOn: &badDep}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad depends_on: got %v", err)
	}
	// 依赖不存在 → ErrStepNotFound
	missingDep := "c0000000-0000-4000-8000-00000000cbbb"
	if _, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "x", DependsOn: &missingDep}); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("missing dependency: got %v, want ErrStepNotFound", err)
	}

	// 成功创建：显式 order 1 + created_by 落库
	assignee := asmDBMemberID
	created, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{
		Name: uniqueAsmStepName(), Description: "desc", StepOrder: 1, AssignedTo: &assignee,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, created.ID) })
	if created.Status != StatusPlanned || created.StepOrder != 1 || created.CreatedBy == nil || *created.CreatedBy != asmDBOwnerID {
		t.Fatalf("created: %+v", created)
	}

	// 自动 order：StepOrder=0 → max+1
	auto, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: uniqueAsmStepName()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, auto.ID) })
	if auto.StepOrder != 2 {
		t.Fatalf("auto order = %d, want 2", auto.StepOrder)
	}

	// 依赖链校验：依赖已存在 → 成功
	dep, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{
		Name: uniqueAsmStepName(), DependsOn: &created.ID, StepOrder: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, dep.ID) })
	if dep.DependsOn == nil || *dep.DependsOn != created.ID {
		t.Fatalf("depends_on not persisted: %+v", dep)
	}
	// 依赖同一项目已存在的步骤均可创建（无环）
	if _, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{
		Name: "x", DependsOn: &created.ID, StepOrder: 4,
	}); err != nil {
		t.Fatal("chain create should succeed")
	}
}

func TestDBCreateStepCycle(t *testing.T) {
	db := openAsmSvcDB(t)
	svc := asmSvc(db, allowAccess{})

	// 线性链：a ← b（b 依赖 a）。service 创建时校验依赖存在且同项目。
	a, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "A", StepOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, a.ID) })
	b, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "B", StepOrder: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, b.ID) })
	// 依赖另一项目步骤 → ErrInvalidInput（跨项目）
	otherProjectStep := "c0000000-0000-4000-8000-00000000cabc"
	if _, err := db.Exec(`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		VALUES ($1, 'ASM_OTHER', '其他项目', 'active', $2, $2)
		ON CONFLICT (id) DO NOTHING`, "c0000000-0000-4000-8000-00000000cabd", asmDBOwnerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM projects WHERE id = 'c0000000-0000-4000-8000-00000000cabd'`) })
	if _, err := db.Exec(`INSERT INTO assembly_steps (id, project_id, name, step_order)
		VALUES ($1, 'c0000000-0000-4000-8000-00000000cabd', '外部步骤', 1)`, otherProjectStep); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, otherProjectStep) })
	if _, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "x", DependsOn: &otherProjectStep, StepOrder: 3}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cross project dependency: got %v, want ErrInvalidInput", err)
	}

	// 环检测走 repo.GetDependencyChain（service 依赖创建后不可变，无法经 API 成环）：
	// b 依赖 a → 链 [a]；再让 a 依赖 b → 遍历成环 ErrDependencyCycle。
	repo := NewRepository(db)
	if _, err := db.Exec(`UPDATE assembly_steps SET depends_on = $2 WHERE id = $1`, b.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	chain, err := repo.GetDependencyChain(b.ID)
	if err != nil || len(chain) != 1 || chain[0] != a.ID {
		t.Fatalf("chain of b = %v err=%v", chain, err)
	}
	if _, err := db.Exec(`UPDATE assembly_steps SET depends_on = $2 WHERE id = $1`, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetDependencyChain(a.ID); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("cycle: got %v, want ErrDependencyCycle", err)
	}
	// 自依赖 → ErrDependencyCycle
	if _, err := db.Exec(`UPDATE assembly_steps SET depends_on = $2 WHERE id = $1`, a.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetDependencyChain(a.ID); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("self cycle: got %v, want ErrDependencyCycle", err)
	}
	// 不存在 → ErrStepNotFound
	if _, err := repo.GetDependencyChain("c0000000-0000-4000-8000-00000000cbbb"); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("missing chain: got %v, want ErrStepNotFound", err)
	}
}

func TestDBListAndGetStep(t *testing.T) {
	db := openAsmSvcDB(t)
	svc := asmSvc(db, allowAccess{})

	a, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "步骤A", StepOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, a.ID) })
	b, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "步骤B", StepOrder: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, b.ID) })

	// 全量列表（按 step_order）
	list, err := svc.List(asmDBProjectID, asmDBMemberID, auth.RoleMember, ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 || list.Items[0].ID != a.ID || list.Items[1].ID != b.ID {
		t.Fatalf("list: %+v", list)
	}
	// status 过滤
	planned, err := svc.List(asmDBProjectID, asmDBMemberID, auth.RoleMember, ListParams{Status: StatusPlanned})
	if err != nil {
		t.Fatal(err)
	}
	if planned.Total != 2 {
		t.Fatalf("planned filter: %+v", planned)
	}
	// 非法 status → ErrInvalidInput
	if _, err := svc.List(asmDBProjectID, asmDBMemberID, auth.RoleMember, ListParams{Status: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad status: got %v", err)
	}
	// 分页：page 2 → 空；page 1 per_page 1 → 1 条
	page2, err := svc.List(asmDBProjectID, asmDBMemberID, auth.RoleMember, ListParams{Page: 2, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page2.Total != 2 || len(page2.Items) != 0 {
		t.Fatalf("page2: %+v", page2)
	}
	// 无权限
	denySvc := asmSvc(db, denyAccess{})
	if _, err := denySvc.List(asmDBProjectID, asmDBMemberID, auth.RoleMember, ListParams{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("deny list: got %v", err)
	}

	// GetByID：命中 / 非法 uuid / 不存在 / 无权限
	got, err := svc.GetByID(a.ID, asmDBMemberID, auth.RoleMember)
	if err != nil || got.ID != a.ID {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	if _, err := svc.GetByID("not-a-uuid", asmDBMemberID, auth.RoleMember); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad uuid: got %v", err)
	}
	if _, err := svc.GetByID("c0000000-0000-4000-8000-00000000cbbb", asmDBMemberID, auth.RoleMember); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("missing: got %v", err)
	}
	if _, err := denySvc.GetByID(a.ID, asmDBMemberID, auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("deny get: got %v", err)
	}
}

func TestDBUpdateStep(t *testing.T) {
	db := openAsmSvcDB(t)
	svc := asmSvc(db, allowAccess{})

	created, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "原名", StepOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, created.ID) })

	// agent 拒绝 / 不存在
	if _, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleAgent, UpdateStepRequest{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent: got %v", err)
	}
	if _, err := svc.Update("c0000000-0000-4000-8000-00000000cbbb", asmDBOwnerID, auth.RoleMember, UpdateStepRequest{}); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("missing: got %v", err)
	}
	// 无字段可改 / 仅 override_reason（无 transition）→ ErrInvalidInput
	if _, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no fields: got %v", err)
	}
	reason := "x"
	if _, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{OverrideReason: &reason}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("override without transition: got %v", err)
	}
	// 改名成功（maintainer 语义由 access 保证，fake 恒放行）
	newName := "改名后的步骤"
	updated, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Name: &newName})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != newName {
		t.Fatalf("renamed: %+v", updated)
	}
	// 空名 → ErrInvalidInput
	empty := "  "
	if _, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Name: &empty}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty name: got %v", err)
	}
	badAssign := "nope"
	if _, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{AssignedTo: &badAssign}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad assigned_to: got %v", err)
	}
	// transition 与字段混用 → ErrInvalidInput
	transition := TransitionStart
	if _, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Name: &newName, Transition: &transition}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("transition with fields: got %v", err)
	}
}

func TestDBTransitionStep(t *testing.T) {
	db := openAsmSvcDB(t)
	svc := asmSvc(db, allowAccess{})
	now := time.Now()
	svc.now = func() time.Time { return now }

	created, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "流转步骤", StepOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, created.ID) })

	// 非法流转：planned → complete 不在 AllowedTransitions
	complete := TransitionComplete
	if _, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &complete}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid transition: got %v, want ErrInvalidTransition", err)
	}
	// start：started_at 落库
	start := TransitionStart
	started, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &start})
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != StatusInProgress || started.StartedAt == nil || started.CompletedAt != nil {
		t.Fatalf("started: %+v", started)
	}
	// pause → resume → complete（completed_at 落库、started_at 保留）
	pause := TransitionPause
	paused, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &pause})
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != StatusPaused || paused.CompletedAt != nil {
		t.Fatalf("paused: %+v", paused)
	}
	resume := TransitionResume
	resumed, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &resume})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != StatusInProgress {
		t.Fatalf("resumed: %+v", resumed)
	}
	completed, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &complete})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusCompleted || completed.CompletedAt == nil || completed.StartedAt == nil {
		t.Fatalf("completed: %+v", completed)
	}
	// 已 completed 再 complete → 非法
	if _, err := svc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &complete}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("re-complete: got %v", err)
	}
	// 无权限
	denySvc := asmSvc(db, denyAccess{})
	if _, err := denySvc.Update(created.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &start}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("deny transition: got %v", err)
	}

	// skip 路径（新建一条）
	skipStep, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "跳过步骤", StepOrder: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, skipStep.ID) })
	// planned 不可直接 skip（AllowedTransitions[planned] = start/cancel）→ 先 start
	if _, err := svc.Update(skipStep.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &start}); err != nil {
		t.Fatal(err)
	}
	skip := TransitionSkip
	skipped, err := svc.Update(skipStep.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &skip})
	if err != nil {
		t.Fatal(err)
	}
	if skipped.Status != StatusSkipped || skipped.CompletedAt == nil {
		t.Fatalf("skipped: %+v", skipped)
	}
	// skipped → start 重启
	if _, err := svc.Update(skipStep.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &start}); err != nil {
		t.Fatalf("restart skipped: %v", err)
	}
	// cancel
	cancel := TransitionCancel
	cancelled, err := svc.Update(skipStep.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &cancel})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StatusCancelled {
		t.Fatalf("cancelled: %+v", cancelled)
	}

	// 并发冲突：fromStatus 与库内实际状态不符 → repo UpdateStatus 0 行 → ErrStepConflict
	conflictStep, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "冲突步骤", StepOrder: 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, conflictStep.ID) })
	repo := NewRepository(db)
	if err := repo.UpdateStatus(conflictStep.ID, "in_progress", StatusCompleted, &now, &now); !errors.Is(err, ErrStepConflict) {
		t.Fatalf("conflict: got %v, want ErrStepConflict", err)
	}
}

func TestDBTransitionDependency(t *testing.T) {
	db := openAsmSvcDB(t)
	svc := asmSvc(db, allowAccess{})

	dep, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "前置步骤", StepOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, dep.ID) })
	child, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "后续步骤", StepOrder: 2, DependsOn: &dep.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, child.ID) })

	start := TransitionStart
	// 依赖未完成 → ErrDependencyPending
	if _, err := svc.Update(child.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &start}); !errors.Is(err, ErrDependencyPending) {
		t.Fatalf("pending dependency: got %v, want ErrDependencyPending", err)
	}
	// 依赖 cancelled + override → 放行（override 生效）
	reason := "依赖已废弃"
	cancel := TransitionCancel
	if _, err := svc.Update(dep.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &cancel}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(child.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &start, OverrideReason: &reason}); err != nil {
		t.Fatalf("override cancelled dependency: %v", err)
	}
	// 依赖 completed → 直接放行
	dep2, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "前置2", StepOrder: 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, dep2.ID) })
	child2, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "后续2", StepOrder: 4, DependsOn: &dep2.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, child2.ID) })
	complete := TransitionComplete
	if _, err := svc.Update(dep2.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &start}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(dep2.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &complete}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(child2.ID, asmDBOwnerID, auth.RoleMember, UpdateStepRequest{Transition: &start}); err != nil {
		t.Fatalf("completed dependency: %v", err)
	}
}

func TestDBReorderAndSoftDelete(t *testing.T) {
	db := openAsmSvcDB(t)
	svc := asmSvc(db, allowAccess{})

	a, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "A", StepOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Create(asmDBProjectID, asmDBOwnerID, auth.RoleMember, CreateStepRequest{Name: "B", StepOrder: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM assembly_steps WHERE id IN ($1,$2)`, a.ID, b.ID)
	})

	// agent / 空列表 / 非法项
	if err := svc.Reorder(asmDBProjectID, asmDBOwnerID, auth.RoleAgent, []ReorderItem{{a.ID, 1}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent reorder: got %v", err)
	}
	if err := svc.Reorder(asmDBProjectID, asmDBOwnerID, auth.RoleMember, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty reorder: got %v", err)
	}
	if err := svc.Reorder(asmDBProjectID, asmDBOwnerID, auth.RoleMember, []ReorderItem{{"bad", 1}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad uuid: got %v", err)
	}
	if err := svc.Reorder(asmDBProjectID, asmDBOwnerID, auth.RoleMember, []ReorderItem{{a.ID, 0}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero order: got %v", err)
	}
	if err := svc.Reorder(asmDBProjectID, asmDBOwnerID, auth.RoleMember, []ReorderItem{{a.ID, 1}, {a.ID, 2}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("dup id: got %v", err)
	}
	if err := svc.Reorder(asmDBProjectID, asmDBOwnerID, auth.RoleMember, []ReorderItem{{a.ID, 1}, {b.ID, 1}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("dup order: got %v", err)
	}
	if err := svc.Reorder(asmDBProjectID, asmDBOwnerID, auth.RoleMember, []ReorderItem{{"c0000000-0000-4000-8000-00000000cbbb", 1}}); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("missing step: got %v", err)
	}
	// 无权限
	denySvc := asmSvc(db, denyAccess{})
	if err := denySvc.Reorder(asmDBProjectID, asmDBMemberID, auth.RoleMember, []ReorderItem{{a.ID, 1}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("deny reorder: got %v", err)
	}

	// 成功：A/B 互换顺序
	if err := svc.Reorder(asmDBProjectID, asmDBOwnerID, auth.RoleMember, []ReorderItem{{a.ID, 2}, {b.ID, 1}}); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(asmDBProjectID, asmDBOwnerID, auth.RoleMember, ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Items[0].ID != b.ID || list.Items[1].ID != a.ID {
		t.Fatalf("after reorder: %+v", list.Items)
	}

	// SoftDelete：创建者本人（member）可删；非创建者 member 需 maintainer
	if err := svc.SoftDelete(a.ID, asmDBOwnerID, auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	// 删除后 GetByID → ErrStepNotFound
	if _, err := svc.GetByID(a.ID, asmDBOwnerID, auth.RoleMember); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("deleted get: got %v", err)
	}
	// 重复删除 → ErrStepNotFound
	if err := svc.SoftDelete(a.ID, asmDBOwnerID, auth.RoleMember); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("double delete: got %v", err)
	}
	// 非创建者 maintainer 可删 / 非创建者 member 拒绝（fake allowAccess 恒放行——用真实 access 场景在 handler 测试覆盖；
	// 此处验证 agent 拒绝）
	if err := svc.SoftDelete(b.ID, asmDBOwnerID, auth.RoleAgent); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent delete: got %v", err)
	}
	if err := svc.SoftDelete("c0000000-0000-4000-8000-00000000cbbb", asmDBMaintainerID, auth.RoleMember); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("missing delete: got %v", err)
	}
}

func TestDBApplyTemplate(t *testing.T) {
	db := openAsmSvcDB(t)
	svc := asmSvc(db, allowAccess{})

	// agent / 模板与内联互斥 / 都缺
	if _, err := svc.ApplyTemplate(asmDBProjectID, asmDBOwnerID, auth.RoleAgent, ApplyTemplateRequest{Steps: []StepDef{{Name: "x", StepOrder: 1}}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent: got %v", err)
	}
	tmplID := asmDBTemplateID
	if _, err := svc.ApplyTemplate(asmDBProjectID, asmDBOwnerID, auth.RoleMember, ApplyTemplateRequest{TemplateID: &tmplID, Steps: []StepDef{{Name: "x", StepOrder: 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("both: got %v", err)
	}
	if _, err := svc.ApplyTemplate(asmDBProjectID, asmDBOwnerID, auth.RoleMember, ApplyTemplateRequest{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("neither: got %v", err)
	}
	// tmplReader 未配置
	if _, err := svc.ApplyTemplate(asmDBProjectID, asmDBOwnerID, auth.RoleMember, ApplyTemplateRequest{TemplateID: &tmplID}); err == nil {
		t.Fatal("unconfigured reader must error")
	}
	// 内联 >30 步骤
	tooMany := make([]StepDef, 31)
	for i := range tooMany {
		tooMany[i] = StepDef{Name: "s", StepOrder: i + 1}
	}
	if _, err := svc.ApplyTemplate(asmDBProjectID, asmDBOwnerID, auth.RoleMember, ApplyTemplateRequest{Steps: tooMany}); err == nil {
		t.Fatal(">30 steps must error")
	}

	// 内联成功：依赖按 DependsOnOrder 映射
	inline, err := svc.ApplyTemplate(asmDBProjectID, asmDBOwnerID, auth.RoleMember, ApplyTemplateRequest{Steps: []StepDef{
		{Name: "接线", StepOrder: 1},
		{Name: "密封", StepOrder: 2, DependsOnOrder: intPtr(1)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, s := range inline {
			db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, s.ID)
		}
	})
	if len(inline) != 2 || inline[0].Status != StatusPlanned || inline[0].StepOrder != 1 {
		t.Fatalf("inline steps: %+v", inline)
	}
	if inline[1].DependsOn == nil || *inline[1].DependsOn != inline[0].ID {
		t.Fatalf("depends_on_order mapping: %+v", inline[1])
	}

	// 模板成功：source_template_id 落库
	svc.ConfigureTemplates(fakeTemplateReader{
		tmpl: &SteptemplatesTemplate{ID: asmDBTemplateID, Name: "装配模板", Kind: "assembly"},
		items: []SteptemplatesItem{
			{ID: "t1", Name: "模板步骤1", StepOrder: 1},
			{ID: "t2", Name: "模板步骤2", StepOrder: 2, DependsOnOrder: intPtr(1)},
		},
	})
	fromTmpl, err := svc.ApplyTemplate(asmDBProjectID, asmDBOwnerID, auth.RoleMember, ApplyTemplateRequest{TemplateID: &tmplID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, s := range fromTmpl {
			db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, s.ID)
		}
	})
	if len(fromTmpl) != 2 || fromTmpl[1].DependsOn == nil || *fromTmpl[1].DependsOn != fromTmpl[0].ID {
		t.Fatalf("template steps: %+v", fromTmpl)
	}
	var sourceID string
	if err := db.QueryRow(`SELECT source_template_id FROM assembly_steps WHERE id = $1`, fromTmpl[0].ID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if sourceID != asmDBTemplateID {
		t.Fatalf("source_template_id = %q", sourceID)
	}
	// 模板不存在 / 类型不匹配
	svc.ConfigureTemplates(fakeTemplateReader{})
	if _, err := svc.ApplyTemplate(asmDBProjectID, asmDBOwnerID, auth.RoleMember, ApplyTemplateRequest{TemplateID: &tmplID}); err == nil {
		t.Fatal("missing template must error")
	}
	svc.ConfigureTemplates(fakeTemplateReader{tmpl: &SteptemplatesTemplate{ID: asmDBTemplateID, Name: "实验模板", Kind: "experiment"}})
	_, err = svc.ApplyTemplate(asmDBProjectID, asmDBOwnerID, auth.RoleMember, ApplyTemplateRequest{TemplateID: &tmplID})
	var commonErr *common.Error
	if !errors.As(err, &commonErr) || commonErr.Code != "bad_request" {
		t.Fatalf("wrong kind: got %v, want common.Error code=bad_request", err)
	}
}

func TestDBProjectAccessAdapterRank(t *testing.T) {
	db := openAsmSvcDB(t)
	repo := NewRepository(db)
	adapter := ProjectAccessAdapter{Repo: &projectsRepoBridge{r: repo}}
	// 用真实 projects 仓储的成员查询语义太重，直接验证 adapter 的 admin 放行与错误路径：
	// admin 角色 → true；未知角色 → false（member==nil）
	ok, err := adapter.CanAccessProject(asmDBProjectID, asmDBAdminID, auth.RoleAdmin, projects.RoleMember)
	if err != nil || !ok {
		t.Fatalf("admin access: %v %v", ok, err)
	}
	ok, err = adapter.CanAccessProject(asmDBProjectID, "00000000-0000-0000-0000-000000009999", auth.RoleMember, projects.RoleMember)
	if err != nil || ok {
		t.Fatalf("unknown user access: %v %v (want false)", ok, err)
	}
}

// projectsRepoBridge 仅供 adapter 测试占位（服务层测试用 allowAccess，不依赖它）。
type projectsRepoBridge struct{ r *Repository }

func (b *projectsRepoBridge) GetByID(id string) (*projects.Project, error) { return nil, nil }
func (b *projectsRepoBridge) GetMember(projectID, userID string) (*projects.ProjectMember, error) {
	return nil, nil
}

func intPtr(v int) *int { return &v }
