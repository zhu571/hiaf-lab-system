//go:build !windows

package system

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHostRunnerSpawnWritesSignedRequest(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "requests")
	logDir := filepath.Join(root, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(root, "key")
	if err := os.WriteFile(keyFile, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &hostRunner{
		cfg: RunnerSpawnConfig{
			RepoRoot: root, ComposeFile: "deploy/docker-compose.yml",
			ProjectName: "deploy", Branch: "main", Flags: []string{"--dry-run"},
		},
		requestDir: reqDir,
		logDir:     logDir,
		keyFile:    keyFile,
	}
	id, err := r.Spawn(context.Background(), &UpdateSession{ID: "sess_1"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if id != RunnerID("host:sess_1") {
		t.Fatalf("id = %q", id)
	}
	data, err := os.ReadFile(filepath.Join(reqDir, "sess_1.json"))
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	var req HostRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.SessionID != "sess_1" || req.Branch != "main" || len(req.Flags) != 1 {
		t.Fatalf("unexpected request: %+v", req)
	}
	if req.Signature == "" || req.Signature != SignHostRequest([]byte("secret"), req) {
		t.Fatalf("signature mismatch: %q", req.Signature)
	}
}

func TestHostRunnerLoadAndApplyRequest(t *testing.T) {
	root := t.TempDir()
	keyFile := filepath.Join(root, "key")
	if err := os.WriteFile(keyFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UPDATE_REQUEST_KEY_FILE", keyFile)
	req := HostRequest{
		SessionID: "sess_3", RepoRoot: root, ComposeFile: "deploy/docker-compose.yml",
		ProjectName: "deploy", Branch: "main", Flags: []string{"--force"},
		RequestedAt: time.Now().Format(time.RFC3339), Nonce: "n1",
	}
	req.Signature = SignHostRequest([]byte("secret"), req)
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "sess_3.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &UpdateConfig{}
	if err := ApplyHostRequest(cfg, path); err != nil {
		t.Fatalf("ApplyHostRequest: %v", err)
	}
	if cfg.SessionID != "sess_3" || !cfg.Force || cfg.Branch != "main" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.DoneFile != filepath.Join(root, ".hermes", "updates", "lab-update-sess_3.done") {
		t.Fatalf("done file = %q", cfg.DoneFile)
	}
}

func TestHostRunnerAliveAndKill(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "logs")
	reqDir := filepath.Join(root, "requests")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &hostRunner{requestDir: reqDir, logDir: logDir}
	id := RunnerID("host:sess_2")
	if !r.Alive(id) {
		t.Fatal("Alive should be true before done")
	}
	if err := r.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(reqDir, "sess_2.json.cancel")); err != nil {
		t.Fatalf("cancel marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "lab-update-sess_2.done"), []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r.Alive(id) {
		t.Fatal("Alive should be false after done marker")
	}
}

func TestHostRunnerReapOldRequests(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "requests")
	if err := os.MkdirAll(reqDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(reqDir, "old.json")
	if err := os.WriteFile(oldPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-7 * time.Hour)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}
	keepPath := filepath.Join(reqDir, "keep.json")
	if err := os.WriteFile(keepPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &hostRunner{requestDir: reqDir, logDir: filepath.Join(root, "logs")}
	if err := r.Reap(context.Background(), map[RunnerID]bool{"host:keep": true}); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old request should be removed: %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("protected request should stay: %v", err)
	}
}
