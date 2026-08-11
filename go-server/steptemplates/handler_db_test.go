package steptemplates

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

// handler 集成测试：与 main.go 一致的中间件链（AuthRequired + AgentContext + Audit +
// RequireIdempotencyKey），真实 DB（TEST_DATABASE_URL 空则跳过）。写端点断言审计落库。

const stpHandlerSecret = "steptemplates-handler-test-secret"

func newStpTemplatesTestRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(stpHandlerSecret))
	svc := NewService(NewRepository(db), db)
	h := NewHandler(svc)
	router := chi.NewRouter()
	router.Route("/api/v1/step-templates", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Post("/generate", h.Generate)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetByID)
			r.Patch("/", h.Update)
			r.Delete("/", h.SoftDelete)
			r.Patch("/items", h.ReplaceItems)
		})
	})
	return router
}

type stpEnvelope struct {
	Data json.RawMessage `json:"data"`
	ID   string          `json:"request_id"`
}

func stpToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(stpHandlerSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func stpRequest(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
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

func stpErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error body %s: %v", rec.Body.String(), err)
	}
	return envelope.Error.Code
}

func stpUniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("stp-h-%d", time.Now().UnixNano())
}

func stpAssertAudit(t *testing.T, db *sql.DB, requestID, action string) {
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

func TestStpHandlerCreateTemplate(t *testing.T) {
	db := openStpTemplatesDB(t)
	router := newStpTemplatesTestRouter(t, db)
	admin := stpToken(t, stpDBAdminID, "admin", auth.RoleAdmin)
	creator := stpToken(t, stpDBCreator, "creator", auth.RoleMember)

	// 401：无 token
	rec := stpRequest(t, router, http.MethodPost, "/api/v1/step-templates", "", stpUniqueKey(t),
		`{"name":"x","kind":"assembly","items":[{"name":"s","step_order":1}]}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", rec.Code)
	}

	// 400：缺 Idempotency-Key
	rec = stpRequest(t, router, http.MethodPost, "/api/v1/step-templates", admin, "",
		`{"name":"x","kind":"assembly","items":[{"name":"s","step_order":1}]}`)
	if rec.Code != http.StatusBadRequest || stpErrorCode(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idempotency key = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 403：member 角色创建（无项目成员身份语义：service 层 requireWriteAccess 拒绝）
	rec = stpRequest(t, router, http.MethodPost, "/api/v1/step-templates", creator, stpUniqueKey(t),
		`{"name":"x","kind":"assembly","items":[{"name":"s","step_order":1}]}`)
	if rec.Code != http.StatusForbidden || stpErrorCode(t, rec) != "permission_denied" {
		t.Fatalf("member create = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 400：空 items
	rec = stpRequest(t, router, http.MethodPost, "/api/v1/step-templates", admin, stpUniqueKey(t),
		`{"name":"x","kind":"assembly","items":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty items = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 201：创建成功 + 审计
	key := stpUniqueKey(t)
	name := fmt.Sprintf("STP-DBTEST-h-%d", time.Now().UnixNano())
	rec = stpRequest(t, router, http.MethodPost, "/api/v1/step-templates", admin, key,
		`{"name":"`+name+`","kind":"assembly","description":"handler 创建","items":[{"name":"步骤A","step_order":1}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope stpEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(envelope.Data, &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != name || created.ID == "" {
		t.Fatalf("created: %+v", created)
	}
	stpAssertAudit(t, db, envelope.ID, "step_template.created")
}

func TestStpHandlerListAndGet(t *testing.T) {
	db := openStpTemplatesDB(t)
	router := newStpTemplatesTestRouter(t, db)
	admin := stpToken(t, stpDBAdminID, "admin", auth.RoleAdmin)

	rec := stpRequest(t, router, http.MethodGet, "/api/v1/step-templates", admin, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope stpEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(envelope.Data, &list); err != nil {
		t.Fatal(err)
	}
	if list.Items == nil {
		t.Fatal("items must not be null")
	}

	// 404：不存在模板
	rec = stpRequest(t, router, http.MethodGet, "/api/v1/step-templates/00000000-0000-0000-0000-00000000dead", admin, "", "")
	if rec.Code != http.StatusNotFound || stpErrorCode(t, rec) != "template_not_found" {
		t.Fatalf("get missing = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestStpHandlerUpdateItemsDelete(t *testing.T) {
	db := openStpTemplatesDB(t)
	router := newStpTemplatesTestRouter(t, db)
	admin := stpToken(t, stpDBAdminID, "admin", auth.RoleAdmin)
	creator := stpToken(t, stpDBCreator, "creator", auth.RoleMember)

	// 先创建（admin）
	name := fmt.Sprintf("STP-DBTEST-h-%d", time.Now().UnixNano())
	rec := stpRequest(t, router, http.MethodPost, "/api/v1/step-templates", admin, stpUniqueKey(t),
		`{"name":"`+name+`","kind":"assembly","items":[{"name":"旧步骤","step_order":1},{"name":"旧步骤2","step_order":2}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope stpEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var created struct{ ID string }
	if err := json.Unmarshal(envelope.Data, &created); err != nil {
		t.Fatal(err)
	}
	id := created.ID

	// 403：member 无权更新他人的模板
	rec = stpRequest(t, router, http.MethodPatch, "/api/v1/step-templates/"+id, creator, stpUniqueKey(t),
		`{"name":"改名"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member update = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}

	// 200：admin 更新 + 审计
	key := stpUniqueKey(t)
	rec = stpRequest(t, router, http.MethodPatch, "/api/v1/step-templates/"+id, admin, key,
		`{"name":"改名后"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	stpAssertAudit(t, db, envelope.ID, "step_template.updated")

	// 200：替换 items（序号复用旧条目的 1,2 → 迁移 037 部分唯一索引生效）+ 审计
	key = stpUniqueKey(t)
	rec = stpRequest(t, router, http.MethodPatch, "/api/v1/step-templates/"+id+"/items", admin, key,
		`{"items":[{"name":"新步骤1","step_order":1},{"name":"新步骤2","step_order":2}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace items = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	stpAssertAudit(t, db, envelope.ID, "step_template.items_replaced")

	// 回查：items 已被替换
	rec = stpRequest(t, router, http.MethodGet, "/api/v1/step-templates/"+id, admin, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Items[0].Name != "新步骤1" {
		t.Fatalf("items after replace: %+v", got.Items)
	}

	// 200：软删 + 审计；删后 404
	key = stpUniqueKey(t)
	rec = stpRequest(t, router, http.MethodDelete, "/api/v1/step-templates/"+id, admin, key, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	stpAssertAudit(t, db, envelope.ID, "step_template.deleted")
	rec = stpRequest(t, router, http.MethodGet, "/api/v1/step-templates/"+id, admin, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete = %d, want 404", rec.Code)
	}
}

func TestStpHandlerGenerate(t *testing.T) {
	db := openStpTemplatesDB(t)
	router := newStpTemplatesTestRouter(t, db)
	admin := stpToken(t, stpDBAdminID, "admin", auth.RoleAdmin)
	creator := stpToken(t, stpDBCreator, "creator", auth.RoleMember)

	// 403：admin 无项目角色 → Generate 被 handler 层 HasAnyProjectRole 拒绝
	rec := stpRequest(t, router, http.MethodPost, "/api/v1/step-templates/generate", admin, stpUniqueKey(t),
		`{"kind":"assembly","prompt":"装个靶室"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("generate no project role = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}

	// creator 有项目成员身份（stpDBProject 种子）但 AI 未配置 → 500 internal_error
	// （生成端点不落库，仅返回候选）
	rec = stpRequest(t, router, http.MethodPost, "/api/v1/step-templates/generate", creator, stpUniqueKey(t),
		`{"kind":"assembly","prompt":"装个靶室"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("generate unconfigured = %d, want 500, body=%s", rec.Code, rec.Body.String())
	}
}
