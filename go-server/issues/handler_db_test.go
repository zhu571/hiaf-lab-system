package issues

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
// 注意：issues handler 目前没有任何 SetAuditAction 调用，审计 action 为 Audit 中间件
// 按路径派生（如 projects.{project_id}.issues / issues.{id} 等）；本文件按派生值断言，
// 语义化 action 缺失另见报告。

const issuesHandlerSecret = "issues-handler-test-secret"

const (
	issuesHOwnerID    = "00000000-0000-0000-0000-00000000b801"
	issuesHMemberID   = "00000000-0000-0000-0000-00000000b802"
	issuesHViewerID   = "00000000-0000-0000-0000-00000000b803"
	issuesHOutsiderID = "00000000-0000-0000-0000-00000000b804"
	issuesHAdminID    = "00000000-0000-0000-0000-00000000b805"
	issuesHAgentID    = "00000000-0000-0000-0000-00000000b806"

	issuesHProjectID    = "c0000000-0000-4000-8000-00000000c801"
	issuesHDraftProject = "c0000000-0000-4000-8000-00000000c802"
	issuesHReportID     = "c0000000-0000-4000-8000-00000000c803"
	issuesHTaskID       = "c0000000-0000-4000-8000-00000000c804"
	issuesHCandidateID  = "c0000000-0000-4000-8000-00000000c805"
)

func newIssuesTestRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(issuesHandlerSecret))
	svc := NewService(NewRepository(db), ProjectAccessAdapter{DB: db, Repo: projects.NewRepository(db)}, dbAgentTaskValidator{db: db})
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// 项目级 issue 端点（main.go /api/v1/projects/{id} 子路由）
	r.Route("/api/v1/projects/{id}", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Use(middleware.RequireProjectPermission(db, middleware.PermRead))
		r.Get("/issues", h.List)
		r.With(middleware.RequireProjectPermission(db, middleware.PermCreateIssue)).Post("/issues", h.Create)
	})
	// 独立 issue 端点（main.go /api/v1/issues）
	r.Route("/api/v1/issues", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetByID)
			r.Patch("/", h.Update)
			r.Post("/transition", h.Transition)
			r.Post("/comments", h.AddComment)
		})
	})
	return r
}

func openIssuesHandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openIssuesSvcDB(t)
	// 追加 handler 专属用户与项目（b801-b806 / c801-c802）
	for _, u := range []struct {
		id       string
		username string
		role     string
	}{
		{issuesHOwnerID, "issues_h_owner", "member"},
		{issuesHMemberID, "issues_h_member", "member"},
		{issuesHViewerID, "issues_h_viewer", "viewer"},
		{issuesHOutsiderID, "issues_h_outsider", "member"},
		{issuesHAdminID, "issues_h_admin", "admin"},
		{issuesHAgentID, "issues_h_agent", "agent"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Issues Handler Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []struct {
		id     string
		code   string
		status string
	}{
		{issuesHProjectID, "ISS_H_ACTIVE", "active"},
		{issuesHDraftProject, "ISS_H_DRAFT", "draft"},
	} {
		if _, err := db.Exec(
			`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
			 VALUES ($1, $2, 'Handler 种子项目', $3, $4, $4)
			 ON CONFLICT (id) DO NOTHING`, p.id, p.code, p.status, issuesHOwnerID); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []struct {
		userID string
		role   string
	}{
		{issuesHOwnerID, "owner"},
		{issuesHMemberID, "member"},
		{issuesHViewerID, "viewer"},
	} {
		if _, err := db.Exec(
			`INSERT INTO project_members (project_id, user_id, role, status, added_by)
			 VALUES ($1, $2, $3, 'active', $2)
			 ON CONFLICT (project_id, user_id) DO NOTHING`, issuesHProjectID, m.userID, m.role); err != nil {
			t.Fatal(err)
		}
	}
	// agent 任务链路（AgentContext 校验 + Create 的 agent 字段 FK）
	if _, err := db.Exec(
		`INSERT INTO daily_reports (id, report_date, author_id)
		 VALUES ($1, '2099-04-01', $2)
		 ON CONFLICT (id) DO NOTHING`, issuesHReportID, issuesHOwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO pending_agent_tasks (id, report_id, status, acting_user_id)
		 VALUES ($1, $2, 'processing', $3)
		 ON CONFLICT (id) DO NOTHING`, issuesHTaskID, issuesHReportID, issuesHOwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO agent_candidate_actions (id, task_id, action_type, project_id, pool_action_key, payload)
		 VALUES ($1, $2, 'create_issue', $3, $4, '{"title":"t"}'::jsonb)
		 ON CONFLICT (id) DO NOTHING`,
		issuesHCandidateID, issuesHTaskID, issuesHProjectID, issuesHTaskID+":create_issue:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM audit_log WHERE user_id IN ($1,$2,$3,$4,$5,$6)`,
			issuesHOwnerID, issuesHMemberID, issuesHViewerID, issuesHOutsiderID, issuesHAdminID, issuesHAgentID)
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id IN (SELECT id FROM issues WHERE project_id IN ($1,$2))`,
			issuesHProjectID, issuesHDraftProject)
		db.Exec(`DELETE FROM issues WHERE project_id IN ($1,$2)`, issuesHProjectID, issuesHDraftProject)
		db.Exec(`DELETE FROM agent_candidate_actions WHERE id = $1`, issuesHCandidateID)
		db.Exec(`DELETE FROM pending_agent_tasks WHERE id = $1`, issuesHTaskID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, issuesHReportID)
		db.Exec(`DELETE FROM project_members WHERE project_id IN ($1,$2)`, issuesHProjectID, issuesHDraftProject)
		db.Exec(`DELETE FROM projects WHERE id IN ($1,$2)`, issuesHProjectID, issuesHDraftProject)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4,$5,$6)`,
			issuesHOwnerID, issuesHMemberID, issuesHViewerID, issuesHOutsiderID, issuesHAdminID, issuesHAgentID)
	})
	return db
}

func issuesToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(issuesHandlerSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func issuesReq(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
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

func issuesEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (json.RawMessage, string) {
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

func issuesErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
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

// dbAgentTaskValidator 复刻 main.go 注入的 agentSvc.ValidateAgentTask 语义：
// 任务存在、status='processing' 且 acting_user_id 匹配。
type dbAgentTaskValidator struct{ db *sql.DB }

func (v dbAgentTaskValidator) ValidateAgentTask(taskID, actingUserID string) (bool, error) {
	var valid bool
	if err := v.db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM pending_agent_tasks WHERE id = $1 AND acting_user_id = $2 AND status = 'processing')`,
		taskID, actingUserID).Scan(&valid); err != nil {
		return false, err
	}
	return valid, nil
}

func uniqueIssuesKey() string {
	return fmt.Sprintf("iss-h-%d", time.Now().UnixNano())
}

func assertIssuesAudit(t *testing.T, db *sql.DB, requestID, action string) {
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

func TestHandlerIssueCreate(t *testing.T) {
	db := openIssuesHandlerDB(t)
	router := newIssuesTestRouter(t, db)
	owner := issuesToken(t, issuesHOwnerID, "issues_h_owner", auth.RoleMember)
	viewer := issuesToken(t, issuesHViewerID, "issues_h_viewer", auth.RoleMember)
	path := "/api/v1/projects/" + issuesHProjectID + "/issues"

	// 403：viewer 无 create_issue 权限
	rec := issuesReq(t, router, http.MethodPost, path, viewer, uniqueIssuesKey(), `{"title":"v"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
	// 400：缺 Idempotency-Key
	rec = issuesReq(t, router, http.MethodPost, path, owner, "", `{"title":"v"}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idem = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：坏 JSON / 空标题
	rec = issuesReq(t, router, http.MethodPost, path, owner, uniqueIssuesKey(), `{bad`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", rec.Code)
	}
	rec = issuesReq(t, router, http.MethodPost, path, owner, uniqueIssuesKey(), `{"title":"  "}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "bad_request" {
		t.Fatalf("empty title = %d body=%s", rec.Code, rec.Body.String())
	}
	// 404：项目不存在（admin 绕过项目权限直达 service）
	admin := issuesToken(t, issuesHAdminID, "issues_h_admin", auth.RoleAdmin)
	rec = issuesReq(t, router, http.MethodPost, "/api/v1/projects/b0000000-0000-4000-8000-000000009999/issues", admin, uniqueIssuesKey(), `{"title":"v"}`)
	if rec.Code != http.StatusNotFound || issuesErrorCode(t, rec) != "project_not_found" {
		t.Fatalf("missing project = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：draft 项目生命周期阻塞
	rec = issuesReq(t, router, http.MethodPost, "/api/v1/projects/"+issuesHDraftProject+"/issues", admin, uniqueIssuesKey(), `{"title":"v"}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "project_lifecycle_blocked" {
		t.Fatalf("draft project = %d body=%s", rec.Code, rec.Body.String())
	}
	// 201：创建成功 + 审计（path 派生 action）
	key := uniqueIssuesKey()
	rec = issuesReq(t, router, http.MethodPost, path, owner, key,
		`{"title":"冷却回路压力偏高","severity":"high","description":"pressure drift"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", rec.Code, rec.Body.String())
	}
	data, requestID := issuesEnvelope(t, rec)
	var created struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		Severity  string `json:"severity"`
		ProjectID string `json:"project_id"`
		AuthorID  string `json:"author_id"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	if created.Title != "冷却回路压力偏高" || created.Status != StatusOpen || created.Severity != SeverityHigh ||
		created.ProjectID != issuesHProjectID || created.AuthorID != issuesHOwnerID {
		t.Fatalf("created: %+v", created)
	}
	assertIssuesAudit(t, db, requestID, "projects."+issuesHProjectID+".issues")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created.ID)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created.ID)
	})
	// 401：无 token
	rec = issuesReq(t, router, http.MethodPost, path, "", uniqueIssuesKey(), `{"title":"v"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", rec.Code)
	}
}

func TestHandlerIssueListGet(t *testing.T) {
	db := openIssuesHandlerDB(t)
	router := newIssuesTestRouter(t, db)
	member := issuesToken(t, issuesHMemberID, "issues_h_member", auth.RoleMember)
	outsider := issuesToken(t, issuesHOutsiderID, "issues_h_outsider", auth.RoleMember)
	path := "/api/v1/projects/" + issuesHProjectID + "/issues"

	// 403：outsider 无成员关系
	rec := issuesReq(t, router, http.MethodGet, path, outsider, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list = %d, want 403", rec.Code)
	}
	// 200：member 列表（默认 open → 空）
	rec = issuesReq(t, router, http.MethodGet, path, member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d body=%s", rec.Code, rec.Body.String())
	}
	data, _ := issuesEnvelope(t, rec)
	var list struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 0 {
		t.Fatalf("empty list total = %d", list.Total)
	}
	// 400：非法 status 过滤
	rec = issuesReq(t, router, http.MethodGet, path+"?status=bogus", member, "", "")
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "bad_request" {
		t.Fatalf("bad status = %d body=%s", rec.Code, rec.Body.String())
	}

	// 种子一条 open issue 后：列表命中 + get 200 / 404 / 403
	created := seedIssueIn(t, db, issuesHProjectID, "open", SeverityLow, "")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created)
	})
	rec = issuesReq(t, router, http.MethodGet, path, member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	rec = issuesReq(t, router, http.MethodGet, "/api/v1/issues/"+created, member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d body=%s", rec.Code, rec.Body.String())
	}
	data, _ = issuesEnvelope(t, rec)
	var issue struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &issue); err != nil {
		t.Fatal(err)
	}
	if issue.ID != created || issue.Status != StatusOpen {
		t.Fatalf("get issue: %+v", issue)
	}
	// 403：outsider get
	rec = issuesReq(t, router, http.MethodGet, "/api/v1/issues/"+created, outsider, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider get = %d, want 403", rec.Code)
	}
	// 404：不存在（admin 直达 service）
	admin := issuesToken(t, issuesHAdminID, "issues_h_admin", auth.RoleAdmin)
	rec = issuesReq(t, router, http.MethodGet, "/api/v1/issues/b0000000-0000-4000-8000-000000009999", admin, "", "")
	if rec.Code != http.StatusNotFound || issuesErrorCode(t, rec) != "issue_not_found" {
		t.Fatalf("missing get = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerIssueUpdate(t *testing.T) {
	db := openIssuesHandlerDB(t)
	router := newIssuesTestRouter(t, db)
	member := issuesToken(t, issuesHMemberID, "issues_h_member", auth.RoleMember)
	viewer := issuesToken(t, issuesHViewerID, "issues_h_viewer", auth.RoleMember)

	created := seedIssueIn(t, db, issuesHProjectID, "open", SeverityMedium, "")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created)
	})
	path := "/api/v1/issues/" + created

	// 400：缺 Idempotency-Key
	rec := issuesReq(t, router, http.MethodPatch, path, member, "", `{"title":"x"}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idem = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：ai_generated 不可变
	rec = issuesReq(t, router, http.MethodPatch, path, member, uniqueIssuesKey(), `{"ai_generated":true}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "bad_request" {
		t.Fatalf("ai_generated mutate = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：agent_task_id 不可变
	rec = issuesReq(t, router, http.MethodPatch, path, member, uniqueIssuesKey(), `{"agent_task_id":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("agent_task_id mutate = %d body=%s", rec.Code, rec.Body.String())
	}
	// 403：viewer 无 update_issue 权限
	rec = issuesReq(t, router, http.MethodPatch, path, viewer, uniqueIssuesKey(), `{"title":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer update = %d, want 403", rec.Code)
	}
	// 200：改标题 + 严重级别 + 审计
	rec = issuesReq(t, router, http.MethodPatch, path, member, uniqueIssuesKey(),
		`{"title":"更新标题","severity":"critical"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID := issuesEnvelope(t, rec)
	assertIssuesAudit(t, db, requestID, "issues."+created)
	// 400：closed 不可改
	if _, err := db.Exec(`UPDATE issues SET status = 'closed' WHERE id = $1`, created); err != nil {
		t.Fatal(err)
	}
	rec = issuesReq(t, router, http.MethodPatch, path, member, uniqueIssuesKey(), `{"title":"x"}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "issue_closed" {
		t.Fatalf("closed update = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerIssueTransition(t *testing.T) {
	db := openIssuesHandlerDB(t)
	router := newIssuesTestRouter(t, db)
	member := issuesToken(t, issuesHMemberID, "issues_h_member", auth.RoleMember)
	viewer := issuesToken(t, issuesHViewerID, "issues_h_viewer", auth.RoleMember)

	created := seedIssueIn(t, db, issuesHProjectID, "open", SeverityMedium, issuesHMemberID)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created)
	})
	path := "/api/v1/issues/" + created + "/transition"

	// 400：缺 Idempotency-Key
	rec := issuesReq(t, router, http.MethodPost, path, member, "", `{"target_status":"in_progress"}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idem = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：非法目标状态
	rec = issuesReq(t, router, http.MethodPost, path, member, uniqueIssuesKey(), `{"target_status":"bogus"}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "bad_request" {
		t.Fatalf("bad target = %d body=%s", rec.Code, rec.Body.String())
	}
	// 403：viewer
	rec = issuesReq(t, router, http.MethodPost, path, viewer, uniqueIssuesKey(), `{"target_status":"in_progress"}`)
	if rec.Code != http.StatusForbidden || issuesErrorCode(t, rec) != "permission_denied" {
		t.Fatalf("viewer transition = %d body=%s", rec.Code, rec.Body.String())
	}
	// 200：open → in_progress，审计 issues.{id} 派生 action
	rec = issuesReq(t, router, http.MethodPost, path, member, uniqueIssuesKey(), `{"target_status":"in_progress","reason":"开始","add_comment":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("transition = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID := issuesEnvelope(t, rec)
	assertIssuesAudit(t, db, requestID, "issues."+created+".transition")
	// 400：同状态 → invalid_transition
	rec = issuesReq(t, router, http.MethodPost, path, member, uniqueIssuesKey(), `{"target_status":"in_progress"}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "invalid_transition" {
		t.Fatalf("same status = %d body=%s", rec.Code, rec.Body.String())
	}
	// 200：in_progress → resolved
	rec = issuesReq(t, router, http.MethodPost, path, member, uniqueIssuesKey(), `{"target_status":"resolved"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：resolved → open 缺 reason
	rec = issuesReq(t, router, http.MethodPost, path, member, uniqueIssuesKey(), `{"target_status":"open"}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "reason_required" {
		t.Fatalf("reopen no reason = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerIssueComments(t *testing.T) {
	db := openIssuesHandlerDB(t)
	router := newIssuesTestRouter(t, db)
	member := issuesToken(t, issuesHMemberID, "issues_h_member", auth.RoleMember)

	created := seedIssueIn(t, db, issuesHProjectID, "open", SeverityMedium, "")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created)
	})
	path := "/api/v1/issues/" + created + "/comments"

	// 400：空内容
	rec := issuesReq(t, router, http.MethodPost, path, member, uniqueIssuesKey(), `{"content":"  "}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "bad_request" {
		t.Fatalf("empty content = %d body=%s", rec.Code, rec.Body.String())
	}
	// 201：评论成功 + 审计（path 派生）
	rec = issuesReq(t, router, http.MethodPost, path, member, uniqueIssuesKey(), `{"content":"收到，明天检查"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment = %d body=%s", rec.Code, rec.Body.String())
	}
	data, requestID := issuesEnvelope(t, rec)
	assertIssuesAudit(t, db, requestID, "issues."+created+".comments")
	var comment struct {
		Content string `json:"content"`
		IssueID string `json:"issue_id"`
	}
	if err := json.Unmarshal(data, &comment); err != nil {
		t.Fatal(err)
	}
	if comment.Content != "收到，明天检查" || comment.IssueID != created {
		t.Fatalf("comment: %+v", comment)
	}
	// 404：issue 不存在（admin 直达）
	admin := issuesToken(t, issuesHAdminID, "issues_h_admin", auth.RoleAdmin)
	rec = issuesReq(t, router, http.MethodPost, "/api/v1/issues/b0000000-0000-4000-8000-000000009999/comments", admin, uniqueIssuesKey(), `{"content":"x"}`)
	if rec.Code != http.StatusNotFound || issuesErrorCode(t, rec) != "issue_not_found" {
		t.Fatalf("missing comment = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：缺 Idempotency-Key
	rec = issuesReq(t, router, http.MethodPost, path, member, "", `{"content":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idem = %d", rec.Code)
	}
}

func TestHandlerIssueAgentFlow(t *testing.T) {
	db := openIssuesHandlerDB(t)
	router := newIssuesTestRouter(t, db)
	agent := issuesToken(t, issuesHAgentID, "issues_h_agent", auth.RoleAgent)
	member := issuesToken(t, issuesHMemberID, "issues_h_member", auth.RoleMember)
	path := "/api/v1/projects/" + issuesHProjectID + "/issues"

	// 普通用户携带 agent 代理头 → 400 invalid_agent_context
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"title":"x"}`))
	req.Header.Set("Authorization", "Bearer "+member)
	req.Header.Set("Idempotency-Key", uniqueIssuesKey())
	req.Header.Set("X-Acting-User-ID", issuesHOwnerID)
	req.Header.Set("X-Agent-Task-ID", issuesHTaskID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "invalid_agent_context" {
		t.Fatalf("member with agent headers = %d body=%s", rec.Code, rec.Body.String())
	}

	// agent 缺代理头 → 400 invalid_agent_context
	rec = issuesReq(t, router, http.MethodPost, path, agent, uniqueIssuesKey(), `{"title":"x"}`)
	if rec.Code != http.StatusBadRequest || issuesErrorCode(t, rec) != "invalid_agent_context" {
		t.Fatalf("agent missing headers = %d body=%s", rec.Code, rec.Body.String())
	}

	// agent 合法创建：ai_generated 落库（acting_user 为 owner，具 create_issue 权限）
	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(
		`{"title":"AI 识别的压力异常","ai_generated":true,"agent_task_id":"`+issuesHTaskID+`","candidate_id":"`+issuesHCandidateID+`"}`))
	req.Header.Set("Authorization", "Bearer "+agent)
	req.Header.Set("Idempotency-Key", uniqueIssuesKey())
	req.Header.Set("X-Acting-User-ID", issuesHOwnerID)
	req.Header.Set("X-Agent-Task-ID", issuesHTaskID)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create = %d body=%s", rec.Code, rec.Body.String())
	}
	data, _ := issuesEnvelope(t, rec)
	var created struct {
		ID          string `json:"id"`
		AiGenerated bool   `json:"ai_generated"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	if !created.AiGenerated {
		t.Fatalf("agent created issue must be ai_generated: %+v", created)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created.ID)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created.ID)
	})
}
