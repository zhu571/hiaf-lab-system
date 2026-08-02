package main

import (
	"testing"
	"time"
)

func TestParseFlagsArgs(t *testing.T) {
	cfg := parseFlags([]string{"--session", "upd_abc1234567", "--repo", "/opt/x", "--force", "--dry-run"})
	if cfg.SessionID != "upd_abc1234567" {
		t.Errorf("SessionID = %q", cfg.SessionID)
	}
	if cfg.RepoRoot != "/opt/x" {
		t.Errorf("RepoRoot = %q", cfg.RepoRoot)
	}
	if !cfg.Force || !cfg.DryRun || cfg.NoRollback {
		t.Errorf("flag 解析错误: force=%v dry=%v noRollback=%v", cfg.Force, cfg.DryRun, cfg.NoRollback)
	}
}

func TestParseFlagsDefaults(t *testing.T) {
	cfg := parseFlags(nil)
	if cfg.SessionID != "" {
		t.Errorf("默认 SessionID = %q, want empty", cfg.SessionID)
	}
	if cfg.RepoRoot != "/opt/hiaf-lab-system" {
		t.Errorf("默认 RepoRoot = %q", cfg.RepoRoot)
	}
	if cfg.ComposeFile != "deploy/docker-compose.yml" {
		t.Errorf("默认 ComposeFile = %q", cfg.ComposeFile)
	}
	if cfg.UpdateTimeout != 30*time.Minute || cfg.RollbackTimeout != 30*time.Minute {
		t.Errorf("默认超时错误: %v / %v", cfg.UpdateTimeout, cfg.RollbackTimeout)
	}
}

func TestParseFlagsEnvFallback(t *testing.T) {
	t.Setenv("UPDATE_SESSION_ID", "upd_env000000")
	t.Setenv("UPDATE_UPDATE_TIMEOUT", "5m")
	t.Setenv("UPDATE_ROLLBACK_TIMEOUT", "1h")
	t.Setenv("UPDATE_NTFY_URL", "http://ntfy:80/x")

	cfg := parseFlags(nil)
	if cfg.SessionID != "upd_env000000" {
		t.Errorf("env SessionID = %q", cfg.SessionID)
	}
	if cfg.UpdateTimeout != 5*time.Minute {
		t.Errorf("UpdateTimeout = %v, want 5m", cfg.UpdateTimeout)
	}
	if cfg.RollbackTimeout != time.Hour {
		t.Errorf("RollbackTimeout = %v, want 1h", cfg.RollbackTimeout)
	}
	if cfg.NtfyURL != "http://ntfy:80/x" {
		t.Errorf("NtfyURL = %q", cfg.NtfyURL)
	}
}

func TestEnvDurationInvalidFallback(t *testing.T) {
	t.Setenv("UPDATE_UPDATE_TIMEOUT", "not-a-duration")
	if d := envDuration("UPDATE_UPDATE_TIMEOUT", 30*time.Minute); d != 30*time.Minute {
		t.Errorf("非法时长应回退默认, got %v", d)
	}
}
