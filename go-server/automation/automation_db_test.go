package automation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// 032 规则表 CRUD（需要 TEST_DATABASE_URL，CI/本地按 scripts/test-go.sh 应用全量迁移 001-036）。
// 只增删本用例自建规则，不动种子规则（其他包的 agent 集成测试依赖种子规则入队）。
func TestAutomationRulesCRUDPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := NewService(NewRepository(db))
	ctx := context.Background()

	// 种子规则随 032 迁移存在（等价 014 硬编码行为）。
	items, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var seedFound bool
	for _, item := range items {
		if item.TriggerEvent == TriggerDailyReportSubmitted && item.Enabled {
			seedFound = true
		}
	}
	if !seedFound {
		t.Fatal("seed rule (日报提交→issue 候选) missing or disabled")
	}

	created, err := svc.Create(ctx, "", CreateRuleRequest{
		Name:         "db-test 规则",
		TriggerEvent: TriggerDailyReportSubmitted,
		Action:       json.RawMessage(`{"type":"enqueue_agent_task","mode":"parse_issues"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Delete(ctx, created.ID)
	if !created.Enabled || created.CreatedBy != nil {
		t.Fatalf("created = %#v", created)
	}

	// 一期仅允许切换 enabled。
	off, err := svc.SetEnabled(ctx, created.ID, UpdateRuleRequest{Enabled: boolPtr(false)})
	if err != nil || off.Enabled {
		t.Fatalf("disable = %#v, %v", off, err)
	}
	on, err := svc.SetEnabled(ctx, created.ID, UpdateRuleRequest{Enabled: boolPtr(true)})
	if err != nil || !on.Enabled || !on.UpdatedAt.After(off.UpdatedAt) {
		t.Fatalf("enable = %#v, %v", on, err)
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEnabled(ctx, created.ID, UpdateRuleRequest{Enabled: boolPtr(true)}); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("deleted rule err = %v, want ErrRuleNotFound", err)
	}
	if err := svc.Delete(ctx, created.ID); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("repeat delete err = %v, want ErrRuleNotFound", err)
	}
}

// 规则派发触发器语义（事务内验证后回滚，对其他并行测试不可见、不污染共享库）：
//  1. 无 enabled 规则 → 提交日报不入队（规则开关真实控制入队行为）；
//  2. 多条 enabled 规则 → 同一提交只入队一个任务（ON CONFLICT 在语句内折叠重复行）。
func TestRuleDispatchTriggerPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const userID = "00000000-0000-0000-0000-00000000d140"
	const reportID = "00000000-0000-0000-0000-00000000d141"

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO users (id, username, password_hash) VALUES ($1, 'automation-dispatch-user', 'unused')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO daily_reports (id, report_date, author_id) VALUES ($1, '2099-04-15', $2)`, reportID, userID); err != nil {
		t.Fatal(err)
	}

	countTasks := func() int {
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM pending_agent_tasks WHERE report_id = $1`, reportID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 1. 全部规则 disabled（含种子规则，仅本事务可见）→ 提交不入队。
	if _, err := tx.Exec(`UPDATE automation_rules SET enabled = false`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE daily_reports SET content_status = 'submitted' WHERE id = $1`, reportID); err != nil {
		t.Fatal(err)
	}
	if n := countTasks(); n != 0 {
		t.Fatalf("all rules disabled: tasks = %d, want 0", n)
	}

	// 2. 恢复种子规则 + 追加第二条 enabled 规则 → 重新提交只入队一个任务。
	if _, err := tx.Exec(`UPDATE automation_rules SET enabled = true`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO automation_rules (name, trigger_event, action)
		VALUES ('db-test 第二规则', 'daily_report.submitted', '{"type":"enqueue_agent_task"}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE daily_reports SET content_status = 'draft' WHERE id = $1`, reportID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE daily_reports SET content_status = 'submitted' WHERE id = $1`, reportID); err != nil {
		t.Fatal(err)
	}
	if n := countTasks(); n != 1 {
		t.Fatalf("two enabled rules: tasks = %d, want 1 (ON CONFLICT 折叠)", n)
	}
}

func boolPtr(v bool) *bool { return &v }
