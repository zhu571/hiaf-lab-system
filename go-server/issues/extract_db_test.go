package issues

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// ResolvedIssuesSince 经验提取取数 db 测试（AI-2）：resolved/closed + 评论 + run_id。
// 固定 UUID 段 c6xx（用户 + 项目/issue）。需要 TEST_DATABASE_URL（CI/本地按
// scripts/test-go.sh 应用全量迁移）。

const (
	exIssuesAuthorID  = "00000000-0000-0000-0000-00000000c601"
	exIssuesProjectID = "c6000000-0000-4000-8000-00000000c601"

	exIssueResolved    = "c6000000-0000-4000-8000-00000000c602"
	exIssueClosed      = "c6000000-0000-4000-8000-00000000c603"
	exIssueOpen        = "c6000000-0000-4000-8000-00000000c604"
	exIssueResolvedOld = "c6000000-0000-4000-8000-00000000c605"
	exRunID            = "c6000000-0000-4000-8000-00000000c606"
)

func openIssuesExtractTestDB(t *testing.T) *sql.DB {
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
		 VALUES ($1, 'ex_issues_user', 'x', '经验提取 Issue 测试', 'member', false, false)
		 ON CONFLICT (id) DO NOTHING`, exIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'PRJ_EX_ISSUES', '经验提取 Issue 测试项目', 'draft', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, exIssuesProjectID, exIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	// run 行（run_id FK 依赖，迁移 021）
	if _, err := db.Exec(
		`INSERT INTO experiment_runs (id, project_id, name, status, created_by)
		 VALUES ($1, $2, '匹配测试 run', 'completed', $3)
		 ON CONFLICT (id) DO NOTHING`, exRunID, exIssuesProjectID, exIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	// resolved + 窗内 + 两条评论 + run_id
	if _, err := db.Exec(
		`INSERT INTO issues (id, project_id, title, description, status, severity, author_id, report_date, occurred_at, resolved_at, run_id)
		 VALUES ($1, $2, '匹配效率偏低', '负载线圈间隙过大导致效率 78%', 'resolved', 'high', $3, DATE '2026-09-14',
		         TIMESTAMPTZ '2026-09-14 09:00:00+08', TIMESTAMPTZ '2026-09-15 11:00:00+08', $4)
		 ON CONFLICT (id) DO NOTHING`, exIssueResolved, exIssuesProjectID, exIssuesAuthorID, exRunID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO issue_comments (issue_id, author_id, content, created_at)
		 VALUES ($1, $2, '已调整间隙，效率恢复 92%', TIMESTAMPTZ '2026-09-15 10:00:00+08'),
		        ($1, $2, '结论：间隙需控制在 0.5mm 内', TIMESTAMPTZ '2026-09-15 10:30:00+08')
		 ON CONFLICT DO NOTHING`, exIssueResolved, exIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	// closed + 窗内（resolved_at 缺失，按 updated_at 近似——显式置窗口内时间）
	if _, err := db.Exec(
		`INSERT INTO issues (id, project_id, title, status, severity, author_id, report_date, occurred_at, updated_at)
		 VALUES ($1, $2, '真空泄漏排查', 'closed', 'medium', $3, DATE '2026-09-13',
		         TIMESTAMPTZ '2026-09-13 14:00:00+08', TIMESTAMPTZ '2026-09-14 09:00:00+08')
		 ON CONFLICT (id) DO NOTHING`, exIssueClosed, exIssuesProjectID, exIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	// open：不返回
	if _, err := db.Exec(
		`INSERT INTO issues (id, project_id, title, status, severity, author_id, report_date, occurred_at)
		 VALUES ($1, $2, '未解决', 'open', 'low', $3, DATE '2026-09-15',
		         TIMESTAMPTZ '2026-09-15 09:00:00+08')
		 ON CONFLICT (id) DO NOTHING`, exIssueOpen, exIssuesProjectID, exIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	// resolved 但窗外（旧）：不返回
	if _, err := db.Exec(
		`INSERT INTO issues (id, project_id, title, status, severity, author_id, report_date, occurred_at, resolved_at)
		 VALUES ($1, $2, '旧解决', 'resolved', 'low', $3, DATE '2026-07-01',
		         TIMESTAMPTZ '2026-07-01 10:00:00+08', TIMESTAMPTZ '2026-07-02 12:00:00+08')
		 ON CONFLICT (id) DO NOTHING`, exIssueResolvedOld, exIssuesProjectID, exIssuesAuthorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_log_links WHERE issue_id IN ($1,$2,$3,$4)`,
			exIssueResolved, exIssueClosed, exIssueOpen, exIssueResolvedOld)
		db.Exec(`DELETE FROM issue_comments WHERE issue_id IN ($1,$2,$3,$4)`,
			exIssueResolved, exIssueClosed, exIssueOpen, exIssueResolvedOld)
		db.Exec(`DELETE FROM issues WHERE id IN ($1,$2,$3,$4)`,
			exIssueResolved, exIssueClosed, exIssueOpen, exIssueResolvedOld)
		db.Exec(`DELETE FROM experiment_runs WHERE id = $1`, exRunID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, exIssuesProjectID)
		db.Exec(`DELETE FROM users WHERE id = $1`, exIssuesAuthorID)
	})
	return db
}

func TestDBResolvedIssuesSince(t *testing.T) {
	db := openIssuesExtractTestDB(t)
	since := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

	items, err := NewRepository(db).ResolvedIssuesSince(context.Background(), since, 10)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ResolvedIssue{}
	for _, item := range items {
		byID[item.ID] = item
	}
	resolved, ok := byID[exIssueResolved]
	if !ok {
		t.Fatalf("resolved issue missing from result: %+v", byID)
	}
	if resolved.ProjectID != exIssuesProjectID || resolved.Title != "匹配效率偏低" {
		t.Fatalf("resolved issue fields: %+v", resolved)
	}
	if len(resolved.Comments) != 2 || resolved.Comments[0] != "已调整间隙，效率恢复 92%" {
		t.Fatalf("resolved comments: %+v", resolved.Comments)
	}
	if resolved.RunID == nil || *resolved.RunID != exRunID {
		t.Fatalf("resolved run_id: %+v", resolved.RunID)
	}
	closed, ok := byID[exIssueClosed]
	if !ok {
		t.Fatalf("closed issue missing from result: %+v", byID)
	}
	if closed.RunID != nil {
		t.Fatalf("closed run_id should be nil: %+v", closed.RunID)
	}
	if _, ok := byID[exIssueOpen]; ok {
		t.Fatal("open issue must not be returned")
	}
	if _, ok := byID[exIssueResolvedOld]; ok {
		t.Fatal("old resolved issue must not be returned")
	}

	// limit 生效
	capped, err := NewRepository(db).ResolvedIssuesSince(context.Background(), since, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != 1 {
		t.Fatalf("capped = %d, want 1", len(capped))
	}

	// 空窗口（晚于全部种子数据）→ 空列表、无错误
	empty, err := NewRepository(db).ResolvedIssuesSince(context.Background(),
		time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty window = %d, want 0", len(empty))
	}
}
