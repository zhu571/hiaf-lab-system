package config

import (
	"strings"
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"PORT", "HTTP_READ_HEADER_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_REQUEST_TIMEOUT",
		"DB_HOST", "DB_PORT", "DB_USER", "DB_NAME", "DB_PASSWORD_FILE",
		"DB_MAX_OPEN", "DB_MAX_IDLE",
		"JWT_SECRET_FILE", "SERVICE_TOKEN_FILE",
		"PY_AGENT_INTERPRET_URL", "PY_AGENT_INTERNAL_TOKEN_FILE",
		"SELF_BASE_URL", "ATTACHMENT_DIR", "REPO_ROOT",
		"E5063A_ADDR", "HIOKI_IM3536_ADDR", "KEYSIGHT_33210A_ADDR",
		"TODOS_SCHEDULER_ENABLED", "WEEKLY_SCHEDULER_ENABLED", "WEEKLY_SUMMARY_AUTHOR_ID",
		"IDEMPOTENCY_V2", "RECOVERER_ENABLED", "HTTP_TIMEOUTS_ENABLED", "REQUEST_ID_PROPAGATION",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.Port != "8000" {
		t.Errorf("port = %q, want 8000", cfg.HTTP.Port)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("read header timeout = %v", cfg.HTTP.ReadHeaderTimeout)
	}
	if cfg.DB.MaxOpen != 25 || cfg.DB.MaxIdle != 5 {
		t.Errorf("db pool = %d/%d", cfg.DB.MaxOpen, cfg.DB.MaxIdle)
	}
	if cfg.App.SelfBaseURL != "http://127.0.0.1:8000" {
		t.Errorf("self base = %q", cfg.App.SelfBaseURL)
	}
	if !cfg.Features.IdempotencyV2 || !cfg.Features.Recoverer {
		t.Errorf("feature defaults = %+v", cfg.Features)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("PORT", "9000")
	t.Setenv("DB_HOST", "db.internal")
	t.Setenv("DB_MAX_OPEN", "10")
	t.Setenv("DB_MAX_IDLE", "2")
	t.Setenv("HTTP_IDLE_TIMEOUT", "45s")
	t.Setenv("IDEMPOTENCY_V2", "false")
	t.Setenv("WEEKLY_SUMMARY_AUTHOR_ID", "u-1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.Port != "9000" || cfg.DB.Host != "db.internal" {
		t.Errorf("override not applied: port=%q host=%q", cfg.HTTP.Port, cfg.DB.Host)
	}
	if cfg.DB.MaxOpen != 10 || cfg.DB.MaxIdle != 2 {
		t.Errorf("db pool = %d/%d", cfg.DB.MaxOpen, cfg.DB.MaxIdle)
	}
	if cfg.HTTP.IdleTimeout != 45*time.Second {
		t.Errorf("idle timeout = %v", cfg.HTTP.IdleTimeout)
	}
	if cfg.Features.IdempotencyV2 {
		t.Error("IDEMPOTENCY_V2=false not applied")
	}
	if cfg.Scheduler.WeeklyAuthorID != "u-1" {
		t.Errorf("weekly author = %q", cfg.Scheduler.WeeklyAuthorID)
	}
}

func TestLoadInvalid(t *testing.T) {
	cases := []struct {
		key   string
		value string
		want  string
	}{
		{"PORT", "70000", "PORT"},
		{"PORT", "abc", "PORT"},
		{"HTTP_READ_HEADER_TIMEOUT", "5", "HTTP_READ_HEADER_TIMEOUT"},
		{"DB_MAX_OPEN", "0", "DB_MAX_OPEN/DB_MAX_IDLE"},
		{"DB_MAX_OPEN", "2", "DB_MAX_OPEN/DB_MAX_IDLE"}, // 配合下面的 DB_MAX_IDLE=5
		{"IDEMPOTENCY_V2", "maybe", "IDEMPOTENCY_V2"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			clearEnv(t)
			if tc.key == "DB_MAX_OPEN" && tc.value == "2" {
				t.Setenv("DB_MAX_IDLE", "5")
			}
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want contains %q", err, tc.want)
			}
		})
	}
}
