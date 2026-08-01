package system

import (
	"context"
	"errors"
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
	reaped       []RunnerID // Reap 记录的受保护外容器名
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

func (f *fakeSessionRunner) Reap(_ context.Context, protect map[RunnerID]bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id := range protect {
		if id != "" {
			f.reaped = append(f.reaped, id)
		}
	}
	return nil
}

func (f *fakeSessionRunner) stats() (spawned, killed, aliveQueries int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spawned, f.killed, f.aliveQueries
}

// noFileRunner 的 Spawn 不创建日志文件，用于验证 tail 对"日志文件延迟出现"的容忍。
type noFileRunner struct{}

func (r *noFileRunner) Spawn(ctx context.Context, sess *UpdateSession) (RunnerID, error) {
	return RunnerID("lab-updater-" + sess.ID), nil
}

func (r *noFileRunner) Kill(id RunnerID) error { return nil }

func (r *noFileRunner) Alive(id RunnerID) bool { return true }

func (r *noFileRunner) Reap(_ context.Context, _ map[RunnerID]bool) error { return nil }

// waitTailExited 等待 tail goroutine 退出（finish 关闭 done 后至多 ~200ms）。
// Windows 上打开中的日志文件无法删除，测试结束前必须等句柄释放。
func waitTailExited(t *testing.T, sess *UpdateSession) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		sess.mu.Lock()
		tailing := sess.tailing
		sess.mu.Unlock()
		if !tailing {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("tail goroutine 未在超时前退出")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// finishAndWaitTail 收尾 session 并等待 tail goroutine 退出（测试清理用）。
func finishAndWaitTail(t *testing.T, svc *Service, sess *UpdateSession) {
	t.Helper()
	svc.finish(sess, 0, true)
	waitTailExited(t, sess)
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

	finishAndWaitTail(t, svc, sess)
}

// TestTriggerGoEngineSkipsScriptCheck go 引擎不依赖 update.sh。
func TestTriggerGoEngineSkipsScriptCheck(t *testing.T) {
	svc := NewService(t.TempDir()) // 临时目录无 .hermes/update.sh
	svc.engine = "go"
	svc.logDir = t.TempDir()
	svc.runner = &fakeSessionRunner{}

	id, err := svc.Trigger()
	if err != nil {
		t.Fatalf("go 引擎不应检查 update.sh: %v", err)
	}
	if sess, ok := svc.SessionStatus(id); ok {
		finishAndWaitTail(t, svc, sess)
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

	waitTailExited(t, sess)
}

// TestRecoverFromDiskRunnerAlive runner 仍存活 → 继续 tail（恢复为 running）。
func TestRecoverFromDiskRunnerAlive(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	svc.runner = &fakeSessionRunner{alive: true}

	id := "upd_alive00000"
	logPath, _, runnerPath := sessionPaths(svc.logDir, id)
	if err := os.WriteFile(logPath, []byte("[UPDATE] line\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := os.WriteFile(runnerPath, []byte("lab-updater-"+id+"\n"), 0o644); err != nil {
		t.Fatalf("write runner file: %v", err)
	}

	sess := svc.recoverFromDisk(id)
	if sess == nil {
		t.Fatal("recoverFromDisk = nil")
	}
	if sess.Status != "running" {
		t.Errorf("status = %q, want running (runner 存活继续 tail)", sess.Status)
	}
	svc.finish(sess, -1, false)
	waitTailExited(t, sess) // 停掉 tail goroutine，防泄漏
}

// TestRecoverFromDiskRunnerDead runner 已死且无 marker → 判中断。
func TestRecoverFromDiskRunnerDead(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	svc.runner = &fakeSessionRunner{alive: false}

	id := "upd_dead000000"
	logPath, _, runnerPath := sessionPaths(svc.logDir, id)
	if err := os.WriteFile(logPath, []byte("[UPDATE] line\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := os.WriteFile(runnerPath, []byte("lab-updater-"+id+"\n"), 0o644); err != nil {
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

	for i, err := range errs {
		if err == nil {
			if sess, ok := svc.SessionStatus(svc.active.ID); ok {
				_ = i
				finishAndWaitTail(t, svc, sess)
				break
			}
		}
	}
}

// TestTailSessionLogWaitsForLogFile 日志文件延迟出现（runner 容器启动慢）时，
// tail 必须重试等待而不是立即把 session 判为失败。
func TestTailSessionLogWaitsForLogFile(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.engine = "go"
	dir := t.TempDir()
	id := "upd_waits00000"
	sess := &UpdateSession{
		ID:         id,
		Status:     "running",
		LogBuffer:  NewRingBuffer(ringBufferCap),
		subs:       make(map[chan SSEEvent]struct{}),
		done:       make(chan struct{}),
		logFile:    filepath.Join(dir, "lab-update-"+id+".log"),
		doneFile:   filepath.Join(dir, "lab-update-"+id+".done"),
		runnerFile: filepath.Join(dir, "lab-update-"+id+".runner"),
		maxSubs:    4,
	}
	svc.runner = &noFileRunner{} // Spawn 不创建日志文件
	svc.runScript(sess)

	time.Sleep(300 * time.Millisecond)
	sess.mu.Lock()
	status := sess.Status
	sess.mu.Unlock()
	if status != "running" {
		t.Fatalf("日志文件未出现时 session 被误判为失败: %q", status)
	}

	if err := os.WriteFile(sess.logFile, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		snap := sess.LogBuffer.Snapshot()
		if len(snap) > 0 && snap[0].text == "hello" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tail 未摄取延迟出现的日志行")
		}
		time.Sleep(50 * time.Millisecond)
	}
	finishAndWaitTail(t, svc, sess)
}

// 重启恢复（#5）：NewService 启动时扫描 logDir，把"有 .log+.runner、无 .done"的
// 中断 session 重建进内存（runner 已死 → 判中断 done，可从 SessionStatus 查到）。
func TestNewServiceScansInterruptedSessions(t *testing.T) {
	dir := t.TempDir()
	id := "upd_bootscan00"
	logPath, _, runnerPath := sessionPaths(dir, id)
	if err := os.WriteFile(logPath, []byte("[UPDATE] line\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := os.WriteFile(runnerPath, []byte("lab-updater-"+id+"\n"), 0o644); err != nil {
		t.Fatalf("write runner: %v", err)
	}
	t.Setenv("UPDATE_LOG_DIR", dir)
	svc := NewService(t.TempDir()) // 真实 runner：无同名容器/进程 → Alive=false
	sess, ok := svc.SessionStatus(id)
	if !ok {
		t.Fatal("NewService 未恢复中断的 session")
	}
	sess.mu.Lock()
	status := sess.Status
	sess.mu.Unlock()
	if status != "done" {
		t.Errorf("status = %q, want done（runner 已死判中断）", status)
	}
}

// 重启恢复（#5）：runner 仍存活的 session 恢复为 running 并挂到 active，
// 重启后的 Trigger 必须收到 ErrUpdateInProgress（防双更新并发）。
func TestRecoveredRunningSessionBlocksTrigger(t *testing.T) {
	dir := t.TempDir()
	id := "upd_bootalive0"
	logPath, _, runnerPath := sessionPaths(dir, id)
	if err := os.WriteFile(logPath, []byte("[UPDATE] line\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := os.WriteFile(runnerPath, []byte("local:"+id+"\n"), 0o644); err != nil {
		t.Fatalf("write runner: %v", err)
	}

	svc := NewService(t.TempDir())
	svc.logDir = dir
	svc.runner = &fakeSessionRunner{alive: true}
	svc.recoverInterruptedSessions() // NewService 启动路径调用的同一函数

	if _, err := svc.Trigger(); !errors.Is(err, ErrUpdateInProgress) {
		t.Errorf("Trigger err = %v, want ErrUpdateInProgress", err)
	}
	svc.mu.Lock()
	sess := svc.active
	svc.mu.Unlock()
	if sess == nil || sess.ID != id {
		t.Fatalf("active 未挂到恢复的 session %s", id)
	}
	svc.finish(sess, -1, false)
	waitTailExited(t, sess)
}

// 回放拼接（#6）：恢复为 running 的 session，回放 = 冻结历史 + 恢复后 tail 的新行
// （新行只进 LogBuffer，新订阅者不应丢失中间行，且 seq 与历史连续）。
func TestReplaySnapshotAppendsTailedLinesAfterHistory(t *testing.T) {
	dir := t.TempDir()
	id := "upd_replaymix0"
	logPath, _, runnerPath := sessionPaths(dir, id)
	if err := os.WriteFile(logPath, []byte("[UPDATE] 旧行一\n[UPDATE] 旧行二\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := os.WriteFile(runnerPath, []byte("lab-updater-"+id+"\n"), 0o644); err != nil {
		t.Fatalf("write runner: %v", err)
	}

	svc := NewService(t.TempDir())
	svc.logDir = dir
	svc.runner = &fakeSessionRunner{alive: true}
	sess := svc.recoverFromDisk(id)
	if sess == nil {
		t.Fatal("recoverFromDisk = nil")
	}
	if sess.Status != "running" {
		t.Fatalf("status = %q, want running", sess.Status)
	}
	sess.ingestLine("[UPDATE] 新行三") // 模拟 tail 续读产生的新行

	evts := sess.replaySnapshot()
	if len(evts) != 3 {
		t.Fatalf("回放事件数 = %d, want 3: %+v", len(evts), evts)
	}
	for i, want := range []string{"[UPDATE] 旧行一", "[UPDATE] 旧行二", "[UPDATE] 新行三"} {
		if evts[i].Text != want || evts[i].Seq != i+1 {
			t.Errorf("事件 %d = %+v, want text=%q seq=%d", i, evts[i], want, i+1)
		}
	}
	svc.finish(sess, 0, true)
	waitTailExited(t, sess)
}

// 孤儿回收（设计 §10）：sweepOnce 必须把"仍存活 session 的 runner"传入受保护名单，
// 孤儿（无对应 session 的容器）交由 runner.Reap 处理；done 的 session 不在保护名单。
func TestSweepOncePassesLiveRunnerIDsToReap(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.engine = "go"
	svc.logDir = t.TempDir()
	fake := &fakeSessionRunner{}
	svc.runner = fake

	// 运行中的 session → 受保护
	runningID := "upd_running00"
	running := &UpdateSession{
		ID: runningID, Status: "running", LogBuffer: NewRingBuffer(ringBufferCap),
		subs: make(map[chan SSEEvent]struct{}), done: make(chan struct{}),
		logFile: filepath.Join(svc.logDir, "x.log"), doneFile: filepath.Join(svc.logDir, "x.done"),
		maxSubs: 4,
	}
	running.setRunnerID(RunnerID("lab-updater-" + runningID))
	// 已完成的 session → 不保护
	doneID := "upd_done000000"
	doneSess := &UpdateSession{
		ID: doneID, Status: "done", DoneAt: time.Now(), LogBuffer: NewRingBuffer(ringBufferCap),
		subs: make(map[chan SSEEvent]struct{}), done: make(chan struct{}),
		logFile: filepath.Join(svc.logDir, "y.log"), doneFile: filepath.Join(svc.logDir, "y.done"),
		maxSubs: 4,
	}
	doneSess.setRunnerID(RunnerID("lab-updater-" + doneID))
	svc.mu.Lock()
	svc.sessions[runningID] = running
	svc.sessions[doneID] = doneSess
	svc.active = running
	svc.mu.Unlock()

	svc.sweepOnce()
	fake.mu.Lock()
	reaped := append([]RunnerID{}, fake.reaped...)
	fake.mu.Unlock()
	if !containsRunner(reaped, RunnerID("lab-updater-"+runningID)) {
		t.Errorf("运行中 session 的 runner 应传入保护名单: %v", reaped)
	}
	if containsRunner(reaped, RunnerID("lab-updater-"+doneID)) {
		t.Errorf("已 done session 的 runner 不应保护: %v", reaped)
	}
}

func containsRunner(ids []RunnerID, want RunnerID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
