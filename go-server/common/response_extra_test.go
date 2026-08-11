package common

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestWriteCreated(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/test", nil)
	r = r.WithContext(SetRequestID(context.Background(), "req-created"))

	WriteCreated(w, r, map[string]string{"id": "1"})

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	var body SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RequestID != "req-created" {
		t.Fatalf("request_id = %q", body.RequestID)
	}
}

func TestWriteJSONEncodeError(t *testing.T) {
	w := httptest.NewRecorder()
	// chan 类型不可 JSON 序列化 → Encode 返回错误，响应仍写 200 头
	err := WriteJSON(w, http.StatusOK, make(chan int))
	if err == nil {
		t.Fatal("expected encode error for chan value")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestWriteErrorDetailsOmittedWhenNil(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	r = r.WithContext(SetRequestID(context.Background(), "req-err"))

	WriteError(w, r, http.StatusForbidden, "permission_denied", "无权", nil)

	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "permission_denied" || body.Error.Message != "无权" || body.RequestID != "req-err" {
		t.Fatalf("body: %+v", body)
	}
	if strings.Contains(w.Body.String(), `"details"`) {
		t.Fatalf("details must be omitted when nil: %s", w.Body.String())
	}
}

// TestOpenDB 依赖 TEST_DATABASE_URL 指向的测试库（与 CI/scripts/test-go.sh 同源）。
func TestOpenDB(t *testing.T) {
	t.Run("missing password", func(t *testing.T) {
		os.Unsetenv("DB_PASSWORD")
		if _, err := OpenDB(); err == nil || !strings.Contains(err.Error(), "secret not found") {
			t.Fatalf("expected secret error, got %v", err)
		}
	})

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "postgres" {
		t.Skipf("unsupported dsn scheme: %s", u.Scheme)
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	host, port := u.Hostname(), u.Port()
	dbname := strings.TrimPrefix(u.Path, "/")
	if dbname == "" {
		t.Skip("no dbname in TEST_DATABASE_URL")
	}

	t.Setenv("DB_HOST", host)
	t.Setenv("DB_PORT", port)
	t.Setenv("DB_USER", user)
	t.Setenv("DB_NAME", dbname)
	t.Setenv("DB_PASSWORD", pass)

	db, err := OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	var one int
	if err := db.QueryRow("SELECT 1").Scan(&one); err != nil {
		t.Fatalf("ping query: %v", err)
	}
	if one != 1 {
		t.Fatalf("SELECT 1 = %d", one)
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("COMMON_TEST_VAR", "set")
	if got := envOrDefault("COMMON_TEST_VAR", "def"); got != "set" {
		t.Fatalf("envOrDefault(set) = %q", got)
	}
	os.Unsetenv("COMMON_TEST_VAR")
	if got := envOrDefault("COMMON_TEST_VAR", "def"); got != "def" {
		t.Fatalf("envOrDefault(unset) = %q", got)
	}
}
