package agent

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// 031 C10 触发器加固回归（需要 TEST_DATABASE_URL，CI/本地按 scripts/test-go.sh 应用全量迁移 001-036）：
//  1. 在途去重：任务 pending 时重复 submit → UPDATE 成功（不再炸唯一约束）且仍只有一个任务；
//  2. 退回再提交：历史任务 done 后重新 submit → 追加新任务行（历史保留）。
//
// 仓库当前无 submitted→draft 回退路径，测试用裸 SQL 模拟未来回退路径以锁定触发器语义。
func TestSubmitTriggerResubmitPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const userID = "00000000-0000-0000-0000-00000000b140"
	const reportID = "00000000-0000-0000-0000-00000000b141"
	cleanup := func() {
		db.Exec(`DELETE FROM agent_candidate_actions WHERE task_id IN (SELECT id FROM pending_agent_tasks WHERE report_id = $1)`, reportID)
		db.Exec(`DELETE FROM pending_agent_tasks WHERE report_id = $1`, reportID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, reportID)
		db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}
	defer cleanup()
	cleanup()

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES ($1, 'agent-resubmit-user', 'unused')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO daily_reports (id, report_date, author_id) VALUES ($1, '2099-02-15', $2)`, reportID, userID); err != nil {
		t.Fatal(err)
	}

	countTasks := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pending_agent_tasks WHERE report_id = $1`, reportID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	resubmit := func() {
		t.Helper()
		// 模拟"退回再提交"：draft → submitted 的状态翻转触发 enqueue。
		if _, err := db.Exec(`UPDATE daily_reports SET content_status = 'draft' WHERE id = $1`, reportID); err != nil {
			t.Fatalf("backout to draft: %v", err)
		}
		if _, err := db.Exec(`UPDATE daily_reports SET content_status = 'submitted' WHERE id = $1`, reportID); err != nil {
			t.Fatalf("resubmit must succeed (031 前此处炸 unique_report_task): %v", err)
		}
	}

	// 首次提交：触发器入队一个任务。
	if _, err := db.Exec(`UPDATE daily_reports SET content_status = 'submitted' WHERE id = $1`, reportID); err != nil {
		t.Fatal(err)
	}
	if n := countTasks(); n != 1 {
		t.Fatalf("after first submit: tasks = %d, want 1", n)
	}

	// 在途去重：任务仍 pending 时重复提交 → UPDATE 静默成功，不追加任务。
	resubmit()
	if n := countTasks(); n != 1 {
		t.Fatalf("in-flight resubmit: tasks = %d, want 1 (ON CONFLICT 静默跳过)", n)
	}

	// 退回再提交：在途任务完结（done）后重新提交 → 追加新任务行，历史保留。
	if _, err := db.Exec(`UPDATE pending_agent_tasks SET status = 'done', completed_at = now() WHERE report_id = $1`, reportID); err != nil {
		t.Fatal(err)
	}
	resubmit()
	if n := countTasks(); n != 2 {
		t.Fatalf("resubmit after done: tasks = %d, want 2 (历史任务保留 + 新任务行)", n)
	}
	var inflight int
	if err := db.QueryRow(
		`SELECT count(*) FROM pending_agent_tasks WHERE report_id = $1 AND status IN ('pending','processing')`, reportID,
	).Scan(&inflight); err != nil || inflight != 1 {
		t.Fatalf("in-flight tasks = %d, want 1, err = %v", inflight, err)
	}
}
