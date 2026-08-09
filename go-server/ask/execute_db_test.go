package ask

import (
	"context"
	"errors"
	"testing"
)

// Execute 集成测试：需要 TEST_DATABASE_URL 且迁移 033 已应用（ask_reader 角色）。
// 覆盖 SET LOCAL ROLE ask_reader + 只读事务 + LIMIT 封顶 + 行集序列化全链路。
func TestExecuteDB(t *testing.T) {
	db := openAskTestDB(t)
	defer db.Close()
	svc := NewService(NewRepository(db), db)

	resp, err := svc.Execute(context.Background(), "SELECT id, content FROM logs LIMIT 5")
	if err != nil {
		t.Fatalf("Execute valid sql: %v", err)
	}
	if resp.TableName != "logs" || resp.RowCount > 5 || len(resp.Rows) != resp.RowCount {
		t.Fatalf("unexpected response: table=%q rows=%d", resp.TableName, resp.RowCount)
	}
	if len(resp.Columns) == 0 {
		t.Fatal("columns missing")
	}
	if resp.Truncated {
		t.Fatal("LIMIT 5 with 5 rows must not be truncated")
	}

	resp, err = svc.Execute(context.Background(), "SELECT * FROM logs")
	if err != nil {
		t.Fatalf("Execute without limit: %v", err)
	}
	if resp.RowCount > maxRows {
		t.Fatalf("rows over cap: %d", resp.RowCount)
	}
	if !resp.Truncated {
		t.Fatal("no-limit query should be marked truncated")
	}

	if _, err := svc.Execute(context.Background(), "SELECT * FROM users"); !errors.Is(err, ErrSQLRejected) {
		t.Fatalf("users must be rejected at parser layer, got %v", err)
	}

	// JSONB 保留结构（projects.tags_json）。
	resp, err = svc.Execute(context.Background(), "SELECT id, tags_json FROM projects LIMIT 1")
	if err != nil {
		t.Fatalf("Execute jsonb: %v", err)
	}
	if len(resp.Rows) > 0 {
		if v, ok := resp.Rows[0]["tags_json"]; !ok || v == nil {
			t.Fatalf("tags_json should be preserved as JSON, got %v", resp.Rows[0])
		}
	}
}
