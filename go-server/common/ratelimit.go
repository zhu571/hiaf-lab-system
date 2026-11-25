package common

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// RateLimitStore 是多副本共享的固定窗口限流器（AR-01）。
// 计数在 Postgres 里原子 UPSERT，进程重启/多副本都不会各自计数。
type RateLimitStore struct {
	DB     *sql.DB
	Scope  string
	Limit  int
	Window time.Duration
}

func NewRateLimitStore(db *sql.DB, scope string, limit int, window time.Duration) *RateLimitStore {
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimitStore{DB: db, Scope: scope, Limit: limit, Window: window}
}

// Allow 返回本窗口内是否未超限。DB 不可用时 fail-open（返回 true）并记日志：
// 限流是保护措施，不应因计数表故障把 AI 功能整体打挂。
func (s *RateLimitStore) Allow(ctx context.Context, subject string) bool {
	if s == nil || s.DB == nil || s.Limit <= 0 {
		return true
	}
	windowStart := time.Now().UTC().Truncate(s.Window)
	var count int
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO rate_limit_counters (scope, subject, window_start, count)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (scope, subject, window_start)
		DO UPDATE SET count = rate_limit_counters.count + 1
		RETURNING count`,
		s.Scope, subject, windowStart,
	).Scan(&count)
	if err != nil {
		slog.Warn("rate limit counter failed, fail-open", "scope", s.Scope, "error", err)
		return true
	}
	return count <= s.Limit
}

// StartRateLimitCleanup 每日清理过期窗口行（保留 1 小时足够排查）。
func StartRateLimitCleanup(ctx context.Context, db *sql.DB) {
	clean := func() {
		if _, err := db.ExecContext(ctx,
			`DELETE FROM rate_limit_counters WHERE window_start < now() - interval '1 hour'`); err != nil && ctx.Err() == nil {
			slog.Warn("rate limit cleanup failed", "error", err)
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
