package ask

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// 集成测试：需要 TEST_DATABASE_URL（CI/本地起 postgres 并跑完迁移 001-033）。
// 结束后清理本用例创建的行。ask_history.user_id 引用 users(id)，
// 用例使用固定 UUID 用户（迁移 001 种子用户范围 00000000-0000-0000-0000-0000000000a1+）。

// 用户取迁移 001 种子用户（ask_history.user_id 引用 users(id)）。
const (
	askUserID    = "a0000000-0000-4000-8000-000000000001"
	askUserID2   = "a0000000-0000-4000-8000-000000000002"
	askHistoryID = "b0000000-0000-4000-8000-000000000101"
)

func openAskTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Skip("TEST_DATABASE_URL unreachable")
	}
	return db
}

func TestAskRepository(t *testing.T) {
	db := openAskTestDB(t)
	defer db.Close()
	repo := NewRepository(db)

	cleanup := func() {
		db.Exec(`DELETE FROM ask_history WHERE user_id IN ($1,$2)`, askUserID, askUserID2)
	}
	cleanup()
	defer cleanup()

	h := &AskHistory{
		UserID:     askUserID,
		RequestID:  "req_ask_test_001",
		Question:   "上周 RF 匹配测试结果怎么样",
		Answer:     "共 3 条记录",
		SQLText:    "SELECT * FROM logs LIMIT 200",
		TableName:  "logs",
		Columns:    []string{"id", "project_id", "content"},
		Rows:       []map[string]any{{"id": "r1", "project_id": "p1", "content": "x"}},
		RowCount:   1,
		DurationMS: 42,
		CreatedAt:  time.Now(),
	}
	if err := repo.SaveAsk(h); err != nil {
		t.Fatalf("SaveAsk: %v", err)
	}
	if h.ID == "" {
		t.Fatal("SaveAsk must return generated id")
	}
	historyID := h.ID

	items, total, err := repo.ListHistory(askUserID, 20, 0)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 item, total=%d len=%d", total, len(items))
	}
	if items[0].Rows != nil {
		t.Fatal("ListHistory must not return rows big field")
	}
	if items[0].Columns == nil || len(items[0].Columns) != 3 {
		t.Fatalf("columns should be restored in list: %v", items[0].Columns)
	}

	got, err := repo.GetHistory(historyID)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if got.Answer != h.Answer || len(got.Rows) != 1 || got.Rows[0]["id"] != "r1" {
		t.Fatalf("GetHistory mismatch: %+v", got)
	}

	// 归属校验：本人可读，他人不可读。
	if _, err := repo.GetHistoryByUser(historyID, askUserID); err != nil {
		t.Fatalf("owner GetHistoryByUser: %v", err)
	}
	if _, err := repo.GetHistoryByUser(historyID, askUserID2); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for other user, got %v", err)
	}
	if _, err := repo.GetHistory("00000000-0000-0000-0000-00000000dead"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for missing id, got %v", err)
	}
}
