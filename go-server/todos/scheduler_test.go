package todos

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- FailureTracker ----------

func TestFailureTrackerAlertOnceAndReset(t *testing.T) {
	f := &FailureTracker{}
	alerts := 0
	if f.Note(false) {
		alerts++
	}
	if f.Note(false) {
		alerts++
	}
	if !f.Note(false) {
		t.Fatal("expected alert on 3rd consecutive failure")
	}
	alerts++
	if alerts != 1 {
		t.Fatalf("expected exactly 1 alert, got %d", alerts)
	}
	if f.consecutive != 0 {
		t.Fatalf("counter should reset after alert, got %d", f.consecutive)
	}
	// 成功后清零
	if f.Note(true) {
		t.Fatal("success must not alert")
	}
	if f.consecutive != 0 {
		t.Fatalf("success should reset counter, got %d", f.consecutive)
	}
}

// ---------- check 边界 ----------

func TestSchedulerCheckBoundaries(t *testing.T) {
	d := newTestDeps()
	d.snap.users = []UserSnapshot{{ID: "u1", Username: "alice"}}
	svc := d.service()
	s := NewScheduler(svc, testLoc, testNow)
	s.interval = time.Minute
	ctx := context.Background()
	at := func(day int, hour, minute int) time.Time {
		return time.Date(2026, 8, day, hour, minute, 0, 0, testLoc)
	}
	// 2026-08-07 是周五，08-08 周六，08-10 周一

	// 09:00 周五 → push
	s.check(ctx, at(7, 9, 0))
	if d.repo.openCalls == 0 {
		t.Fatal("expected push at 09:00 weekday")
	}
	// 09:00 周六 → 不 push
	before := d.repo.openCalls
	s.check(ctx, at(8, 9, 0))
	if d.repo.openCalls != before {
		t.Fatal("push must be skipped on weekend")
	}
	// 23:00 周五（明日周六）→ 不 generate
	before = d.repo.inflightCalls
	s.check(ctx, at(7, 23, 0))
	if d.repo.inflightCalls != before {
		t.Fatal("generate must be skipped when tomorrow is weekend")
	}
	// 23:00 周日（明日周一）→ generate
	before = d.repo.inflightCalls
	s.check(ctx, at(9, 23, 0))
	if d.repo.inflightCalls == before {
		t.Fatal("expected generate at 23:00 when tomorrow is weekday")
	}
	// 08:30 → rollover
	before = d.repo.rolloverCalls
	s.check(ctx, at(10, 8, 30))
	if d.repo.rolloverCalls == before {
		t.Fatal("expected rollover at 08:30")
	}
	// 23:30 → cleanup
	before = d.repo.cleanupCalls
	s.check(ctx, at(10, 23, 30))
	if d.repo.cleanupCalls == before {
		t.Fatal("expected cleanup at 23:30")
	}
	// 错过窗口（09:01 等非整点分钟）→ 不触发任何 job
	before = d.repo.openCalls + d.repo.rolloverCalls + d.repo.cleanupCalls + d.repo.inflightCalls
	s.check(ctx, at(10, 9, 1))
	if d.repo.openCalls+d.repo.rolloverCalls+d.repo.cleanupCalls+d.repo.inflightCalls != before {
		t.Fatal("missed window must not trigger any job")
	}
}

// ---------- Run：启动顺延补跑 + 优雅关闭 ----------

func TestSchedulerRunCatchupRollover(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	// 启动时间 09:00（已过 08:30）→ 补跑 rollover
	s := NewScheduler(svc, testLoc, func() time.Time {
		return time.Date(2026, 8, 7, 9, 0, 0, 0, testLoc)
	})
	s.interval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	deadline := time.After(2 * time.Second)
	for d.repo.countRollover() == 0 {
		select {
		case <-deadline:
			t.Fatal("expected rollover catchup at startup")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	s.wg.Wait()
}

func TestSchedulerRunNoCatchupBeforeWindow(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	s := NewScheduler(svc, testLoc, func() time.Time {
		return time.Date(2026, 8, 7, 7, 0, 0, 0, testLoc)
	})
	s.interval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	// 07:00 启动不补跑；ticker 20ms 也不会恰好命中 08:30 → 无 rollover
	time.Sleep(60 * time.Millisecond)
	cancel()
	s.wg.Wait()
	if d.repo.countRollover() != 0 {
		t.Fatalf("rollover must not run before 08:30, got %d calls", d.repo.countRollover())
	}
}

func TestSchedulerGracefulShutdown(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	// 让 PushDaily 在途时取消 ctx：注入一个慢 push（fakePub 阻塞无法注入 delay，
	// 改用 fakeReports? 不行。用 svc 包装：直接在 check 中模拟 job 在途 → wg.Wait 完成）
	s := NewScheduler(svc, testLoc, testNow)
	s.interval = time.Minute
	var started atomic.Bool
	done := make(chan struct{})
	slowJob := func(ctx context.Context) error {
		started.Store(true)
		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
		close(done)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	// 模拟 ctx 取消前已有一批在途
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		slowJob(ctx)
	}()
	<-func() <-chan struct{} {
		ch := make(chan struct{})
		go func() {
			for !started.Load() {
				time.Sleep(time.Millisecond)
			}
			close(ch)
		}()
		return ch
	}()
	cancel()
	s.drain()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight batch did not finish during drain")
	}
}

func TestSchedulerNoteFailureSendsAlert(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	s := NewScheduler(svc, testLoc, testNow)
	alerted := make(chan string, 1)
	s.sendAlert = func(title, msg string) error {
		alerted <- title
		return nil
	}
	// 3 次失败 → 告警一次
	s.noteFailure(false)
	s.noteFailure(false)
	s.noteFailure(false)
	select {
	case title := <-alerted:
		if title == "" {
			t.Fatal("empty alert title")
		}
	case <-time.After(time.Second):
		t.Fatal("expected alert after 3 failures")
	}
	// 清零后下一次失败重新计数（第 4 次失败不告警）
	select {
	case <-alerted:
		t.Fatal("unexpected alert on 4th failure")
	default:
	}
	// 连续再 3 次失败 → 再次告警
	s.noteFailure(false)
	s.noteFailure(false)
	s.noteFailure(false)
	select {
	case <-alerted:
	case <-time.After(time.Second):
		t.Fatal("expected second alert")
	}
}

func TestSchedulerGenerateRunsIssueSync(t *testing.T) {
	d := newTestDeps()
	d.snap.users = []UserSnapshot{{ID: "u1", Username: "alice"}}
	svc := d.service()
	s := NewScheduler(svc, testLoc, testNow)
	// 2026-08-09 周日 23:00（明日周一）→ issue_sync 前置 + 生成
	s.check(context.Background(), time.Date(2026, 8, 9, 23, 0, 0, 0, testLoc))
	if d.repo.issueSyncCalls == 0 {
		t.Fatal("23:00 生成必须先跑 issue_sync（方案 §4 前置联动）")
	}
	if d.repo.inflightCalls == 0 {
		t.Fatal("expected generate after issue_sync")
	}
	// 审计顺序：issue_sync 在前、generate 在后
	if len(d.audit.actions) < 2 || d.audit.actions[0] != ActionIssueSync || d.audit.actions[1] != ActionGenerate {
		t.Fatalf("unexpected audit actions: %v", d.audit.actions)
	}
	// 2026-08-07 周五 23:00（明日周六）→ 不生成也不同步
	d2 := newTestDeps()
	s2 := NewScheduler(d2.service(), testLoc, testNow)
	s2.check(context.Background(), time.Date(2026, 8, 7, 23, 0, 0, 0, testLoc))
	if d2.repo.issueSyncCalls != 0 || d2.repo.inflightCalls != 0 {
		t.Fatal("周末前夜必须跳过 issue_sync 与 generate")
	}
}

func TestSchedulerWeekendFailureNotTracked(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	s := NewScheduler(svc, testLoc, testNow)
	// 周六 09:00 不执行 push → noteFailure 不被调用（周末不计失败）
	before := d.repo.openCalls
	s.check(context.Background(), time.Date(2026, 8, 8, 9, 0, 0, 0, testLoc)) // 周六
	if d.repo.openCalls != before {
		t.Fatal("weekend push must not run")
	}
}
