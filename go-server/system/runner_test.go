package system

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeSessionRunner 测试注入的 SessionRunner 实现。
// Spawn 时创建日志文件，使 tail 一直循环（session 保持 running），便于并发/超时测试。
type fakeSessionRunner struct {
	mu           sync.Mutex
	spawned      int
	killed       int
	aliveQueries int
	alive        bool
}

func (f *fakeSessionRunner) Spawn(ctx context.Context, sess *UpdateSession) (RunnerID, error) {
	f.mu.Lock()
	f.spawned++
	f.mu.Unlock()
	if sess.logFile != "" {
		_ = os.WriteFile(sess.logFile, nil, 0o644)
	}
	return RunnerID("lab-updater-" + sess.ID), nil
}

func (f *fakeSessionRunner) Kill(id RunnerID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killed++
	return nil
}

func (f *fakeSessionRunner) Alive(id RunnerID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aliveQueries++
	return f.alive
}

func (f *fakeSessionRunner) stats() (spawned, killed, aliveQueries int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spawned, f.killed, f.aliveQueries
}

// TestServiceGoEngineSpawnsRunner UPDATE_ENGINE=go：Trigger 走 runner 派发而非脚本。
func TestServiceGoEngineSpawnsRunner(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.engine = "go"
	svc.logDir = t.TempDir()
	fake := &fakeSessionRunner{}
	svc.runner = fake

	id, err := svc.Trigger()
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if !validSessionID(id) {
		t.Errorf("Trigger 返回非法 session id: %q", id)
	}
	if spawned, _, _ := fake.stats(); spawned != 1 {
		t.Errorf("runner spawned = %d, want 1", spawned)
	}
	sess, ok := svc.SessionStatus(id)
	if !ok {
		t.Fatal("session not found")
	}
	sess.mu.Lock()
	rid := sess.runnerID
	sess.mu.Unlock()
	if rid != RunnerID("lab-updater-"+id) {
		t.Errorf("session runnerID = %q, want %q", rid, "lab-updater-"+id)
	}
}

// TestTriggerGoEngineSkipsScriptCheck go 引擎不依赖 update.sh。
func TestTriggerGoEngineSkipsScriptCheck(t *testing.T) {
	svc := NewService(t.TempDir()) // 临时目录无 .hermes/update.sh
	svc.engine = "go"
	svc.logDir = t.TempDir()
	svc.runner = &fakeSessionRunner{}

	if _, err := svc.Trigger(); err != nil {
		t.Fatalf("go 引擎不应检查 update.sh: %v", err)
	}
}

// TestServiceTimeoutKillsRunner 超时看门狗 → kill runner 并判失败。
func TestServiceTimeoutKillsRunner(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.engine = "go"
	svc.timeout = 100 * time.Millisecond
	fake := &fakeSessionRunner{alive: true}
	svc.runner = fake

	dir := t.TempDir()
	id := "upd_timeout000"
	sess := &UpdateSession{
		ID:        id,
		Status:    "running",
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		logFile:   filepath.Join(dir, "x.log"),
		doneFile:  filepath.Join(dir, "x.done"),
		maxSubs:   4,
	}
	if err := os.WriteFile(sess.logFile, nil, 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	svc.runScript(sess)

	select {
	case <-sess.done:
	case <-time.After(3 * time.Second):
		t.Fatal("超时看门狗未收尾 session")
	}
	sess.mu.Lock()
	status, exitCode := sess.Status, sess.ExitCode
	sess.mu.Unlock()
	if status != "done" || exitCode != -2 {
		t.Errorf("timeout 后 status=%q exit_code=%d, want done/-2", status, exitCode)
	}
	if _, killed, _ := fake.stats(); killed != 1 {
		t.Errorf("runner killed = %d, want 1", killed)
	}
}

// TestRecoverFromDiskRunnerAlive runner 仍存活 → 继续 tail（恢复为 running）。
func TestRecoverFromDiskRunnerAlive(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	svc.runner = &fakeSessionRunner{alive: true}

	id := "upd_alive00000"
	logPath := filepath.Join(svc.logDir, "lab-update-"+id+".log")
	if err := os.WriteFile(logPath, []byte("[UPDATE] line\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := os.WriteFile(logPath+".runner", []byte("lab-updater-"+id+"\n"), 0o644); err != nil {
		t.Fatalf("write runner file: %v", err)
	}

	sess := svc.recoverFromDisk(id)
	if sess == nil {
		t.Fatal("recoverFromDisk = nil")
	}
	if sess.Status != "running" {
		t.Errorf("status = %q, want running (runner 存活继续 tail)", sess.Status)
	}
	svc.finish(sess, -1, false) // 停掉 tail goroutine，防泄漏
}

// TestRecoverFromDiskRunnerDead runner 已死且无 marker → 判中断。
func TestRecoverFromDiskRunnerDead(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	svc.runner = &fakeSessionRunner{alive: false}

	id := "upd_dead000000"
	logPath := filepath.Join(svc.logDir, "lab-update-"+id+".log")
	if err := os.WriteFile(logPath, []byte("[UPDATE] line\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := os.WriteFile(logPath+".runner", []byte("lab-updater-"+id+"\n"), 0o644); err != nil {
		t.Fatalf("write runner file: %v", err)
	}

	sess := svc.recoverFromDisk(id)
	if sess == nil {
		t.Fatal("recoverFromDisk = nil")
	}
	if sess.Status != "done" {
		t.Errorf("status = %q, want done (runner 已死判中断)", sess.Status)
	}
	evts := sess.replaySnapshot()
	if len(evts) < 2 || evts[len(evts)-1].Type != "error" {
		t.Errorf("应有中断 error 事件: %+v", evts)
	}
}

// TestTriggerConcurrentSingleSession 并发 Trigger 只产生一个运行中的 session。
func TestTriggerConcurrentSingleSession(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.engine = "go"
	svc.logDir = t.TempDir()
	// alive=true：tail 判定 runner 存活，首个 session 保持 running，后续 Trigger 才能互斥
	svc.runner = &fakeSessionRunner{alive: true}

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Trigger()
		}(i)
	}
	wg.Wait()

	okCount := 0
	for _, err := range errs {
		if err == nil {
			okCount++
		}
	}
	if okCount != 1 {
		t.Errorf("并发 Trigger 成功 %d 次, want 1: %v", okCount, errs)
	}
}
