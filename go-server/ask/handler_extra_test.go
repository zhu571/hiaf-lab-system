package ask

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// writeError 语义化错误 → HTTP 状态码映射全表（handler 层安全边界）。
func TestWriteErrorMappings(t *testing.T) {
	h := NewHandler(nil)
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"not found", ErrNotFound, http.StatusNotFound, "not_found"},
		{"invalid input", ErrInvalidInput, http.StatusBadRequest, "bad_request"},
		{"sql rejected", ErrSQLRejected, http.StatusBadRequest, "sql_rejected"},
		{"sql exec", ErrSQLExec, http.StatusUnprocessableEntity, "sql_execution_failed"},
		{"rate limited", ErrRateLimited, http.StatusTooManyRequests, "rate_limited"},
		{"upstream", ErrUpstream, http.StatusBadGateway, "upstream_error"},
		{"wrapped upstream", errors.Join(ErrUpstream, errors.New("py-agent 挂了")), http.StatusBadGateway, "upstream_error"},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(common.SetRequestID(req.Context(), "req-map"))
			rec := httptest.NewRecorder()
			h.writeError(rec, req, tc.err)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", envelope.Error.Code, tc.wantCode)
			}
		})
	}
}

// Chat 无 claims → 401（handler 自带兜底，不依赖中间件顺序）。
func TestChatHandler_Unauthorized(t *testing.T) {
	h := NewHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ask/chat", strings.NewReader(`{"question":"x"}`))
	rec := httptest.NewRecorder()
	h.Chat(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("chat no claims = %d, want 401", rec.Code)
	}
}

// Chat 非法 JSON → 400（经 AuthRequired 注入 claims 后触发 decode 失败路径）。
func TestChatHandler_BadJSON(t *testing.T) {
	middleware.SetJWTSecret([]byte("ask-handler-secret"))
	token, err := middleware.GenerateToken("u1", "alice", "member", 0, []byte("ask-handler-secret"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ask/chat", strings.NewReader(`{not json`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	middleware.AuthRequired(http.HandlerFunc(h.Chat)).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("chat bad json = %d, want 400", rec.Code)
	}
}

// Execute 非 service call → 403。
func TestExecuteHandler_ForbiddenWithoutServiceToken(t *testing.T) {
	middleware.SetJWTSecret([]byte("ask-handler-secret"))
	token, err := middleware.GenerateToken("u1", "alice", "member", 0, []byte("ask-handler-secret"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ask/execute", strings.NewReader(`{"sql":"SELECT 1"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	middleware.AuthRequired(http.HandlerFunc(h.Execute)).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("execute user jwt = %d, want 403", rec.Code)
	}
}

// Execute 全链路：ServiceToken → IsServiceCall → 只读执行。
func TestExecuteHandler_ServiceTokenChain(t *testing.T) {
	db := openAskTestDB(t)
	defer db.Close()
	ensureAskFixture(t, db)
	middleware.SetServiceToken("ask-st-0123456789abcdef")
	t.Cleanup(func() { middleware.SetServiceToken("") })
	h := NewHandler(NewService(NewRepository(db), db))
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.ServiceToken())
	router.Use(middleware.AuthRequired)
	router.Post("/api/v1/ask/execute", h.Execute)

	// 200：service token + 合法 SQL + 调用方用户（R2 行级隔离按 user_id 取可访问项目）
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ask/execute", strings.NewReader(`{"sql":"SELECT id FROM logs LIMIT 3","user_id":"`+askUserID+`"}`))
	req.Header.Set("Authorization", "Bearer ask-st-0123456789abcdef")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("execute = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			SQL       string `json:"sql"`
			TableName string `json:"table_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.TableName != "logs" {
		t.Fatalf("table_name = %q", envelope.Data.TableName)
	}

	// 400：非法 SQL（users 不在白名单）→ sql_rejected
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ask/execute", strings.NewReader(`{"sql":"SELECT * FROM users"}`))
	req.Header.Set("Authorization", "Bearer ask-st-0123456789abcdef")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("execute rejected = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 400：请求体解析失败
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ask/execute", strings.NewReader(`{bad`))
	req.Header.Set("Authorization", "Bearer ask-st-0123456789abcdef")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("execute bad json = %d", rec.Code)
	}

	// 401：错误 token（ServiceToken 白名单命中后拒绝）
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ask/execute", strings.NewReader(`{"sql":"SELECT 1"}`))
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("execute wrong token = %d, want 401", rec.Code)
	}
}

// History 列表分页 + HistoryByID 404（真实 DB）。
func TestHistoryHandlerList(t *testing.T) {
	db := openAskTestDB(t)
	defer db.Close()
	ensureAskFixture(t, db)
	middleware.SetJWTSecret([]byte("ask-handler-secret"))
	token, err := middleware.GenerateToken(askUserID, "alice", "member", 0, []byte("ask-handler-secret"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db), db)
	h := NewHandler(svc)
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.AuthRequired)
	router.Get("/api/v1/ask/history", h.History)
	router.Get("/api/v1/ask/history/{id}", h.HistoryByID)

	db.Exec(`DELETE FROM ask_history WHERE user_id = $1`, askUserID)
	t.Cleanup(func() { db.Exec(`DELETE FROM ask_history WHERE user_id = $1`, askUserID) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ask/history", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("history = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Items   []json.RawMessage `json:"items"`
			Total   int               `json:"total"`
			Page    int               `json:"page"`
			PerPage int               `json:"per_page"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Total != 0 || envelope.Data.Page != 1 || envelope.Data.PerPage != 20 {
		t.Fatalf("list envelope: %+v", envelope.Data)
	}

	// 合法 UUID 但不存在 → 404
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ask/history/00000000-0000-0000-0000-00000000dead", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("history detail 404 = %d", rec.Code)
	}

	// 无 token → 401
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ask/history", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("history no token = %d, want 401", rec.Code)
	}
}
