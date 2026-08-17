//go:build windows

package system

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
)

// newRunner Windows 本地开发实现：进程内 goroutine 跑真实流水线，注入 fake CmdRunner
// 让流水线在无 docker 环境端到端执行（detect/dry-run/回滚分派与真实一致）。
// 承诺：Windows 可编译、可单测、可本地跑流水线逻辑；不承诺 Windows 上跑真实容器部署。
func newRunner(s *Service, _ string) SessionRunner {
	return &localRunner{
		repoRoot:    s.repoRoot,
		composeFile: s.composeFile,
		projectName: s.projectName,
		ntfyURL:     s.ntfyURL,
		backupDir:   s.backupDir,
		force:       s.force,
		dryRun:      s.dryRun,
		noRollback:  s.noRollback,
		active:      make(map[string]context.CancelFunc),
	}
}

// localRunner 以进程内 goroutine 运行 updater.RunSteps。
type localRunner struct {
	repoRoot    string
	composeFile string
	projectName string
	ntfyURL     string
	backupDir   string
	force       bool
	dryRun      bool
	noRollback  bool

	mu     sync.Mutex
	active map[string]context.CancelFunc
}

func (r *localRunner) Spawn(ctx context.Context, sess *UpdateSession) (RunnerID, error) {
	id := RunnerID("local:" + sess.ID)
	runCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.active[string(id)] = cancel
	r.mu.Unlock()

	cfg := &UpdateConfig{
		RepoRoot:        r.repoRoot,
		ComposeFile:     r.composeFile,
		ProjectName:     r.projectName,
		SessionID:       sess.ID,
		LogFile:         sess.logFile,
		DoneFile:        sess.doneFile,
		NtfyURL:         r.ntfyURL,
		BackupDir:       r.backupDir,
		Force:           r.force,
		DryRun:          r.dryRun,
		NoRollback:      r.noRollback,
		UpdateTimeout:   defaultTimeout,
		RollbackTimeout: defaultTimeout,
	}
	log := newLocalLogger(sess.logFile)
	p := NewPipeline(cfg, localDevCmdRunner(r.repoRoot), log)
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.active, string(id))
			r.mu.Unlock()
		}()
		defer log.Close() // Windows 下打开中的日志文件无法删除，退出时关闭句柄
		p.Run(runCtx)
	}()
	return id, nil
}

func (r *localRunner) Kill(id RunnerID) error {
	r.mu.Lock()
	cancel, ok := r.active[string(id)]
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return nil
}

func (r *localRunner) Alive(id RunnerID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.active[string(id)]
	return ok
}

// Reap Windows 本地开发实现无孤儿容器概念：进程内 goroutine 由本进程生命周期天然清理。
func (r *localRunner) Reap(ctx context.Context, protect map[RunnerID]bool) error {
	return nil
}

// localDevCmdRunner 是 Windows 本地跑的 fake CmdRunner：
// git/df/curl 走真实或模拟，docker 一律模拟为不可用（流水线确定性失败、逻辑可跑）。
func localDevCmdRunner(repoRoot string) CmdRunner {
	realGit := NewExecRunner(repoRoot, nil)
	return &fakeCmdRunner{fn: func(c Call) (string, string, error) {
		switch c.Name {
		case "git":
			return realGit.Run(context.Background(), c.Name, c.Args...)
		case "df":
			return "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/root 12345678 1234567 10981111 1% /opt\n", "", nil
		case "docker":
			return "", "docker 不可用（Windows 本地模拟）", errors.New("docker 不可用")
		default:
			return "", "", nil
		}
	}}
}

// newLocalLogger 复用 Logger；日志文件不可用时退回 stdout，保证输出不蒸发。
func newLocalLogger(path string) *Logger {
	l, err := NewLogger(path, nil)
	if err != nil {
		slog.Warn("更新日志文件不可用，退回 stdout", "path", path, "error", err)
		l, _ = NewLogger("", os.Stdout)
	}
	return l
}
