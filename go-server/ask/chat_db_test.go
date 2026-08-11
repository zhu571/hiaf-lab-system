package ask

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// mockAgent 返回一个 /v1/ask mock：记录请求头/体并回放预设响应。
func mockAgent(t *testing.T, status int, body string, hook func(r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hook != nil {
			hook(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func newAskSvc(t *testing.T, agentURL string) *Service {
	t.Helper()
	db := openAskTestDB(t)
	t.Cleanup(func() { db.Close() })
	svc := NewService(NewRepository(db), db)
	if agentURL != "" {
		svc.agentURL = agentURL
		svc.agentToken = "agent-tok"
	}
	return svc
}

// TestChat_FullFlow Chat 全链路：校验 → 限流 → 调 py-agent → 落库（含 rows 快照）→ 返回。
func TestChat_FullFlow(t *testing.T) {
	var gotAuth string
	server := mockAgent(t, 200, `{"answer":"共 2 条","sql":"SELECT id FROM logs LIMIT 2",
		"table":"logs","columns":["id"],"rows":[{"id":"r1"},{"id":"r2"}],"row_count":2,"model":"deepseek-chat"}`, func(r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	})
	defer server.Close()
	svc := newAskSvc(t, server.URL)
	db := svc.db

	db.Exec(`DELETE FROM ask_history WHERE user_id = $1`, askUserID)
	t.Cleanup(func() { db.Exec(`DELETE FROM ask_history WHERE user_id = $1`, askUserID) })

	ctx := common.SetRequestID(context.Background(), "req_chat_001")
	resp, err := svc.Chat(ctx, askUserID, " 上周测试结果怎么样  ")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Answer != "共 2 条" || resp.TableName != "logs" || resp.RowCount != 2 {
		t.Fatalf("resp: %+v", resp)
	}
	if len(resp.Rows) != 2 || resp.Rows[0]["id"] != "r1" {
		t.Fatalf("rows: %+v", resp.Rows)
	}
	if resp.Truncated {
		t.Fatal("small snapshot must not be truncated")
	}
	if resp.ID == "" {
		t.Fatal("resp.ID empty")
	}
	if gotAuth != "Bearer agent-tok" {
		t.Fatalf("auth header = %q", gotAuth)
	}

	// 落库可查：本人可见，question 被 TrimSpace。
	got, err := svc.GetByUser(resp.ID, askUserID)
	if err != nil {
		t.Fatalf("GetByUser: %v", err)
	}
	if got.Question != "上周测试结果怎么样" || got.RequestID != "req_chat_001" {
		t.Fatalf("persisted: %+v", got)
	}
	if len(got.Rows) != 2 || got.RowCount != 2 {
		t.Fatalf("persisted rows: %+v", got.Rows)
	}
}

// TestChat_UpstreamErrors py-agent 不可达/非 200 → ErrUpstream（前端 502 依据）。
func TestChat_UpstreamErrors(t *testing.T) {
	t.Run("connection refused", func(t *testing.T) {
		svc := newAskSvc(t, "http://127.0.0.1:1")
		_, err := svc.Chat(context.Background(), askUserID, "x")
		if !errors.Is(err, ErrUpstream) {
			t.Fatalf("want ErrUpstream, got %v", err)
		}
	})
	t.Run("non-200", func(t *testing.T) {
		server := mockAgent(t, 500, `{"error":"boom"}`, nil)
		defer server.Close()
		svc := newAskSvc(t, server.URL)
		_, err := svc.Chat(context.Background(), askUserID, "x")
		if !errors.Is(err, ErrUpstream) {
			t.Fatalf("want ErrUpstream, got %v", err)
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		server := mockAgent(t, 200, `{not json`, nil)
		defer server.Close()
		svc := newAskSvc(t, server.URL)
		_, err := svc.Chat(context.Background(), askUserID, "x")
		if !errors.Is(err, ErrUpstream) {
			t.Fatalf("want ErrUpstream, got %v", err)
		}
	})
}

// TestChat_ValidationAndRateLimit 输入校验 + 限流（不触库）。
func TestChat_ValidationAndRateLimit(t *testing.T) {
	svc := &Service{rlCalls: map[string][]time.Time{}}

	if _, err := svc.Chat(context.Background(), "u1", "   "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty question: %v", err)
	}
	long := strings.Repeat("长", 1001)
	if _, err := svc.Chat(context.Background(), "u1", long); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("long question: %v", err)
	}
	// agent 未配置：先消费限流槽位，返回未配置错误
	if _, err := svc.Chat(context.Background(), "u1", "q"); err == nil || !strings.Contains(err.Error(), "未配置") {
		t.Fatalf("unconfigured agent: %v", err)
	}
	// 10 次后限流
	for i := 0; i < 9; i++ {
		if _, err := svc.Chat(context.Background(), "u1", "q"); err == nil {
			t.Fatal("expected unconfigured error")
		}
	}
	if _, err := svc.Chat(context.Background(), "u1", "q"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate limited: %v", err)
	}
}

// TestChat_RequestIDTruncation 超长 request_id（>64）落库截断不 500。
func TestChat_RequestIDTruncation(t *testing.T) {
	server := mockAgent(t, 200, `{"answer":"ok","columns":["id"],"rows":[]}`, nil)
	defer server.Close()
	svc := newAskSvc(t, server.URL)
	db := svc.db
	db.Exec(`DELETE FROM ask_history WHERE user_id = $1`, askUserID)
	t.Cleanup(func() { db.Exec(`DELETE FROM ask_history WHERE user_id = $1`, askUserID) })

	longID := "req_" + strings.Repeat("x", 100)
	ctx := common.SetRequestID(context.Background(), longID)
	resp, err := svc.Chat(ctx, askUserID, "问题")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	got, err := svc.GetByUser(resp.ID, askUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.RequestID) > 64 {
		t.Fatalf("request_id not truncated: %d chars", len(got.RequestID))
	}
	// 超限快照收缩：返回空 rows 时 columns 也空，落库安全
	if resp.Truncated {
		t.Fatal("empty snapshot must not be truncated")
	}
}

// TestChat_AgentOversizedSnapshot py-agent 返回超限快照 → 落库前按 execute 同规则收缩。
func TestChat_AgentOversizedSnapshot(t *testing.T) {
	big := strings.Repeat("x", 500)
	rows := make([]map[string]any, 0, 300)
	for i := 0; i < 300; i++ {
		rows = append(rows, map[string]any{"id": "1", "content": big, "meta": big})
	}
	payload, err := json.Marshal(map[string]any{
		"answer": "超限", "columns": []string{"id", "content", "meta"}, "rows": rows, "row_count": 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := mockAgent(t, 200, string(payload), nil)
	defer server.Close()
	svc := newAskSvc(t, server.URL)
	db := svc.db
	db.Exec(`DELETE FROM ask_history WHERE user_id = $1`, askUserID)
	t.Cleanup(func() { db.Exec(`DELETE FROM ask_history WHERE user_id = $1`, askUserID) })

	resp, err := svc.Chat(context.Background(), askUserID, "超限问题")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !resp.Truncated {
		t.Fatal("oversized snapshot must be truncated")
	}
	// 列裁剪优先：300 行 500 字符大列超出 256KB → 裁掉 meta 大列、保留 300 行。
	if len(resp.Rows) != 300 {
		t.Fatalf("rows should all be kept after column pruning, got %d", len(resp.Rows))
	}
	for _, c := range resp.Columns {
		if c == "meta" {
			t.Fatalf("meta column should be pruned, got %v", resp.Columns)
		}
	}
	if snapshotSize(resp.Rows) > snapshotBudget {
		t.Fatalf("snapshot over budget after pruning: %d bytes", snapshotSize(resp.Rows))
	}
	if resp.RowCount != 300 {
		t.Fatalf("row_count should stay 300, got %d", resp.RowCount)
	}
}

// TestBuildSchema schema 组装 + 10min 缓存；白名单表存在 → 无缺表。
func TestBuildSchema(t *testing.T) {
	db := openAskTestDB(t)
	defer db.Close()
	svc := NewService(NewRepository(db), db)

	missing, err := svc.whitelistDiff(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("whitelist tables missing: %v", missing)
	}

	schema, err := svc.BuildSchema(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(schema, "logs:") {
		t.Fatalf("schema missing logs table: %.200s", schema)
	}
	if !strings.Contains(schema, schemaRelations) {
		t.Fatalf("schema missing relations hint")
	}
	// 缓存：第二次调用命中缓存返回同一文本
	schema2, err := svc.BuildSchema(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if schema != schema2 {
		t.Fatal("schema cache miss")
	}
}

// TestStartRetentionTask 保留任务：启动即跑一轮 + ctx 取消退出（不挂后台进程）。
func TestStartRetentionTask(t *testing.T) {
	db := openAskTestDB(t)
	defer db.Close()
	svc := NewService(NewRepository(db), db)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.StartRetentionTask(ctx)
		close(done)
	}()
	// 给首轮 run() + 日志留时间，然后取消
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StartRetentionTask did not exit on ctx cancel")
	}
}

// TestServiceListPagination List 分页参数钳制。
func TestServiceListPagination(t *testing.T) {
	db := openAskTestDB(t)
	defer db.Close()
	svc := NewService(NewRepository(db), db)
	db.Exec(`DELETE FROM ask_history WHERE user_id = $1`, askUserID)
	t.Cleanup(func() { db.Exec(`DELETE FROM ask_history WHERE user_id = $1`, askUserID) })

	for i := 0; i < 3; i++ {
		if err := svc.repo.SaveAsk(&AskHistory{
			UserID: askUserID, RequestID: "req_pg", Question: "q",
			Columns: []string{"id"}, RowCount: 1, Model: "test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := svc.List(askUserID, 0, 0) // page<1→1, perPage≤0→20
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || total != 3 {
		t.Fatalf("list: len=%d total=%d", len(items), total)
	}
	items, _, err = svc.List(askUserID, 2, 1000) // perPage>50→50
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("page 2 offset: %d", len(items))
	}
}
