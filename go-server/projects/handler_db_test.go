package projects

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
)

// handler 集成测试：与 main.go 一致挂 AuthRequired + RequireIdempotencyKey + Audit +
// RequireProjectPermission/RequireRole 中间件（真实 DB，TEST_DATABASE_URL 空则跳过）。
// handler 层只断言状态码 + 响应体；审计写入由 Audit 中间件负责，此处顺带校验 audit_log 落库。
// 注意：生产 projects handler 写端点调用 SetAuditAction 登记语义化 action
// （projects.create / projects.update / projects.transition / projects.members.add 等），
// 断言必须与生产实际落库值一致；path 派生 action 在成员删除等长路径下会超出
// audit_log.action varchar(64) 截断（22001），这也是写端点必须 SetAuditAction 的原因。

const handlerTestSecret = "projects-handler-test-secret"

func newProjectsTestRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(handlerTestSecret))
	svc := NewService(NewRepository(db), nil, nil)
	h := NewHandler(svc)
	router := chi.NewRouter()
	router.Route("/api/v1/projects", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", h.List)
		r.With(middleware.RequireRole(auth.RoleAdmin, auth.RoleMaintainer)).Post("/", h.Create)
		r.Route("/{id}", func(r chi.Router) {
			r.Use(middleware.RequireProjectPermission(db, middleware.PermRead))
			r.Get("/", h.GetByID)
			r.Get("/members", h.ListMembers)
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireProjectPermission(db, middleware.PermManageProject))
				r.Patch("/", h.Update)
				r.Post("/transition", h.TransitionStatus)
			})
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireProjectPermission(db, middleware.PermManageMembers))
				r.Post("/members", h.AddMember)
				r.Patch("/members/{userID}", h.UpdateMemberRole)
				r.Delete("/members/{userID}", h.RemoveMember)
			})
		})
	})
	return router
}

type responseEnvelope struct {
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"request_id"`
}

type handlerErrorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func projectsToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(handlerTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func projectsRequest(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeProjectsError(t *testing.T, rec *httptest.ResponseRecorder) handlerErrorEnvelope {
	t.Helper()
	var envelope handlerErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error body: %s, err=%v", rec.Body.String(), err)
	}
	return envelope
}

func assertAuditWritten(t *testing.T, db *sql.DB, requestID, action string) {
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

func uniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("prj-h-%d", time.Now().UnixNano())
}

func TestHandlerCreateProject(t *testing.T) {
	db := openProjectsTestDB(t)
	router := newProjectsTestRouter(t, db)
	owner := projectsToken(t, dbOwnerUserID, "owner", auth.RoleMaintainer)
	viewer := projectsToken(t, dbMemberUserID, "member", auth.RoleMember)

	// 403：viewer/member 角色被 RequireRole 拦截
	rec := projectsRequest(t, router, http.MethodPost, "/api/v1/projects", viewer, uniqueKey(t),
		`{"code":"H1","name":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}

	// 400：缺 Idempotency-Key
	rec = projectsRequest(t, router, http.MethodPost, "/api/v1/projects", owner, "",
		`{"code":"H2","name":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idempotency key = %d, want 400", rec.Code)
	}
	if code := decodeProjectsError(t, rec).Error.Code; code != "missing_idempotency_key" {
		t.Fatalf("code = %q", code)
	}

	// 400：请求体解析失败
	rec = projectsRequest(t, router, http.MethodPost, "/api/v1/projects", owner, uniqueKey(t), `{not json`)
	if rec.Code != http.StatusBadRequest || decodeProjectsError(t, rec).Error.Code != "bad_request" {
		t.Fatalf("bad json = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 400：空 name → ErrInvalidInput
	rec = projectsRequest(t, router, http.MethodPost, "/api/v1/projects", owner, uniqueKey(t),
		`{"code":"H3","name":"  "}`)
	if rec.Code != http.StatusBadRequest || decodeProjectsError(t, rec).Error.Code != "bad_request" {
		t.Fatalf("empty name = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 409：code 已占用（种子项目 PRJ_DBTEST_DRAFT）
	rec = projectsRequest(t, router, http.MethodPost, "/api/v1/projects", owner, uniqueKey(t),
		`{"code":"PRJ_DBTEST_DRAFT","name":"重复"}`)
	if rec.Code != http.StatusConflict || decodeProjectsError(t, rec).Error.Code != "project_code_taken" {
		t.Fatalf("code taken = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 201：创建成功，断言默认值与 audit 落库
	key := uniqueKey(t)
	rec = projectsRequest(t, router, http.MethodPost, "/api/v1/projects", owner, key,
		`{"code":"H-PROJ-1","name":"处理器项目","visibility":"workspace","tags":["a"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID         string   `json:"id"`
		Code       string   `json:"code"`
		Status     string   `json:"status"`
		Visibility string   `json:"visibility"`
		OwnerUser  string   `json:"owner_user_id"`
		Tags       []string `json:"tags"`
	}
	if err := json.Unmarshal(envelope.Data, &created); err != nil {
		t.Fatal(err)
	}
	if created.Code != "H-PROJ-1" || created.Status != StatusDraft || created.Visibility != VisibilityWorkspace ||
		created.OwnerUser != dbOwnerUserID || len(created.Tags) != 1 {
		t.Fatalf("created: %+v", created)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM projects WHERE id = $1`, created.ID) })
	assertAuditWritten(t, db, envelope.RequestID, "projects.create")

	// 401：无 token
	rec = projectsRequest(t, router, http.MethodPost, "/api/v1/projects", "", uniqueKey(t), `{"code":"H4","name":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", rec.Code)
	}
}

func TestHandlerListProjects(t *testing.T) {
	db := openProjectsTestDB(t)
	router := newProjectsTestRouter(t, db)
	member := projectsToken(t, dbMemberUserID, "member", auth.RoleMember)

	// 200：member 只看到成员项目（种子 draft）
	rec := projectsRequest(t, router, http.MethodGet, "/api/v1/projects", member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var items []struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		MemberCount  int    `json:"member_count"`
		OpenIssueCnt int    `json:"open_issue_count"`
	}
	if err := json.Unmarshal(envelope.Data, &items); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range items {
		if p.ID == dbDraftProjectID {
			found = true
			if p.Status != StatusDraft || p.MemberCount != 2 {
				t.Fatalf("draft project stats: %+v", p)
			}
		}
	}
	if !found {
		t.Fatalf("member list missing draft project: %+v", items)
	}

	// 200：status 过滤 + 空结果
	rec = projectsRequest(t, router, http.MethodGet, "/api/v1/projects?status=archived", member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered list = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	items = items[:0]
	if err := json.Unmarshal(envelope.Data, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("archived filter for member: %+v", items)
	}

	// 400：非法 status
	rec = projectsRequest(t, router, http.MethodGet, "/api/v1/projects?status=bogus", member, "", "")
	if rec.Code != http.StatusBadRequest || decodeProjectsError(t, rec).Error.Code != "bad_request" {
		t.Fatalf("bogus status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerGetByID(t *testing.T) {
	db := openProjectsTestDB(t)
	router := newProjectsTestRouter(t, db)
	member := projectsToken(t, dbMemberUserID, "member", auth.RoleMember)
	outsider := projectsToken(t, dbOutsiderUserID, "outsider", auth.RoleMember)

	// 200：详情 + 响应体断言
	rec := projectsRequest(t, router, http.MethodGet, "/api/v1/projects/"+dbDraftProjectID, member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var project struct {
		ID           string `json:"id"`
		Code         string `json:"code"`
		Status       string `json:"status"`
		CommentPlcy  string `json:"comment_policy"`
		MemberCount  int    `json:"member_count"`
		OpenIssueCnt int    `json:"open_issue_count"`
	}
	if err := json.Unmarshal(envelope.Data, &project); err != nil {
		t.Fatal(err)
	}
	if project.ID != dbDraftProjectID || project.Code != "PRJ_DBTEST_DRAFT" ||
		project.Status != StatusDraft || project.CommentPlcy != CommentPolicyMembers || project.MemberCount != 2 {
		t.Fatalf("project: %+v", project)
	}

	// 403：无成员关系
	rec = projectsRequest(t, router, http.MethodGet, "/api/v1/projects/"+dbDraftProjectID, outsider, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider get = %d, want 403", rec.Code)
	}

	// 404：不存在（admin 绕过 RequireProjectPermission 直达 handler，触发 ErrProjectNotFound）
	admin := projectsToken(t, dbAdminUserID, "admin", auth.RoleAdmin)
	rec = projectsRequest(t, router, http.MethodGet, "/api/v1/projects/b0000000-0000-4000-8000-000000009999", admin, "", "")
	if rec.Code != http.StatusNotFound || decodeProjectsError(t, rec).Error.Code != "project_not_found" {
		t.Fatalf("missing get = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerUpdateProject(t *testing.T) {
	db := openProjectsTestDB(t)
	router := newProjectsTestRouter(t, db)
	owner := projectsToken(t, dbOwnerUserID, "owner", auth.RoleMember)
	member := projectsToken(t, dbMemberUserID, "member", auth.RoleMember)

	// 403：member 无 manage_project 权限
	rec := projectsRequest(t, router, http.MethodPatch, "/api/v1/projects/"+dbDraftProjectID, member, uniqueKey(t),
		`{"name":"改名"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member update = %d, want 403", rec.Code)
	}

	// 400：非法日期
	rec = projectsRequest(t, router, http.MethodPatch, "/api/v1/projects/"+dbDraftProjectID, owner, uniqueKey(t),
		`{"start_date":"2026/01/01"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad date = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 200：改名成功 + 响应体断言 + audit 落库
	key := uniqueKey(t)
	rec = projectsRequest(t, router, http.MethodPatch, "/api/v1/projects/"+dbDraftProjectID, owner, key,
		`{"name":"处理器改名项目","short_name":"改名"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var updated struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(envelope.Data, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "处理器改名项目" {
		t.Fatalf("updated name: %+v", updated)
	}
	assertAuditWritten(t, db, envelope.RequestID, "projects.update")

	// 404：不存在（admin 绕过 RequireProjectPermission 直达 handler，触发 ErrProjectNotFound）
	admin := projectsToken(t, dbAdminUserID, "admin", auth.RoleAdmin)
	rec = projectsRequest(t, router, http.MethodPatch, "/api/v1/projects/b0000000-0000-4000-8000-000000009999", admin, uniqueKey(t),
		`{"name":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing update = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerTransitionStatus(t *testing.T) {
	db := openProjectsTestDB(t)
	router := newProjectsTestRouter(t, db)
	owner := projectsToken(t, dbOwnerUserID, "owner", auth.RoleMember)
	member := projectsToken(t, dbMemberUserID, "member", auth.RoleMember)

	// 403：member 非 owner，service 层 requireOwnerOrAdmin 拒绝
	rec := projectsRequest(t, router, http.MethodPost, "/api/v1/projects/"+dbDraftProjectID+"/transition", member, uniqueKey(t),
		`{"action":"activate"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member transition = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}

	// 400：非法流转（draft 上 complete）
	rec = projectsRequest(t, router, http.MethodPost, "/api/v1/projects/"+dbDraftProjectID+"/transition", owner, uniqueKey(t),
		`{"action":"complete"}`)
	if rec.Code != http.StatusBadRequest || decodeProjectsError(t, rec).Error.Code != "invalid_transition" {
		t.Fatalf("invalid transition = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 200：draft → active，断言响应体与审计
	key := uniqueKey(t)
	rec = projectsRequest(t, router, http.MethodPost, "/api/v1/projects/"+dbDraftProjectID+"/transition", owner, key,
		`{"action":"activate"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("transition = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Project struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"project"`
		Warnings []json.RawMessage `json:"warnings"`
	}
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Project.ID != dbDraftProjectID || result.Project.Status != StatusActive || len(result.Warnings) != 0 {
		t.Fatalf("transition result: %+v", result)
	}
	assertAuditWritten(t, db, envelope.RequestID, "projects.transition")

	// 400：reactivate 缺 reason（archived 种子项目）
	rec = projectsRequest(t, router, http.MethodPost, "/api/v1/projects/"+dbArchivedProjectID+"/transition", owner, uniqueKey(t),
		`{"action":"reactivate"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reactivate no reason = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 400：缺 Idempotency-Key
	rec = projectsRequest(t, router, http.MethodPost, "/api/v1/projects/"+dbDraftProjectID+"/transition", owner, "",
		`{"action":"complete"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idempotency key = %d, want 400", rec.Code)
	}
}

func TestHandlerMembers(t *testing.T) {
	db := openProjectsTestDB(t)
	router := newProjectsTestRouter(t, db)
	owner := projectsToken(t, dbOwnerUserID, "owner", auth.RoleMember)
	member := projectsToken(t, dbMemberUserID, "member", auth.RoleMember)
	path := "/api/v1/projects/" + dbDraftProjectID

	// ListMembers：403（member 无 read？member 有 read，outsider 无成员关系才 403）—— 用 outsider 测 403
	outsider := projectsToken(t, dbOutsiderUserID, "outsider", auth.RoleMember)
	rec := projectsRequest(t, router, http.MethodGet, path+"/members", outsider, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list members = %d, want 403", rec.Code)
	}

	// 200：成员列表 3 人（owner + member 种子 + 本次新增 viewer）
	key := uniqueKey(t)
	rec = projectsRequest(t, router, http.MethodPost, path+"/members", owner, key,
		`{"user_id":"`+dbOutsiderUserID+`","role":"viewer"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add member = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var added struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.Unmarshal(envelope.Data, &added); err != nil {
		t.Fatal(err)
	}
	if added.UserID != dbOutsiderUserID || added.Role != RoleViewer {
		t.Fatalf("added: %+v", added)
	}
	assertAuditWritten(t, db, envelope.RequestID, "projects.members.add")

	// 400：非法角色
	rec = projectsRequest(t, router, http.MethodPost, path+"/members", owner, uniqueKey(t),
		`{"user_id":"`+dbOutsiderUserID+`","role":"boss"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad role = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 404：用户不存在
	rec = projectsRequest(t, router, http.MethodPost, path+"/members", owner, uniqueKey(t),
		`{"user_id":"b0000000-0000-4000-8000-000000009999","role":"member"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing user = %d, want 404", rec.Code)
	}

	// 200：列表 3 人
	rec = projectsRequest(t, router, http.MethodGet, path+"/members", owner, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list members = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var members []struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.Unmarshal(envelope.Data, &members); err != nil {
		t.Fatal(err)
	}
	if len(members) != 3 {
		t.Fatalf("members: %+v", members)
	}

	// 200：viewer → member 升职
	rec = projectsRequest(t, router, http.MethodPatch, path+"/members/"+dbOutsiderUserID, owner, uniqueKey(t),
		`{"role":"member"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update role = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var upgraded struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(envelope.Data, &upgraded); err != nil {
		t.Fatal(err)
	}
	if upgraded.Role != RoleMember {
		t.Fatalf("upgraded: %+v", upgraded)
	}
	assertAuditWritten(t, db, envelope.RequestID, "projects.members.update")

	// 400：降级最后 owner → last_owner
	rec = projectsRequest(t, router, http.MethodPatch, path+"/members/"+dbOwnerUserID, owner, uniqueKey(t),
		`{"role":"member"}`)
	if rec.Code != http.StatusBadRequest || decodeProjectsError(t, rec).Error.Code != "last_owner" {
		t.Fatalf("demote last owner = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 403：member 无 manage_members 权限
	rec = projectsRequest(t, router, http.MethodDelete, path+"/members/"+dbOutsiderUserID, member, uniqueKey(t), "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member remove = %d, want 403", rec.Code)
	}

	// 200：移除 outsider，响应体 success=true
	key = uniqueKey(t)
	rec = projectsRequest(t, router, http.MethodDelete, path+"/members/"+dbOutsiderUserID, owner, key, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove = %d, body=%s", rec.Code, rec.Body.String())
	}
	var okEnvelope struct {
		Data struct {
			Success bool `json:"success"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &okEnvelope); err != nil {
		t.Fatal(err)
	}
	if !okEnvelope.Data.Success {
		t.Fatalf("remove success: %+v", okEnvelope)
	}
	assertAuditWritten(t, db, okEnvelope.RequestID, "projects.members.remove")
}
