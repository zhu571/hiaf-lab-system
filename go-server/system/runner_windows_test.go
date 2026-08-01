//go:build windows

package system

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLocalRunnerLifecycle Windows 本地 runner：docker 不可用时流水线快速失败，
// goroutine 退出后 Alive 转 false，日志文件句柄释放（defer log.Close）。
func TestLocalRunnerLifecycle(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(dir)
	svc.engine = "go"
	svc.logDir = dir

	id := "upd_local00000"
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

	runner := newRunner(svc, "go")
	rid, err := runner.Spawn(context.Background(), sess)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if !strings.HasPrefix(string(rid), "local:") {
		t.Errorf("runner id = %q, want local: 前缀", rid)
	}

	deadline := time.Now().Add(10 * time.Second)
	for runner.Alive(rid) {
		if time.Now().After(deadline) {
			t.Fatal("local runner 未在超时前退出")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := runner.Kill(rid); err != nil {
		t.Errorf("Kill: %v", err)
	}
}

// TestLocalRunnerKillActive Kill 对运行中的 runner 生效：取消其上下文。
func TestLocalRunnerKillActive(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(dir)
	svc.engine = "go"
	svc.logDir = dir

	id := "upd_kill000000"
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

	runner := newRunner(svc, "go")
	rid, err := runner.Spawn(context.Background(), sess)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := runner.Kill(rid); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for runner.Alive(rid) {
		if time.Now().After(deadline) {
			t.Fatal("Kill 后 runner 未退出")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
