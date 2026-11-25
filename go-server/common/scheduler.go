package common

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// ClaimScheduledRun 尝试认领一次定时任务（AR-01）。
// 返回 true 表示本副本获得执行权；false 表示其他副本已认领或本次已执行过。
// job 为任务名，runKey 为窗口标识（如 2026-11-25、2026-W47）。
func ClaimScheduledRun(ctx context.Context, db *sql.DB, job, runKey string) (bool, error) {
	if db == nil {
		return true, nil
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO scheduler_claims (job, run_key) VALUES ($1, $2)
		 ON CONFLICT (job, run_key) DO NOTHING`,
		job, runKey,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// StartSchedulerClaimCleanup 每日清理 30 天前的认领记录。
func StartSchedulerClaimCleanup(ctx context.Context, db *sql.DB) {
	clean := func() {
		if _, err := db.ExecContext(ctx,
			`DELETE FROM scheduler_claims WHERE claimed_at < now() - interval '30 days'`); err != nil && ctx.Err() == nil {
			slog.Warn("scheduler claim cleanup failed", "error", err)
		}
	}
	clean()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			clean()
		}
	}
}
