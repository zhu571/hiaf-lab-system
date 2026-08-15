package middleware

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// WriteSystemAudit 回归：detail 是 JSONB 列，lib/pq 不接受 map 直传（必须先 Marshal）。
// 需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移 001-036，001 建 audit_log.detail）。
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

// R4：客户端断连（请求 context 取消）不得丢审计行——审计写用独立 5s 超时
// context，业务响应完成后仍能落库。inner handler 在写响应前取消请求 ctx，
// 模拟"发完写请求立即断开 socket"的取证对抗场景。
func TestAuditMiddlewareSurvivesClientDisconnect(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const action = "audit.disconnect.test"
	t.Cleanup(func() { db.Exec(`DELETE FROM audit_log WHERE action = $1`, action) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetAuditAction(r.Context(), action)
		cancel() // 模拟断连：请求 context 在响应阶段即取消
		w.WriteHeader(http.StatusOK)
	})
	handler := RequestID(Audit(db)(inner))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("inner handler status = %d", rr.Code)
	}

	var requestID, method string
	err = db.QueryRow(
		`SELECT request_id, method FROM audit_log WHERE action = $1 ORDER BY id DESC LIMIT 1`, action,
	).Scan(&requestID, &method)
	if err != nil {
		t.Fatalf("audit row must survive request context cancellation: %v", err)
	}
	if requestID == "" || method != http.MethodPost {
		t.Fatalf("audit row corrupted: request_id=%q method=%q", requestID, method)
	}
}

// S5/S6 端到端（迁移 036）：登录成功路径——handler 经 SetAuditLastLogin/SetAuditUsername
// 登记，Audit 中间件在同一事务内落 last_login 与审计行（含 username 列）。
func TestAuditMiddlewareWritesLastLoginAndUsernameInSameTx(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	username := fmt.Sprintf("lastlogin-%d", time.Now().UnixNano())
	var userID string
	err = db.QueryRow(
		`INSERT INTO users (username, password_hash, display_name, role, must_change_pw)
		 VALUES ($1, 'x', 'Last Login Test', 'member', false)
		 RETURNING id`, username,
	).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, userID) })

	const sourceIP = "203.0.113.9"
	// 全链路：SourceGate（公网直连 peer，login 白名单放行）→ Audit（同一事务落 last_login+审计）。
	t.Setenv("LAB_PROXY_SHARED_SECRET", "test-secret")
	t.Setenv("SOURCE_GATE_ENABLED", "true")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetAuditLastLogin(r.Context(), userID, sourceIP)
		SetAuditUsername(r.Context(), username)
		w.WriteHeader(http.StatusOK)
	})
	handler := SourceGate()(Audit(db)(inner))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = sourceIP + ":1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var gotIP string
	if err := db.QueryRow(`SELECT last_login_ip FROM users WHERE id = $1`, userID).Scan(&gotIP); err != nil {
		t.Fatal(err)
	}
	if gotIP != sourceIP {
		t.Fatalf("last_login_ip = %q, want %q", gotIP, sourceIP)
	}
	var gotUsername, gotClientIP string
	err = db.QueryRow(
		`SELECT username, client_ip FROM audit_log WHERE username = $1 ORDER BY id DESC LIMIT 1`, username,
	).Scan(&gotUsername, &gotClientIP)
	if err != nil {
		t.Fatal(err)
	}
	if gotUsername != username {
		t.Fatalf("audit username = %q, want %q", gotUsername, username)
	}
	if gotClientIP != sourceIP {
		t.Fatalf("audit client_ip = %q, want %q", gotClientIP, sourceIP)
	}
}
