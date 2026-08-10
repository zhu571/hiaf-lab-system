package logs

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// service token by-date 全链路测试（方案 §10）：SERVICE_TOKEN 白名单 + user_id 参数 +
// latest=true 回退 + 普通 JWT 忽略 user_id 强制取自己 + actor_type='system' 审计。
// 需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移 001-036）。

const (
	svcUserA = "00000000-0000-0000-0000-00000000c001"
	svcUserB = "00000000-0000-0000-0000-00000000c002"
)

func openLogsTestDB(t *testing.T) *sql.DB {
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
	_, err = db.Exec(`
		INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
		VALUES ($1, 'svc_alice', 'x', 'Alice', 'member', false, false),
		       ($2, 'svc_bob', 'x', 'Bob', 'member', false, false)
		ON CONFLICT (id) DO NOTHING`, svcUserA, svcUserB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM daily_report_log_links WHERE daily_report_id IN (SELECT id FROM daily_reports WHERE author_id IN ($1,$2))`, svcUserA, svcUserB)
		db.Exec(`DELETE FROM daily_reports WHERE author_id IN ($1,$2)`, svcUserA, svcUserB)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, svcUserA, svcUserB)
	})
	return db
}

func seedReport(t *testing.T, db *sql.DB, authorID, date, rawText string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO daily_reports (report_date, author_id, raw_text, summary, content_status, quality_status)
		 VALUES ($1::date, $2, $3, '', 'submitted', 'passed')
		 ON CONFLICT (report_date, author_id) DO UPDATE SET raw_text = EXCLUDED.raw_text`,
		date, authorID, rawText)
	if err != nil {
		t.Fatal(err)
	}
}

func buildByDateRouter(db *sql.DB) http.Handler {
	repo := NewRepository(db)
	svc := NewService(repo, "Asia/Shanghai", nil)
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.ServiceToken())
	r.Route("/api/v1/daily-reports", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		// 与生产 main.go 一致挂 AgentContext：service call 必须能穿过（claims==nil）。
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Get("/by-date", h.GetReportByDate)
	})
	return r
}

func readRawText(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Data struct {
			RawText string `json:"raw_text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad response body: %v", err)
	}
	return resp.Data.RawText
}

func TestByDateServiceToken(t *testing.T) {
	db := openLogsTestDB(t)
	seedReport(t, db, svcUserA, "2026-08-07", "alice 日报")
	seedReport(t, db, svcUserB, "2026-08-07", "bob 日报")
	middleware.SetServiceToken("svc-secret")

	router := buildByDateRouter(db)

	// service token + user_id → 返回目标用户日报
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date?user_id="+svcUserB+"&date=2026-08-07", nil)
	r.Header.Set("Authorization", "Bearer svc-secret")
	router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("service token expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := readRawText(t, w); got != "bob 日报" {
		t.Fatalf("expected bob report, got %q", got)
	}
	// 审计分支（方案 §10 v13）：service token 调用产生的审计行 actor_type='system'
	var actor string
	if err := db.QueryRow(
		`SELECT actor_type FROM audit_log WHERE request_id = $1 ORDER BY id DESC LIMIT 1`,
		w.Header().Get("X-Request-Id"),
	).Scan(&actor); err != nil {
		t.Fatalf("audit row missing: %v", err)
	}
	if actor != "system" {
		t.Fatalf("expected actor_type=system, got %q", actor)
	}
}

func TestByDateInvalidToken(t *testing.T) {
	db := openLogsTestDB(t)
	seedReport(t, db, svcUserA, "2026-08-07", "alice 日报")
	middleware.SetServiceToken("svc-secret")

	router := buildByDateRouter(db)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date?user_id="+svcUserA, nil)
	r.Header.Set("Authorization", "Bearer wrong")
	router.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token expected 401, got %d", w.Code)
	}
}

func TestByDateJWTForcesOwnUser(t *testing.T) {
	db := openLogsTestDB(t)
	seedReport(t, db, svcUserA, "2026-08-07", "alice 日报")
	seedReport(t, db, svcUserB, "2026-08-07", "bob 日报")

	// 普通 JWT：alice 传 user_id=bob → 忽略，取自己
	token, err := middleware.GenerateToken(svcUserA, "svc_alice", "member", 1, []byte("test-secret-32-bytes-long!!!!!"))
	if err != nil {
		t.Fatal(err)
	}
	middleware.SetJWTSecret([]byte("test-secret-32-bytes-long!!!!!"))

	router := buildByDateRouter(db)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date?user_id="+svcUserB+"&date=2026-08-07", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("JWT expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := readRawText(t, w); got != "alice 日报" {
		t.Fatalf("JWT must ignore user_id and return own report, got %q", got)
	}
}

func TestByDateLatestFallback(t *testing.T) {
	db := openLogsTestDB(t)
	// 周五有日报；周一（08-10）无日报 → latest=true 回溯取周五
	seedReport(t, db, svcUserA, "2026-08-07", "周五日报")
	middleware.SetServiceToken("svc-secret")

	router := buildByDateRouter(db)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date?user_id="+svcUserA+"&date=2026-08-10&latest=true", nil)
	r.Header.Set("Authorization", "Bearer svc-secret")
	router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("latest fallback expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := readRawText(t, w); got != "周五日报" {
		t.Fatalf("expected friday fallback, got %q", got)
	}
}

func TestByDateUnknownUser404(t *testing.T) {
	db := openLogsTestDB(t)
	middleware.SetServiceToken("svc-secret")

	router := buildByDateRouter(db)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/daily-reports/by-date?user_id=00000000-0000-0000-0000-00000000c0ff&date=2026-08-07", nil)
	r.Header.Set("Authorization", "Bearer svc-secret")
	router.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown user expected 404, got %d", w.Code)
	}
}
