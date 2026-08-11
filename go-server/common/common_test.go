package common

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetGetRequestID(t *testing.T) {
	ctx := SetRequestID(context.Background(), "req-abc")
	if got := GetRequestID(ctx); got != "req-abc" {
		t.Fatalf("GetRequestID = %q, want req-abc", got)
	}
	if got := GetRequestID(context.Background()); got != "" {
		t.Fatalf("GetRequestID on plain ctx = %q, want empty", got)
	}
}

func TestReadSecret(t *testing.T) {
	t.Run("file wins", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "secret")
		if err := os.WriteFile(path, []byte("  file-secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := ReadSecret(path, "READSECRET_TEST_ENV")
		if err != nil {
			t.Fatal(err)
		}
		if got != "file-secret" {
			t.Fatalf("ReadSecret = %q, want file-secret", got)
		}
	})
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("READSECRET_TEST_ENV", "env-secret")
		got, err := ReadSecret("/nonexistent/secrets/x", "READSECRET_TEST_ENV")
		if err != nil {
			t.Fatal(err)
		}
		if got != "env-secret" {
			t.Fatalf("ReadSecret = %q, want env-secret", got)
		}
	})
	t.Run("both missing errors", func(t *testing.T) {
		os.Unsetenv("READSECRET_TEST_ENV")
		_, err := ReadSecret("/nonexistent/secrets/x", "READSECRET_TEST_ENV")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "READSECRET_TEST_ENV") {
			t.Fatalf("error should mention env key: %v", err)
		}
	})
}
