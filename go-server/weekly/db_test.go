package weekly

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/experiences"
	"github.com/zhu571/hiaf-lab-system/go-server/issues"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
)

// 集成测试（db_test 模式，TEST_DATABASE_URL 门控）：真实 logs/issues/experiences
// 仓储 + fake LLM + fake notifier，验证周报全链路——取数 → LLM → 落库 experiences →
// 每周幂等复用。固定 UUID 段 e3xx（用户）。

const (
	wkAuthorID  = "00000000-0000-0000-0000-00000000e301"
	wkReportID  = "e0000000-0000-4000-8000-00000000e301"
	wkProjectID = "e0000000-0000-4000-8000-00000000e302"
	wkIssueID   = "e0000000-0000-4000-8000-00000000e303"
	wkIssueID2  = "e0000000-0000-4000-8000-00000000e304"
)

func openWeeklyTestDB(t *testing.T) *sql.DB {
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
		 VALUES ($1, 'weekly_dbtest_user', 'x', 'Weekly DB Test', 'maintainer', false, false)
		 ON CONFLICT (id) DO NOTHING`, wkAuthorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'PRJ_WEEKLY_DBTEST', '周报集成测试项目', 'draft', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, wkProjectID, wkAuthorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO daily_reports (id, report_date, author_id, raw_text, summary, content_status)
		 VALUES ($1, DATE '2026-08-04', $2, '今天完成了匹配电路装配', '装配匹配电路', 'confirmed')
		 ON CONFLICT (id) DO NOTHING`, wkReportID, wkAuthorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO issues (id, project_id, title, status, severity, author_id, report_date, created_at)
		 VALUES ($1, $2, 'RF 匹配漂移', 'in_progress', 'high', $3, DATE '2026-08-05', TIMESTAMPTZ '2026-08-05 10:00:00+08')
		 ON CONFLICT (id) DO NOTHING`, wkIssueID, wkProjectID, wkAuthorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO issues (id, project_id, title, status, severity, author_id, report_date, created_at, resolved_at)
		 VALUES ($1, $2, '历史已解决问题', 'resolved', 'low', $3, DATE '2026-07-01',
		         TIMESTAMPTZ '2026-07-01 10:00:00+08', TIMESTAMPTZ '2026-07-03 15:00:00+08')
		 ON CONFLICT (id) DO NOTHING`, wkIssueID2, wkProjectID, wkAuthorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM experiences WHERE author_id = $1 OR title LIKE '周报 %'`, wkAuthorID)
		db.Exec(`DELETE FROM issues WHERE id IN ($1,$2)`, wkIssueID, wkIssueID2)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, wkReportID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, wkProjectID)
		db.Exec(`DELETE FROM users WHERE id = $1`, wkAuthorID)
	})
	return db
}

// 本地窄接口适配（main_bridges 属 package main，测试包内自建等价适配）。
type dbReportReader struct{ repo *logs.Repository }

func (d dbReportReader) WeeklyReports(ctx context.Context, from, to string) ([]ReportEntry, error) {
	entries, err := d.repo.WeeklyReports(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]ReportEntry, len(entries))
	for i, e := range entries {
		out[i] = ReportEntry{ReportDate: e.ReportDate, AuthorName: e.AuthorName, RawText: e.RawText, Summary: e.Summary}
	}
	return out, nil
}

type dbIssueStatsReader struct{ repo *issues.Repository }

func (d dbIssueStatsReader) WeeklyIssueStats(ctx context.Context, from, to string) (IssueStats, error) {
	stats, err := d.repo.WeeklyIssueStats(ctx, from, to)
	if err != nil {
		return IssueStats{}, err
	}
	return IssueStats{Created: stats.Created, Resolved: stats.Resolved, OpenHighCritical: stats.OpenHighCritical}, nil
}

type dbExperienceStore struct{ repo *experiences.Repository }

func (d dbExperienceStore) FindWeeklySummary(title string) (*SavedSummary, error) {
	exp, err := d.repo.FindWeeklySummary(title)
	if err != nil || exp == nil {
		return nil, err
	}
	return &SavedSummary{ID: exp.ID, Title: exp.Title, Markdown: exp.Content, CreatedAt: exp.CreatedAt}, nil
}

func (d dbExperienceStore) SaveWeeklySummary(authorID, title, content string) (*SavedSummary, error) {
	exp, err := d.repo.CreateWeeklySummary(authorID, title, content)
	if err != nil {
		return nil, err
	}
	return &SavedSummary{ID: exp.ID, Title: exp.Title, Markdown: exp.Content, CreatedAt: exp.CreatedAt}, nil
}

// fakeDBLLM 返回固定周报（模拟 py-agent 两步 LLM 输出）。
type fakeDBLLM struct{ calls int }

func (f *fakeDBLLM) Summarize(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	f.calls++
	return &LLMResponse{
		Status: "ok", Title: "周报 2026-08-03 ~ 2026-08-09",
		Summary: "本周完成匹配电路装配。", Markdown: "## 本周进展\n完成匹配电路装配。\n\n## 问题与风险\nRF 匹配漂移。",
		Highlights: []string{"完成匹配电路装配"}, Problems: []string{"RF 匹配漂移"},
	}, nil
}

type dbNotifier struct{ calls int }

func (d *dbNotifier) Send(topic, title, msg, clickURL, priority string, tags []string) error {
	d.calls++
	return nil
}

func newWeeklyDBService(t *testing.T) (*Service, *sql.DB, *fakeDBLLM, *dbNotifier) {
	t.Helper()
	db := openWeeklyTestDB(t)
	llm := &fakeDBLLM{}
	notifier := &dbNotifier{}
	svc := NewService(
		dbReportReader{repo: logs.NewRepository(db)},
		dbIssueStatsReader{repo: issues.NewRepository(db)},
		dbExperienceStore{repo: experiences.NewRepository(db)},
		llm, notifier, testLoc, testNow,
	)
	return svc, db, llm, notifier
}

func TestDBWeeklyGenerateEndToEnd(t *testing.T) {
	svc, db, llm, notifier := newWeeklyDBService(t)

	result, err := svc.Generate(context.Background(), wkAuthorID, "", true)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Reused || result.ID == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if llm.calls != 1 || notifier.calls != 1 {
		t.Fatalf("llm=%d notify=%d, want 1/1", llm.calls, notifier.calls)
	}

	// 落库断言：published + global + tags 含 weekly_summary + 正文一致
	var status, content string
	var projectID sql.NullString
	var aiGenerated bool
	var tagsJSON []byte
	if err := db.QueryRow(
		`SELECT status, project_id, content, ai_generated, tags_json FROM experiences WHERE id = $1`, result.ID,
	).Scan(&status, &projectID, &content, &aiGenerated, &tagsJSON); err != nil {
		t.Fatalf("query experience: %v", err)
	}
	if status != "published" || projectID.Valid || !aiGenerated {
		t.Fatalf("experience row: status=%q project=%v ai=%v", status, projectID.Valid, aiGenerated)
	}
	if content != "## 本周进展\n完成匹配电路装配。\n\n## 问题与风险\nRF 匹配漂移。" {
		t.Fatalf("content mismatch: %q", content)
	}
	if !bytesContains(tagsJSON, []byte("weekly_summary")) {
		t.Fatalf("tags missing weekly_summary: %s", tagsJSON)
	}

	// 每周幂等：再次生成同一周 → 复用，不重复调 LLM
	before := llm.calls
	again, err := svc.Generate(context.Background(), wkAuthorID, "", true)
	if err != nil {
		t.Fatalf("Generate again: %v", err)
	}
	if !again.Reused || again.ID != result.ID {
		t.Fatalf("expected reuse, got %+v", again)
	}
	if llm.calls != before {
		t.Fatal("reused week must not call llm")
	}
}

func TestDBWeeklyIssueStats(t *testing.T) {
	db := openWeeklyTestDB(t)
	stats, err := issues.NewRepository(db).WeeklyIssueStats(context.Background(), "2026-08-03", "2026-08-09")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Created != 1 { // 2026-08-05 创建 1 条（created_at 按周过滤，测试库共享也不受影响）
		t.Fatalf("created = %d, want 1", stats.Created)
	}
	// open_high_critical 是全局当前态计数，测试库共享会叠加其他用例数据 → 只做下限断言
	if stats.OpenHighCritical < 1 { // 至少包含本用例 in_progress + high 的 RF 漂移
		t.Fatalf("open_high_critical = %d, want >= 1", stats.OpenHighCritical)
	}
}

func bytesContains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}
