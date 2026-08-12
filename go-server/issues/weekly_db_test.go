package issues

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

// WeeklyIssueStats 周报取数 db 测试（AI-1）：created/resolved 按周窗口计数 +
// 全局未解决 high/critical 当前态。固定 UUID 段 c5xx（用户 + 项目/issue）。
// 需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移）。
// 注：测试库共享，OpenHighCritical 为全局当前态 → 只做下限断言；created/resolved
// 用独立周窗口（2026-09-07 ~ 2026-09-13）避开 weekly/db_test 的 08-03 窗口。

const (
	wkIssuesAuthorID  = "00000000-0000-0000-0000-00000000c501"
	wkIssuesProjectID = "c5000000-0000-4000-8000-00000000c501"

	wkIssueInWeek      = "c5000000-0000-4000-8000-00000000c502"
	wkIssueResolved    = "c5000000-0000-4000-8000-00000000c503"
	wkIssueResolvedOld = "c5000000-0000-4000-8000-00000000c504"
)

func openIssuesWeeklyTestDB(t *testing.T) *sql.DB {
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
	if _, err := db.Exec(
		`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
		 VALUES ($1, 'wk_issues_user', 'x', '周报 Issue 测试', 'member', false, false)
		 ON CONFLICT (id) DO NOTHING`, wkIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'PRJ_WK_ISSUES', '周报 Issue 测试项目', 'draft', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, wkIssuesProjectID, wkIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	// issue1：窗内创建（2026-09-08）+ 未解决 + high → created + open_high_critical 均命中
	if _, err := db.Exec(
		`INSERT INTO issues (id, project_id, title, status, severity, author_id, report_date, created_at)
		 VALUES ($1, $2, 'RF 匹配漂移', 'in_progress', 'high', $3, DATE '2026-09-08', TIMESTAMPTZ '2026-09-08 10:00:00+08')
		 ON CONFLICT (id) DO NOTHING`, wkIssueInWeek, wkIssuesProjectID, wkIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	// issue2：窗外创建（2026-08-20）、窗内解决（2026-09-09）→ resolved 命中
	if _, err := db.Exec(
		`INSERT INTO issues (id, project_id, title, status, severity, author_id, report_date, created_at, resolved_at)
		 VALUES ($1, $2, '窗内解决', 'resolved', 'low', $3, DATE '2026-08-20',
		         TIMESTAMPTZ '2026-08-20 10:00:00+08', TIMESTAMPTZ '2026-09-09 15:00:00+08')
		 ON CONFLICT (id) DO NOTHING`, wkIssueResolved, wkIssuesProjectID, wkIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	// issue3：窗外解决（2026-08-01）→ 不计数
	if _, err := db.Exec(
		`INSERT INTO issues (id, project_id, title, status, severity, author_id, report_date, created_at, resolved_at)
		 VALUES ($1, $2, '旧解决', 'resolved', 'low', $3, DATE '2026-07-01',
		         TIMESTAMPTZ '2026-07-01 10:00:00+08', TIMESTAMPTZ '2026-08-01 12:00:00+08')
		 ON CONFLICT (id) DO NOTHING`, wkIssueResolvedOld, wkIssuesProjectID, wkIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_log_links WHERE issue_id IN ($1,$2,$3)`, wkIssueInWeek, wkIssueResolved, wkIssueResolvedOld)
		db.Exec(`DELETE FROM issue_comments WHERE issue_id IN ($1,$2,$3)`, wkIssueInWeek, wkIssueResolved, wkIssueResolvedOld)
		db.Exec(`DELETE FROM issues WHERE id IN ($1,$2,$3)`, wkIssueInWeek, wkIssueResolved, wkIssueResolvedOld)
		db.Exec(`DELETE FROM projects WHERE id = $1`, wkIssuesProjectID)
		db.Exec(`DELETE FROM users WHERE id = $1`, wkIssuesAuthorID)
	})
	return db
}

func TestDBWeeklyIssueStats(t *testing.T) {
	db := openIssuesWeeklyTestDB(t)

	stats, err := NewRepository(db).WeeklyIssueStats(context.Background(), "2026-09-07", "2026-09-13")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Created != 1 { // 仅 issue1 窗内创建
		t.Fatalf("created = %d, want 1", stats.Created)
	}
	if stats.Resolved != 1 { // 仅 issue2 窗内解决（issue3 窗外不算）
		t.Fatalf("resolved = %d, want 1", stats.Resolved)
	}
	if stats.OpenHighCritical < 1 { // 至少包含本用例 in_progress + high 的 issue1
		t.Fatalf("open_high_critical = %d, want >= 1", stats.OpenHighCritical)
	}

	// 空窗口 → 全 0、无错误
	empty, err := NewRepository(db).WeeklyIssueStats(context.Background(), "2026-10-05", "2026-10-11")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Created != 0 || empty.Resolved != 0 {
		t.Fatalf("empty window stats: %+v", empty)
	}
}
