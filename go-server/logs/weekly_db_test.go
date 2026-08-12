package logs

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

// WeeklyReports 周报取数 db 测试（AI-1）：日期范围过滤 + 作者展示名 + 升序。
// 固定 UUID 段 c4xx（用户）+ c4000000-…c4xx（日报）。需要 TEST_DATABASE_URL
// （CI/本地按 scripts/test-go.sh 应用全量迁移）。

const (
	wkLogsUserA = "00000000-0000-0000-0000-00000000c401"
	wkLogsUserB = "00000000-0000-0000-0000-00000000c402"

	wkLogsReport1 = "c4000000-0000-4000-8000-00000000c401"
	wkLogsReport2 = "c4000000-0000-4000-8000-00000000c402"
	wkLogsReport3 = "c4000000-0000-4000-8000-00000000c403"
)

func openLogsWeeklyTestDB(t *testing.T) *sql.DB {
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
		 VALUES ($1, 'wk_logs_a', 'x', '周报测试员甲', 'member', false, false),
		        ($2, 'wk_logs_b', 'x', '周报测试员乙', 'member', false, false)
		 ON CONFLICT (id) DO NOTHING`, wkLogsUserA, wkLogsUserB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id IN ($1,$2,$3)`, wkLogsReport1, wkLogsReport2, wkLogsReport3)
		db.Exec(`DELETE FROM daily_reports WHERE id IN ($1,$2,$3)`, wkLogsReport1, wkLogsReport2, wkLogsReport3)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, wkLogsUserA, wkLogsUserB)
	})
	return db
}

func seedWeeklyReport(t *testing.T, db *sql.DB, id, authorID, date, rawText, summary string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO daily_reports (id, report_date, author_id, raw_text, summary, content_status)
		 VALUES ($1, $2::date, $3, $4, $5, 'confirmed')
		 ON CONFLICT (id) DO UPDATE SET raw_text = EXCLUDED.raw_text, summary = EXCLUDED.summary,
		                                report_date = EXCLUDED.report_date, content_status = EXCLUDED.content_status`,
		id, date, authorID, rawText, summary); err != nil {
		t.Fatal(err)
	}
}

func TestDBWeeklyReports(t *testing.T) {
	db := openLogsWeeklyTestDB(t)
	// 窗口 2026-09-07 ~ 2026-09-13：窗内 2 条（不同作者）+ 窗外 1 条（2026-08-01）
	seedWeeklyReport(t, db, wkLogsReport1, wkLogsUserA, "2026-09-08", "完成匹配电路装配", "装配匹配电路")
	seedWeeklyReport(t, db, wkLogsReport2, wkLogsUserB, "2026-09-09", "低温靶调试", "靶体调试")
	seedWeeklyReport(t, db, wkLogsReport3, wkLogsUserA, "2026-08-01", "上月记录", "旧摘要")

	entries, err := NewRepository(db).WeeklyReports(context.Background(), "2026-09-07", "2026-09-13")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2: %+v", len(entries), entries)
	}
	if entries[0].ReportDate != "2026-09-08" || entries[0].AuthorName != "周报测试员甲" ||
		entries[0].RawText != "完成匹配电路装配" || entries[0].Summary != "装配匹配电路" {
		t.Fatalf("first entry: %+v", entries[0])
	}
	if entries[1].ReportDate != "2026-09-09" || entries[1].AuthorName != "周报测试员乙" {
		t.Fatalf("second entry: %+v", entries[1])
	}
	// 升序保证（周报优先收尾事实，丢最旧保最新）
	if entries[0].ReportDate >= entries[1].ReportDate {
		t.Fatalf("entries not ascending: %+v", entries)
	}

	// 空窗口 → 空切片、无错误
	empty, err := NewRepository(db).WeeklyReports(context.Background(), "2026-09-14", "2026-09-20")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty window entries = %d, want 0", len(empty))
	}
}
