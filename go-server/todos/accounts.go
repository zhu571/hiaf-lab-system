package todos

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// cmdRunner 抽象 ntfy CLI 子进程执行：支持按调用传 env（NTFY_PASSWORD 免交互传密，
// 密码不出现在 argv/ps）。与 system.CmdRunner 同型，但后者不支持 per-call env，
// 故在本模块内定义（测试注入 fake 断言 argv 与 env）。
type cmdRunner interface {
	Run(ctx context.Context, name string, args []string, env []string) (stdout, stderr string, err error)
}

type execNtfyRunner struct{}

// NewExecNtfyRunner 构造真实 ntfy CLI 执行器。
func NewExecNtfyRunner() cmdRunner {
	return execNtfyRunner{}
}

func (execNtfyRunner) Run(ctx context.Context, name string, args []string, env []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// ntfyCLIClient 生产实现：直写共享 auth-file（server 容器内 ntfy CLI，两容器挂同一卷）。
type ntfyCLIClient struct {
	runner  cmdRunner
	backoff func(attempt int) time.Duration
}

func NewNtfyCLIClient(runner cmdRunner) ntfyClient {
	return &ntfyCLIClient{runner: runner, backoff: func(attempt int) time.Duration {
		// 指数退避：1s、2s、4s（database is locked 双保险，WAL+busy_timeout 之上）。
		return time.Duration(1<<uint(attempt)) * time.Second
	}}
}

// runCLI 执行 ntfy 命令并处理 database is locked 重试（≤3 次）与 stderr 脱敏日志。
func (c *ntfyCLIClient) runCLI(ctx context.Context, args []string, env []string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		stdout, stderr, err := c.runner.Run(ctx, "ntfy", args, env)
		if err == nil {
			return stdout, nil
		}
		lastErr = err
		msg := strings.ToLower(stderr)
		if strings.Contains(msg, "database is locked") && attempt < 2 {
			time.Sleep(c.backoff(attempt))
			continue
		}
		// stderr 脱敏后作为错误返回（ntfy CLI 不会回显 env 传入的密码）；
		// stderr 为空时回退到退出错误本身，避免丢失 "exit status 1" 等诊断。
		if trimmed := strings.TrimSpace(stderr); trimmed != "" {
			return "", errors.New(trimmed)
		}
		return "", err
	}
	return "", lastErr
}

// EnsureUser 建号；密码经 env 传递。CLI 报"用户已存在"类错误视为成功（幂等）。
func (c *ntfyCLIClient) EnsureUser(name, password string) error {
	_, err := c.runCLI(context.Background(), []string{"user", "add", name}, []string{"NTFY_PASSWORD=" + password})
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "already exists") || strings.Contains(msg, "exists") {
		return nil
	}
	return err
}

// ResetPassword 重置密码（provision 流程：旧密码立即失效）。
func (c *ntfyCLIClient) ResetPassword(name, password string) error {
	_, err := c.runCLI(context.Background(), []string{"user", "change-pass", name}, []string{"NTFY_PASSWORD=" + password})
	return err
}

// EnsureAccess 授予/刷新 topic 权限（重复授予幂等）。
func (c *ntfyCLIClient) EnsureAccess(topic, user, perm string) error {
	_, err := c.runCLI(context.Background(), []string{"access", user, topic, perm}, nil)
	return err
}

// provisionStore 内存一次性中转 provision_token（进程重启即失效，用户重新 provision 即可）。
type provisionEntry struct {
	token     string
	userID    string
	password  string
	expiresAt time.Time
}

type provisionStore struct {
	mu      sync.Mutex
	entries map[string]provisionEntry // token → entry
	byUser  map[string]string         // userID → 当前有效 token
}

func newProvisionStore() *provisionStore {
	return &provisionStore{entries: map[string]provisionEntry{}, byUser: map[string]string{}}
}

func (s *provisionStore) put(userID, password string, expiresAt time.Time) provisionEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if oldToken, ok := s.byUser[userID]; ok {
		delete(s.entries, oldToken) // 再次 provision 作废旧 token
	}
	token := randomToken()
	entry := provisionEntry{token: token, userID: userID, password: password, expiresAt: expiresAt}
	s.entries[token] = entry
	s.byUser[userID] = token
	return entry
}

// take 一次性兑换：命中且未过期则删除条目并返回；否则 (entry, false)。
func (s *provisionStore) take(token string, now time.Time) (provisionEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[token]
	if !ok {
		return provisionEntry{}, false
	}
	delete(s.entries, token)
	if entry.userID != "" {
		delete(s.byUser, entry.userID)
	}
	if now.After(entry.expiresAt) {
		return provisionEntry{}, false
	}
	return entry, true
}

func randomToken() string {
	// 32 字节 hex 一次性 token（crypto/rand，与 randomPassword 同源）。
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
