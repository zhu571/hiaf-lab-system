//go:build !windows

package system

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// hostRequest 是 server 写给宿主侧 systemd 更新单元的请求文件（AR-06b）。
type hostRequest struct {
	SessionID   string   `json:"session_id"`
	RepoRoot    string   `json:"repo_root"`
	ComposeFile string   `json:"compose_file"`
	ProjectName string   `json:"project_name"`
	Branch      string   `json:"branch"`
	Flags       []string `json:"flags"`
	RequestedAt string   `json:"requested_at"`
	Nonce       string   `json:"nonce"`
	Signature   string   `json:"signature,omitempty"`
}

// hostRunner 不调用 Docker API：只把请求落盘，由宿主侧 systemd path 单元执行。
// 因此 server 容器无需挂载 docker.sock。
type hostRunner struct {
	cfg        RunnerSpawnConfig
	requestDir string
	logDir     string
	keyFile    string
}

func (r *hostRunner) Spawn(ctx context.Context, sess *UpdateSession) (RunnerID, error) {
	if err := os.MkdirAll(r.requestDir, 0o700); err != nil {
		return "", fmt.Errorf("create request dir: %w", err)
	}
	req := hostRequest{
		SessionID:   sess.ID,
		RepoRoot:    r.cfg.RepoRoot,
		ComposeFile: r.cfg.ComposeFile,
		ProjectName: r.cfg.ProjectName,
		Branch:      r.cfg.Branch,
		Flags:       append([]string(nil), r.cfg.Flags...),
		RequestedAt: time.Now().Format(time.RFC3339),
		Nonce:       fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid()),
	}
	if key, err := r.readKey(); err != nil {
		return "", err
	} else if len(key) > 0 {
		req.Signature = signHostRequest(key, req)
	}
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal update request: %w", err)
	}
	path := filepath.Join(r.requestDir, sess.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", fmt.Errorf("write update request: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("publish update request: %w", err)
	}
	return RunnerID("host:" + sess.ID), nil
}

func (r *hostRunner) Kill(id RunnerID) error {
	sessionID, err := hostSessionID(id)
	if err != nil {
		return err
	}
	// 宿主侧 runner 在每个步骤边界检查 .cancel；不直接杀进程，避免半回滚。
	if err := os.MkdirAll(r.requestDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(r.requestDir, sessionID+".json.cancel")
	return os.WriteFile(path, []byte(time.Now().Format(time.RFC3339)), 0o600)
}

func (r *hostRunner) Alive(id RunnerID) bool {
	sessionID, err := hostSessionID(id)
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(r.logDir, "lab-update-"+sessionID+".done")); err == nil {
		return false
	}
	return true
}

func (r *hostRunner) Reap(ctx context.Context, protect map[RunnerID]bool) error {
	entries, err := os.ReadDir(r.requestDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := time.Now().Add(-6 * time.Hour)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		sessionID := strings.TrimSuffix(name, ".json")
		if protect[RunnerID("host:"+sessionID)] {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(r.requestDir, name)); err != nil && !os.IsNotExist(err) {
			slog.Warn("reap host update request", "session", sessionID, "error", err)
		}
		_ = os.Remove(filepath.Join(r.requestDir, name+".cancel"))
	}
	return nil
}

func (r *hostRunner) readKey() ([]byte, error) {
	if r.keyFile == "" {
		return nil, nil
	}
	key, err := os.ReadFile(r.keyFile)
	if err != nil {
		return nil, fmt.Errorf("read update request key: %w", err)
	}
	return []byte(strings.TrimSpace(string(key))), nil
}

func hostSessionID(id RunnerID) (string, error) {
	s := string(id)
	if !strings.HasPrefix(s, "host:") || len(s) <= len("host:") {
		return "", fmt.Errorf("invalid host runner id %q", s)
	}
	return strings.TrimPrefix(s, "host:"), nil
}

// signHostRequest 对稳定字段做 HMAC，宿主侧 runner 用同一 key 验签后再执行。
func signHostRequest(key []byte, req hostRequest) string {
	canonical := strings.Join([]string{
		req.SessionID, req.RepoRoot, req.ComposeFile, req.ProjectName,
		req.Branch, strings.Join(req.Flags, ","), req.RequestedAt, req.Nonce,
	}, "\n")
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}
