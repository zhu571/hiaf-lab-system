package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNewServiceUpdateEnvDefaults R10/R12/R15/R2：UPDATE_* env 的 server 侧默认值。
func TestNewServiceUpdateEnvDefaults(t *testing.T) {
	t.Setenv("UPDATE_UPDATE_TIMEOUT", "")
	t.Setenv("UPDATE_BRANCH", "")
	t.Setenv("UPDATE_SCRIPT_PATH", "")
	t.Setenv("UPDATE_GIT_HOME", "")
	repo := t.TempDir()
	s := NewService(repo)

	// R10：看门狗默认 30min
	if s.timeout != 30*time.Minute {
		t.Errorf("timeout = %v, want 30m", s.timeout)
	}
	// R12：分支默认 main
	if s.branch != "main" {
		t.Errorf("branch = %q, want main", s.branch)
	}
	// R15：脚本路径默认 <repo>/.hermes/update.sh（绝对路径）
	if want := filepath.Join(repo, ".hermes", "update.sh"); s.scriptPath != want {
		t.Errorf("scriptPath = %q, want %q", s.scriptPath, want)
	}
	// R2：git 凭据家默认 <repo>/.hermes/runner-home（专用 deploy-key 目录）
	if want := filepath.Join(repo, ".hermes", "runner-home"); s.gitHome != want {
		t.Errorf("gitHome = %q, want %q", s.gitHome, want)
	}

	t.Setenv("UPDATE_UPDATE_TIMEOUT", "45m")
	t.Setenv("UPDATE_BRANCH", "release")
	t.Setenv("UPDATE_SCRIPT_PATH", ".hermes/update-alt.sh")
	t.Setenv("UPDATE_GIT_HOME", "/srv/keys")
	s2 := NewService(t.TempDir())
	if s2.timeout != 45*time.Minute {
		t.Errorf("timeout env = %v, want 45m", s2.timeout)
	}
	if s2.branch != "release" {
		t.Errorf("branch env = %q, want release", s2.branch)
	}
	if !filepath.IsAbs(s2.scriptPath) || filepath.Base(s2.scriptPath) != "update-alt.sh" {
		t.Errorf("相对 UPDATE_SCRIPT_PATH 应解析为仓库内绝对路径: %q", s2.scriptPath)
	}
	if s2.gitHome != "/srv/keys" {
		t.Errorf("gitHome env = %q, want /srv/keys", s2.gitHome)
	}
}

// writeExecutable 原子落盘一个可执行脚本：先写同目录临时文件、fsync、close，
// 再 rename 到目标名。rename 后目标名指向一个已完全关闭的 inode，避免 CI 上
// close 与 fork/exec 的竞态导致 exec 报 ETXTBSY（text file busy）。
func writeExecutable(t *testing.T, path string, content []byte) {
	t.Helper()
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-exec-*")
	if err != nil {
		t.Fatal(err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		t.Fatal(err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		t.Fatal(err)
	}
}

// gitLsRemoteRetry 调用 s.gitLsRemote()；生产实现会吞掉 exec 错误返回空串，
// 因此这里对空结果做有限次重试，兜住残余的 ETXTBSY 瞬时抖动。
func gitLsRemoteRetry(t *testing.T, s *Service) string {
	t.Helper()
	var got string
	for i := 0; i < 20; i++ {
		if got = s.gitLsRemote(); got != "" {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	return got
}

func TestGitLsRemoteUsesDeployKey(t *testing.T) {
	dir := t.TempDir()
	gitHome := filepath.Join(dir, "runner-home")
	capture := filepath.Join(dir, "ssh-command")
	git := filepath.Join(dir, "git")
	writeExecutable(t, git, []byte("#!/bin/sh\nprintf '%s\\n' \"$GIT_SSH_COMMAND\" > \"$CAPTURE\"\nprintf '0123456789012345678901234567890123456789\\tHEAD\\n'\n"))
	t.Setenv("PATH", dir)
	t.Setenv("CAPTURE", capture)
	s := &Service{repoRoot: dir, gitHome: gitHome}
	if got := gitLsRemoteRetry(t, s); got != "0123456789012345678901234567890123456789" {
		t.Fatalf("gitLsRemote = %q", got)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	command := string(data)
	for _, want := range []string{
		"-o BatchMode=yes",
		"-o StrictHostKeyChecking=accept-new",
		"-o IdentitiesOnly=yes",
		"-i " + filepath.Join(gitHome, ".ssh", "id_ed25519"),
		"-o UserKnownHostsFile=" + filepath.Join(gitHome, ".ssh", "known_hosts"),
	} {
		if !strings.Contains(command, want) {
			t.Errorf("GIT_SSH_COMMAND 缺少 %q: %q", want, command)
		}
	}
}

// TestPipelineGitRunHonorsCtx R13：gitRun 透传 ctx 取消（exec.CommandContext 生效），
// 单命令超时由 context.WithTimeout 包装（真实 120s 上限不做慢测）。
func TestPipelineGitRunHonorsCtx(t *testing.T) {
	fake := &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		if c.ctxErr != nil {
			return "", "", c.ctxErr
		}
		return "ok\n", "", nil
	}}
	p := &Pipeline{cfg: &UpdateConfig{RepoRoot: "/r"}, cmds: fake, log: &Logger{}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.gitRun(ctx, "-C", "/r", "fetch", "origin"); err == nil {
		t.Error("ctx 已取消时 gitRun 应返回错误")
	}
	if _, err := p.gitRun(context.Background(), "-C", "/r", "rev-parse", "HEAD"); err != nil {
		t.Errorf("正常 ctx 不应报错: %v", err)
	}
}
