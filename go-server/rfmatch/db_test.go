package rfmatch

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

// 集成测试：RF 匹配记录 CRUD、作废（记录里无「查看」按钮但保留「作废」——按代码实况）、
// 频率/电路参数校验。固定 UUID 段 d2xx（用户）+ d0000000-…d2xx（项目），避开其他包。

const (
	rfAdminID    = "00000000-0000-0000-0000-00000000d201"
	rfOwnerID    = "00000000-0000-0000-0000-00000000d202"
	rfMemberID   = "00000000-0000-0000-0000-00000000d203"
	rfOutsiderID = "00000000-0000-0000-0000-00000000d204"

	rfProjectID = "d0000000-0000-4000-8000-00000000d201"
)

func openRFMatchingTestDB(t *testing.T) *sql.DB {
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
		{rfAdminID, "rf_dbtest_admin", "admin"},
		{rfOwnerID, "rf_dbtest_owner", "member"},
		{rfMemberID, "rf_dbtest_member", "member"},
		{rfOutsiderID, "rf_dbtest_outsider", "member"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'RF DB Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
		 VALUES ($1, 'PRJ_RF_DBTEST', 'RF 集成测试项目', 'draft', $2, $2)
		 ON CONFLICT (id) DO NOTHING`, rfProjectID, rfOwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 VALUES ($1, $2, 'owner', 'active', $2), ($1, $3, 'member', 'active', $2)
		 ON CONFLICT (project_id, user_id) DO NOTHING`, rfProjectID, rfOwnerID, rfMemberID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM rf_matching_records WHERE project_id = $1`, rfProjectID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, rfProjectID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4)`, rfAdminID, rfOwnerID, rfMemberID, rfOutsiderID)
	})
	return db
}

func rfTestService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	db := openRFMatchingTestDB(t)
	return NewService(NewRepository(db), ProjectAccessAdapter{Repo: projects.NewRepository(db)}), db
}

func ptr[T any](v T) *T { return &v }

func rfStatus(s string) *string { return &s }

func rfUniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("rf-h-%d", time.Now().UnixNano())
}

// ---------- service 层 ----------

func TestDBCreateValidation(t *testing.T) {
	svc, db := rfTestService(t)

	// 非法 device / freq<=0 / status 缺失 → ErrInvalidInput
	if _, err := svc.Create(rfProjectID, rfMemberID, auth.RoleMember, CreateRFMatchingRequest{
		Device: "bogus", FrequencyMHz: 10, Status: rfStatus(StatusPass),
	}); err != ErrInvalidInput {
		t.Fatalf("bad device: got %v", err)
	}
	if _, err := svc.Create(rfProjectID, rfMemberID, auth.RoleMember, CreateRFMatchingRequest{
		Device: DeviceRFQ, FrequencyMHz: 0, Status: rfStatus(StatusPass),
	}); err != ErrInvalidInput {
		t.Fatalf("zero freq: got %v", err)
	}
	if _, err := svc.Create(rfProjectID, rfMemberID, auth.RoleMember, CreateRFMatchingRequest{
		Device: DeviceRFQ, FrequencyMHz: 10,
	}); err != ErrInvalidInput {
		t.Fatalf("missing status: got %v", err)
	}
	if _, err := svc.Create(rfProjectID, rfMemberID, auth.RoleMember, CreateRFMatchingRequest{
		Device: DeviceRFQ, FrequencyMHz: 10, Status: rfStatus("bogus"),
	}); err != ErrInvalidInput {
		t.Fatalf("bad status: got %v", err)
	}
	// 项目不存在 → ErrProjectNotFound
	if _, err := svc.Create("d0000000-0000-4000-8000-000000009999", rfMemberID, auth.RoleMember,
		CreateRFMatchingRequest{Device: DeviceRFQ, FrequencyMHz: 10, Status: rfStatus(StatusPass)}); err != ErrProjectNotFound {
		t.Fatalf("missing project: got %v", err)
	}
	// 非成员 → ErrForbidden；viewer 不够 member 权限
	if _, err := svc.Create(rfProjectID, rfOutsiderID, auth.RoleMember,
		CreateRFMatchingRequest{Device: DeviceRFQ, FrequencyMHz: 10, Status: rfStatus(StatusPass)}); err != ErrForbidden {
		t.Fatalf("outsider create: got %v", err)
	}
	// 成功：默认 measured_at=now，measured_by=user，电路参数 trim
	measuredAt := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	record, err := svc.Create(rfProjectID, rfMemberID, auth.RoleMember, CreateRFMatchingRequest{
		Device: DeviceRFQ, FrequencyMHz: 81.25, S11: ptr(-24.5),
		InputFreq: ptr(162.5), InputVoltage: ptr(1.2), InputPower: ptr(-10.0),
		InputDesc: "  输入描述  ", TransformerTurns: "  1:4  ", CapacitanceText: " 3.3pF ",
		Status: rfStatus(StatusPass), Notes: "  通过  ", MeasuredAt: &measuredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM rf_matching_records WHERE id = $1`, record.ID) })
	if record.ProjectID != rfProjectID || record.InputDesc != "输入描述" || record.TransformerTurns != "1:4" ||
		record.Status == nil || *record.Status != StatusPass || !record.MeasuredAt.Equal(measuredAt) ||
		record.MeasuredBy == nil || *record.MeasuredBy != rfMemberID || record.S11 == nil || *record.S11 != -24.5 {
		t.Fatalf("created record: %+v", record)
	}
}

func TestDBGetByIDAndList(t *testing.T) {
	svc, db := rfTestService(t)
	status := StatusPass
	created, err := svc.Create(rfProjectID, rfMemberID, auth.RoleMember, CreateRFMatchingRequest{
		Device: DeviceRFCarpet, FrequencyMHz: 100, Status: &status,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM rf_matching_records WHERE id = $1`, created.ID) })

	// 404：不存在
	if _, err := svc.GetByID("d0000000-0000-4000-8000-000000009999", rfMemberID, auth.RoleMember); err != ErrRecordNotFound {
		t.Fatalf("missing get: got %v", err)
	}
	// 403：outsider（viewer 都不行）
	if _, err := svc.GetByID(created.ID, rfOutsiderID, auth.RoleMember); err != ErrForbidden {
		t.Fatalf("outsider get: got %v", err)
	}
	// 200：owner 可读
	got, err := svc.GetByID(created.ID, rfOwnerID, auth.RoleMember)
	if err != nil || got.ID != created.ID || got.Device != DeviceRFCarpet {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	// 非法 id → ErrInvalidInput
	if _, err := svc.GetByID("not-a-uuid", rfMemberID, auth.RoleMember); err != ErrInvalidInput {
		t.Fatalf("bad id: got %v", err)
	}

	// List：device 过滤 / status 过滤 / 分页 / 非法过滤值
	list, err := svc.List(rfProjectID, rfMemberID, auth.RoleMember, ListParams{})
	if err != nil || list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("plain list: %+v err=%v", list, err)
	}
	byDevice, err := svc.List(rfProjectID, rfMemberID, auth.RoleMember, ListParams{Device: DeviceRFQ})
	if err != nil || byDevice.Total != 0 {
		t.Fatalf("device filter: %+v err=%v", byDevice, err)
	}
	byStatus, err := svc.List(rfProjectID, rfMemberID, auth.RoleMember, ListParams{Status: StatusPass})
	if err != nil || byStatus.Total != 1 {
		t.Fatalf("status filter: %+v err=%v", byStatus, err)
	}
	if _, err := svc.List(rfProjectID, rfMemberID, auth.RoleMember, ListParams{Device: "bogus"}); err != ErrInvalidInput {
		t.Fatalf("bad device filter: got %v", err)
	}
	if _, err := svc.List(rfProjectID, rfMemberID, auth.RoleMember, ListParams{Status: "bogus"}); err != ErrInvalidInput {
		t.Fatalf("bad status filter: got %v", err)
	}
	// 非法项目 id / 无权限
	if _, err := svc.List("bad", rfMemberID, auth.RoleMember, ListParams{}); err != ErrInvalidInput {
		t.Fatalf("bad project id: got %v", err)
	}
	if _, err := svc.List(rfProjectID, rfOutsiderID, auth.RoleMember, ListParams{}); err != ErrForbidden {
		t.Fatalf("outsider list: got %v", err)
	}
	// 分页：per_page 上限 100、page 归一化
	paged, err := svc.List(rfProjectID, rfMemberID, auth.RoleMember, ListParams{Page: 1, PerPage: 100})
	if err != nil || paged.PerPage != 100 {
		t.Fatalf("paged list: %+v err=%v", paged, err)
	}
}

func TestDBUpdateAndVoid(t *testing.T) {
	svc, db := rfTestService(t)
	status := StatusPass
	created, err := svc.Create(rfProjectID, rfMemberID, auth.RoleMember, CreateRFMatchingRequest{
		Device: DeviceQPIG, FrequencyMHz: 50, Status: &status,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM rf_matching_records WHERE id = $1`, created.ID) })

	// Update：非法 status → ErrInvalidInput
	badStatus := "bogus"
	if _, err := svc.Update(created.ID, rfMemberID, auth.RoleMember, UpdateRFMatchingRequest{Status: &badStatus}); err != ErrInvalidInput {
		t.Fatalf("bad status: got %v", err)
	}
	// Update：非成员 403；不存在 404
	if _, err := svc.Update(created.ID, rfOutsiderID, auth.RoleMember, UpdateRFMatchingRequest{Notes: ptr("x")}); err != ErrForbidden {
		t.Fatalf("outsider update: got %v", err)
	}
	if _, err := svc.Update("d0000000-0000-4000-8000-000000009999", rfMemberID, auth.RoleMember,
		UpdateRFMatchingRequest{Notes: ptr("x")}); err != ErrRecordNotFound {
		t.Fatalf("missing update: got %v", err)
	}
	// Update 成功：改 status + trim 文本 + 数值字段
	newStatus := StatusAdjust
	updated, err := svc.Update(created.ID, rfMemberID, auth.RoleMember, UpdateRFMatchingRequest{
		Status: &newStatus, S11: ptr(-30.1), Notes: ptr("  调整中  "), CapacitanceText: ptr(" 10pF "),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status == nil || *updated.Status != StatusAdjust || updated.S11 == nil || *updated.S11 != -30.1 ||
		updated.Notes != "调整中" || updated.CapacitanceText != "10pF" {
		t.Fatalf("updated: %+v", updated)
	}

	// MarkVoid：agent 角色禁止；非创建者非 owner → 403
	if err := svc.MarkVoid(created.ID, "agent-user", auth.RoleAgent, "x"); err != ErrForbidden {
		t.Fatalf("agent void: got %v", err)
	}
	if err := svc.MarkVoid(created.ID, rfOutsiderID, auth.RoleMember, "x"); err != ErrForbidden {
		t.Fatalf("outsider void: got %v", err)
	}
	// owner 作废他人记录 → OK；作废后 GetByID/List 均不可见
	if err := svc.MarkVoid(created.ID, rfOwnerID, auth.RoleMember, "记录有误"); err != nil {
		t.Fatalf("owner void: %v", err)
	}
	if _, err := svc.GetByID(created.ID, rfOwnerID, auth.RoleMember); err != ErrRecordNotFound {
		t.Fatalf("get voided: got %v", err)
	}
	list, err := svc.List(rfProjectID, rfMemberID, auth.RoleMember, ListParams{})
	if err != nil || list.Total != 0 {
		t.Fatalf("list after void: %+v err=%v", list, err)
	}
	// 重复作废 → ErrRecordNotFound（is_void=false 条件不命中）
	if err := svc.MarkVoid(created.ID, rfOwnerID, auth.RoleMember, "again"); err != ErrRecordNotFound {
		t.Fatalf("double void: got %v", err)
	}
	// 作废记录可被创建者之外验证：直接查库确认 void 元数据
	var voidedBy, reason string
	var voidedAt sql.NullTime
	if err := db.QueryRow(`SELECT voided_by, void_reason, voided_at FROM rf_matching_records WHERE id = $1`,
		created.ID).Scan(&voidedBy, &reason, &voidedAt); err != nil {
		t.Fatal(err)
	}
	if voidedBy != rfOwnerID || reason != "记录有误" || !voidedAt.Valid {
		t.Fatalf("void metadata: by=%s reason=%s at=%v", voidedBy, reason, voidedAt)
	}

	// 创建者作废自己的记录：新建一条验证
	status2 := StatusPass
	mine, err := svc.Create(rfProjectID, rfMemberID, auth.RoleMember, CreateRFMatchingRequest{
		Device: DeviceRFQ, FrequencyMHz: 75, Status: &status2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM rf_matching_records WHERE id = $1`, mine.ID) })
	if err := svc.MarkVoid(mine.ID, rfMemberID, auth.RoleMember, "本人作废"); err != nil {
		t.Fatalf("creator void own: %v", err)
	}
}

// ---------- handler 层 ----------

const rfHandlerTestSecret = "rfmatch-handler-test-secret"

func newRFMatchingTestRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(rfHandlerTestSecret))
	svc := NewService(NewRepository(db), ProjectAccessAdapter{Repo: projects.NewRepository(db)})
	h := NewHandler(svc)
	router := chi.NewRouter()
	router.Route("/api/v1/projects/{id}", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Use(middleware.RequireProjectPermission(db, middleware.PermRead))
		r.Get("/rf-matching", h.List)
		r.Post("/rf-matching", h.Create)
	})
	router.Route("/api/v1/rf-matching/{id}", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", h.GetByID)
		r.Patch("/", h.Update)
		r.Delete("/", h.MarkVoid)
	})
	return router
}

func rfToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(rfHandlerTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func rfRequest(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
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

func rfErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error body: %s, err=%v", rec.Body.String(), err)
	}
	return envelope.Error.Code
}

func assertRFAudit(t *testing.T, db *sql.DB, requestID, action string) {
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

func TestHandlerRFMatching(t *testing.T) {
	db := openRFMatchingTestDB(t)
	router := newRFMatchingTestRouter(t, db)
	member := rfToken(t, rfMemberID, "member", auth.RoleMember)
	outsider := rfToken(t, rfOutsiderID, "outsider", auth.RoleMember)
	path := "/api/v1/projects/" + rfProjectID + "/rf-matching"

	// 400：缺 Idempotency-Key；401：无 token
	rec := rfRequest(t, router, http.MethodPost, path, member, "", `{"device":"rfq","frequency_mhz":10,"status":"pass"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idempotency key = %d", rec.Code)
	}
	rec = rfRequest(t, router, http.MethodPost, path, "", rfUniqueKey(t), `{"device":"rfq","frequency_mhz":10,"status":"pass"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", rec.Code)
	}
	// 400：未知字段
	rec = rfRequest(t, router, http.MethodPost, path, member, rfUniqueKey(t), `{"device":"rfq","frequency_mhz":10,"status":"pass","hack":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：参数校验失败
	rec = rfRequest(t, router, http.MethodPost, path, member, rfUniqueKey(t), `{"device":"bogus","frequency_mhz":10,"status":"pass"}`)
	if rec.Code != http.StatusBadRequest || rfErrorCode(t, rec) != "bad_request" {
		t.Fatalf("bad device = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 403：outsider（RequireProjectPermission 拦截）
	rec = rfRequest(t, router, http.MethodPost, path, outsider, rfUniqueKey(t), `{"device":"rfq","frequency_mhz":10,"status":"pass"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider create = %d", rec.Code)
	}
	// 201：创建成功 + 审计 rf_matching.create
	key := rfUniqueKey(t)
	rec = rfRequest(t, router, http.MethodPost, path, member, key,
		`{"device":"rfq","frequency_mhz":81.25,"s11":-24.5,"status":"pass","notes":"首次匹配"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == "" {
		t.Fatalf("create response: %+v", envelope)
	}
	assertRFAudit(t, db, envelope.RequestID, "rf_matching.create")
	recordPath := "/api/v1/rf-matching/" + envelope.Data.ID

	// GET 列表：200 + 1 条；outsider 403
	rec = rfRequest(t, router, http.MethodGet, path, member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, body=%s", rec.Code, rec.Body.String())
	}
	var listEnv struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listEnv); err != nil {
		t.Fatal(err)
	}
	if listEnv.Data.Total != 1 {
		t.Fatalf("list total = %d", listEnv.Data.Total)
	}
	// 400：非法 status 过滤
	rec = rfRequest(t, router, http.MethodGet, path+"?status=bogus", member, "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad filter = %d", rec.Code)
	}

	// GET by id：200；404
	rec = rfRequest(t, router, http.MethodGet, recordPath, member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = rfRequest(t, router, http.MethodGet, "/api/v1/rf-matching/d0000000-0000-4000-8000-000000009999", member, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get = %d", rec.Code)
	}

	// PATCH：200 + 审计 rf_matching.update；不可修改字段 → 400
	rec = rfRequest(t, router, http.MethodPatch, recordPath, member, rfUniqueKey(t),
		`{"status":"adjust","notes":" 调整 "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	assertRFAudit(t, db, envelope.RequestID, "rf_matching.update")
	rec = rfRequest(t, router, http.MethodPatch, recordPath, member, rfUniqueKey(t),
		`{"project_id":"d0000000-0000-4000-8000-00000000d201"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("immutable field = %d, body=%s", rec.Code, rec.Body.String())
	}

	// DELETE：200 + 审计 rf_matching.delete；再删 404
	rec = rfRequest(t, router, http.MethodDelete, recordPath, member, rfUniqueKey(t), `{"reason":"写错了"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("void = %d, body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	assertRFAudit(t, db, envelope.RequestID, "rf_matching.delete")
	rec = rfRequest(t, router, http.MethodDelete, recordPath, member, rfUniqueKey(t), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("double void = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 404：作废后不可查
	rec = rfRequest(t, router, http.MethodGet, recordPath, member, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get voided = %d", rec.Code)
	}
}
