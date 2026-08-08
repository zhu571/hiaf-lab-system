package middleware

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
)

// WriteSystemAudit 回归：detail 是 JSONB 列，lib/pq 不接受 map 直传（必须先 Marshal）。
// 需要 TEST_DATABASE_URL（迁移 001 建 audit_log.detail）。
func TestWriteSystemAuditDB(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const action = "todos.test_audit"
	t.Cleanup(func() { db.Exec(`DELETE FROM audit_log WHERE action = $1`, action) })

	if err := WriteSystemAudit(context.Background(), db, action, map[string]any{
		"count": 3, "date": "2026-08-07",
	}); err != nil {
		t.Fatalf("WriteSystemAudit with detail must succeed: %v", err)
	}
	var actorType, detail string
	err = db.QueryRow(
		`SELECT actor_type, detail::text FROM audit_log WHERE action = $1 ORDER BY id DESC LIMIT 1`, action,
	).Scan(&actorType, &detail)
	if err != nil {
		t.Fatal(err)
	}
	if actorType != "system" {
		t.Fatalf("expected actor_type=system, got %q", actorType)
	}
	if !strings.Contains(detail, `"count"`) || !strings.Contains(detail, "2026-08-07") {
		t.Fatalf("detail not persisted as JSONB: %s", detail)
	}
}
