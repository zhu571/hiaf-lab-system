package todos

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fakeCmdRunner 记录 argv/env 并按脚本返回结果。
type fakeCmdRunner struct {
	calls    int
	results  []runResult // 依次消费；不足则用最后一个
	lastArgs []string
	lastEnv  []string
}

type runResult struct {
	stdout string
	stderr string
	err    bool
}

func (f *fakeCmdRunner) Run(ctx context.Context, name string, args []string, env []string) (string, string, error) {
	f.calls++
	f.lastArgs = args
	f.lastEnv = env
	idx := f.calls - 1
	if idx >= len(f.results) {
		idx = len(f.results) - 1
	}
	if idx < 0 {
		return "", "", nil
	}
	r := f.results[idx]
	if r.err {
		return r.stdout, r.stderr, &fakeExitErr{msg: r.stderr}
	}
	return r.stdout, r.stderr, nil
}

type fakeExitErr struct{ msg string }

func (e *fakeExitErr) Error() string { return e.msg }

func TestEnsureUserPasswordOnlyInEnv(t *testing.T) {
	f := &fakeCmdRunner{results: []runResult{{}}}
	c := &ntfyCLIClient{runner: f, backoff: func(int) time.Duration { return 0 }}
	if err := c.EnsureUser("todo-alice", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	// argv 不含密码
	for _, arg := range f.lastArgs {
		if strings.Contains(arg, "s3cr3t") {
			t.Fatalf("password leaked into argv: %v", f.lastArgs)
		}
	}
	// env 含 NTFY_PASSWORD
	joined := strings.Join(f.lastEnv, "\n")
	if !strings.Contains(joined, "NTFY_PASSWORD=s3cr3t") {
		t.Fatalf("NTFY_PASSWORD missing from env: %v", f.lastEnv)
	}
	if len(f.lastArgs) != 3 || f.lastArgs[0] != "user" || f.lastArgs[1] != "add" || f.lastArgs[2] != "todo-alice" {
		t.Fatalf("unexpected argv: %v", f.lastArgs)
	}
}

func TestEnsureUserAlreadyExistsIsSuccess(t *testing.T) {
	for _, msg := range []string{"user todo-alice already exists", "user already exists: todo-alice"} {
		f := &fakeCmdRunner{results: []runResult{{stderr: msg, err: true}}}
		c := &ntfyCLIClient{runner: f, backoff: func(int) time.Duration { return 0 }}
		if err := c.EnsureUser("todo-alice", "pw"); err != nil {
			t.Fatalf("already-exists should be success, got %v", err)
		}
	}
}

func TestEnsureUserRealError(t *testing.T) {
	f := &fakeCmdRunner{results: []runResult{{stderr: "boom", err: true}}}
	c := &ntfyCLIClient{runner: f, backoff: func(int) time.Duration { return 0 }}
	if err := c.EnsureUser("todo-alice", "pw"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestDatabaseLockedRetry(t *testing.T) {
	// 前两次 database is locked，第三次成功
	f := &fakeCmdRunner{results: []runResult{
		{stderr: "database is locked", err: true},
		{stderr: "database is locked", err: true},
		{},
	}}
	c := &ntfyCLIClient{runner: f, backoff: func(int) time.Duration { return 0 }}
	if err := c.EnsureUser("todo-alice", "pw"); err != nil {
		t.Fatal(err)
	}
	if f.calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", f.calls)
	}
}

func TestDatabaseLockedExhaustsRetries(t *testing.T) {
	f := &fakeCmdRunner{results: []runResult{
		{stderr: "database is locked", err: true},
		{stderr: "database is locked", err: true},
		{stderr: "database is locked", err: true},
	}}
	c := &ntfyCLIClient{runner: f, backoff: func(int) time.Duration { return 0 }}
	if err := c.EnsureUser("todo-alice", "pw"); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if f.calls != 3 {
		t.Fatalf("expected 3 attempts max, got %d", f.calls)
	}
}

func TestResetPasswordAndAccessArgv(t *testing.T) {
	f := &fakeCmdRunner{results: []runResult{{}}}
	c := &ntfyCLIClient{runner: f, backoff: func(int) time.Duration { return 0 }}
	if err := c.ResetPassword("todo-alice", "pw1"); err != nil {
		t.Fatal(err)
	}
	if len(f.lastArgs) != 3 || f.lastArgs[0] != "user" || f.lastArgs[1] != "change-pass" || f.lastArgs[2] != "todo-alice" {
		t.Fatalf("unexpected change-pass argv: %v", f.lastArgs)
	}
	if err := c.EnsureAccess("lab-todos-abc", "todo-alice", "read-only"); err != nil {
		t.Fatal(err)
	}
	if len(f.lastArgs) != 4 || f.lastArgs[0] != "access" || f.lastArgs[3] != "read-only" {
		t.Fatalf("unexpected access argv: %v", f.lastArgs)
	}
}

// ---------- provisionStore ----------

func TestProvisionStoreOneTimeAndExpiry(t *testing.T) {
	s := newProvisionStore()
	now := testNow()
	e1 := s.put("u1", "pw1", now.Add(24*time.Hour))

	// 一次性：take 后条目消失
	got, ok := s.take(e1.token, now)
	if !ok || got.password != "pw1" {
		t.Fatalf("first take failed: %+v %v", got, ok)
	}
	if _, ok := s.take(e1.token, now); ok {
		t.Fatal("second take must fail")
	}

	// 过期
	e2 := s.put("u2", "pw2", now.Add(time.Hour))
	if _, ok := s.take(e2.token, now.Add(2*time.Hour)); ok {
		t.Fatal("expired token must fail")
	}

	// 再次 provision 作废旧 token
	e3 := s.put("u3", "pw3", now.Add(time.Hour))
	e4 := s.put("u3", "pw4", now.Add(time.Hour))
	if _, ok := s.take(e3.token, now); ok {
		t.Fatal("old token must be invalidated by re-provision")
	}
	if _, ok := s.take(e4.token, now); !ok {
		t.Fatal("new token must work")
	}
}
