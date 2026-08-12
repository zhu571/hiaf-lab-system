package weekly

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Scheduler 每周日 20:00（loc 时区）触发周报生成（单实例假设：docker compose
// 单 server 容器天然满足）。错过窗口不补跑（周日 20:00 未命中即跳过，手动端点可兜底）。
// 时钟/间隔可注入便于测试；authorID 为空（未配置）时整体不启动。
type Scheduler struct {
	svc       *Service
	authorID  string
	loc       *time.Location
	now       func() time.Time
	interval  time.Duration
	tracker   *FailureTracker
	sendAlert func(title, msg string) error
	wg        sync.WaitGroup
}

func NewScheduler(svc *Service, authorID string, loc *time.Location, now func() time.Time) *Scheduler {
	return &Scheduler{
		svc: svc, authorID: authorID, loc: loc, now: now, interval: time.Minute,
		tracker: &FailureTracker{},
	}
}

// SetAlertReporter 注入连续失败告警上报器（main.go 接 alertSvc.Report）。
func (s *Scheduler) SetAlertReporter(fn func(title, msg string) error) {
	s.sendAlert = fn
}

// Run 启动调度循环：ctx 取消后在途批完成（上限 30s）。
func (s *Scheduler) Run(ctx context.Context) {
	if s.authorID == "" {
		slog.Warn("weekly scheduler skipped: WEEKLY_SUMMARY_AUTHOR_ID 未配置")
		return
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

// check 命中每周日 20:00 窗口触发生成（错过窗口不补跑，手动端点兜底）。
// authorID 未配置时保持静默（Run 在启动时已拦截一次，此处纵深防手工调用）。
func (s *Scheduler) check(ctx context.Context, t time.Time) {
	if s.authorID == "" {
		return
	}
	if t.Weekday() != time.Sunday || t.Hour() != 20 || t.Minute() != 0 {
		return
	}
	s.runGenerate(ctx)
}

func (s *Scheduler) runGenerate(ctx context.Context) {
	s.wg.Add(1)
	defer s.wg.Done()
	_, err := s.svc.Generate(ctx, s.authorID, "", true)
	if err != nil {
		if errors.Is(err, ErrNoReports) {
			// 本周无日报是合法状态（休假/停机），不计入失败链，避免误导性告警。
			slog.Info("weekly scheduler skipped: no reports this week")
		} else {
			slog.Error("weekly scheduler job failed", "error", err)
		}
	}
	s.noteFailure(err == nil || errors.Is(err, ErrNoReports))
}

// noteFailure 失败计数：连续 3 周失败 → lab-alerts 一次并清零。
func (s *Scheduler) noteFailure(ok bool) {
	if !s.tracker.Note(ok) {
		return
	}
	if s.sendAlert != nil {
		if err := s.sendAlert("周报连续失败告警", "周报生成连续 3 周失败，请检查 py-agent、WEEKLY_SUMMARY_AUTHOR_ID 与 server 日志"); err != nil {
			slog.Error("weekly failure alert send failed", "error", err)
		}
	}
}

func (s *Scheduler) drain() {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		slog.Warn("weekly scheduler drain timed out")
	}
}

// FailureTracker 连续失败计数（内存态，重启清零——已接受，对齐 todos 先例）。
type FailureTracker struct {
	consecutive int
}

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
