package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/agent"
	"github.com/zhu571/hiaf-lab-system/go-server/alert"
	"github.com/zhu571/hiaf-lab-system/go-server/ask"
	"github.com/zhu571/hiaf-lab-system/go-server/audit"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/electrical"
	"github.com/zhu571/hiaf-lab-system/go-server/internal/config"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/todos"
	"github.com/zhu571/hiaf-lab-system/go-server/translations"
	"github.com/zhu571/hiaf-lab-system/go-server/weekly"
)

type backgroundDeps struct {
	ctx         context.Context
	cfg         *config.Config
	db          *sql.DB
	loc         *time.Location
	translation *translations.Service
	todos       *todos.Service
	weekly      *weekly.Service
	electrical  *electrical.Service
	ask         *ask.Service
	audit       *audit.Service
	alert       *alert.Service
	agent       *agent.Service
}

// startBackgroundTasks 启动后台调度/清理/对账任务；HTTP server 启动前调用。
func startBackgroundTasks(d backgroundDeps) {
	d.translation.Start(d.ctx)
	if d.cfg.Scheduler.TodosEnabled {
		sched := todos.NewScheduler(d.todos, d.loc, time.Now)
		sched.SetRunClaimer(func(ctx context.Context, job, runKey string) (bool, error) {
			return common.ClaimScheduledRun(ctx, d.db, job, runKey)
		})
		// 连续失败告警收敛到告警中心（warning/todos），由 alert 模块聚合去重后推送。
		sched.SetAlertReporter(func(title, msg string) error {
			_, err := d.alert.Report(context.Background(), "warning", "todos", title, msg)
			return err
		})
		go sched.Run(d.ctx)
		slog.Info("todos scheduler enabled")
	}
	// 周报定时调度（AI-1）：每周日 20:00；WEEKLY_SUMMARY_AUTHOR_ID 未配置时跳过。
	if d.cfg.Scheduler.WeeklyEnabled {
		weeklySched := weekly.NewScheduler(d.weekly, d.cfg.Scheduler.WeeklyAuthorID, d.loc, time.Now)
		weeklySched.SetRunClaimer(func(ctx context.Context, job, runKey string) (bool, error) {
			return common.ClaimScheduledRun(ctx, d.db, job, runKey)
		})
		weeklySched.SetAlertReporter(func(title, msg string) error {
			_, err := d.alert.Report(context.Background(), "warning", "weekly", title, msg)
			return err
		})
		go weeklySched.Run(d.ctx)
		slog.Info("weekly scheduler enabled")
	}
	// 电器监控：代码默认只提供单次采集；ELECTRICAL_MONITOR_ENABLED=true 时启动常驻 worker。
	go d.electrical.Run(d.ctx)
	// ask_history 快照保留任务（P2-3）：立即执行一次 + 每 24h，90 天前快照置 NULL。
	go d.ask.StartRetentionTask(d.ctx)
	// 审计月分区维护（AR-02）：启动补齐 + 每日检查，防止跨月后审计 INSERT 无分区。
	go d.audit.StartPartitionMaintenance(d.ctx)
	// 共享限流计数清理（AR-01）：启动立即 + 每日清过期窗口。
	go common.StartRateLimitCleanup(d.ctx, d.db)
	// 调度认领记录清理（AR-01）：保留 30 天。
	go common.StartSchedulerClaimCleanup(d.ctx, d.db)
	// 幂等 key 清理（AR-03）：启动立即 + 每小时清 expires_at 过期行。
	go middleware.StartIdempotencyCleanup(d.ctx, d.db)
	// 告警中心维护任务：TTL 24h 兜底（启动立即 + 每小时）+ 90 天滚动清理（每日 04:00）。
	go d.alert.StartMaintenance(d.ctx)
	// 候选执行对账（AR-17）：approved 超 10 分钟未执行 → 重新执行/告警。
	go agent.StartCandidateReconciler(d.ctx, d.agent, 5*time.Minute)
}
