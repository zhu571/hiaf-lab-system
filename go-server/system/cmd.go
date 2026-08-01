package system

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"sync"
)

// Call 记录一次命令调用（fake runner 用于断言流水线的命令序列）。
type Call struct {
	Name string
	Args []string
}

// CmdRunner 抽象 updater.go 的全部 docker/git 子命令执行。
// 生产用 execRunner（真实 os/exec），测试注入 fakeCmdRunner。
type CmdRunner interface {
	// Run 执行命令并返回 stdout/stderr/退出错误。
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
	// RunOK 只关心退出码，stdout/stderr 忽略。
	RunOK(ctx context.Context, name string, args ...string) error
}

// execRunner 真实执行器：在 dir 目录执行，叠加 env 环境变量。
type execRunner struct {
	dir string
	env []string
}

// NewExecRunner 构造真实 CmdRunner。dir 为命令工作目录；env 追加到进程环境。
func NewExecRunner(dir string, env []string) CmdRunner {
	return &execRunner{dir: dir, env: env}
}

func (r *execRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), r.env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func (r *execRunner) RunOK(ctx context.Context, name string, args ...string) error {
	_, _, err := r.Run(ctx, name, args...)
	return err
}

// fakeCmdRunner 测试注入：记录每次调用的命令与参数，可预设返回。
type fakeCmdRunner struct {
	mu    sync.Mutex
	calls []Call
	// fn 为每次调用的处理器；nil 时返回空输出与 nil 错误。
	fn func(call Call) (string, string, error)
}

func (f *fakeCmdRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, Call{Name: name, Args: args})
	f.mu.Unlock()
	if f.fn == nil {
		return "", "", nil
	}
	return f.fn(Call{Name: name, Args: args})
}

func (f *fakeCmdRunner) RunOK(ctx context.Context, name string, args ...string) error {
	_, _, err := f.Run(ctx, name, args...)
	return err
}

// callsSnapshot 返回已记录的命令调用副本。
func (f *fakeCmdRunner) callsSnapshot() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	copy(out, f.calls)
	return out
}

// matchCommand 判断调用是否命中 name + 子串约束（args 前缀）。
func (c Call) match(name string, argSubstrings ...string) bool {
	if c.Name != name {
		return false
	}
	for _, sub := range argSubstrings {
		found := false
		for _, a := range c.Args {
			if a == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
