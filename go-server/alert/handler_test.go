package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	mw "github.com/zhu571/hiaf-lab-system/go-server/middleware"

	_ "github.com/lib/pq"
)

// alertTestStack 复刻 main.go 的告警路由中间件链（方案 §4 矩阵）：
// ServiceToken → CSRF → AuthRequired → Audit → [RequireIdempotencyKey → RequireRoleOrService]。
func alertTestStack(db *sql.DB, h http.Handler, resolveGuard bool) http.Handler {
	var stack http.Handler = h
	if resolveGuard {
		stack = mw.RequireIdempotencyKey(db)(stack)
		stack = mw.RequireRoleOrService("admin", "maintainer")(stack)
	}
	stack = mw.Audit(db)(stack)
	stack = mw.AuthRequired(stack)
	stack = mw.CSRF(stack)
	stack = mw.ServiceToken()(stack)
	return stack
}

func newAlertTestEnv(t *testing.T) (*sql.DB, *fakeSender, *Service) {
	t.Helper()
	db := openAlertTestDB(t)
	if _, err := db.Exec(`DELETE FROM alerts`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM idempotency_keys`); err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{}
	svc := NewService(NewRepository(db), sender, db)
	return db, sender, svc
}

// 迁移 001 种子用户（audit_log.user_id 引用 users(id)，测试 JWT 必须用真实 UUID）。
const (
	seedAdminID      = "a0000000-0000-4000-8000-000000000001"
	seedMemberID     = "a0000000-0000-4000-8000-000000000002"
	seedViewerID     = "a0000000-0000-4000-8000-000000000003"
	seedMaintainerID = "a0000000-0000-4000-8000-000000000005"
)

// testRequest 构造带 request_id / JWT / CSRF / 幂等头的请求。
func testRequest(t *testing.T, method, path, body, token string, csrf bool, idemKey string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r = r.WithContext(common.SetRequestID(r.Context(), "req-alert-test"))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if csrf {
		r.AddCookie(&http.Cookie{Name: "csrf_token", Value: "csrf-test"})
		r.Header.Set("X-CSRF-Token", "csrf-test")
	}
	if idemKey != "" {
		r.Header.Set("Idempotency-Key", idemKey)
	}
	return r
}

func alertJWT(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := mw.GenerateToken(userID, username, role, 0, []byte("alert-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// ---------- report 鉴权矩阵 ----------

func TestReportAuthMatrix(t *testing.T) {
	mw.SetJWTSecret([]byte("alert-test-secret"))
	db, sender, svc := newAlertTestEnv(t)
	defer db.Close()
	mw.SetServiceToken("svc-alert-token")
	h := NewHandler(svc)
	stack := alertTestStack(db, http.HandlerFunc(h.Report), false)

	// 无 token → 403（CSRF 先于 AuthRequired 拦截 POST；与全站无 token POST 行为一致）。
	rr := httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/report",
		`{"level":"warning","source":"ioc","title":"t","detail":"d"}`, "", false, ""))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("no token: expected 403 (csrf), got %d: %s", rr.Code, rr.Body.String())
	}

	// 错误 service token → 401。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/report",
		`{"level":"warning","source":"ioc","title":"t","detail":"d"}`, "wrong-token", false, ""))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: expected 401, got %d", rr.Code)
	}

	// 用户 JWT → handler 级 403（仅内部服务可调用；用户通道不可 report）。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/report",
		`{"level":"warning","source":"ioc","title":"t","detail":"d"}`, alertJWT(t, seedMemberID, "alice", "member"), true, ""))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("user jwt report: expected 403, got %d: %s", rr.Code, rr.Body.String())
	}

	// 合法 service token → 200 + 契约字段。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/report",
		`{"level":"warning","source":"ioc","title":"OPC UA 断连 >30s","detail":"模拟"}`, "svc-alert-token", false, ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("service report: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data struct {
			AlertID         string `json:"alert_id"`
			Deduplicated    bool   `json:"deduplicated"`
			OccurrenceCount int    `json:"occurrence_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.AlertID == "" || resp.Data.Deduplicated || resp.Data.OccurrenceCount != 1 {
		t.Fatalf("report response: %+v", resp.Data)
	}

	// 枚举/长度校验 → 400。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/report",
		`{"level":"fatal","source":"ioc","title":"t","detail":"d"}`, "svc-alert-token", false, ""))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad level: expected 400, got %d", rr.Code)
	}

	// 重复上报（同一窗口）→ deduplicated=true（HTTP 层聚合去重）。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/report",
		`{"level":"warning","source":"ioc","title":"OPC UA 断连 >30s","detail":"模拟2"}`, "svc-alert-token", false, ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("second report: expected 200, got %d", rr.Code)
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Data.Deduplicated || resp.Data.OccurrenceCount != 2 {
		t.Fatalf("dedup report response: %+v", resp.Data)
	}
	if sender.total() != 1 {
		t.Fatalf("dedup must not resend, got %d sends", sender.total())
	}
}

// ---------- resolve 双通道矩阵 ----------

func TestResolveDualChannel(t *testing.T) {
	mw.SetJWTSecret([]byte("alert-test-secret"))
	db, _, svc := newAlertTestEnv(t)
	defer db.Close()
	mw.SetServiceToken("svc-alert-token")
	h := NewHandler(svc)

	// 造一条 active 告警。
	res, err := svc.Report(context.Background(), LevelWarning, SourceWatchdog, "lab-server 健康检查失败", "x")
	if err != nil {
		t.Fatal(err)
	}

	// member → 403。
	stack := alertTestStack(db, http.HandlerFunc(h.Resolve), true)
	rr := httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/resolve",
		`{"id":"`+res.AlertID+`"}`, alertJWT(t, seedMemberID, "alice", "member"), true, "key-member"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("member resolve: expected 403, got %d", rr.Code)
	}
	if a := mustAlert(t, db, res.AlertID); a.Status != StatusActive {
		t.Fatal("member must not resolve alert")
	}

	// admin → 200（按 id），resolved_by=username。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/resolve",
		`{"id":"`+res.AlertID+`"}`, alertJWT(t, seedAdminID, "admin1", "admin"), true, "key-admin-1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("admin resolve: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if a := mustAlert(t, db, res.AlertID); a.Status != StatusResolved || a.ResolvedBy != "admin1" {
		t.Fatalf("admin resolve attribution: %+v", a)
	}

	// 重复 resolve 同一 id → 幂等 200（换新幂等键）。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/resolve",
		`{"id":"`+res.AlertID+`"}`, alertJWT(t, seedAdminID, "admin1", "admin"), true, "key-admin-2"))
	if rr.Code != http.StatusOK {
		t.Fatalf("idempotent resolve: expected 200, got %d", rr.Code)
	}

	// maintainer 按 source+title → 400（用户通道必须按 id 解除）。
	if _, err := svc.Report(context.Background(), LevelWarning, SourceWatchdog, "lab-server 健康检查失败", "再犯"); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/resolve",
		`{"source":"watchdog","title":"lab-server 健康检查失败"}`, alertJWT(t, seedMaintainerID, "maint1", "maintainer"), true, "key-maint"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("maintainer resolve by source: expected 400, got %d: %s", rr.Code, rr.Body.String())
	}

	// maintainer 按 id → 200。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/resolve",
		`{"id":"`+res.AlertID+`"}`, alertJWT(t, seedMaintainerID, "maint1", "maintainer"), true, "key-maint-id"))
	if rr.Code != http.StatusOK {
		t.Fatalf("maintainer resolve by id: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// service token → 200（无 CSRF、无 Idempotency-Key，双豁免）。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/resolve",
		`{"source":"watchdog","title":"lab-server 健康检查失败"}`, "svc-alert-token", false, ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("service resolve: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	// 匹配不到 active 行 → 幂等 success。
	if rr.Body.String() == "" || !strings.Contains(rr.Body.String(), `"resolved":true`) {
		t.Fatalf("service resolve body: %s", rr.Body.String())
	}
}

func TestResolveUserGuardRequirements(t *testing.T) {
	mw.SetJWTSecret([]byte("alert-test-secret"))
	db, _, svc := newAlertTestEnv(t)
	defer db.Close()
	mw.SetServiceToken("svc-alert-token")
	h := NewHandler(svc)
	res, err := svc.Report(context.Background(), LevelWarning, SourceWatchdog, "鉴权守卫告警", "x")
	if err != nil {
		t.Fatal(err)
	}
	stack := alertTestStack(db, http.HandlerFunc(h.Resolve), true)
	body := `{"id":"` + res.AlertID + `"}`
	admin := alertJWT(t, seedAdminID, "admin1", "admin")

	// 用户通道缺 CSRF → 403 csrf_failed。
	rr := httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/resolve", body, admin, false, "key-g1"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("missing csrf: expected 403, got %d", rr.Code)
	}

	// 用户通道缺 Idempotency-Key → 400。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/resolve", body, admin, true, ""))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key: expected 400, got %d", rr.Code)
	}

	// 用户通道 Idempotency-Key 复用 → 409。
	req := testRequest(t, http.MethodPost, "/api/v1/alerts/resolve", body, admin, true, "key-g2")
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first resolve: expected 200, got %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("idempotency key reuse: expected 409, got %d", rr.Code)
	}

	// 无 token 用户通道 → 403（CSRF 先拦截）。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodPost, "/api/v1/alerts/resolve", body, "", false, ""))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("no token resolve: expected 403 (csrf), got %d", rr.Code)
	}
}

// ---------- list / detail ----------

func TestListDetailOverHTTP(t *testing.T) {
	mw.SetJWTSecret([]byte("alert-test-secret"))
	db, _, svc := newAlertTestEnv(t)
	defer db.Close()
	mw.SetServiceToken("svc-alert-token")
	h := NewHandler(svc)
	if _, err := svc.Report(context.Background(), LevelWarning, SourceWatchdog, "列表页告警", "x"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Report(context.Background(), LevelWarning, SourceUpdater, "详情页告警", "y")
	if err != nil {
		t.Fatal(err)
	}

	// list：JWT 全员可读（member 即可），默认 active。
	stack := alertTestStack(db, http.HandlerFunc(h.List), false)
	rr := httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodGet, "/api/v1/alerts", "", alertJWT(t, seedViewerID, "bob", "viewer"), false, ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rr.Code)
	}
	var list struct {
		Data struct {
			Items []Alert `json:"items"`
			Total int     `json:"total"`
			Limit int     `json:"limit"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Data.Total != 2 || len(list.Data.Items) != 2 {
		t.Fatalf("list: total=%d items=%d", list.Data.Total, len(list.Data.Items))
	}

	// limit 超上限 → 响应返回截断后的实际值（5000 → 200）。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodGet, "/api/v1/alerts?limit=5000", "", alertJWT(t, seedViewerID, "bob", "viewer"), false, ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("list limit truncation: expected 200, got %d", rr.Code)
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Data.Limit != maxListLimit {
		t.Fatalf("list limit must be truncated to %d, got %d", maxListLimit, list.Data.Limit)
	}

	// list 非法 status → 400。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodGet, "/api/v1/alerts?status=bogus", "", alertJWT(t, seedViewerID, "bob", "viewer"), false, ""))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad status: expected 400, got %d", rr.Code)
	}

	// detail。
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", res.AlertID)
	stack = alertTestStack(db, http.HandlerFunc(h.Get), false)
	req := testRequest(t, http.MethodGet, "/api/v1/alerts/"+res.AlertID, "", alertJWT(t, seedViewerID, "bob", "viewer"), false, "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("detail: expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"title":"详情页告警"`) {
		t.Fatalf("detail body: %s", rr.Body.String())
	}

	// 非法 UUID → 404（不送 PG）。
	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("id", "not-a-uuid")
	req = testRequest(t, http.MethodGet, "/api/v1/alerts/not-a-uuid", "", alertJWT(t, seedViewerID, "bob", "viewer"), false, "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx2))
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("invalid uuid: expected 404, got %d", rr.Code)
	}

	// 36 位但分组错误的 UUID → 404（不进 PG 防 500）。
	badUUID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rctx3 := chi.NewRouteContext()
	rctx3.URLParams.Add("id", badUUID)
	req = testRequest(t, http.MethodGet, "/api/v1/alerts/"+badUUID, "", alertJWT(t, seedViewerID, "bob", "viewer"), false, "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx3))
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("malformed 36-char uuid: expected 404, got %d", rr.Code)
	}

	// 无 token → 401。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodGet, "/api/v1/alerts", "", "", false, ""))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token list: expected 401, got %d", rr.Code)
	}

	// service token 对 GET /alerts（非白名单路径）不消费，走用户通道 → 401。
	rr = httptest.NewRecorder()
	stack.ServeHTTP(rr, testRequest(t, http.MethodGet, "/api/v1/alerts", "", "svc-alert-token", false, ""))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("service token on non-whitelist GET: expected 401, got %d", rr.Code)
	}
}

// 编译期断言：Service 实现 Reporter 窄接口（接入点注入用）。
var _ Reporter = (*Service)(nil)
