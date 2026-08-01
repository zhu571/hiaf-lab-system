//go:build !windows

package system

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// newRunner 根据 UPDATE_ENGINE 构造平台 runner。
// go 引擎与 shell 兜底引擎都派发 detached runner 容器（I1：独立于 server 容器，
// server 重建不影响它）；区别只在 entrypoint —— go 跑 lab-update 流水线，shell 跑
// update.sh。shell 引擎不再在 server 容器内直接 bash（server 容器非 root 且仓库只读，
// 无法写 .git，无法满足 I1），统一走 runner 容器 + 仓库 rw 挂载。
func newRunner(s *Service, engine string) SessionRunner {
	return &dockerRunner{cmds: NewExecRunner("", nil), cfg: s.spawnConfig()}
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

// orphanReapAge 孤儿 runner 判定阈值：server 进程重启前 spawn 但未登记 session 的
// 更新容器，其自身 30min 更新预算内应完成并自行退出（--rm 清理）；
// 超过该阈值仍未退出且不属于任何存活 session → 判定为孤儿，强制回收。
const orphanReapAge = 40 * time.Minute

// Reap 列出 lab-updater-* 容器，回收运行超过 orphanReapAge 且不在 protect 集合中的孤儿。
// server 崩溃前 spawn 成功但未写 .runner 文件的容器由此兜底清理（设计 §10）。
func (r *dockerRunner) Reap(ctx context.Context, protect map[RunnerID]bool) error {
	out, _, err := r.cmds.Run(ctx, "docker", "ps", "-q", "--filter", "name=lab-updater-")
	if err != nil {
		return err
	}
	for _, cid := range strings.Fields(out) {
		insp, _, ierr := r.cmds.Run(ctx, "docker", "inspect",
			"-f", "{{.Name}} {{.State.Running}} {{.State.StartedAt}}", cid)
		if ierr != nil {
			continue
		}
		parts := strings.Fields(insp)
		if len(parts) < 3 || parts[1] != "true" {
			continue // inspect 失败或已不在运行，交给 docker 自身清理
		}
		name := strings.TrimPrefix(parts[0], "/")
		started, perr := time.Parse(time.RFC3339, parts[2])
		if perr != nil || time.Since(started) < orphanReapAge {
			continue // 启动时间不可解析或未超阈值：保持观望
		}
		if protect[RunnerID(name)] {
			continue // 仍属于存活 session，不能动
		}
		slog.Info("reap orphan update runner container", "container", name)
		r.cmds.Run(ctx, "docker", "kill", name)
		r.cmds.Run(ctx, "docker", "rm", "-f", name)
	}
	return nil
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
// shell 引擎复用同一 runner 容器（I1），entrypoint 换成 bash update.sh。
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
