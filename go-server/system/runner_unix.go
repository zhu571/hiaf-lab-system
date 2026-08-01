//go:build !windows

package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// newRunner 根据 UPDATE_ENGINE 构造平台 runner：
//   - go：detached runner 容器（复用当前 server 镜像，I1）
//   - shell：bash setsid 兜底（现状行为）
func newRunner(s *Service, engine string) SessionRunner {
	if engine == "go" {
		return &dockerRunner{cmds: NewExecRunner("", nil), cfg: s.spawnConfig()}
	}
	return &shellRunner{repoRoot: s.repoRoot, scriptPath: s.scriptPath}
}

// shellRunner 保留现有 bash 兜底引擎：setsid 脱离启动 update.sh。
type shellRunner struct {
	repoRoot   string
	scriptPath string
}

func (r *shellRunner) Spawn(_ context.Context, sess *UpdateSession) (RunnerID, error) {
	if _, err := os.Stat(r.scriptPath); err != nil {
		return "", ErrScriptMissing
	}
	cmd := exec.Command("/bin/bash", r.scriptPath)
	cmd.Dir = r.repoRoot
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append(os.Environ(),
		"UPDATE_SESSION_ID="+sess.ID,
		"UPDATE_LOG_FILE="+sess.logFile,
		"UPDATE_DONE_FILE="+sess.doneFile,
	)
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrScriptStartFailed, err)
	}
	defer devNull.Close()
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrScriptStartFailed, err)
	}
	return RunnerID(fmt.Sprintf("pid:%d", cmd.Process.Pid)), nil
}

func (r *shellRunner) Kill(id RunnerID) error {
	pid := parsePID(id)
	if pid <= 0 {
		return nil
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL) // 进程组
	_ = syscall.Kill(pid, syscall.SIGKILL)  // 兜底单进程
	return nil
}

func (r *shellRunner) Alive(id RunnerID) bool {
	pid := parsePID(id)
	if pid <= 0 {
		return false
	}
	return syscall.Kill(-pid, 0) == nil || syscall.Kill(pid, 0) == nil
}

func parsePID(id RunnerID) int {
	s := strings.TrimPrefix(string(id), "pid:")
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// dockerRunner 通过 docker CLI 以 `docker run -d` 派发独立 runner 容器。
type dockerRunner struct {
	cmds CmdRunner
	cfg  RunnerSpawnConfig
}

func (r *dockerRunner) Spawn(ctx context.Context, sess *UpdateSession) (RunnerID, error) {
	cfg := r.cfg
	cfg.SessionID = sess.ID
	cfg.LogFile = sess.logFile
	cfg.DoneFile = sess.doneFile
	if cfg.RunnerImage == "" {
		img, err := r.resolveImage(ctx)
		if err != nil {
			return "", fmt.Errorf("%w: 解析 runner 镜像失败: %v", ErrScriptStartFailed, err)
		}
		cfg.RunnerImage = img
	}
	args := buildRunnerCmd(cfg)
	_, stderr, err := r.cmds.Run(ctx, "docker", args...)
	if err != nil {
		return "", fmt.Errorf("%w: %v %s", ErrScriptStartFailed, err, strings.TrimSpace(stderr))
	}
	return RunnerID("lab-updater-" + sess.ID), nil
}

// resolveImage 用 compose config --images server 解析当前 server 镜像名（runner 复用旧镜像）。
func (r *dockerRunner) resolveImage(ctx context.Context) (string, error) {
	composeArgs := []string{"compose", "-f", r.cfg.composeFileAbs(), "-p", r.cfg.project(), "config", "--images", "server"}
	out, _, err := r.cmds.Run(ctx, "docker", composeArgs...)
	if err != nil {
		return "", err
	}
	images, perr := parseComposeImages([]byte(out))
	if perr != nil || len(images) == 0 {
		return "", fmt.Errorf("compose config --images server 无输出")
	}
	return images[0], nil
}

func (r *dockerRunner) Kill(id RunnerID) error {
	ctx := context.Background()
	_, _, _ = r.cmds.Run(ctx, "docker", "kill", string(id))
	_, _, _ = r.cmds.Run(ctx, "docker", "rm", "-f", string(id))
	return nil
}

func (r *dockerRunner) Alive(id RunnerID) bool {
	out, _, err := r.cmds.Run(context.Background(), "docker", "inspect", "-f", "{{.State.Running}}", string(id))
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "true"
}

func (c RunnerSpawnConfig) composeFileAbs() string {
	if filepath.IsAbs(c.ComposeFile) {
		return c.ComposeFile
	}
	return filepath.Join(c.RepoRoot, c.ComposeFile)
}

func (c RunnerSpawnConfig) project() string {
	if c.ProjectName != "" {
		return c.ProjectName
	}
	base := filepath.Base(filepath.Dir(c.composeFileAbs()))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "deploy"
	}
	return base
}

// buildRunnerCmd 组装 `docker run -d` 参数（纯函数，可单测）。
// 仓库以宿主绝对路径挂载（git/secrets/migrations/构建上下文一致），git 凭据只读透传。
func buildRunnerCmd(cfg RunnerSpawnConfig) []string {
	args := []string{
		"run", "-d", "--rm",
		"--name", "lab-updater-" + cfg.SessionID,
		"--network", "host",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", cfg.RepoRoot + ":" + cfg.RepoRoot,
	}
	if cfg.LogDir != "" {
		args = append(args, "-v", cfg.LogDir+":"+cfg.LogDir)
	}
	if cfg.BackupDir != "" {
		args = append(args, "-v", cfg.BackupDir+":"+cfg.BackupDir)
	}
	if cfg.GitHome != "" {
		args = append(args,
			"-v", filepath.Join(cfg.GitHome, ".gitconfig")+":"+filepath.Join(cfg.GitHome, ".gitconfig")+":ro",
			"-v", filepath.Join(cfg.GitHome, ".ssh")+":"+filepath.Join(cfg.GitHome, ".ssh")+":ro",
			"-e", "HOME="+cfg.GitHome,
		)
	}
	if cfg.RunUID > 0 && cfg.RunGID > 0 {
		args = append(args, "--user", strconv.Itoa(cfg.RunUID)+":"+strconv.Itoa(cfg.RunGID))
	}
	if cfg.DockerGID > 0 {
		args = append(args, "--group-add", strconv.Itoa(cfg.DockerGID))
	}
	args = append(args,
		"-e", "UPDATE_SESSION_ID="+cfg.SessionID,
		"-e", "UPDATE_LOG_FILE="+cfg.LogFile,
		"-e", "UPDATE_DONE_FILE="+cfg.DoneFile,
		"-e", "UPDATE_REPO_ROOT="+cfg.RepoRoot,
		"-e", "UPDATE_PROJECT="+cfg.project(),
		"-e", "UPDATE_COMPOSE_FILE="+cfg.ComposeFile,
		"-e", "UPDATE_NTFY_URL="+cfg.NtfyURL,
		"-e", "UPDATE_BACKUP_DIR="+cfg.BackupDir,
		"-e", "GIT_TERMINAL_PROMPT=0",
	)
	args = append(args, cfg.RunnerImage)
	if cfg.Engine == "shell" {
		args = append(args, "bash", filepath.Join(cfg.RepoRoot, ".hermes", "update.sh"))
	} else {
		args = append(args, "lab-update", "--session", cfg.SessionID, "--repo", cfg.RepoRoot)
	}
	return append(args, cfg.Flags...)
}
