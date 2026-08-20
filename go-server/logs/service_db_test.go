package logs

import (
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

// service 集成测试：需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移）。
// logs.Service 的 repo 是具体 *Repository 但 access 是接口，故 service 层用真实 repo +
// 细粒度 fake access（按 perm 返回），覆盖日报 CRUD、提交硬阻塞/警告/force、日志 CRUD、
// 软删除链路（daily_report_log_links）。固定 UUID 种子 + t.Cleanup 清理，CI -p 1 串行。

const (
	logsDBUserA    = "00000000-0000-0000-0000-00000000b901"
	logsDBUserB    = "00000000-0000-0000-0000-00000000b902"
	logsDBAdmin    = "00000000-0000-0000-0000-00000000b903"
	logsDBProject  = "c0000000-0000-4000-8000-00000000c901"
	logsDBDraftPrj = "c0000000-0000-4000-8000-00000000c902"
	logsDBReportA  = "c0000000-0000-4000-8000-00000000c903"
	logsDBReportB  = "c0000000-0000-4000-8000-00000000c904"
	logsDBLogA     = "c0000000-0000-4000-8000-00000000c905"
	logsDBLogB     = "c0000000-0000-4000-8000-00000000c906"
)

func openLogsSvcDB(t *testing.T) *sql.DB {
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
		{logsDBUserA, "logs_db_usera", "member"},
		{logsDBUserB, "logs_db_userb", "member"},
		{logsDBAdmin, "logs_db_admin", "admin"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Logs DB Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []struct {
		id     string
		code   string
		status string
	}{
		{logsDBProject, "LOGS_DBTEST_ACTIVE", projects.StatusActive},
		{logsDBDraftPrj, "LOGS_DBTEST_DRAFT", projects.StatusDraft},
	} {
		if _, err := db.Exec(
			`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
			 VALUES ($1, $2, 'DB 种子项目', $3, $4, $4)
			 ON CONFLICT (id) DO NOTHING`, p.id, p.code, p.status, logsDBUserA); err != nil {
			t.Fatal(err)
		}
	}
	// 日报：A 草稿（raw_text 含日志内容）、B 已提交
	for _, r := range []struct {
		id      string
		date    string
		author  string
		raw     string
		summary string
		status  string
		quality string
	}{
		{logsDBReportA, "2099-05-01", logsDBUserA, "今天完成了装配与测试", "测试摘要", "draft", QualityUnchecked},
		{logsDBReportB, "2099-05-01", logsDBUserB, "B 的日报", "B 摘要", "submitted", QualityPassed},
	} {
		if _, err := db.Exec(
			`INSERT INTO daily_reports (id, report_date, author_id, raw_text, summary, content_status, quality_status)
			 VALUES ($1, $2::date, $3, $4, $5, $6, $7)
			 ON CONFLICT (id) DO NOTHING`, r.id, r.date, r.author, r.raw, r.summary, r.status, r.quality); err != nil {
			t.Fatal(err)
		}
	}
	// 日志：A 草稿（日期=日报日期、内容在日报 raw 中）、B 已确认（另一日期）
	for _, l := range []struct {
		id      string
		project string
		author  string
		date    string
		content string
		status  string
	}{
		{logsDBLogA, logsDBProject, logsDBUserA, "2099-05-01T10:00:00+08:00", "完成了装配与测试", LogStatusDraft},
		{logsDBLogB, logsDBProject, logsDBUserB, "2099-05-02T10:00:00+08:00", "确认的工作记录", LogStatusConfirmed},
	} {
		if _, err := db.Exec(
			`INSERT INTO logs (id, project_id, author_id, occurred_at, category, content, source, content_status)
			 VALUES ($1, $2, $3, $4::timestamptz, 'assembly', $5, 'manual', $6)
			 ON CONFLICT (id) DO NOTHING`, l.id, l.project, l.author, l.date, l.content, l.status); err != nil {
			t.Fatal(err)
		}
	}
	// 链接：A 报告 ← 日志 A
	if _, err := db.Exec(
		`INSERT INTO daily_report_log_links (daily_report_id, log_id)
		 VALUES ($1, $2) ON CONFLICT DO NOTHING`, logsDBReportA, logsDBLogA); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id IN ($1,$2)`, logsDBReportA, logsDBReportB)
		db.Exec(`DELETE FROM daily_reports WHERE id IN ($1,$2)`, logsDBReportA, logsDBReportB)
		db.Exec(`DELETE FROM logs WHERE id IN ($1,$2)`, logsDBLogA, logsDBLogB)
		db.Exec(`DELETE FROM project_members WHERE project_id IN ($1,$2)`, logsDBProject, logsDBDraftPrj)
		db.Exec(`DELETE FROM projects WHERE id IN ($1,$2)`, logsDBProject, logsDBDraftPrj)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, logsDBUserA, logsDBUserB, logsDBAdmin)
	})
	return db
}

// logsAccess 细粒度 fake：按权限返回不同结果，模拟 member/viewer 差异。
type logsAccess struct {
	can bool
	// perPerm 覆盖：key 为权限名
	perPerm map[middleware.Permission]bool
	exists  bool
	status  string
}

func (f logsAccess) ProjectExists(projectID string) (bool, error) {
	if f.exists {
		return true, nil
	}
	return f.status != "" || f.can, nil
}

func (f logsAccess) ProjectStatus(projectID string) (string, error) {
	if f.status != "" {
		return f.status, nil
	}
	return projects.StatusActive, nil
}

func (f logsAccess) HasProjectPermission(projectID, userID string, perm middleware.Permission) (bool, error) {
	if v, ok := f.perPerm[perm]; ok {
		return v, nil
	}
	return f.can, nil
}

func (f logsAccess) ListProjectsWithPermission(userID string, perm middleware.Permission) ([]middleware.ProjectSummary, error) {
	return []middleware.ProjectSummary{{ID: logsDBProject, Name: "DB 项目"}}, nil
}

func logsSvc(db *sql.DB, access ProjectAccessChecker) *Service {
	return NewService(NewRepository(db), "Asia/Shanghai", access)
}

func TestDBGetOrCreateTodayReport(t *testing.T) {
	db := openLogsSvcDB(t)
	svc := logsSvc(db, logsAccess{can: true})

	// 首次创建：draft、无 raw_text
	report, err := svc.GetOrCreateTodayReport(logsDBUserA)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id = $1`, report.ID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, report.ID)
	})
	if report.ContentStatus != ReportStatusDraft || report.AuthorID != logsDBUserA {
		t.Fatalf("today report: %+v", report)
	}
	// 幂等：再次获取同一份
	again, err := svc.GetOrCreateTodayReport(logsDBUserA)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != report.ID {
		t.Fatalf("today report not idempotent: %s vs %s", again.ID, report.ID)
	}
	// 非法时区 → ErrInvalidTimeZone
	badSvc := NewService(NewRepository(db), "Not/AZone", logsAccess{can: true})
	if _, err := badSvc.GetOrCreateTodayReport(logsDBUserA); !errors.Is(err, ErrInvalidTimeZone) {
		t.Fatalf("bad timezone: got %v, want ErrInvalidTimeZone", err)
	}
}

func TestDBGetReportByDateLatest(t *testing.T) {
	db := openLogsSvcDB(t)
	svc := logsSvc(db, logsAccess{can: true})

	// 按日期精确取（含 logs 填充）
	report, err := svc.GetReportByDateLatest(logsDBUserA, "2099-05-01", false)
	if err != nil {
		t.Fatal(err)
	}
	if report.ID != logsDBReportA || report.RawText != "今天完成了装配与测试" {
		t.Fatalf("by date: %+v", report)
	}
	if len(report.Logs) != 1 || report.Logs[0].ID != logsDBLogA {
		t.Fatalf("report logs: %+v", report.Logs)
	}
	// latest=true 回溯：B 在 05-03 取到 05-01 的提交日报
	back, err := svc.GetReportByDateLatest(logsDBUserB, "2099-05-03", true)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != logsDBReportB {
		t.Fatalf("latest fallback: %+v", back)
	}
	// 空日期默认今天；非法日期 → ErrInvalidInput；零日报用户 → ErrReportNotFound
	if _, err := svc.GetReportByDateLatest(logsDBUserA, "2099/05/01", false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad date: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.GetReportByDateLatest(logsDBUserA, "2099-05-09", false); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("missing report: got %v, want ErrReportNotFound", err)
	}
	if _, err := svc.GetReportByDateLatest("00000000-0000-0000-0000-000000009999", "", false); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("no reports user: got %v, want ErrReportNotFound", err)
	}
}

func TestDBGetReportByID(t *testing.T) {
	db := openLogsSvcDB(t)
	svc := logsSvc(db, logsAccess{can: true})

	// 404 / 非 owner 成员 403 / admin 放行 / owner 命中
	if _, err := svc.GetReportByID("00000000-0000-0000-0000-000000009999", logsDBUserA, "member"); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("missing: got %v, want ErrReportNotFound", err)
	}
	if _, err := svc.GetReportByID(logsDBReportA, logsDBUserB, "member"); !errors.Is(err, ErrNotReportOwner) {
		t.Fatalf("non owner: got %v, want ErrNotReportOwner", err)
	}
	admin, err := svc.GetReportByID(logsDBReportA, logsDBAdmin, "admin")
	if err != nil || admin == nil || admin.ID != logsDBReportA {
		t.Fatalf("admin get: %+v err=%v", admin, err)
	}
	owner, err := svc.GetReportByID(logsDBReportA, logsDBUserA, "member")
	if err != nil {
		t.Fatal(err)
	}
	if len(owner.Logs) != 1 {
		t.Fatalf("owner report logs: %+v", owner.Logs)
	}
}

func TestDBUpdateReportRawText(t *testing.T) {
	db := openLogsSvcDB(t)
	svc := logsSvc(db, logsAccess{can: true})

	// 404 / 非 owner
	if _, err := svc.UpdateReportRawText("00000000-0000-0000-0000-000000009999", logsDBUserA, "x"); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("missing: got %v, want ErrReportNotFound", err)
	}
	if _, err := svc.UpdateReportRawText(logsDBReportA, logsDBUserB, "x"); !errors.Is(err, ErrNotReportOwner) {
		t.Fatalf("non owner: got %v, want ErrNotReportOwner", err)
	}
	// 已提交不可改
	if _, err := svc.UpdateReportRawText(logsDBReportB, logsDBUserB, "x"); !errors.Is(err, ErrAlreadySubmitted) {
		t.Fatalf("submitted: got %v, want ErrAlreadySubmitted", err)
	}
	// 成功：raw_text 原文落库
	updated, err := svc.UpdateReportRawText(logsDBReportA, logsDBUserA, "1. 完成了装配与测试；2. 校准了流量计")
	if err != nil {
		t.Fatal(err)
	}
	if updated.RawText != "1. 完成了装配与测试；2. 校准了流量计" {
		t.Fatalf("raw text: %q", updated.RawText)
	}
	var raw string
	if err := db.QueryRow(`SELECT raw_text FROM daily_reports WHERE id = $1`, logsDBReportA).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != "1. 完成了装配与测试；2. 校准了流量计" {
		t.Fatalf("persisted raw text: %q", raw)
	}
	// 部分更新：只传摘要保留原文，显式空串可清空摘要。
	summary := "摘要"
	updated, err = svc.UpdateReport(logsDBReportA, logsDBUserA, UpdateDailyReportRequest{Summary: &summary})
	if err != nil || updated.Summary != "摘要" || updated.RawText != "1. 完成了装配与测试；2. 校准了流量计" {
		t.Fatalf("summary-only update: %+v, %v", updated, err)
	}
	emptySummary := ""
	if _, err = svc.UpdateReport(logsDBReportA, logsDBUserA, UpdateDailyReportRequest{Summary: &emptySummary}); err != nil {
		t.Fatal(err)
	}
	// 恢复种子 raw_text（种子 ON CONFLICT DO NOTHING 不重置可变字段，
	// 不恢复会与 TestDBGetReportByDateLatest 的原文断言产生测试顺序耦合）
	t.Cleanup(func() {
		db.Exec(`UPDATE daily_reports SET raw_text = '今天完成了装配与测试', summary = '' WHERE id = $1`, logsDBReportA)
	})
}

func TestDBSubmitReport(t *testing.T) {
	db := openLogsSvcDB(t)

	// 硬阻塞逐一验证
	noAccess := logsAccess{can: false}
	svcNo := logsSvc(db, noAccess)
	if _, err := svcNo.SubmitReport("c0000000-0000-4000-8000-00000000c9ff", logsDBUserA, "member", false); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("missing report: got %v, want ErrReportNotFound", err)
	}
	if _, err := svcNo.SubmitReport(logsDBReportA, logsDBUserB, "member", false); !errors.Is(err, ErrNotReportOwner) {
		t.Fatalf("non owner: got %v", err)
	}
	if _, err := svcNo.SubmitReport(logsDBReportB, logsDBUserB, "member", false); !errors.Is(err, ErrAlreadySubmitted) {
		t.Fatalf("not draft: got %v", err)
	}
	if _, err := svcNo.SubmitReport(logsDBReportA, logsDBUserA, "member", false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("no access: got %v", err)
	}

	// 空 raw_text → ErrEmptyRawText（造一份空草稿）
	emptyReport := "c0000000-0000-4000-8000-00000000c910"
	if _, err := db.Exec(`INSERT INTO daily_reports (id, report_date, author_id, raw_text)
		VALUES ($1, '2099-05-04', $2, '')`, emptyReport, logsDBUserA); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM daily_reports WHERE id = $1`, emptyReport) })
	svcOK := logsSvc(db, logsAccess{can: true})
	if _, err := svcOK.SubmitReport(emptyReport, logsDBUserA, "member", false); !errors.Is(err, ErrEmptyRawText) {
		t.Fatalf("empty raw: got %v, want ErrEmptyRawText", err)
	}

	// 无链接日志 → ErrNoLogEntries（造一份草稿日报，无链接）
	noLogReport := "c0000000-0000-4000-8000-00000000c911"
	if _, err := db.Exec(`INSERT INTO daily_reports (id, report_date, author_id, raw_text)
		VALUES ($1, '2099-05-05', $2, '有内容但没日志')`, noLogReport, logsDBUserA); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM daily_reports WHERE id = $1`, noLogReport) })
	if _, err := svcOK.SubmitReport(noLogReport, logsDBUserA, "member", false); !errors.Is(err, ErrNoLogEntries) {
		t.Fatalf("no logs: got %v, want ErrNoLogEntries", err)
	}

	// voided 日志 → ErrLogVoided
	voidLog := "c0000000-0000-4000-8000-00000000c912"
	voidReport := "c0000000-0000-4000-8000-00000000c913"
	if _, err := db.Exec(`INSERT INTO logs (id, project_id, author_id, content, content_status)
		VALUES ($1, $2, $3, '废弃记录', 'voided')`, voidLog, logsDBProject, logsDBUserA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO daily_reports (id, report_date, author_id, raw_text)
		VALUES ($1, '2099-05-06', $2, '含废弃记录')`, voidReport, logsDBUserA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO daily_report_log_links (daily_report_id, log_id) VALUES ($1,$2)`, voidReport, voidLog); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id = $1`, voidReport)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, voidReport)
		db.Exec(`DELETE FROM logs WHERE id = $1`, voidLog)
	})
	if _, err := svcOK.SubmitReport(voidReport, logsDBUserA, "member", false); !errors.Is(err, ErrLogVoided) {
		t.Fatalf("voided log: got %v, want ErrLogVoided", err)
	}

	// 警告不强制 → blocked；force → submitted + quality=warnings
	// （raw 不含日志内容 + summary 空 → 3 条警告）
	warnReport := "c0000000-0000-4000-8000-00000000c914"
	warnLog := "c0000000-0000-4000-8000-00000000c915"
	if _, err := db.Exec(`INSERT INTO logs (id, project_id, author_id, occurred_at, content, content_status)
		VALUES ($1, $2, $3, '2099-05-06T10:00:00+08:00', '另一条内容', 'draft')`, warnLog, logsDBProject, logsDBUserA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO daily_reports (id, report_date, author_id, raw_text, summary)
		VALUES ($1, '2099-05-07', $2, '与日志无关的原文', '')`, warnReport, logsDBUserA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO daily_report_log_links (daily_report_id, log_id) VALUES ($1,$2)`, warnReport, warnLog); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id = $1`, warnReport)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, warnReport)
		db.Exec(`DELETE FROM logs WHERE id = $1`, warnLog)
	})
	blocked, err := svcOK.SubmitReport(warnReport, logsDBUserA, "member", false)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked.Blocked {
		t.Fatal("warnings without force must block")
	}
	wantCodes := map[string]bool{"log_still_draft": false, "date_mismatch": false, "raw_text_without_matching_log": false, "summary_empty": false}
	for _, w := range blocked.Warnings {
		if _, ok := wantCodes[w.Code]; ok {
			wantCodes[w.Code] = true
		}
	}
	for code, seen := range wantCodes {
		if !seen {
			t.Fatalf("missing warning %q", code)
		}
	}
	// force=true → 提交成功，quality=warnings
	forced, err := svcOK.SubmitReport(warnReport, logsDBUserA, "member", true)
	if err != nil {
		t.Fatal(err)
	}
	if forced.Blocked || forced.Report.ContentStatus != ReportStatusSubmitted || forced.Report.QualityStatus != QualityWarnings {
		t.Fatalf("forced: %+v", forced)
	}

	// 干净提交：确认日志 + 日期一致 + raw 含内容 + summary 非空 → 无警告 passed
	cleanReport := "c0000000-0000-4000-8000-00000000c916"
	cleanLog := "c0000000-0000-4000-8000-00000000c917"
	if _, err := db.Exec(`INSERT INTO logs (id, project_id, author_id, occurred_at, content, content_status)
		VALUES ($1, $2, $3, '2099-05-08T10:00:00+08:00', '装配匹配电路', 'confirmed')`, cleanLog, logsDBProject, logsDBUserA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO daily_reports (id, report_date, author_id, raw_text, summary)
		VALUES ($1, '2099-05-08', $2, '今天装配匹配电路完成', '完成装配')`, cleanReport, logsDBUserA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO daily_report_log_links (daily_report_id, log_id) VALUES ($1,$2)`, cleanReport, cleanLog); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id = $1`, cleanReport)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, cleanReport)
		db.Exec(`DELETE FROM logs WHERE id = $1`, cleanLog)
	})
	clean, err := svcOK.SubmitReport(cleanReport, logsDBUserA, "member", false)
	if err != nil {
		t.Fatal(err)
	}
	if clean.Blocked || len(clean.Warnings) != 0 || clean.Report.ContentStatus != ReportStatusSubmitted || clean.Report.QualityStatus != QualityPassed {
		t.Fatalf("clean submit: %+v", clean)
	}
}

func TestDBCreateLog(t *testing.T) {
	db := openLogsSvcDB(t)
	svc := logsSvc(db, logsAccess{can: true})

	// 输入校验（不触库）
	if _, err := svc.CreateLog("", logsDBUserA, "member", CreateLogRequest{Content: "x"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty project: got %v", err)
	}
	if _, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{Content: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty content: got %v", err)
	}
	if _, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{Content: "x", Category: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad category: got %v", err)
	}
	if _, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{Content: "x", Source: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad source: got %v", err)
	}
	badTime := "2026/07/01"
	if _, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{Content: "x", OccurredAt: &badTime}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad occurred_at: got %v", err)
	}
	// 项目不存在 / 无权限
	missingSvc := logsSvc(db, logsAccess{can: false, exists: false})
	if _, err := missingSvc.CreateLog("00000000-0000-0000-0000-000000009999", logsDBUserA, "member", CreateLogRequest{Content: "x"}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing project: got %v", err)
	}
	noAccess := logsSvc(db, logsAccess{can: false, exists: true})
	if _, err := noAccess.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{Content: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("no access: got %v", err)
	}
	rawSnippet := "今天完成了装配与测试"
	if _, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{Content: "x", RawSnippet: &rawSnippet}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("snippet without report: got %v, want ErrInvalidInput", err)
	}
	shortSnippet := "完成了装配"
	if _, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{Content: "x", DailyReportID: ptrString(logsDBReportA), RawSnippet: &shortSnippet}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("partial snippet: got %v, want ErrInvalidInput", err)
	}

	// 成功创建 + 日报链接
	reportID := logsDBReportA
	item, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{
		Category: "rf", Content: "RF 匹配网络调谐", DailyReportID: &reportID, RawSnippet: &rawSnippet,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE log_id = $1`, item.ID)
		db.Exec(`DELETE FROM logs WHERE id = $1`, item.ID)
	})
	if item.Content != "RF 匹配网络调谐" || item.RawSnippet == nil || *item.RawSnippet != rawSnippet || item.Category != "rf" || item.Source != SourceManual || item.ContentStatus != LogStatusDraft {
		t.Fatalf("created log: %+v", item)
	}
	byID, err := NewRepository(db).GetByID(item.ID)
	if err != nil || byID.RawSnippet == nil || *byID.RawSnippet != rawSnippet {
		t.Fatalf("GetByID raw snippet: item=%+v err=%v", byID, err)
	}
	listed, _, err := NewRepository(db).List(logsDBProject, LogListParams{Status: LogStatusDraft})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, listedItem := range listed {
		if listedItem.ID == item.ID && listedItem.RawSnippet != nil && *listedItem.RawSnippet == rawSnippet {
			found = true
		}
	}
	if !found {
		t.Fatalf("List did not preserve raw snippet: %+v", listed)
	}
	newContent := "编辑后的日志内容"
	updated, err := svc.UpdateLog(item.ID, logsDBUserA, "member", UpdateLogRequest{Content: &newContent})
	if err != nil || updated.RawSnippet == nil || *updated.RawSnippet != rawSnippet {
		t.Fatalf("UpdateLog changed raw snippet: item=%+v err=%v", updated, err)
	}
	var linkCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM daily_report_log_links WHERE log_id = $1`, item.ID).Scan(&linkCount); err != nil {
		t.Fatal(err)
	}
	if linkCount != 1 {
		t.Fatalf("report link = %d, want 1", linkCount)
	}
	linked, err := NewRepository(db).GetLogsByReport(reportID)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, linkedItem := range linked {
		if linkedItem.ID == item.ID && linkedItem.RawSnippet != nil && *linkedItem.RawSnippet == rawSnippet {
			found = true
		}
	}
	if !found {
		t.Fatalf("GetLogsByReport did not preserve raw snippet: %+v", linked)
	}
	if _, err := db.Exec(`UPDATE daily_reports SET raw_text = '原文已修改' WHERE id = $1`, reportID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{Content: "x", DailyReportID: &reportID, RawSnippet: &rawSnippet}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("stale snippet: got %v, want ErrInvalidInput", err)
	}
	if _, err := db.Exec(`UPDATE daily_reports SET raw_text = '今天完成了装配与测试' WHERE id = $1`, reportID); err != nil {
		t.Fatal(err)
	}

	// 日报不存在 / 日报非本人 → 错误（日志本身已创建，但返回错误）
	missingReport := "c0000000-0000-4000-8000-000000009999"
	if _, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{
		Content: "x", DailyReportID: &missingReport,
	}); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("missing report: got %v, want ErrReportNotFound", err)
	}
	otherReport := logsDBReportB
	if _, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{
		Content: "x", DailyReportID: &otherReport,
	}); !errors.Is(err, ErrNotReportOwner) {
		t.Fatalf("other's report: got %v, want ErrNotReportOwner", err)
	}
}

func TestDBListReports(t *testing.T) {
	db := openLogsSvcDB(t)
	svc := logsSvc(db, logsAccess{can: true})

	// 默认（仅自己）：A 有 1 份草稿 + 测试中临时创建
	list, total, err := svc.ListReports(ReportListParams{AuthorID: logsDBUserA})
	if err != nil {
		t.Fatal(err)
	}
	if total < 1 || list[0].ID != logsDBReportA {
		t.Fatalf("list reports: %+v total=%d", list, total)
	}
	// status / date / keyword 过滤
	byStatus, total, err := svc.ListReports(ReportListParams{AuthorID: logsDBUserA, Status: ReportStatusDraft})
	if err != nil || total < 1 {
		t.Fatalf("status filter: %+v total=%d err=%v", byStatus, total, err)
	}
	byDate, total, err := svc.ListReports(ReportListParams{AuthorID: logsDBUserA, Date: "2099-05-01"})
	if err != nil || total != 1 {
		t.Fatalf("date filter: %+v total=%d err=%v", byDate, total, err)
	}
	_, total, err = svc.ListReports(ReportListParams{AuthorID: logsDBUserA, Keyword: "测试摘要"})
	if err != nil || total != 1 {
		t.Fatalf("keyword filter: total=%d err=%v", total, err)
	}
	// 非法参数
	if _, _, err := svc.ListReports(ReportListParams{AuthorID: logsDBUserA, Status: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad status: got %v", err)
	}
	if _, _, err := svc.ListReports(ReportListParams{AuthorID: logsDBUserA, Date: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad date: got %v", err)
	}
}

func TestDBListLogsAndGetLog(t *testing.T) {
	db := openLogsSvcDB(t)
	svc := logsSvc(db, logsAccess{can: true})

	// 非法参数
	if _, err := svc.ListLogs(logsDBProject, logsDBUserA, "member", LogListParams{Status: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad status: got %v", err)
	}
	if _, err := svc.ListLogs(logsDBProject, logsDBUserA, "member", LogListParams{Category: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad category: got %v", err)
	}
	if _, err := svc.ListLogs(logsDBProject, logsDBUserA, "member", LogListParams{DateFrom: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad date_from: got %v", err)
	}
	noAccess := logsSvc(db, logsAccess{can: false, exists: true})
	if _, err := noAccess.ListLogs(logsDBProject, logsDBUserA, "member", LogListParams{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("no access list: got %v", err)
	}

	// 默认 status=confirmed：只有日志 B
	result, err := svc.ListLogs(logsDBProject, logsDBUserA, "member", LogListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].ID != logsDBLogB {
		t.Fatalf("default confirmed list: %+v", result)
	}
	// status=draft → 日志 A（测试期间可能有多条临时日志，断言包含即可）
	draftResult, err := svc.ListLogs(logsDBProject, logsDBUserA, "member", LogListParams{Status: LogStatusDraft})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range draftResult.Items {
		if item.ID == logsDBLogA {
			found = true
		}
	}
	if !found {
		t.Fatalf("draft list missing logA: %+v", draftResult.Items)
	}
	// category 过滤
	rfResult, err := svc.ListLogs(logsDBProject, logsDBUserA, "member", LogListParams{Category: "assembly"})
	if err != nil {
		t.Fatal(err)
	}
	if rfResult.Total < 1 {
		t.Fatalf("assembly filter: %+v", rfResult)
	}

	// GetLog：404 / 无权限 / 命中
	if _, err := svc.GetLog("00000000-0000-0000-0000-000000009999", logsDBUserA, "member"); !errors.Is(err, ErrLogNotFound) {
		t.Fatalf("missing log: got %v", err)
	}
	if _, err := noAccess.GetLog(logsDBLogA, logsDBUserA, "member"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("no access get: got %v", err)
	}
	item, err := svc.GetLog(logsDBLogA, logsDBUserA, "member")
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != logsDBLogA {
		t.Fatalf("get log: %+v", item)
	}
}

func TestDBUpdateLog(t *testing.T) {
	db := openLogsSvcDB(t)
	// member 场景：canUpdateAny=false、canUpdateOwn=true、canRead=true
	memberAccess := logsAccess{perPerm: map[middleware.Permission]bool{
		middleware.PermRead:         true,
		middleware.PermUpdateAnyLog: false,
		middleware.PermUpdateOwnLog: true,
	}}
	svc := logsSvc(db, memberAccess)

	// 404
	if _, err := svc.UpdateLog("00000000-0000-0000-0000-000000009999", logsDBUserA, "member", UpdateLogRequest{}); !errors.Is(err, ErrLogNotFound) {
		t.Fatalf("missing: got %v", err)
	}
	// 非 draft 不可改（B 已 confirmed）
	if _, err := svc.UpdateLog(logsDBLogB, logsDBUserB, "member", UpdateLogRequest{}); !errors.Is(err, ErrLogNotDraft) {
		t.Fatalf("not draft: got %v", err)
	}
	// 别人的日志（自己只 canOwn）→ ErrLogOwnerMismatch
	content := "x"
	if _, err := svc.UpdateLog(logsDBLogA, logsDBUserB, "member", UpdateLogRequest{Content: &content}); !errors.Is(err, ErrLogOwnerMismatch) {
		t.Fatalf("owner mismatch: got %v, want ErrLogOwnerMismatch", err)
	}
	// 输入校验
	badCategory := "bogus"
	if _, err := svc.UpdateLog(logsDBLogA, logsDBUserA, "member", UpdateLogRequest{Category: &badCategory}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad category: got %v", err)
	}
	emptyContent := "  "
	if _, err := svc.UpdateLog(logsDBLogA, logsDBUserA, "member", UpdateLogRequest{Content: &emptyContent}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty content: got %v", err)
	}
	badStatus := "bogus"
	if _, err := svc.UpdateLog(logsDBLogA, logsDBUserA, "member", UpdateLogRequest{ContentStatus: &badStatus}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad content_status: got %v", err)
	}
	// draft 项目 → ErrProjectLifecycleBlocked（access 报告非 active）
	draftSvc := logsSvc(db, logsAccess{status: projects.StatusDraft})
	if _, err := draftSvc.UpdateLog(logsDBLogA, logsDBUserA, "member", UpdateLogRequest{Content: &content}); !errors.Is(err, ErrProjectLifecycleBlocked) {
		t.Fatalf("draft project: got %v, want ErrProjectLifecycleBlocked", err)
	}
	// 无任何权限 → ErrForbidden
	noAccess := logsSvc(db, logsAccess{perPerm: map[middleware.Permission]bool{
		middleware.PermUpdateAnyLog: false,
		middleware.PermUpdateOwnLog: false,
	}})
	if _, err := noAccess.UpdateLog(logsDBLogA, logsDBUserA, "member", UpdateLogRequest{Content: &content}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("no perms: got %v", err)
	}

	// 成功：改内容 + 确认（draft → confirmed）
	newContent := "更新后的工作内容"
	confirmed := LogStatusConfirmed
	updated, err := svc.UpdateLog(logsDBLogA, logsDBUserA, "member", UpdateLogRequest{Content: &newContent, ContentStatus: &confirmed})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content != newContent || updated.ContentStatus != LogStatusConfirmed {
		t.Fatalf("updated: %+v", updated)
	}
	// 成功后不再 draft → 再改 → ErrLogNotDraft（repo 更新 0 行）
	if _, err := svc.UpdateLog(logsDBLogA, logsDBUserA, "member", UpdateLogRequest{Content: &newContent}); !errors.Is(err, ErrLogNotDraft) {
		t.Fatalf("after confirm: got %v, want ErrLogNotDraft", err)
	}
	// 恢复种子状态（后续用例可能依赖）
	if _, err := db.Exec(`UPDATE logs SET content_status = 'draft', content = '完成了装配与测试' WHERE id = $1`, logsDBLogA); err != nil {
		t.Fatal(err)
	}
}

func TestDBReportAndLogSoftDeleteChain(t *testing.T) {
	db := openLogsSvcDB(t)
	svc := logsSvc(db, logsAccess{can: true})

	// 软删除链路：删除日志 → 日报链接级联删除（CASCADE），GetLogsByReport 不再返回
	item, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{
		Category: "vacuum", Content: "真空度记录",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE log_id = $1`, item.ID)
		db.Exec(`DELETE FROM logs WHERE id = $1`, item.ID)
	})
	reportID := logsDBReportA
	if _, err := svc.CreateLog(logsDBProject, logsDBUserA, "member", CreateLogRequest{
		Category: "vacuum", Content: "链接到日报的日志", DailyReportID: &reportID,
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	linked, err := repo.GetLogsByReport(logsDBReportA)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) < 2 {
		t.Fatalf("report logs before delete: %d", len(linked))
	}
	// 硬删日志（无软删端点，直删验证 CASCADE 语义与查询过滤）
	if _, err := db.Exec(`DELETE FROM logs WHERE id = $1`, item.ID); err != nil {
		t.Fatal(err)
	}
	after, err := repo.GetLogsByReport(logsDBReportA)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range after {
		if l.ID == item.ID {
			t.Fatalf("deleted log still in report: %+v", after)
		}
	}
}

func TestDBRawTextMatching(t *testing.T) {
	// 纯函数分支：raw 命中内容（小写归一化）/ 未命中
	items := []Log{{ID: "l1", Content: "匹配电路"}, {ID: "l2", Content: "  "}}
	if !rawTextHasMatchingLog("今天 ASSEMBLY 匹配电路 完成", items) {
		t.Fatal("matching content should be detected")
	}
	if rawTextHasMatchingLog("完全没有相关内容", items) {
		t.Fatal("unrelated raw text must not match")
	}
	one := "测试了一下qpig两个rf之间的电阻是4.4M欧姆"
	two := "RF匹配通过"
	strict := []Log{{Content: "测试了q-pig两个rf之间的电阻为4.4M欧姆", RawSnippet: &one}, {Content: "改写后的内容", RawSnippet: &two}}
	if !rawTextHasMatchingLog(one+"。"+two+"。", strict) {
		t.Fatal("exact snippets should cover rewritten log content")
	}
	report := DailyReport{RawText: one + "。" + two + "。", ReportDate: "2026-08-20", Summary: "完成测试"}
	for i := range strict {
		strict[i].ContentStatus = LogStatusConfirmed
		strict[i].OccurredAt = time.Date(2026, 8, 20, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	}
	for _, warning := range submitWarnings(report, strict) {
		if warning.Code == "raw_text_without_matching_log" {
			t.Fatalf("complete snippets produced unexpected warning: %+v", warning)
		}
	}
	if rawTextHasMatchingLog(one+"。"+two+"。遗漏第三段", strict) {
		t.Fatal("missing segment must not be covered")
	}
	report.RawText += "遗漏第三段"
	foundWarning := false
	for _, warning := range submitWarnings(report, strict) {
		foundWarning = foundWarning || warning.Code == "raw_text_without_matching_log"
	}
	if !foundWarning {
		t.Fatal("missing segment must produce raw_text_without_matching_log")
	}
	duplicate := "完成测试"
	if rawTextHasMatchingLog("完成测试。完成测试。", []Log{{RawSnippet: &duplicate}}) {
		t.Fatal("one snippet must not cover two duplicate segments")
	}
	if !rawTextHasMatchingLog("完成测试。完成测试。", []Log{{RawSnippet: &duplicate}, {RawSnippet: &duplicate}}) {
		t.Fatal("two snippets should cover two duplicate segments")
	}
	if rawTextHasMatchingLog(one+"。"+two, []Log{{RawSnippet: &one}, {Content: two}}) {
		t.Fatal("nil snippet must not downgrade or add strict coverage")
	}
}

func ptrString(value string) *string { return &value }
