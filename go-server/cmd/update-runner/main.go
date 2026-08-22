// lab-update 是 Go 引擎的 runner 二进制入口：从 flag/env 组装 UpdateConfig，
// 在独立 runner 容器（unix）或进程内 goroutine（windows 本地）中执行 7 步流水线。
// 日志以文本行写入 UPDATE_LOG_FILE（server 侧 tail 该文件推 SSE），结束写 done marker。
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/system"
)

func main() {
	os.Exit(run())
}

// run 返回进程退出码；用 return 代替直接 os.Exit，保证 defer（关闭日志文件等）执行。
func run() int {
	cfg := parseFlags(os.Args[1:])

	log, err := system.NewLogger(cfg.LogFile, os.Stdout)
	if err != nil {
		return 3
	}
	defer log.Close()

	// R8：与宿主机 update.sh 争用同一把仓库共享锁（.hermes/updates/lab-update.lock），
	// 防止 Web 触发更新与手工脚本并发操作同一仓库/compose 项目。
	// 拿不到锁也要写 done marker，让 server 侧 SSE 正常收尾并给出原因。
	release, err := system.AcquireUpdateLock(cfg.RepoRoot)
	if err != nil {
		log.Linef("[ERROR] %v", err)
		_ = system.WriteDoneMarker(cfg.DoneFile, system.DoneMarker{
			ExitCode: 1, EndedAt: time.Now().UTC().Format(time.RFC3339),
		})
		return 1
	}
	defer release()

	// 网络保护与 update.sh 对齐：禁止交互式凭据提示 + 低网速超时兜底。
	env := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_HTTP_LOW_SPEED_LIMIT=1024",
		"GIT_HTTP_LOW_SPEED_TIME=30",
	}
	cmds := system.NewExecRunner(cfg.RepoRoot, env)

	// SIGINT/SIGTERM 都要响应：docker stop 默认发 SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return system.NewPipeline(cfg, cmds, log).Run(ctx)
}

// parseFlags 解析 runner 入参，支持 flag 与 env 双来源（flag 优先）。
// 支持 --session/--repo/--force/--dry-run/--no-rollback，其余从 UPDATE_* env 读取。
func parseFlags(args []string) *system.UpdateConfig {
	fs := flag.NewFlagSet("lab-update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		session    = fs.String("session", envOr("UPDATE_SESSION_ID", ""), "更新 session ID")
		repo       = fs.String("repo", envOr("UPDATE_REPO_ROOT", "/opt/hiaf-lab-system"), "仓库根目录")
		compose    = fs.String("compose-file", envOr("UPDATE_COMPOSE_FILE", "deploy/docker-compose.yml"), "compose 文件（相对仓库）")
		project    = fs.String("project", envOr("UPDATE_PROJECT", ""), "compose 项目名")
		branch     = fs.String("branch", envOr("UPDATE_BRANCH", "main"), "更新分支")
		force      = fs.Bool("force", false, "跳过变更检测，全量重建")
		dryRun     = fs.Bool("dry-run", false, "仅检测变更，不执行实际操作")
		noRollback = fs.Bool("no-rollback", false, "失败时不回滚")
	)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	return &system.UpdateConfig{
		RepoRoot:        *repo,
		ComposeFile:     *compose,
		ProjectName:     *project,
		Branch:          *branch,
		SessionID:       *session,
		LogFile:         envOr("UPDATE_LOG_FILE", ""),
		DoneFile:        envOr("UPDATE_DONE_FILE", ""),
		NtfyURL:         envOr("UPDATE_NTFY_URL", "http://localhost:8085/lab-system"),
		BackupDir:       envOr("UPDATE_BACKUP_DIR", ""),
		Force:           *force,
		DryRun:          *dryRun,
		NoRollback:      *noRollback,
		UpdateTimeout:   envDuration("UPDATE_UPDATE_TIMEOUT", 30*time.Minute),
		RollbackTimeout: envDuration("UPDATE_ROLLBACK_TIMEOUT", 30*time.Minute),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envDuration 解析时长 env，非法或缺失时回退默认值。
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
