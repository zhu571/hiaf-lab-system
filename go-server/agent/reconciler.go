package agent

import (
	"context"
	"log/slog"
	"time"
)

// StartCandidateReconciler 周期对账 approved 超时未执行的候选（AR-17）。
// 启动立即跑一轮，之后每 interval 一轮；单轮失败只记日志，不退出。
func StartCandidateReconciler(ctx context.Context, svc *Service, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	run := func() {
		retried, failed, err := svc.ReconcileStaleCandidates(ctx, 10*time.Minute, 3, 20)
		if err != nil {
			slog.Warn("agent candidate reconciler failed", "error", err)
			return
		}
		if retried > 0 || failed > 0 {
			slog.Info("agent candidate reconciled", "retried", retried, "failed", failed)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
