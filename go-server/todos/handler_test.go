package todos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// handler 测试：注入 claims + RequestID，直接调 handler 方法，验证错误码映射与成功路径。

func newHandlerReq(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	r = r.WithContext(common.SetRequestID(r.Context(), "req-test"))
	return r
}

func TestHandlerErrorMapping(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	h := NewHandler(svc)

	// 未登录 → 401
	rr := httptest.NewRecorder()
	h.List(rr, newHandlerReq(t, http.MethodGet, "/api/v1/todos", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	token := genTestToken(t, "u1", "alice", auth.RoleMember)
	authed := func(hfn http.HandlerFunc) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
			middleware.AuthRequired(http.HandlerFunc(hfn)).ServeHTTP(w, r)
		})
	}

	// done 不存在 → 404 todo_not_found
	rr = httptest.NewRecorder()
	authed(h.Done).ServeHTTP(rr, chiRoute(newHandlerReq(t, http.MethodPatch, "/api/v1/todos/t999/done", nil), "t999"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("done missing expected 404, got %d: %s", rr.Code, rr.Body.String())
	}

	// agent 角色 → 403 agent_forbidden
	agentToken := genTestToken(t, "u9", "agent", auth.RoleAgent)
	rr = httptest.NewRecorder()
	authedAgent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+agentToken)
		middleware.AuthRequired(http.HandlerFunc(h.Create)).ServeHTTP(w, r)
	})
	authedAgent.ServeHTTP(rr, chiRoute(newHandlerReq(t, http.MethodPost, "/api/v1/todos", CreateRequest{Title: "x"}), ""))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("agent expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// chiRoute 把 {id} 路由参数注入 context（chi.URLParam 依赖）。
func chiRoute(r *http.Request, id string) *http.Request {
	if id == "" {
		return r
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	return r.WithContext(ctx)
}

func genTestToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	middleware.SetJWTSecret([]byte("test-secret-32-bytes-long!!!!!"))
	tok, err := middleware.GenerateToken(userID, username, role, 1, []byte("test-secret-32-bytes-long!!!!!"))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestHandlerProvisionRedeemErrors(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	h := NewHandler(svc)
	token := genTestToken(t, "u1", "alice", auth.RoleMember)
	authed := func(hfn http.HandlerFunc) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
			middleware.AuthRequired(http.HandlerFunc(hfn)).ServeHTTP(w, r)
		})
	}

	// 无效 provision token → 401 invalid_provision_token
	rr := httptest.NewRecorder()
	authed(h.Redeem).ServeHTTP(rr, chiRoute(newHandlerReq(t, http.MethodPost, "/api/v1/todos/notification-topic/redeem", map[string]string{"provision_token": "bad"}), ""))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("invalid provision token expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error.Code != "invalid_provision_token" {
		t.Fatalf("expected invalid_provision_token, got %s", resp.Error.Code)
	}

	// 限流 → 429 rate_limited
	svc.rlCalls["u1"] = make([]time.Time, 10)
	for i := range svc.rlCalls["u1"] {
		svc.rlCalls["u1"][i] = testNow()
	}
	rr = httptest.NewRecorder()
	authed(h.Provision).ServeHTTP(rr, chiRoute(newHandlerReq(t, http.MethodPost, "/api/v1/todos/notification-topic/provision", nil), ""))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited expected 429, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandlerCreateAndList(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	h := NewHandler(svc)
	token := genTestToken(t, "u1", "alice", auth.RoleMember)
	authed := func(hfn http.HandlerFunc) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
			middleware.AuthRequired(http.HandlerFunc(hfn)).ServeHTTP(w, r)
		})
	}
	authedAs := func(hfn http.HandlerFunc, tok string) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+tok)
			middleware.AuthRequired(http.HandlerFunc(hfn)).ServeHTTP(w, r)
		})
	}

	// 创建成功 → 201
	rr := httptest.NewRecorder()
	authed(h.Create).ServeHTTP(rr, chiRoute(newHandlerReq(t, http.MethodPost, "/api/v1/todos", CreateRequest{Title: "写日报", Priority: PriorityHigh}), ""))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// 列表成功 → 200 + updated_at 字段
	rr = httptest.NewRecorder()
	authed(h.List).ServeHTTP(rr, chiRoute(newHandlerReq(t, http.MethodGet, "/api/v1/todos?date=2026-08-07", nil), ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("list expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data []Todo `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Title != "写日报" || resp.Data[0].UpdatedAt.IsZero() {
		t.Fatalf("unexpected list data: %+v", resp.Data)
	}

	// 错误码映射：service 层 ErrForbidden → 403 permission_denied（viewer 对共享项 defer）
	d.snap.roles["u3"] = map[string]string{"p1": auth.RoleViewer}
	d.perm.allowed["p1"] = map[string]bool{"u1": true}
	pid := "p1"
	shared, err := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "共享", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	viewerToken := genTestToken(t, "u3", "carol", auth.RoleViewer)
	rr = httptest.NewRecorder()
	authedAs(h.Defer, viewerToken).ServeHTTP(rr, chiRoute(newHandlerReq(t, http.MethodPatch, "/api/v1/todos/"+shared.ID+"/defer", nil), shared.ID))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("viewer defer expected 403, got %d", rr.Code)
	}

	// 状态冲突：done 后重复 done → 409 state_conflict
	owned, _ := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "一次性"})
	if _, err := svc.Done(owned.ID, "u1", auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	authed(h.Done).ServeHTTP(rr, chiRoute(newHandlerReq(t, http.MethodPatch, "/api/v1/todos/"+owned.ID+"/done", nil), owned.ID))
	if rr.Code != http.StatusConflict {
		t.Fatalf("double done expected 409, got %d", rr.Code)
	}
	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatal(err)
	}
	if errResp.Error.Code != "state_conflict" {
		t.Fatalf("expected state_conflict, got %s", errResp.Error.Code)
	}
}

func TestHandlerWriteErrorFallback(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	h := NewHandler(svc)
	// 未知错误 → 500 internal_error
	err := errors.New("boom")
	d.repo.getErr = err
	token := genTestToken(t, "u1", "alice", auth.RoleMember)
	rr := httptest.NewRecorder()
	r := newHandlerReq(t, http.MethodPatch, "/api/v1/todos/t1/done", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	middleware.AuthRequired(http.HandlerFunc(h.Done)).ServeHTTP(rr, chiRoute(r, "t1"))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("unknown error expected 500, got %d", rr.Code)
	}
}
