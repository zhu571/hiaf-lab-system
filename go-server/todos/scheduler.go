package todos

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Scheduler 编排四个定时 job（单实例假设：docker compose 单 server 容器天然满足，
// 多副本部署需外部锁）。时钟与间隔可注入便于测试。
type Scheduler struct {
	svc       *Service
	loc       *time.Location
	now       func() time.Time
	interval  time.Duration
	tracker   *FailureTracker
	sendAlert func(title, msg string) error
	wg        sync.WaitGroup
}

func NewScheduler(svc *Service, loc *time.Location, now func() time.Time) *Scheduler {
	return &Scheduler{
		svc: svc, loc: loc, now: now, interval: time.Minute,
		tracker: &FailureTracker{},
		// sendAlert 由 main.go 注入（alertSvc.Report，level=warning/source=todos，
		// 统一聚合去重后推送）；未注入时连续失败告警静默，保持 scheduler 可测。
	}
}

// SetAlertReporter 注入连续失败告警上报器（main.go 接 alertSvc.Report）。
func (s *Scheduler) SetAlertReporter(fn func(title, msg string) error) {
	s.sendAlert = fn
}

// Run 启动调度循环：启动时顺延补跑（错过 08:30 则补跑，幂等）；ctx 取消后
// 不再起新批、在途批完成退出（上限 30s）。
func (s *Scheduler) Run(ctx context.Context) {
	start := s.now().In(s.loc)
	rolloverDue := start.Hour() > 8 || (start.Hour() == 8 && start.Minute() >= 30)
	if rolloverDue && ctx.Err() == nil {
		s.runBatch(ctx, "rollover", s.svc.RunRollover)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.drain()
			return
		case t := <-ticker.C:
			s.check(ctx, t.In(s.loc))
		}
	}
}

// check 按当前时间触发对应 job（错过窗口策略：生成不补跑、推送不补推、顺延补跑已在 Run 处理）。
func (s *Scheduler) check(ctx context.Context, t time.Time) {
	switch {
	case t.Hour() == 8 && t.Minute() == 30:
		s.runBatch(ctx, "rollover", s.svc.RunRollover)
	case t.Hour() == 9 && t.Minute() == 0 && isWeekday(t):
		ok := s.runBatch(ctx, "push", s.svc.PushDaily)
		s.noteFailure(ok)
	case t.Hour() == 23 && t.Minute() == 0:
		tomorrow := t.AddDate(0, 0, 1)
		if isWeekday(tomorrow) {
			// issue 联动同步是生成的前置（方案 §4）；周末不跑生成则不同步。
			ok := s.runBatch(ctx, "generate", func(ctx context.Context) error {
				if err := s.svc.RunIssueSync(ctx); err != nil {
					return err
				}
				return s.svc.GenerateForDate(ctx, tomorrow.Format(time.DateOnly))
			})
			s.noteFailure(ok)
		}
	case t.Hour() == 23 && t.Minute() == 30:
		s.runBatch(ctx, "cleanup", s.svc.RunCleanup)
	}
}

func (s *Scheduler) runBatch(ctx context.Context, name string, fn func(context.Context) error) bool {
	s.wg.Add(1)
	defer s.wg.Done()
	if err := fn(ctx); err != nil {
		slog.Error("todos scheduler job failed", "job", name, "error", err)
		return false
	}
	return true
}

// noteFailure 失败计数：连续 3 天（job 运行日）失败 → lab-alerts 一次并清零。
func (s *Scheduler) noteFailure(ok bool) {
	if !s.tracker.Note(ok) {
		return
	}
	if s.sendAlert != nil {
		if err := s.sendAlert("Todolist 连续失败告警", "生成/推送连续 3 天失败，请检查 py-agent、ntfy 与 server 日志"); err != nil {
			slog.Error("todos failure alert send failed", "error", err)
		}
	}
}

// drain 等待在途批完成，上限 30s（ctx 超时强制退出）。
func (s *Scheduler) drain() {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		slog.Warn("todos scheduler drain timed out")
	}
}

// FailureTracker 连续失败计数（内存态，重启清零——已接受）。
type FailureTracker struct {
	consecutive int
}

// Note 记录一次尝试结果；连续 3 次失败返回 true（告警触发）并清零。
func (f *FailureTracker) Note(ok bool) bool {
	if ok {
		f.consecutive = 0
		return false
	}
	f.consecutive++
	if f.consecutive >= 3 {
		f.consecutive = 0
		return true
	}
	return false
}

func isWeekday(t time.Time) bool {
	return t.Weekday() != time.Saturday && t.Weekday() != time.Sunday
}
