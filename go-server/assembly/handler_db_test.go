package assembly

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
// assembly handler 全部写端点调用 SetAuditAction（assembly.create/update/transition/
// transition.override/reorder/delete/template_applied），逐一生断言义 action 落库。

const asmHandlerSecret = "assembly-handler-test-secret"

const (
	asmHOwnerID    = "00000000-0000-0000-0000-00000000bc01"
	asmHMemberID   = "00000000-0000-0000-0000-00000000bc02"
	asmHViewerID   = "00000000-0000-0000-0000-00000000bc03"
	asmHOutsiderID = "00000000-0000-0000-0000-00000000bc04"
	asmHAgentID    = "00000000-0000-0000-0000-00000000bc05"

	asmHProjectID  = "c0000000-0000-4000-8000-00000000cc01"
	asmHTemplateID = "c0000000-0000-4000-8000-00000000cc02"
	asmHReportID   = "c0000000-0000-4000-8000-00000000cc03"
	asmHTaskID     = "c0000000-0000-4000-8000-00000000cc04"
)

func newAsmTestRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(asmHandlerSecret))
	repo := NewRepository(db)
	svc := NewService(repo, ProjectAccessAdapter{Repo: &asmProjectsRepo{db: db}})
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Route("/api/v1/projects/{id}", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Use(middleware.RequireProjectPermission(db, middleware.PermRead))
		r.Get("/assembly", h.List)
		r.Post("/assembly", h.Create)
		r.Post("/assembly/apply-template", h.ApplyTemplate)
	})
	r.Route("/api/v1/assembly", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Post("/reorder", h.Reorder)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetByID)
			r.Patch("/", h.Update)
			r.Delete("/", h.SoftDelete)
		})
	})
	return r
}

// asmProjectsRepo 用真实 projects 仓储（与 main.go 相同的注入形态）。
type asmProjectsRepo struct{ db *sql.DB }

func (b *asmProjectsRepo) GetByID(id string) (*projects.Project, error) {
	return projects.NewRepository(b.db).GetByID(id)
}

func (b *asmProjectsRepo) GetMember(projectID, userID string) (*projects.ProjectMember, error) {
	return projects.NewRepository(b.db).GetMember(projectID, userID)
}

func openAsmHandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openAsmSvcDB(t)
	for _, u := range []struct {
		id       string
		username string
		role     string
	}{
		{asmHOwnerID, "asm_h_owner", "member"},
		{asmHMemberID, "asm_h_member", "member"},
		{asmHViewerID, "asm_h_viewer", "viewer"},
		{asmHOutsiderID, "asm_h_outsider", "member"},
		{asmHAgentID, "asm_h_agent", "agent"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Asm Handler Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'ASM_H_ACTIVE', 'Handler 种子项目', 'active', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, asmHProjectID, asmHOwnerID); err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		userID string
		role   string
	}{
		{asmHOwnerID, "owner"},
		{asmHMemberID, "member"},
		{asmHViewerID, "viewer"},
	} {
		if _, err := db.Exec(
			`INSERT INTO project_members (project_id, user_id, role, status, added_by)
			 VALUES ($1, $2, $3, 'active', $2)
			 ON CONFLICT (project_id, user_id) DO NOTHING`, asmHProjectID, m.userID, m.role); err != nil {
			t.Fatal(err)
		}
	}
	// agent 任务链路（AgentContext 校验）
	if _, err := db.Exec(
		`INSERT INTO daily_reports (id, report_date, author_id)
		 VALUES ($1, '2099-08-01', $2)
		 ON CONFLICT (id) DO NOTHING`, asmHReportID, asmHOwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO pending_agent_tasks (id, report_id, status, acting_user_id)
		 VALUES ($1, $2, 'processing', $3)
		 ON CONFLICT (id) DO NOTHING`, asmHTaskID, asmHReportID, asmHOwnerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM audit_log WHERE user_id IN ($1,$2,$3,$4,$5)`,
			asmHOwnerID, asmHMemberID, asmHViewerID, asmHOutsiderID, asmHAgentID)
		db.Exec(`DELETE FROM pending_agent_tasks WHERE id = $1`, asmHTaskID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, asmHReportID)
		db.Exec(`DELETE FROM assembly_steps WHERE project_id = $1`, asmHProjectID)
		db.Exec(`DELETE FROM project_members WHERE project_id = $1`, asmHProjectID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, asmHProjectID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4,$5)`,
			asmHOwnerID, asmHMemberID, asmHViewerID, asmHOutsiderID, asmHAgentID)
	})
	return db
}

func asmToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(asmHandlerSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func asmReq(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
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

func asmEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (json.RawMessage, string) {
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

func asmErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
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

func uniqueAsmKey() string {
	return fmt.Sprintf("asm-h-%d", time.Now().UnixNano())
}

func assertAsmAudit(t *testing.T, db *sql.DB, requestID, action string) {
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

func TestHandlerAsmCreate(t *testing.T) {
	db := openAsmHandlerDB(t)
	router := newAsmTestRouter(t, db)
	owner := asmToken(t, asmHOwnerID, "asm_h_owner", auth.RoleMember)
	viewer := asmToken(t, asmHViewerID, "asm_h_viewer", auth.RoleMember)
	path := "/api/v1/projects/" + asmHProjectID + "/assembly"

	// 403：viewer（service requireAccess minRole=member）
	rec := asmReq(t, router, http.MethodPost, path, viewer, uniqueAsmKey(), `{"name":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
	// 400：缺 Idempotency-Key / 空名
	rec = asmReq(t, router, http.MethodPost, path, owner, "", `{"name":"x"}`)
	if rec.Code != http.StatusBadRequest || asmErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idem = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = asmReq(t, router, http.MethodPost, path, owner, uniqueAsmKey(), `{"name":"  "}`)
	if rec.Code != http.StatusBadRequest || asmErrorCode(t, rec) != "bad_request" {
		t.Fatalf("empty name = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：未知字段（DisallowUnknownFields）
	rec = asmReq(t, router, http.MethodPost, path, owner, uniqueAsmKey(), `{"name":"x","bogus":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d body=%s", rec.Code, rec.Body.String())
	}
	// 404：项目不存在（admin 绕过）
	admin := asmToken(t, asmHOwnerID, "asm_h_owner", auth.RoleAdmin)
	rec = asmReq(t, router, http.MethodPost, "/api/v1/projects/c0000000-0000-4000-8000-00000000cfff/assembly", admin, uniqueAsmKey(), `{"name":"x"}`)
	if rec.Code != http.StatusNotFound || asmErrorCode(t, rec) != "project_not_found" {
		t.Fatalf("missing project = %d body=%s", rec.Code, rec.Body.String())
	}
	// 201：创建成功 + 语义审计 assembly.create
	rec = asmReq(t, router, http.MethodPost, path, owner, uniqueAsmKey(),
		`{"name":"装配匹配电路","description":"绕线","step_order":1}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", rec.Code, rec.Body.String())
	}
	data, requestID := asmEnvelope(t, rec)
	var step struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		StepOrder int    `json:"step_order"`
	}
	if err := json.Unmarshal(data, &step); err != nil {
		t.Fatal(err)
	}
	if step.Name != "装配匹配电路" || step.Status != StatusPlanned || step.StepOrder != 1 {
		t.Fatalf("created: %+v", step)
	}
	assertAsmAudit(t, db, requestID, "assembly.create")
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, step.ID) })
	// 401：无 token
	rec = asmReq(t, router, http.MethodPost, path, "", uniqueAsmKey(), `{"name":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", rec.Code)
	}
}

func TestHandlerAsmListGet(t *testing.T) {
	db := openAsmHandlerDB(t)
	router := newAsmTestRouter(t, db)
	owner := asmToken(t, asmHOwnerID, "asm_h_owner", auth.RoleMember)
	outsider := asmToken(t, asmHOutsiderID, "asm_h_outsider", auth.RoleMember)
	path := "/api/v1/projects/" + asmHProjectID + "/assembly"

	// 200：空列表
	rec := asmReq(t, router, http.MethodGet, path, owner, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：非法 status
	rec = asmReq(t, router, http.MethodGet, path+"?status=bogus", owner, "", "")
	if rec.Code != http.StatusBadRequest || asmErrorCode(t, rec) != "bad_request" {
		t.Fatalf("bad status = %d body=%s", rec.Code, rec.Body.String())
	}
	// 403：outsider（RequireProjectPermission PermRead）
	rec = asmReq(t, router, http.MethodGet, path, outsider, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list = %d, want 403", rec.Code)
	}

	// 种子两条后：列表 2 条 + get 命中
	stepID := seedAsmStep(t, db, asmHProjectID, "步骤一", StatusPlanned, 1)
	stepID2 := seedAsmStep(t, db, asmHProjectID, "步骤二", StatusPlanned, 2)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM assembly_steps WHERE id IN ($1,$2)`, stepID, stepID2)
	})
	rec = asmReq(t, router, http.MethodGet, path, owner, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	data, _ := asmEnvelope(t, rec)
	var list struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 {
		t.Fatalf("list total = %d", list.Total)
	}
	// status 过滤：planned=2，in_progress=0
	rec = asmReq(t, router, http.MethodGet, path+"?status=in_progress", owner, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered list = %d", rec.Code)
	}
	// get 200 / 404 / 403
	rec = asmReq(t, router, http.MethodGet, "/api/v1/assembly/"+stepID, owner, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d body=%s", rec.Code, rec.Body.String())
	}
	data, _ = asmEnvelope(t, rec)
	var step struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &step); err != nil {
		t.Fatal(err)
	}
	if step.ID != stepID || step.Name != "步骤一" {
		t.Fatalf("get step: %+v", step)
	}
	rec = asmReq(t, router, http.MethodGet, "/api/v1/assembly/c0000000-0000-4000-8000-00000000cfff", owner, "", "")
	if rec.Code != http.StatusNotFound || asmErrorCode(t, rec) != "assembly_step_not_found" {
		t.Fatalf("missing get = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = asmReq(t, router, http.MethodGet, "/api/v1/assembly/"+stepID, outsider, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider get = %d, want 403", rec.Code)
	}
}

func TestHandlerAsmUpdateAndTransition(t *testing.T) {
	db := openAsmHandlerDB(t)
	router := newAsmTestRouter(t, db)
	owner := asmToken(t, asmHOwnerID, "asm_h_owner", auth.RoleMember)
	member := asmToken(t, asmHMemberID, "asm_h_member", auth.RoleMember)

	stepID := seedAsmStep(t, db, asmHProjectID, "待更新步骤", StatusPlanned, 1)
	t.Cleanup(func() { db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, stepID) })
	path := "/api/v1/assembly/" + stepID

	// 400：缺 Idempotency-Key
	rec := asmReq(t, router, http.MethodPatch, path, owner, "", `{"name":"x"}`)
	if rec.Code != http.StatusBadRequest || asmErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idem = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：空 name
	rec = asmReq(t, router, http.MethodPatch, path, owner, uniqueAsmKey(), `{"name":" "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name = %d body=%s", rec.Code, rec.Body.String())
	}
	// 403：member 无 maintainer（字段更新）
	rec = asmReq(t, router, http.MethodPatch, path, member, uniqueAsmKey(), `{"name":"新名"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member update = %d, want 403", rec.Code)
	}
	// 200：改名 + 审计 assembly.update
	rec = asmReq(t, router, http.MethodPatch, path, owner, uniqueAsmKey(), `{"name":"改名后"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID := asmEnvelope(t, rec)
	assertAsmAudit(t, db, requestID, "assembly.update")

	// 400：transition 与字段混用
	rec = asmReq(t, router, http.MethodPatch, path, owner, uniqueAsmKey(), `{"name":"x","transition":"start"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("transition with fields = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：非法流转（planned → complete）
	rec = asmReq(t, router, http.MethodPatch, path, owner, uniqueAsmKey(), `{"transition":"complete"}`)
	if rec.Code != http.StatusBadRequest || asmErrorCode(t, rec) != "invalid_transition" {
		t.Fatalf("invalid transition = %d body=%s", rec.Code, rec.Body.String())
	}
	// 200：start 流转 + 审计 assembly.transition
	rec = asmReq(t, router, http.MethodPatch, path, owner, uniqueAsmKey(), `{"transition":"start"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID = asmEnvelope(t, rec)
	assertAsmAudit(t, db, requestID, "assembly.transition")
	// 200：complete
	rec = asmReq(t, router, http.MethodPatch, path, owner, uniqueAsmKey(), `{"transition":"complete"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete = %d body=%s", rec.Code, rec.Body.String())
	}

	// override 路径：依赖被取消，start + override_reason → 200 + 审计 assembly.transition.override
	depID := seedAsmStep(t, db, asmHProjectID, "被取消的依赖", StatusCancelled, 2)
	childID := seedAsmStepDep(t, db, asmHProjectID, "等待依赖的步骤", StatusPlanned, 3, depID)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM assembly_steps WHERE id IN ($1,$2)`, depID, childID)
	})
	// 无 override → 400 bad_request（ErrDependencyPending）
	rec = asmReq(t, router, http.MethodPatch, "/api/v1/assembly/"+childID, owner, uniqueAsmKey(), `{"transition":"start"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pending dep = %d body=%s", rec.Code, rec.Body.String())
	}
	// 带 override → 200 + 语义审计
	rec = asmReq(t, router, http.MethodPatch, "/api/v1/assembly/"+childID, owner, uniqueAsmKey(),
		`{"transition":"start","override_reason":"依赖已废弃"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("override = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID = asmEnvelope(t, rec)
	assertAsmAudit(t, db, requestID, "assembly.transition.override")
}

func TestHandlerAsmReorderAndDelete(t *testing.T) {
	db := openAsmHandlerDB(t)
	router := newAsmTestRouter(t, db)
	owner := asmToken(t, asmHOwnerID, "asm_h_owner", auth.RoleMember)

	stepID := seedAsmStep(t, db, asmHProjectID, "步骤A", StatusPlanned, 1)
	stepID2 := seedAsmStep(t, db, asmHProjectID, "步骤B", StatusPlanned, 2)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM assembly_steps WHERE id IN ($1,$2)`, stepID, stepID2)
	})

	// 400：缺 Idempotency-Key / 空 steps / 重复 order
	rec := asmReq(t, router, http.MethodPost, "/api/v1/assembly/reorder", owner, "",
		`{"project_id":"`+asmHProjectID+`","steps":[{"id":"`+stepID+`","step_order":2}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idem = %d", rec.Code)
	}
	rec = asmReq(t, router, http.MethodPost, "/api/v1/assembly/reorder", owner, uniqueAsmKey(),
		`{"project_id":"`+asmHProjectID+`","steps":[]}`)
	if rec.Code != http.StatusBadRequest || asmErrorCode(t, rec) != "bad_request" {
		t.Fatalf("empty steps = %d body=%s", rec.Code, rec.Body.String())
	}
	// 200：交换顺序 + 语义审计 assembly.reorder
	rec = asmReq(t, router, http.MethodPost, "/api/v1/assembly/reorder", owner, uniqueAsmKey(),
		`{"project_id":"`+asmHProjectID+`","steps":[{"id":"`+stepID+`","step_order":2},{"id":"`+stepID2+`","step_order":1}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID := asmEnvelope(t, rec)
	assertAsmAudit(t, db, requestID, "assembly.reorder")
	var order1, order2 int
	if err := db.QueryRow(`SELECT step_order FROM assembly_steps WHERE id = $1`, stepID).Scan(&order1); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT step_order FROM assembly_steps WHERE id = $1`, stepID2).Scan(&order2); err != nil {
		t.Fatal(err)
	}
	if order1 != 2 || order2 != 1 {
		t.Fatalf("orders: %d %d", order1, order2)
	}

	// 软删：200 + 审计 assembly.delete；再 get → 404
	rec = asmReq(t, router, http.MethodDelete, "/api/v1/assembly/"+stepID, owner, uniqueAsmKey(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID = asmEnvelope(t, rec)
	assertAsmAudit(t, db, requestID, "assembly.delete")
	rec = asmReq(t, router, http.MethodGet, "/api/v1/assembly/"+stepID, owner, "", "")
	if rec.Code != http.StatusNotFound || asmErrorCode(t, rec) != "assembly_step_not_found" {
		t.Fatalf("get deleted = %d body=%s", rec.Code, rec.Body.String())
	}
	// 重复删除 → 404
	rec = asmReq(t, router, http.MethodDelete, "/api/v1/assembly/"+stepID, owner, uniqueAsmKey(), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("double delete = %d", rec.Code)
	}
}

func TestHandlerAsmApplyTemplate(t *testing.T) {
	db := openAsmHandlerDB(t)
	router := newAsmTestRouter(t, db)
	owner := asmToken(t, asmHOwnerID, "asm_h_owner", auth.RoleMember)
	path := "/api/v1/projects/" + asmHProjectID + "/assembly/apply-template"

	// 400：都缺 / 都有 / 未知字段
	rec := asmReq(t, router, http.MethodPost, path, owner, uniqueAsmKey(), `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("neither = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = asmReq(t, router, http.MethodPost, path, owner, uniqueAsmKey(),
		`{"template_id":"`+asmHTemplateID+`","steps":[{"name":"x","step_order":1}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("both = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：缺 Idempotency-Key
	rec = asmReq(t, router, http.MethodPost, path, owner, "", `{"steps":[{"name":"x","step_order":1}]}`)
	if rec.Code != http.StatusBadRequest || asmErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idem = %d body=%s", rec.Code, rec.Body.String())
	}
	// 201：内联步骤 + 依赖映射 + 语义审计 assembly.template_applied
	rec = asmReq(t, router, http.MethodPost, path, owner, uniqueAsmKey(),
		`{"steps":[{"name":"接线","step_order":1},{"name":"密封","step_order":2,"depends_on_order":1}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("inline = %d body=%s", rec.Code, rec.Body.String())
	}
	data, requestID := asmEnvelope(t, rec)
	var steps []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &steps); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Status != StatusPlanned {
		t.Fatalf("inline steps: %+v", steps)
	}
	assertAsmAudit(t, db, requestID, "assembly.template_applied")
	t.Cleanup(func() {
		for _, s := range steps {
			db.Exec(`DELETE FROM assembly_steps WHERE id = $1`, s.ID)
		}
	})
	// 依赖映射落库
	var dep sql.NullString
	if err := db.QueryRow(`SELECT depends_on FROM assembly_steps WHERE id = $1`, steps[1].ID).Scan(&dep); err != nil {
		t.Fatal(err)
	}
	if !dep.Valid || dep.String != steps[0].ID {
		t.Fatalf("depends_on mapping: %v", dep)
	}
}

func TestHandlerAsmAgentForbidden(t *testing.T) {
	db := openAsmHandlerDB(t)
	router := newAsmTestRouter(t, db)
	agent := asmToken(t, asmHAgentID, "asm_h_agent", auth.RoleAgent)

	// agent 缺代理头 → 400 invalid_agent_context（AgentContext 先于白名单）
	rec := asmReq(t, router, http.MethodPost, "/api/v1/projects/"+asmHProjectID+"/assembly", agent, uniqueAsmKey(),
		`{"name":"x"}`)
	if rec.Code != http.StatusBadRequest || asmErrorCode(t, rec) != "invalid_agent_context" {
		t.Fatalf("agent no headers = %d body=%s", rec.Code, rec.Body.String())
	}
	// agent 带合法任务头 → 装配写端点不在白名单 → 403 agent_action_forbidden
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+asmHProjectID+"/assembly", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Authorization", "Bearer "+agent)
	req.Header.Set("Idempotency-Key", uniqueAsmKey())
	req.Header.Set("X-Acting-User-ID", asmHOwnerID)
	req.Header.Set("X-Agent-Task-ID", asmHTaskID)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || asmErrorCode(t, rr) != "agent_action_forbidden" {
		t.Fatalf("agent create = %d body=%s, want 403 agent_action_forbidden", rr.Code, rr.Body.String())
	}
	// agent PATCH → 白名单只放行 GET/POST 白名单路径 → 403
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/assembly/c0000000-0000-4000-8000-00000000cfff", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Authorization", "Bearer "+agent)
	req.Header.Set("Idempotency-Key", uniqueAsmKey())
	req.Header.Set("X-Acting-User-ID", asmHOwnerID)
	req.Header.Set("X-Agent-Task-ID", asmHTaskID)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("agent update = %d, want 403", rr.Code)
	}
}

func seedAsmStep(t *testing.T, db *sql.DB, projectID, name, status string, order int) string {
	t.Helper()
	var id string
	if err := db.QueryRow(
		`INSERT INTO assembly_steps (project_id, name, status, step_order, created_by)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`, projectID, name, status, order, asmHOwnerID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedAsmStepDep(t *testing.T, db *sql.DB, projectID, name, status string, order int, dependsOn string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(
		`INSERT INTO assembly_steps (project_id, name, status, step_order, depends_on, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id`, projectID, name, status, order, dependsOn, asmHOwnerID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
