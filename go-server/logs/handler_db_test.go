package logs

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

// handler 集成测试：与 main.go 一致挂 AuthRequired + AgentContext + Audit +
// RequireIdempotencyKey + RequireProjectPermission 中间件（真实 DB）。
// 审计断言：daily_report.by_date / daily_report.ai_parsed 有 handler SetAuditAction，
// 其余为 Audit 中间件按路径派生 action（daily-reports.today 等）。

const logsHandlerSecret = "logs-handler-test-secret"

const (
	logsHUserA   = "00000000-0000-0000-0000-00000000ba01"
	logsHUserB   = "00000000-0000-0000-0000-00000000ba02"
	logsHViewer  = "00000000-0000-0000-0000-00000000ba03"
	logsHProject = "c0000000-0000-4000-8000-00000000ca01"
)

func newLogsTestRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(logsHandlerSecret))
	repo := NewRepository(db)
	svc := NewService(repo, "Asia/Shanghai", ProjectAccessAdapter{DB: db, Repo: projects.NewRepository(db)})
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1/daily-reports", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", h.ListReports)
		r.Post("/today", h.GetOrCreateTodayReport)
		r.Get("/by-date", h.GetReportByDate)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetReportByID)
			r.Patch("/", h.UpdateReportRawText)
			r.Post("/submit", h.SubmitReport)
			r.Post("/ai-parse", h.AiParseReport)
		})
	})
	r.Route("/api/v1/projects/{id}", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Use(middleware.RequireProjectPermission(db, middleware.PermRead))
		r.Get("/logs", h.ListLogs)
		r.With(middleware.RequireProjectPermission(db, middleware.PermCreateLog)).Post("/logs", h.CreateLog)
	})
	r.Route("/api/v1/logs", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetLog)
			r.Patch("/", h.UpdateLog)
		})
	})
	return r
}

func openLogsHandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openLogsSvcDB(t)
	for _, u := range []struct {
		id       string
		username string
		role     string
	}{
		{logsHUserA, "logs_h_usera", "member"},
		{logsHUserB, "logs_h_userb", "member"},
		{logsHViewer, "logs_h_viewer", "viewer"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Logs Handler Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'LOGS_H_ACTIVE', 'Handler 种子项目', 'active', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, logsHProject, logsHUserA); err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		userID string
		role   string
	}{
		{logsHUserA, "owner"},
		{logsHUserB, "member"},
		{logsHViewer, "viewer"},
	} {
		if _, err := db.Exec(
			`INSERT INTO project_members (project_id, user_id, role, status, added_by)
			 VALUES ($1, $2, $3, 'active', $2)
			 ON CONFLICT (project_id, user_id) DO NOTHING`, logsHProject, m.userID, m.role); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM audit_log WHERE user_id IN ($1,$2,$3)`, logsHUserA, logsHUserB, logsHViewer)
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id IN (SELECT id FROM daily_reports WHERE author_id IN ($1,$2,$3))`,
			logsHUserA, logsHUserB, logsHViewer)
		db.Exec(`DELETE FROM daily_reports WHERE author_id IN ($1,$2,$3)`, logsHUserA, logsHUserB, logsHViewer)
		db.Exec(`DELETE FROM logs WHERE author_id IN ($1,$2,$3)`, logsHUserA, logsHUserB, logsHViewer)
		db.Exec(`DELETE FROM project_members WHERE project_id = $1`, logsHProject)
		db.Exec(`DELETE FROM projects WHERE id = $1`, logsHProject)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, logsHUserA, logsHUserB, logsHViewer)
	})
	return db
}

func logsToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(logsHandlerSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func logsReq(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func logsEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (json.RawMessage, string) {
	t.Helper()
	var envelope struct {
		Data      json.RawMessage `json:"data"`
		RequestID string          `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("body: %s err=%v", rec.Body.String(), err)
	}
	return envelope.Data, envelope.RequestID
}

func logsErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error body: %s err=%v", rec.Body.String(), err)
	}
	return envelope.Error.Code
}

func uniqueLogsKey() string {
	return fmt.Sprintf("logs-h-%d", time.Now().UnixNano())
}

func assertLogsAudit(t *testing.T, db *sql.DB, requestID, action string) {
	t.Helper()
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE request_id = $1 AND action = $2`, requestID, action,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit_log rows = %d, want 1 (request_id=%s action=%s)", count, requestID, action)
	}
}

func TestHandlerTodayAndRawText(t *testing.T) {
	db := openLogsHandlerDB(t)
	router := newLogsTestRouter(t, db)
	userA := logsToken(t, logsHUserA, "logs_h_usera", auth.RoleMember)

	// 401：无 token
	rec := logsReq(t, router, http.MethodPost, "/api/v1/daily-reports/today", "", uniqueLogsKey(), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", rec.Code)
	}
	// 400：缺 Idempotency-Key
	rec = logsReq(t, router, http.MethodPost, "/api/v1/daily-reports/today", userA, "", "")
	if rec.Code != http.StatusBadRequest || logsErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idem = %d body=%s", rec.Code, rec.Body.String())
	}
	// 200：取今日草稿 + 审计（path 派生）
	rec = logsReq(t, router, http.MethodPost, "/api/v1/daily-reports/today", userA, uniqueLogsKey(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("today = %d body=%s", rec.Code, rec.Body.String())
	}
	data, requestID := logsEnvelope(t, rec)
	var report struct {
		ID            string `json:"id"`
		ContentStatus string `json:"content_status"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.ContentStatus != ReportStatusDraft {
		t.Fatalf("today report: %+v", report)
	}
	assertLogsAudit(t, db, requestID, "daily-reports.today")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id = $1`, report.ID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, report.ID)
	})

	// PATCH raw_text：200 + 原文落库
	rec = logsReq(t, router, http.MethodPatch, "/api/v1/daily-reports/"+report.ID, userA, uniqueLogsKey(),
		`{"raw_text":"1. 完成了装配；2. 校准了流量计"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch raw = %d body=%s", rec.Code, rec.Body.String())
	}
	_, patchReqID := logsEnvelope(t, rec)
	assertLogsAudit(t, db, patchReqID, "daily-reports."+report.ID)
	var raw string
	if err := db.QueryRow(`SELECT raw_text FROM daily_reports WHERE id = $1`, report.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != "1. 完成了装配；2. 校准了流量计" {
		t.Fatalf("raw text = %q", raw)
	}
	// 400：缺 Idempotency-Key；404：不存在
	rec = logsReq(t, router, http.MethodPatch, "/api/v1/daily-reports/"+report.ID, userA, "", `{"raw_text":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("patch no idem = %d", rec.Code)
	}
	rec = logsReq(t, router, http.MethodPatch, "/api/v1/daily-reports/c0000000-0000-4000-8000-00000000cfff", userA, uniqueLogsKey(), `{"raw_text":"x"}`)
	if rec.Code != http.StatusNotFound || logsErrorCode(t, rec) != "report_not_found" {
		t.Fatalf("patch missing = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerSubmitReport(t *testing.T) {
	db := openLogsHandlerDB(t)
	router := newLogsTestRouter(t, db)
	userA := logsToken(t, logsHUserA, "logs_h_usera", auth.RoleMember)

	// 造草稿：raw + 确认日志 + 链接（干净提交）
	reportID := seedHandlerReport(t, db, logsHUserA, "draft", "2099-06-01", "今天装配匹配电路完成", "完成装配")
	logID := seedHandlerLog(t, db, logsHProject, logsHUserA, "confirmed", "2099-06-01T10:00:00+08:00", "装配匹配电路")
	if _, err := db.Exec(`INSERT INTO daily_report_log_links (daily_report_id, log_id) VALUES ($1,$2)`, reportID, logID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id = $1`, reportID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, reportID)
		db.Exec(`DELETE FROM logs WHERE id = $1`, logID)
	})
	path := "/api/v1/daily-reports/" + reportID + "/submit"

	// 400：缺 Idempotency-Key
	rec := logsReq(t, router, http.MethodPost, path, userA, "", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idem = %d", rec.Code)
	}
	// 200：提交成功（无警告）→ submitted + passed + 审计（path 派生）
	rec = logsReq(t, router, http.MethodPost, path, userA, uniqueLogsKey(), `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit = %d body=%s", rec.Code, rec.Body.String())
	}
	data, requestID := logsEnvelope(t, rec)
	var result struct {
		Blocked bool `json:"blocked"`
		Report  struct {
			ContentStatus string `json:"content_status"`
			QualityStatus string `json:"quality_status"`
		} `json:"report"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Blocked || result.Report.ContentStatus != ReportStatusSubmitted || result.Report.QualityStatus != QualityPassed {
		t.Fatalf("submit result: %+v", result)
	}
	assertLogsAudit(t, db, requestID, "daily-reports."+reportID+".submit")
	// 重复提交 → 400 already_submitted
	rec = logsReq(t, router, http.MethodPost, path, userA, uniqueLogsKey(), `{}`)
	if rec.Code != http.StatusBadRequest || logsErrorCode(t, rec) != "already_submitted" {
		t.Fatalf("resubmit = %d body=%s", rec.Code, rec.Body.String())
	}

	// 空 raw_text → 400 empty_raw_text（另一草稿）
	emptyID := seedHandlerReport(t, db, logsHUserA, "draft", "2099-06-02", "", "")
	t.Cleanup(func() { db.Exec(`DELETE FROM daily_reports WHERE id = $1`, emptyID) })
	rec = logsReq(t, router, http.MethodPost, "/api/v1/daily-reports/"+emptyID+"/submit", userA, uniqueLogsKey(), `{}`)
	if rec.Code != http.StatusBadRequest || logsErrorCode(t, rec) != "empty_raw_text" {
		t.Fatalf("empty submit = %d body=%s", rec.Code, rec.Body.String())
	}
	// 无日志 → 400 no_log_entries
	noLogID := seedHandlerReport(t, db, logsHUserA, "draft", "2099-06-03", "有内容没日志", "")
	t.Cleanup(func() { db.Exec(`DELETE FROM daily_reports WHERE id = $1`, noLogID) })
	rec = logsReq(t, router, http.MethodPost, "/api/v1/daily-reports/"+noLogID+"/submit", userA, uniqueLogsKey(), `{}`)
	if rec.Code != http.StatusBadRequest || logsErrorCode(t, rec) != "no_log_entries" {
		t.Fatalf("no logs submit = %d body=%s", rec.Code, rec.Body.String())
	}
	// 404：日报不存在
	rec = logsReq(t, router, http.MethodPost, "/api/v1/daily-reports/c0000000-0000-4000-8000-00000000cfff/submit", userA, uniqueLogsKey(), `{}`)
	if rec.Code != http.StatusNotFound || logsErrorCode(t, rec) != "report_not_found" {
		t.Fatalf("missing submit = %d body=%s", rec.Code, rec.Body.String())
	}
	// 403：非本人
	otherID := seedHandlerReport(t, db, logsHUserB, "draft", "2099-06-04", "B 的日报内容", "B 摘要")
	t.Cleanup(func() { db.Exec(`DELETE FROM daily_reports WHERE id = $1`, otherID) })
	rec = logsReq(t, router, http.MethodPost, "/api/v1/daily-reports/"+otherID+"/submit", userA, uniqueLogsKey(), `{}`)
	if rec.Code != http.StatusForbidden || logsErrorCode(t, rec) != "permission_denied" {
		t.Fatalf("non owner submit = %d body=%s", rec.Code, rec.Body.String())
	}

	// 400：链接日志已废弃 → log_voided
	voidLogID := seedHandlerLog(t, db, logsHProject, logsHUserA, LogStatusVoided, "2099-06-06T10:00:00+08:00", "废弃的工作记录")
	voidReportID := seedHandlerReport(t, db, logsHUserA, "draft", "2099-06-06", "含废弃记录的日报", "摘要")
	if _, err := db.Exec(`INSERT INTO daily_report_log_links (daily_report_id, log_id) VALUES ($1,$2)`, voidReportID, voidLogID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id = $1`, voidReportID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, voidReportID)
		db.Exec(`DELETE FROM logs WHERE id = $1`, voidLogID)
	})
	rec = logsReq(t, router, http.MethodPost, "/api/v1/daily-reports/"+voidReportID+"/submit", userA, uniqueLogsKey(), `{}`)
	if rec.Code != http.StatusBadRequest || logsErrorCode(t, rec) != "log_voided" {
		t.Fatalf("voided submit = %d body=%s", rec.Code, rec.Body.String())
	}

	// 403：链接日志属于本人无权限的项目（submit 校验每条日志的项目读权限）
	permProject := "c0000000-0000-4000-8000-00000000ca02"
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'LOGS_H_P2', '仅 B 成员项目', 'active', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, permProject, logsHUserB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 VALUES ($1, $2, 'owner', 'active', $2)
		 ON CONFLICT (project_id, user_id) DO NOTHING`, permProject, logsHUserB); err != nil {
		t.Fatal(err)
	}
	permLogID := seedHandlerLog(t, db, permProject, logsHUserB, LogStatusConfirmed, "2099-06-07T10:00:00+08:00", "B 项目的记录")
	permReportID := seedHandlerReport(t, db, logsHUserA, "draft", "2099-06-07", "引用了 B 项目的记录", "摘要")
	if _, err := db.Exec(`INSERT INTO daily_report_log_links (daily_report_id, log_id) VALUES ($1,$2)`, permReportID, permLogID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id = $1`, permReportID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, permReportID)
		db.Exec(`DELETE FROM logs WHERE id = $1`, permLogID)
		db.Exec(`DELETE FROM project_members WHERE project_id = $1`, permProject)
		db.Exec(`DELETE FROM projects WHERE id = $1`, permProject)
	})
	rec = logsReq(t, router, http.MethodPost, "/api/v1/daily-reports/"+permReportID+"/submit", userA, uniqueLogsKey(), `{}`)
	if rec.Code != http.StatusForbidden || logsErrorCode(t, rec) != "permission_denied" {
		t.Fatalf("log project no access = %d body=%s", rec.Code, rec.Body.String())
	}

	// 警告 + force=true → 200 submitted + quality=warnings
	warnReportID := seedHandlerReport(t, db, logsHUserA, "draft", "2099-06-05", "与日志无关的原文", "")
	warnLogID := seedHandlerLog(t, db, logsHProject, logsHUserA, "draft", "2099-06-04T10:00:00+08:00", "别的内容")
	if _, err := db.Exec(`INSERT INTO daily_report_log_links (daily_report_id, log_id) VALUES ($1,$2)`, warnReportID, warnLogID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id = $1`, warnReportID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, warnReportID)
		db.Exec(`DELETE FROM logs WHERE id = $1`, warnLogID)
	})
	rec = logsReq(t, router, http.MethodPost, "/api/v1/daily-reports/"+warnReportID+"/submit", userA, uniqueLogsKey(), `{"force":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("force submit = %d body=%s", rec.Code, rec.Body.String())
	}
	data, _ = logsEnvelope(t, rec)
	var forced struct {
		Blocked bool `json:"blocked"`
		Report  struct {
			QualityStatus string `json:"quality_status"`
		} `json:"report"`
		Warnings []struct {
			Code string `json:"code"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal(data, &forced); err != nil {
		t.Fatal(err)
	}
	if forced.Blocked || forced.Report.QualityStatus != QualityWarnings || len(forced.Warnings) == 0 {
		t.Fatalf("forced result: %+v", forced)
	}
}

func TestHandlerByDateAndListReports(t *testing.T) {
	db := openLogsHandlerDB(t)
	router := newLogsTestRouter(t, db)
	userA := logsToken(t, logsHUserA, "logs_h_usera", auth.RoleMember)

	reportID := seedHandlerReport(t, db, logsHUserA, "submitted", "2099-06-10", "A 的日报", "A 摘要")
	t.Cleanup(func() { db.Exec(`DELETE FROM daily_reports WHERE id = $1`, reportID) })

	// 200：by-date 命中 + 语义 action daily_report.by_date
	rec := logsReq(t, router, http.MethodGet, "/api/v1/daily-reports/by-date?date=2099-06-10", userA, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("by-date = %d body=%s", rec.Code, rec.Body.String())
	}
	data, requestID := logsEnvelope(t, rec)
	var report struct {
		ID     string `json:"id"`
		Author string `json:"author_name"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.ID != reportID {
		t.Fatalf("by-date report: %+v", report)
	}
	assertLogsAudit(t, db, requestID, "daily_report.by_date")
	// 404：无日报
	rec = logsReq(t, router, http.MethodGet, "/api/v1/daily-reports/by-date?date=2099-06-11", userA, "", "")
	if rec.Code != http.StatusNotFound || logsErrorCode(t, rec) != "report_not_found" {
		t.Fatalf("by-date missing = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：非法日期
	rec = logsReq(t, router, http.MethodGet, "/api/v1/daily-reports/by-date?date=bogus", userA, "", "")
	if rec.Code != http.StatusBadRequest || logsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("by-date bad = %d body=%s", rec.Code, rec.Body.String())
	}
	// 200：GetReportByID 详情
	rec = logsReq(t, router, http.MethodGet, "/api/v1/daily-reports/"+reportID, userA, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get report = %d body=%s", rec.Code, rec.Body.String())
	}
	// 403：非本人
	userB := logsToken(t, logsHUserB, "logs_h_userb", auth.RoleMember)
	rec = logsReq(t, router, http.MethodGet, "/api/v1/daily-reports/"+reportID, userB, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other's report = %d, want 403", rec.Code)
	}
	// 200：列表（自己 1 条）+ 非法 status → 400
	rec = logsReq(t, router, http.MethodGet, "/api/v1/daily-reports", userA, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list reports = %d", rec.Code)
	}
	rec = logsReq(t, router, http.MethodGet, "/api/v1/daily-reports?status=bogus", userA, "", "")
	if rec.Code != http.StatusBadRequest || logsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("list bad status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerCreateListUpdateLog(t *testing.T) {
	db := openLogsHandlerDB(t)
	router := newLogsTestRouter(t, db)
	userA := logsToken(t, logsHUserA, "logs_h_usera", auth.RoleMember)
	viewer := logsToken(t, logsHViewer, "logs_h_viewer", auth.RoleMember)
	path := "/api/v1/projects/" + logsHProject + "/logs"

	// 403：viewer 无 create_log
	rec := logsReq(t, router, http.MethodPost, path, viewer, uniqueLogsKey(), `{"content":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create = %d, want 403", rec.Code)
	}
	// 400：非法 category / 缺 Idempotency-Key
	rec = logsReq(t, router, http.MethodPost, path, userA, uniqueLogsKey(), `{"content":"x","category":"bogus"}`)
	if rec.Code != http.StatusBadRequest || logsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("bad category = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = logsReq(t, router, http.MethodPost, path, userA, "", `{"content":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idem = %d", rec.Code)
	}
	// 201：创建成功 + 审计
	rec = logsReq(t, router, http.MethodPost, path, userA, uniqueLogsKey(),
		`{"category":"cryo","content":"冷头温度稳定 79.6K","occurred_at":"2099-07-01T09:00:00+08:00"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create log = %d body=%s", rec.Code, rec.Body.String())
	}
	data, requestID := logsEnvelope(t, rec)
	var item struct {
		ID       string `json:"id"`
		Category string `json:"category"`
		Source   string `json:"source"`
	}
	if err := json.Unmarshal(data, &item); err != nil {
		t.Fatal(err)
	}
	if item.Category != "cryo" || item.Source != SourceManual {
		t.Fatalf("created log: %+v", item)
	}
	assertLogsAudit(t, db, requestID, "projects."+logsHProject+".logs")
	t.Cleanup(func() { db.Exec(`DELETE FROM logs WHERE id = $1`, item.ID) })

	// 200：列表（默认 confirmed）+ status 过滤 + 400 非法 status
	rec = logsReq(t, router, http.MethodGet, path, userA, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list logs = %d", rec.Code)
	}
	rec = logsReq(t, router, http.MethodGet, path+"?status=draft", userA, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list draft = %d", rec.Code)
	}
	rec = logsReq(t, router, http.MethodGet, path+"?status=bogus", userA, "", "")
	if rec.Code != http.StatusBadRequest || logsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("list bad status = %d body=%s", rec.Code, rec.Body.String())
	}
	// 403：outsider 无成员关系（用 viewer 在他人项目场景：viewer 是成员，改测 userB 非成员？b 是成员——直接测不存在用户 token）
	outsider := logsToken(t, "00000000-0000-0000-0000-000000009999", "nobody", auth.RoleMember)
	rec = logsReq(t, router, http.MethodGet, path, outsider, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list = %d, want 403", rec.Code)
	}

	// GET /api/v1/logs/{id}：200 命中 / 404
	rec = logsReq(t, router, http.MethodGet, "/api/v1/logs/"+item.ID, userA, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get log = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = logsReq(t, router, http.MethodGet, "/api/v1/logs/c0000000-0000-4000-8000-00000000cfff", userA, "", "")
	if rec.Code != http.StatusNotFound || logsErrorCode(t, rec) != "log_not_found" {
		t.Fatalf("get missing log = %d body=%s", rec.Code, rec.Body.String())
	}

	// PATCH：draft 可改 + 审计；确认后不可再改
	rec = logsReq(t, router, http.MethodPatch, "/api/v1/logs/"+item.ID, userA, uniqueLogsKey(),
		`{"content":"更新后的冷头记录"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update log = %d body=%s", rec.Code, rec.Body.String())
	}
	_, patchReqID := logsEnvelope(t, rec)
	assertLogsAudit(t, db, patchReqID, "logs."+item.ID)
	rec = logsReq(t, router, http.MethodPatch, "/api/v1/logs/"+item.ID, userA, uniqueLogsKey(),
		`{"content_status":"confirmed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm log = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = logsReq(t, router, http.MethodPatch, "/api/v1/logs/"+item.ID, userA, uniqueLogsKey(),
		`{"content":"不能再改"}`)
	if rec.Code != http.StatusForbidden || logsErrorCode(t, rec) != "log_not_draft" {
		t.Fatalf("update confirmed = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：缺 Idempotency-Key
	rec = logsReq(t, router, http.MethodPatch, "/api/v1/logs/"+item.ID, userA, "", `{"content":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update no idem = %d", rec.Code)
	}
}

func TestHandlerAiParseUnconfigured(t *testing.T) {
	db := openLogsHandlerDB(t)
	router := newLogsTestRouter(t, db)
	userA := logsToken(t, logsHUserA, "logs_h_usera", auth.RoleMember)

	reportID := seedHandlerReport(t, db, logsHUserA, "draft", "2099-06-20", "今天的工作内容", "")
	t.Cleanup(func() { db.Exec(`DELETE FROM daily_reports WHERE id = $1`, reportID) })
	path := "/api/v1/daily-reports/" + reportID + "/ai-parse"

	// 502：parser 未配置 → upstream_error + 语义 action daily_report.ai_parsed
	rec := logsReq(t, router, http.MethodPost, path, userA, uniqueLogsKey(), "")
	if rec.Code != http.StatusBadGateway || logsErrorCode(t, rec) != "upstream_error" {
		t.Fatalf("ai-parse unconfigured = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID := logsEnvelope(t, rec)
	assertLogsAudit(t, db, requestID, "daily_report.ai_parsed")
	// 400：缺 Idempotency-Key
	rec = logsReq(t, router, http.MethodPost, path, userA, "", "")
	if rec.Code != http.StatusBadRequest || logsErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("ai-parse no idem = %d body=%s", rec.Code, rec.Body.String())
	}
}

func seedHandlerReport(t *testing.T, db *sql.DB, author, status, date, raw, summary string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(
		`INSERT INTO daily_reports (report_date, author_id, raw_text, summary, content_status, quality_status)
		 VALUES ($1::date, $2, $3, $4, $5, 'unchecked')
		 RETURNING id`, date, author, raw, summary, status).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedHandlerLog(t *testing.T, db *sql.DB, project, author, status, occurredAt, content string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(
		`INSERT INTO logs (project_id, author_id, occurred_at, category, content, source, content_status)
		 VALUES ($1, $2, $3::timestamptz, 'assembly', $4, 'manual', $5)
		 RETURNING id`, project, author, occurredAt, content, status).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
