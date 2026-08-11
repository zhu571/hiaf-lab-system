package steptemplates

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// repository 集成测试：需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移）。
// 固定 UUID 种子 + t.Cleanup 清理，CI 以 -p 1 串行跑。UUID 段 f0xx（避开其他包已用段）。

const (
	stpDBAdminID  = "00000000-0000-0000-0000-00000000f001"
	stpDBMemberID = "00000000-0000-0000-0000-00000000f003"
	stpDBProject  = "c0000000-0000-4000-8000-00000000f101"
)

// var 以便 &stpDBCreator 取地址传给 *string 字段。
var stpDBCreator = "00000000-0000-0000-0000-00000000f002"

func openStpTemplatesDB(t *testing.T) *sql.DB {
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
		{stpDBAdminID, "stp_dbtest_admin", "admin"},
		{stpDBCreator, "stp_dbtest_creator", "member"},
		{stpDBMemberID, "stp_dbtest_member", "member"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'STP DB Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'PRJ_STP_DBTEST', 'STP 种子项目', 'active', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, stpDBProject, stpDBCreator); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 VALUES ($1, $2, 'owner', 'active', $2)
		 ON CONFLICT (project_id, user_id) DO NOTHING`, stpDBProject, stpDBCreator); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM step_templates WHERE name LIKE 'STP-DBTEST-%'`)
		db.Exec(`DELETE FROM project_members WHERE project_id = $1`, stpDBProject)
		db.Exec(`DELETE FROM projects WHERE id = $1`, stpDBProject)
		// 先删 audit_log（handler 集成测试经 Audit 中间件落审计行，users 外键
		// NO ACTION，不先删则 DELETE users 必 FK 失败、种子用户泄漏进共享库）。
		db.Exec(`DELETE FROM audit_log WHERE user_id IN ($1,$2,$3)`, stpDBAdminID, stpDBCreator, stpDBMemberID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, stpDBAdminID, stpDBCreator, stpDBMemberID)
	})
	return db
}

func stpTestTemplate(name string) (*StepTemplate, []StepTemplateItem) {
	tmpl := &StepTemplate{
		Name:        name,
		Kind:        "assembly",
		Description: "描述",
		CreatedBy:   &stpDBCreator,
	}
	items := []StepTemplateItem{
		{Name: "步骤二", Description: "d2", StepOrder: 2, DependsOnOrder: intPtr(1), Meta: []byte(`{"pump":true}`)},
		{Name: "步骤一", Description: "d1", StepOrder: 1},
	}
	return tmpl, items
}

func TestRepoCreateAndGet(t *testing.T) {
	db := openStpTemplatesDB(t)
	repo := NewRepository(db)
	name := fmt.Sprintf("STP-DBTEST-%d", time.Now().UnixNano())

	tmpl, items := stpTestTemplate(name)
	created, err := repo.Create(tmpl, items)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("created: %+v", created)
	}
	if created.Kind != "assembly" || created.Description != "描述" {
		t.Fatalf("created: %+v", created)
	}
	// Create 后 items 回填排序
	if len(created.Items) != 2 || created.Items[0].Name != "步骤一" || created.Items[0].StepOrder != 1 {
		t.Fatalf("created items: %+v", created.Items)
	}
	if *created.Items[1].DependsOnOrder != 1 {
		t.Fatalf("depends_on_order: %+v", created.Items[1])
	}

	got, err := repo.GetByID(created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != name || len(got.Items) != 2 || got.Items[1].Meta == nil {
		t.Fatalf("GetByID: %+v", got)
	}

	// 不存在 → nil,nil
	missing, err := repo.GetByID("00000000-0000-0000-0000-00000000dead")
	if err != nil || missing != nil {
		t.Fatalf("missing: %v %v", missing, err)
	}
}

// TestRepoGetTemplateWithItems apply-template 适配器（assembly/runs 经 main.go 桥接只读复用）。
func TestRepoGetTemplateWithItems(t *testing.T) {
	db := openStpTemplatesDB(t)
	repo := NewRepository(db)
	name := fmt.Sprintf("STP-DBTEST-%d", time.Now().UnixNano())

	tmpl, items := stpTestTemplate(name)
	created, err := repo.Create(tmpl, items)
	if err != nil {
		t.Fatal(err)
	}

	tmplGot, itemsGot, err := repo.GetTemplateWithItems(created.ID)
	if err != nil {
		t.Fatalf("GetTemplateWithItems: %v", err)
	}
	if tmplGot.ID != created.ID || len(itemsGot) != 2 || itemsGot[1].TemplateID != created.ID {
		t.Fatalf("adapter result: %+v %+v", tmplGot, itemsGot)
	}

	tmplNil, itemsNil, err := repo.GetTemplateWithItems("00000000-0000-0000-0000-00000000dead")
	if err != nil || tmplNil != nil || itemsNil != nil {
		t.Fatalf("missing: %v %v %v", tmplNil, itemsNil, err)
	}
}

func TestRepoList(t *testing.T) {
	db := openStpTemplatesDB(t)
	repo := NewRepository(db)
	base := fmt.Sprintf("STP-DBTEST-%d", time.Now().UnixNano())

	// 3 个同 kind、2 个含关键字 "靶室"
	for i := 0; i < 3; i++ {
		tmpl := &StepTemplate{Name: fmt.Sprintf("%s-%d", base, i), Kind: "assembly", CreatedBy: &stpDBCreator}
		if i == 0 {
			tmpl.Description = "含靶室描述"
		}
		if _, err := repo.Create(tmpl, []StepTemplateItem{{Name: "s", StepOrder: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	experimentTmpl := &StepTemplate{Name: base + "-exp", Kind: "experiment", CreatedBy: &stpDBCreator}
	if _, err := repo.Create(experimentTmpl, []StepTemplateItem{{Name: "s", StepOrder: 1}}); err != nil {
		t.Fatal(err)
	}

	items, total, err := repo.List("", "", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(items) != 4 {
		t.Fatalf("list all: total=%d len=%d", total, len(items))
	}

	items, total, err = repo.List("assembly", "", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("list kind: total=%d len=%d", total, len(items))
	}

	items, total, err = repo.List("", "靶室", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].Description != "含靶室描述" {
		t.Fatalf("list query: total=%d items=%+v", total, items)
	}

	// 分页：第 2 页每页 2 条
	items, total, err = repo.List("assembly", "", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 1 {
		t.Fatalf("paged: total=%d len=%d", total, len(items))
	}
}

func TestRepoUpdate(t *testing.T) {
	db := openStpTemplatesDB(t)
	repo := NewRepository(db)
	name := fmt.Sprintf("STP-DBTEST-%d", time.Now().UnixNano())

	tmpl, items := stpTestTemplate(name)
	created, err := repo.Create(tmpl, items)
	if err != nil {
		t.Fatal(err)
	}

	newName := "STP-DBTEST-" + name + "-renamed"
	desc := "新描述"
	updated, err := repo.Update(created.ID, UpdateTemplateRequest{Name: &newName, Description: &desc})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != newName || updated.Description != desc {
		t.Fatalf("updated: %+v", updated)
	}

	// 只更新 name：description 保留
	only := "STP-DBTEST-" + name + "-only"
	updated, err = repo.Update(created.ID, UpdateTemplateRequest{Name: &only})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != only || updated.Description != desc {
		t.Fatalf("partial update: %+v", updated)
	}

	// 不存在 → nil
	u, err := repo.Update("00000000-0000-0000-0000-00000000dead", UpdateTemplateRequest{Name: &newName})
	if err != nil || u != nil {
		t.Fatalf("update missing: %v %v", u, err)
	}
}

func TestRepoReplaceItems(t *testing.T) {
	db := openStpTemplatesDB(t)
	repo := NewRepository(db)
	name := fmt.Sprintf("STP-DBTEST-%d", time.Now().UnixNano())

	tmpl, items := stpTestTemplate(name)
	created, err := repo.Create(tmpl, items)
	if err != nil {
		t.Fatal(err)
	}
	oldItemID := created.Items[0].ID

	newItems := []StepTemplateItem{
		{Name: "新步骤", Description: "nd", StepOrder: 1},
	}
	if err := repo.ReplaceItems(created.ID, newItems); err != nil {
		t.Fatalf("ReplaceItems: %v", err)
	}

	got, err := repo.GetByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "新步骤" || got.Items[0].ID == oldItemID {
		t.Fatalf("items after replace: %+v", got.Items)
	}
}

func TestRepoSoftDelete(t *testing.T) {
	db := openStpTemplatesDB(t)
	repo := NewRepository(db)
	name := fmt.Sprintf("STP-DBTEST-%d", time.Now().UnixNano())

	tmpl, items := stpTestTemplate(name)
	created, err := repo.Create(tmpl, items)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.SoftDelete(created.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	got, err := repo.GetByID(created.ID)
	if err != nil || got != nil {
		t.Fatalf("deleted template still visible: %v %v", got, err)
	}
	// 软删后 items 也随 deleted_at 过滤不可见
	tmplGot, itemsGot, err := repo.GetTemplateWithItems(created.ID)
	if err != nil || tmplGot != nil || len(itemsGot) != 0 {
		t.Fatalf("adapter after delete: %v %v %v", tmplGot, itemsGot, err)
	}
	// 重复删除 → 错误
	if err := repo.SoftDelete(created.ID); err == nil || !strings.Contains(err.Error(), "template not found") {
		t.Fatalf("double delete: %v", err)
	}
}

func TestItemMeta(t *testing.T) {
	if m := itemMeta(nil); len(m) != 0 {
		t.Fatalf("nil meta: %v", m)
	}
	if m := itemMeta([]byte(`{bad`)); len(m) != 0 {
		t.Fatalf("bad meta: %v", m)
	}
	m := itemMeta([]byte(`{"pump":true,"speed":3}`))
	if m["pump"] != true || m["speed"] != float64(3) {
		t.Fatalf("meta: %v", m)
	}
}

func TestHasAnyProjectRoleDB(t *testing.T) {
	db := openStpTemplatesDB(t)
	svc := NewService(NewRepository(db), db)

	exists, err := svc.HasAnyProjectRole(stpDBCreator)
	if err != nil || !exists {
		t.Fatalf("creator should have role: %v %v", exists, err)
	}
	exists, err = svc.HasAnyProjectRole(stpDBMemberID)
	if err != nil || exists {
		t.Fatalf("outsider should not have role: %v %v", exists, err)
	}
}
