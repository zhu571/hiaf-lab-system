package system

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HostRequest 是 server 写给宿主侧 systemd 更新单元的请求文件（AR-06b）。
type HostRequest struct {
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

// LoadHostRequest 读取并（配置了 key 时）校验宿主侧更新请求。
func LoadHostRequest(path, keyFile string) (*HostRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read host request: %w", err)
	}
	var req HostRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("decode host request: %w", err)
	}
	if req.SessionID == "" || req.RepoRoot == "" {
		return nil, fmt.Errorf("host request missing session_id/repo_root")
	}
	if keyFile != "" {
		key, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("read update request key: %w", err)
		}
		if !verifyHostRequest([]byte(strings.TrimSpace(string(key))), req) {
			return nil, fmt.Errorf("host request signature mismatch")
		}
	}
	return &req, nil
}

// SignHostRequest 对稳定字段做 HMAC，宿主侧 runner 用同一 key 验签后再执行。
func SignHostRequest(key []byte, req HostRequest) string {
	canonical := strings.Join([]string{
		req.SessionID, req.RepoRoot, req.ComposeFile, req.ProjectName,
		req.Branch, strings.Join(req.Flags, ","), req.RequestedAt, req.Nonce,
	}, "\n")
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyHostRequest(key []byte, req HostRequest) bool {
	expected := SignHostRequest(key, req)
	return hmac.Equal([]byte(req.Signature), []byte(expected))
}

// ApplyHostRequest 把请求文件应用到 runner 配置（宿主侧 lab-update --request 用）。
func ApplyHostRequest(cfg *UpdateConfig, path string) error {
	req, err := LoadHostRequest(path, os.Getenv("UPDATE_REQUEST_KEY_FILE"))
	if err != nil {
		return err
	}
	cfg.RepoRoot = req.RepoRoot
	cfg.ComposeFile = req.ComposeFile
	cfg.ProjectName = req.ProjectName
	cfg.Branch = req.Branch
	cfg.SessionID = req.SessionID
	for _, flag := range req.Flags {
		switch flag {
		case "--force":
			cfg.Force = true
		case "--dry-run":
			cfg.DryRun = true
		case "--no-rollback":
			cfg.NoRollback = true
		}
	}
	if cfg.LogFile == "" {
		cfg.LogFile = filepath.Join(req.RepoRoot, ".hermes", "updates", "lab-update-"+req.SessionID+".log")
	}
	if cfg.DoneFile == "" {
		cfg.DoneFile = filepath.Join(req.RepoRoot, ".hermes", "updates", "lab-update-"+req.SessionID+".done")
	}
	return nil
}
