package runs

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
	"github.com/zhu571/hiaf-lab-system/go-server/steptemplates"
)

// handler 集成测试：与 main.go 一致挂 AuthRequired + Audit + RequireIdempotencyKey +
// RequireProjectPermission（experiment-runs 挂在 projects/{id} 下）中间件（真实 DB）。
// 审计断言走 Audit 中间件实际落库的 action（runs handler 均调用 SetAuditAction）。

const runHandlerTestSecret = "runs-handler-test-secret"

type runsResponseEnvelope struct {
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"request_id"`
}

type runsErrorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func newRunsTestRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(runHandlerTestSecret))
	projectsRepo := projects.NewRepository(db)
	svc := NewService(NewRepository(db), ProjectAccessAdapter{Repo: projectsRepo})
	svc.ConfigureTemplates(dbTemplateReader{repo: steptemplates.NewRepository(db)})
	h := NewHandler(svc)
	router := chi.NewRouter()
	router.Route("/api/v1/projects/{id}", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Use(middleware.RequireProjectPermission(db, middleware.PermRead))
		r.Get("/experiment-runs", h.List)
		r.Post("/experiment-runs", h.Create)
	})
	router.Route("/api/v1/experiment-runs/{id}", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", h.GetByID)
		r.Patch("/", h.Update)
		r.Delete("/", h.SoftDelete)
		r.Post("/daily-reports/{report_id}", h.AddReportLink)
		r.Delete("/daily-reports/{report_id}", h.RemoveReportLink)
		r.Get("/steps", h.HandleListSteps)
		r.Post("/steps", h.HandleCreateStep)
		r.Post("/steps/apply-template", h.HandleApplyTemplate)
	})
	router.Route("/api/v1/run-steps", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Post("/reorder", h.HandleReorderSteps)
		r.Route("/{id}", func(r chi.Router) {
			r.Patch("/", h.HandleUpdateStep)
			r.Delete("/", h.HandleDeleteStep)
		})
	})
	return router
}

func runsToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(runHandlerTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func runsRequest(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
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

func runsEnvelope(t *testing.T, rec *httptest.ResponseRecorder) runsResponseEnvelope {
	t.Helper()
	var envelope runsResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("body: %s, err=%v", rec.Body.String(), err)
	}
	return envelope
}

func runsErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope runsErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error body: %s, err=%v", rec.Body.String(), err)
	}
	return envelope.Error.Code
}

func assertRunAudit(t *testing.T, db *sql.DB, requestID, action string) {
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

func runsUniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("runs-h-%d", time.Now().UnixNano())
}

func createRunViaHandler(t *testing.T, router http.Handler, token string) (string, string) {
	t.Helper()
	rec := runsRequest(t, router, http.MethodPost, "/api/v1/projects/"+runTestProjectID+"/experiment-runs",
		token, runsUniqueKey(t), `{"name":"handler-run","campaign":"camp-h"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create run = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope := runsEnvelope(t, rec)
	var run struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data, &run); err != nil {
		t.Fatal(err)
	}
	return run.ID, envelope.RequestID
}

func TestHandlerRunCreate(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	viewer := runsToken(t, runViewerUserID, "viewer", auth.RoleMember)
	path := "/api/v1/projects/" + runTestProjectID + "/experiment-runs"

	// 403：viewer 无创建权限（RequireProjectPermission 放行 read，service 拒）
	rec := runsRequest(t, router, http.MethodPost, path, viewer, runsUniqueKey(t), `{"name":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
	// 400：缺 Idempotency-Key
	rec = runsRequest(t, router, http.MethodPost, path, owner, "", `{"name":"x"}`)
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idempotency key = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：请求体解析失败
	rec = runsRequest(t, router, http.MethodPost, path, owner, runsUniqueKey(t), `{not json`)
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("bad json = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 404：项目不存在（admin 绕过 RequireProjectPermission 直达 handler）
	admin := runsToken(t, runAdminUserID, "admin", auth.RoleAdmin)
	rec = runsRequest(t, router, http.MethodPost, "/api/v1/projects/b0000000-0000-4000-8000-000000009999/experiment-runs",
		admin, runsUniqueKey(t), `{"name":"x"}`)
	if rec.Code != http.StatusNotFound || runsErrorCode(t, rec) != "project_not_found" {
		t.Fatalf("missing project = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 403：非 admin 访问不存在项目（RequireProjectPermission 先行拒绝）
	rec = runsRequest(t, router, http.MethodPost, "/api/v1/projects/b0000000-0000-4000-8000-000000009999/experiment-runs",
		owner, runsUniqueKey(t), `{"name":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin missing project = %d, want 403", rec.Code)
	}
	// 201：创建成功 + audit 落库
	rec = runsRequest(t, router, http.MethodPost, path, owner, runsUniqueKey(t), `{"name":"h-proj-run-1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope := runsEnvelope(t, rec)
	var run struct {
		ID      string   `json:"id"`
		Status  string   `json:"status"`
		RunType string   `json:"run_type"`
		GasType string   `json:"gas_type"`
		HasBeam bool     `json:"has_beam"`
		Devices []string `json:"devices"`
		Owner   string   `json:"created_by"`
	}
	if err := json.Unmarshal(envelope.Data, &run); err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusPlanned || run.RunType != RunTypeTest || run.GasType != GasTypeHe ||
		run.HasBeam || len(run.Devices) != 0 || run.Owner != runOwnerUserID {
		t.Fatalf("created: %+v", run)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiment_runs WHERE id = $1`, run.ID) })
	assertRunAudit(t, db, envelope.RequestID, "experiment_run.create")

	// 401：无 token
	rec = runsRequest(t, router, http.MethodPost, path, "", runsUniqueKey(t), `{"name":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", rec.Code)
	}
}

func TestHandlerRunList(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	outsider := runsToken(t, runOutsiderUserID, "outsider", auth.RoleMember)
	createRunViaHandler(t, router, owner)
	createRunViaHandler(t, router, owner)
	path := "/api/v1/projects/" + runTestProjectID + "/experiment-runs"

	// 200：列表 + 过滤
	rec := runsRequest(t, router, http.MethodGet, path+"?status=planned", owner, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope := runsEnvelope(t, rec)
	var result struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || len(result.Items) != 2 {
		t.Fatalf("list result: %+v", result)
	}
	// 400：非法 status
	rec = runsRequest(t, router, http.MethodGet, path+"?status=bogus", owner, "", "")
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("bogus status = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 403：outsider
	rec = runsRequest(t, router, http.MethodGet, path, outsider, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list = %d, want 403", rec.Code)
	}
}

func TestHandlerRunGetByID(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	member := runsToken(t, runMemberUserID, "member", auth.RoleMember)
	outsider := runsToken(t, runOutsiderUserID, "outsider", auth.RoleMember)
	runID, _ := createRunViaHandler(t, router, owner)

	// 200：成员可读
	rec := runsRequest(t, router, http.MethodGet, "/api/v1/experiment-runs/"+runID, member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope := runsEnvelope(t, rec)
	var run struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data, &run); err != nil {
		t.Fatal(err)
	}
	if run.ID != runID {
		t.Fatalf("get run: %+v", run)
	}
	// 404：不存在
	rec = runsRequest(t, router, http.MethodGet, "/api/v1/experiment-runs/b0000000-0000-4000-8000-000000009999", member, "", "")
	if rec.Code != http.StatusNotFound || runsErrorCode(t, rec) != "experiment_run_not_found" {
		t.Fatalf("missing get = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 403：outsider
	rec = runsRequest(t, router, http.MethodGet, "/api/v1/experiment-runs/"+runID, outsider, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider get = %d, want 403", rec.Code)
	}
}

func TestHandlerRunUpdate(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	member := runsToken(t, runMemberUserID, "member", auth.RoleMember)
	runID, _ := createRunViaHandler(t, router, owner)
	path := "/api/v1/experiment-runs/" + runID

	// 403：非创建者 member 无 maintainer 权限
	rec := runsRequest(t, router, http.MethodPatch, path, member, runsUniqueKey(t), `{"name":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member update = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
	// 400：非法流转
	rec = runsRequest(t, router, http.MethodPatch, path, owner, runsUniqueKey(t), `{"transition":"complete"}`)
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "invalid_transition" {
		t.Fatalf("invalid transition = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：流转 + 元数据混用
	rec = runsRequest(t, router, http.MethodPatch, path, owner, runsUniqueKey(t), `{"transition":"start","name":"x"}`)
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("transition+metadata = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：缺 Idempotency-Key
	rec = runsRequest(t, router, http.MethodPatch, path, owner, "", `{"name":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idempotency = %d", rec.Code)
	}
	// 404：不存在（admin 直达 handler）
	admin := runsToken(t, runAdminUserID, "admin", auth.RoleAdmin)
	rec = runsRequest(t, router, http.MethodPatch, "/api/v1/experiment-runs/b0000000-0000-4000-8000-000000009999",
		admin, runsUniqueKey(t), `{"name":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing update = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
	// 200：元数据更新 + audit；流转更新 + audit
	rec = runsRequest(t, router, http.MethodPatch, path, owner, runsUniqueKey(t), `{"name":"handler-renamed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope := runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "experiment_run.update")
	var updated struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(envelope.Data, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "handler-renamed" {
		t.Fatalf("updated: %+v", updated)
	}
	rec = runsRequest(t, router, http.MethodPatch, path, owner, runsUniqueKey(t), `{"transition":"start"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("transition = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope = runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "experiment_run.update")
	var transitioned struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(envelope.Data, &transitioned); err != nil {
		t.Fatal(err)
	}
	if transitioned.Status != StatusActive {
		t.Fatalf("transitioned: %+v", transitioned)
	}
}

func TestHandlerRunDelete(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	member := runsToken(t, runMemberUserID, "member", auth.RoleMember)
	runID, _ := createRunViaHandler(t, router, owner)

	// 403：非创建者 member
	rec := runsRequest(t, router, http.MethodDelete, "/api/v1/experiment-runs/"+runID, member, runsUniqueKey(t), "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member delete = %d, want 403", rec.Code)
	}
	// 200：创建者删除 + audit
	rec = runsRequest(t, router, http.MethodDelete, "/api/v1/experiment-runs/"+runID, owner, runsUniqueKey(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope := runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "experiment_run.delete")
	// 404：重复删除
	rec = runsRequest(t, router, http.MethodDelete, "/api/v1/experiment-runs/"+runID, owner, runsUniqueKey(t), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("double delete = %d, want 404", rec.Code)
	}
}

func TestHandlerReportLinks(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	runID, _ := createRunViaHandler(t, router, owner)
	if _, err := db.Exec(
		`INSERT INTO daily_reports (id, report_date, author_id) VALUES ($1, '2026-08-01', $2)
		 ON CONFLICT (id) DO NOTHING`, runTestReportID, runOwnerUserID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM daily_reports WHERE id = $1`, runTestReportID) })
	path := "/api/v1/experiment-runs/" + runID + "/daily-reports/" + runTestReportID

	// 200：添加关联 + audit
	rec := runsRequest(t, router, http.MethodPost, path, owner, runsUniqueKey(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("add link = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope := runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "experiment_run.link.create")
	// 200：移除关联 + audit
	rec = runsRequest(t, router, http.MethodDelete, path, owner, runsUniqueKey(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove link = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope = runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "experiment_run.link.delete")
	// 404：移除不存在的关联
	rec = runsRequest(t, router, http.MethodDelete, path, owner, runsUniqueKey(t), "")
	if rec.Code != http.StatusNotFound || runsErrorCode(t, rec) != "report_link_not_found" {
		t.Fatalf("remove missing link = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRunSteps(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	member := runsToken(t, runMemberUserID, "member", auth.RoleMember)
	runID, _ := createRunViaHandler(t, router, owner)
	path := "/api/v1/experiment-runs/" + runID + "/steps"

	// 201：创建步骤 + audit（依赖缺省，order 自动 max+1）
	rec := runsRequest(t, router, http.MethodPost, path, member, runsUniqueKey(t), `{"name":"h-step-1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create step = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope := runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "run_step.create")
	var step struct {
		ID        string `json:"id"`
		StepOrder int    `json:"step_order"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(envelope.Data, &step); err != nil {
		t.Fatal(err)
	}
	if step.StepOrder != 1 || step.Status != StepStatusPlanned {
		t.Fatalf("created step: %+v", step)
	}
	// 400：空名
	rec = runsRequest(t, router, http.MethodPost, path, member, runsUniqueKey(t), `{"name":"  "}`)
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("empty name = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：坏 JSON
	rec = runsRequest(t, router, http.MethodPost, path, member, runsUniqueKey(t), `{oops`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", rec.Code)
	}
	// 404：依赖步骤不存在
	rec = runsRequest(t, router, http.MethodPost, path, member, runsUniqueKey(t),
		`{"name":"x","depends_on":"b0000000-0000-4000-8000-000000009999"}`)
	if rec.Code != http.StatusNotFound || runsErrorCode(t, rec) != "run_step_not_found" {
		t.Fatalf("missing dependency = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 200：列表
	rec = runsRequest(t, router, http.MethodGet, path, member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list steps = %d", rec.Code)
	}
	envelope = runsEnvelope(t, rec)
	var stepList struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(envelope.Data, &stepList); err != nil {
		t.Fatal(err)
	}
	if stepList.Total != 1 {
		t.Fatalf("step list total: %+v", stepList)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM run_steps WHERE id = $1`, step.ID) })
}

func TestHandlerRunStepUpdate(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	member := runsToken(t, runMemberUserID, "member", auth.RoleMember)
	runID, _ := createRunViaHandler(t, router, owner)
	path := "/api/v1/experiment-runs/" + runID + "/steps"
	rec := runsRequest(t, router, http.MethodPost, path, member, runsUniqueKey(t), `{"name":"su-1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create step = %d", rec.Code)
	}
	envelope := runsEnvelope(t, rec)
	var step struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data, &step); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM run_steps WHERE id = $1`, step.ID) })
	stepPath := "/api/v1/run-steps/" + step.ID

	// 403：非创建者 member 更新元数据（maintainer 语义）
	other := runsToken(t, runOutsiderUserID, "outsider", auth.RoleMember)
	rec = runsRequest(t, router, http.MethodPatch, stepPath, other, runsUniqueKey(t), `{"name":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider update step = %d, want 403", rec.Code)
	}
	// 400：空更新
	rec = runsRequest(t, router, http.MethodPatch, stepPath, member, runsUniqueKey(t), `{}`)
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("empty update = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：流转 + 元数据混用
	rec = runsRequest(t, router, http.MethodPatch, stepPath, member, runsUniqueKey(t), `{"transition":"start","name":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("transition+meta = %d", rec.Code)
	}
	// 200：owner（maintainer 级）改元数据 + audit（run_step.update；步骤元数据无创建者豁免）
	rec = runsRequest(t, router, http.MethodPatch, stepPath, owner, runsUniqueKey(t), `{"name":"su-renamed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update step = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope = runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "run_step.update")
	// 200：流转 start + audit（run_step.transition）
	rec = runsRequest(t, router, http.MethodPatch, stepPath, member, runsUniqueKey(t), `{"transition":"start"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("transition step = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope = runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "run_step.transition")
	var stepped struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(envelope.Data, &stepped); err != nil {
		t.Fatal(err)
	}
	if stepped.Status != StepStatusInProgress {
		t.Fatalf("stepped: %+v", stepped)
	}
	// 400：非法流转（in_progress → resume）
	rec = runsRequest(t, router, http.MethodPatch, stepPath, member, runsUniqueKey(t), `{"transition":"resume"}`)
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "invalid_transition" {
		t.Fatalf("invalid step transition = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 404：步骤不存在
	rec = runsRequest(t, router, http.MethodPatch, "/api/v1/run-steps/b0000000-0000-4000-8000-000000009999",
		member, runsUniqueKey(t), `{"name":"x"}`)
	if rec.Code != http.StatusNotFound || runsErrorCode(t, rec) != "run_step_not_found" {
		t.Fatalf("missing step = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRunStepDelete(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	member := runsToken(t, runMemberUserID, "member", auth.RoleMember)
	runID, _ := createRunViaHandler(t, router, owner)
	path := "/api/v1/experiment-runs/" + runID + "/steps"
	rec := runsRequest(t, router, http.MethodPost, path, member, runsUniqueKey(t), `{"name":"sd-1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create step = %d", rec.Code)
	}
	envelope := runsEnvelope(t, rec)
	var step struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data, &step); err != nil {
		t.Fatal(err)
	}
	stepPath := "/api/v1/run-steps/" + step.ID

	// 403：非创建者 outsider 删除（member 无权）
	other := runsToken(t, runOutsiderUserID, "outsider", auth.RoleMember)
	rec = runsRequest(t, router, http.MethodDelete, stepPath, other, runsUniqueKey(t), "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider delete step = %d, want 403", rec.Code)
	}
	// 200：创建者删除 + audit
	rec = runsRequest(t, router, http.MethodDelete, stepPath, member, runsUniqueKey(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete step = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope = runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "run_step.delete")
	// 404：重复删除
	rec = runsRequest(t, router, http.MethodDelete, stepPath, member, runsUniqueKey(t), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("double delete step = %d, want 404", rec.Code)
	}
	_ = owner
}

func TestHandlerRunStepReorder(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	member := runsToken(t, runMemberUserID, "member", auth.RoleMember)
	runID, _ := createRunViaHandler(t, router, owner)
	stepsPath := "/api/v1/experiment-runs/" + runID + "/steps"
	var ids []string
	for _, name := range []string{"re-1", "re-2"} {
		rec := runsRequest(t, router, http.MethodPost, stepsPath, member, runsUniqueKey(t), `{"name":"`+name+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s = %d", name, rec.Code)
		}
		envelope := runsEnvelope(t, rec)
		var step struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(envelope.Data, &step); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, step.ID)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM run_steps WHERE run_id = $1`, runID) })
	reorderPath := "/api/v1/run-steps/reorder"
	body := fmt.Sprintf(`{"run_id":%q,"steps":[{"id":%q,"step_order":2},{"id":%q,"step_order":1}]}`,
		runID, ids[0], ids[1])

	// 400：空 steps（run_id 需合法 UUID，先过 getAccessible）
	rec := runsRequest(t, router, http.MethodPost, reorderPath, owner, runsUniqueKey(t),
		fmt.Sprintf(`{"run_id":%q,"steps":[]}`, runID))
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("empty steps = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：坏 JSON
	rec = runsRequest(t, router, http.MethodPost, reorderPath, owner, runsUniqueKey(t), `{nope`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", rec.Code)
	}
	// 404：步骤不存在
	rec = runsRequest(t, router, http.MethodPost, reorderPath, owner, runsUniqueKey(t),
		`{"run_id":`+fmt.Sprintf("%q", runID)+`,"steps":[{"id":"b0000000-0000-4000-8000-000000009999","step_order":1}]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing step reorder = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 200：重排成功 + audit
	rec = runsRequest(t, router, http.MethodPost, reorderPath, owner, runsUniqueKey(t), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope := runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "run_step.reorder")
	// 落库校验：步骤顺序对调
	rec = runsRequest(t, router, http.MethodGet, stepsPath, member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list after reorder = %d", rec.Code)
	}
	envelope = runsEnvelope(t, rec)
	var list struct {
		Items []struct {
			ID        string `json:"id"`
			StepOrder int    `json:"step_order"`
		} `json:"items"`
	}
	if err := json.Unmarshal(envelope.Data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 || list.Items[0].ID != ids[1] || list.Items[1].ID != ids[0] {
		t.Fatalf("after reorder: %+v", list)
	}
}

func TestHandlerApplyTemplate(t *testing.T) {
	db := openRunsTestDB(t)
	router := newRunsTestRouter(t, db)
	owner := runsToken(t, runOwnerUserID, "owner", auth.RoleMember)
	seedRunTemplate(t, db)
	runID, _ := createRunViaHandler(t, router, owner)
	path := "/api/v1/experiment-runs/" + runID + "/steps/apply-template"
	t.Cleanup(func() { db.Exec(`DELETE FROM run_steps WHERE run_id = $1`, runID) })

	// 400：两者都给 → bad_request
	rec := runsRequest(t, router, http.MethodPost, path, owner, runsUniqueKey(t),
		`{"template_id":"`+runTestTemplateID+`","steps":[{"name":"x","step_order":1}]}`)
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("both sources = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：都不给
	rec = runsRequest(t, router, http.MethodPost, path, owner, runsUniqueKey(t), `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no source = %d", rec.Code)
	}
	// 201：内联步骤 + audit
	rec = runsRequest(t, router, http.MethodPost, path, owner, runsUniqueKey(t),
		`{"steps":[{"name":"inline-1","step_order":1},{"name":"inline-2","step_order":2}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("inline apply = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope := runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "run_step.template_applied")
	var steps []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(envelope.Data, &steps); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("inline steps: %+v", steps)
	}
	// 201：模板应用 + audit
	rec = runsRequest(t, router, http.MethodPost, path, owner, runsUniqueKey(t),
		`{"template_id":"`+runTestTemplateID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("template apply = %d, body=%s", rec.Code, rec.Body.String())
	}
	envelope = runsEnvelope(t, rec)
	assertRunAudit(t, db, envelope.RequestID, "run_step.template_applied")
	// 400：错误 kind 模板
	if _, err := db.Exec(`UPDATE step_templates SET kind = 'assembly' WHERE id = $1`, runTestTemplateID); err != nil {
		t.Fatal(err)
	}
	rec = runsRequest(t, router, http.MethodPost, path, owner, runsUniqueKey(t),
		`{"template_id":"`+runTestTemplateID+`"}`)
	if rec.Code != http.StatusBadRequest || runsErrorCode(t, rec) != "bad_request" {
		t.Fatalf("wrong kind = %d, body=%s", rec.Code, rec.Body.String())
	}
}
