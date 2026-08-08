package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// 幂等中间件复用验证（方案 §10）：同 Idempotency-Key 重复提交只生效一次。
// 需要 TEST_DATABASE_URL（迁移 023 建 idempotency_keys 表）。
func TestRequireIdempotencyKeyReplay(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const key = "idem-test-replay-key"
	db.Exec(`DELETE FROM idempotency_keys WHERE idempotency_key = $1`, key)
	t.Cleanup(func() { db.Exec(`DELETE FROM idempotency_keys WHERE idempotency_key = $1`, key) })

	var ran int
	handler := RequireIdempotencyKey(db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ran++
		common.WriteSuccess(w, r, map[string]string{"ok": "true"})
	}))

	do := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/todos", nil)
		r = r.WithContext(common.SetRequestID(r.Context(), "req-"+string(rune('a'+ran))))
		r.Header.Set("Idempotency-Key", key)
		handler.ServeHTTP(w, r)
		return w
	}

	w1 := do()
	if w1.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", w1.Code)
	}
	if ran != 1 {
		t.Fatalf("expected 1 handler run, got %d", ran)
	}
	w2 := do()
	if w2.Code != http.StatusConflict {
		t.Fatalf("replay expected 409, got %d", w2.Code)
	}
	if ran != 1 {
		t.Fatalf("replay must not reach handler, got %d runs", ran)
	}
}
