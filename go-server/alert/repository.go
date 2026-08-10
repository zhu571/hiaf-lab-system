package alert

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Repository 是 alerts 表的仓储：只访问本模块表（无跨模块访问）。
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// Report 是上报事务（单事务，防 race，方案 §5.1）：
//  1. SELECT ... FOR UPDATE 锁定同 source+title 的 active 行；
//  2. 命中且窗口内（last_seen 距今 ≤10min）→ 计数+1、刷新 last_seen/detail，不重发；
//  3. 命中但窗口外（复发）→ 计数重置 1、刷新 last_seen/detail、清 resolved_at，重发；
//  4. 未命中 → INSERT；并发撞部分唯一索引 → ON CONFLICT 窗口判定累加/重置。
//
// 窗口判定在 2/3 用注入时钟 now 求值（单测可控）；ON CONFLICT 分支用 SQL now()，
// 极小概率窗口判定偏差只影响「是否重发 ntfy」，唯一索引保证绝无双 active 行
// （硬约束：行唯一是防双发的最终防线）。
func (r *Repository) Report(ctx context.Context, level, source, title, detail string, now time.Time) (*ReportResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var (
		id              string
		occurrenceCount int
		firstSeen       time.Time
		lastSeen        time.Time
		deduplicated    bool
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, occurrence_count, first_seen, last_seen
		   FROM alerts WHERE source = $1 AND title = $2 AND status = 'active' FOR UPDATE`,
		source, title,
	).Scan(&id, &occurrenceCount, &firstSeen, &lastSeen)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		// 未命中 → INSERT；并发撞唯一索引 → ON CONFLICT 窗口判定。
		// RETURNING 中 xmax=0 判定「本次为插入」（并发冲突更新时 xmax 非 0），
		// out_of_window 判定 ON CONFLICT 分支是否窗口外复发（决定是否重发）。
		var inserted, outOfWindow bool
		err = tx.QueryRowContext(ctx,
			`INSERT INTO alerts (level, source, title, detail)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (source, title) WHERE status = 'active'
			 DO UPDATE SET
			   occurrence_count = CASE
			       WHEN alerts.last_seen < now() - interval '10 minutes' THEN 1
			       ELSE alerts.occurrence_count + 1
			   END,
			   last_seen = now(),
			   detail = EXCLUDED.detail,
			   resolved_at = NULL
			 RETURNING id, occurrence_count, first_seen,
			   (xmax = 0) AS inserted,
			   (alerts.last_seen < now() - interval '10 minutes') AS out_of_window`,
			level, source, title, detail,
		).Scan(&id, &occurrenceCount, &firstSeen, &inserted, &outOfWindow)
		if err != nil {
			return nil, err
		}
		// 全新 active 行 → 发 ntfy；并发窗口内合并 → 不发；并发窗口外复发 → 重发。
		deduplicated = !inserted && !outOfWindow
	case err != nil:
		return nil, err
	default:
		if now.Sub(lastSeen) <= dedupWindow {
			// 窗口内合并：计数+1、刷新 last_seen/detail（不重发）。
			err = tx.QueryRowContext(ctx,
				`UPDATE alerts
				    SET occurrence_count = occurrence_count + 1, last_seen = $2, detail = $3
				  WHERE id = $1 AND status = 'active'
				  RETURNING occurrence_count`,
				id, now, detail,
			).Scan(&occurrenceCount)
			if err != nil {
				return nil, err
			}
			deduplicated = true
		} else {
			// 窗口外复发：复用 active 行，计数重置 1、清 resolved_at（重发 ntfy）。
			err = tx.QueryRowContext(ctx,
				`UPDATE alerts
				    SET occurrence_count = 1, last_seen = $2, detail = $3, resolved_at = NULL
				  WHERE id = $1 AND status = 'active'
				  RETURNING occurrence_count`,
				id, now, detail,
			).Scan(&occurrenceCount)
			if err != nil {
				return nil, err
			}
			deduplicated = false
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ReportResult{AlertID: id, Deduplicated: deduplicated, OccurrenceCount: occurrenceCount}, nil
}

// Get 按 id 查询单条记录。
func (r *Repository) Get(ctx context.Context, id string) (*Alert, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, level, source, title, detail, status, occurrence_count,
		        first_seen, last_seen, resolved_at, resolved_by, created_at
		   FROM alerts WHERE id = $1`, id)
	a, err := scanAlert(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// List 按可选 status 过滤分页；active 按 last_seen DESC，resolved 按 resolved_at DESC。
func (r *Repository) List(ctx context.Context, status string, limit, offset int) ([]Alert, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM alerts WHERE ($1 = '' OR status = $1)`, status,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, level, source, title, detail, status, occurrence_count,
		        first_seen, last_seen, resolved_at, resolved_by, created_at
		   FROM alerts
		  WHERE ($1 = '' OR status = $1)
		  ORDER BY (CASE WHEN status = 'resolved' THEN resolved_at ELSE last_seen END) DESC, id DESC
		  LIMIT $2 OFFSET $3`,
		status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]Alert, 0, limit)
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *a)
	}
	return items, total, rows.Err()
}

// ResolveByID 按 id 解除 active 告警（前端手动 resolve）；resolved 后幂等成功。
// 返回是否实际变更（供审计 detail 记录）。
func (r *Repository) ResolveByID(ctx context.Context, id, resolvedBy string) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE alerts SET status = 'resolved', resolved_at = now(), resolved_by = $2
		  WHERE id = $1 AND status = 'active'`,
		id, resolvedBy)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ResolveBySource 按 source+title 精确匹配解除 active 告警（内部恢复上报）；
// 不匹配则幂等返回 success（不报错）。返回是否实际变更。
func (r *Repository) ResolveBySource(ctx context.Context, source, title, resolvedBy string) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE alerts SET status = 'resolved', resolved_at = now(), resolved_by = $3
		  WHERE source = $1 AND title = $2 AND status = 'active'`,
		source, title, resolvedBy)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ResolveByTTL 是 TTL 兜底扫描（单语句，天然幂等）：active 且 last_seen < cutoff
// → 置 resolved（resolved_by='ttl'）。返回影响行数。
func (r *Repository) ResolveByTTL(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE alerts SET status = 'resolved', resolved_at = now(), resolved_by = 'ttl'
		  WHERE status = 'active' AND last_seen < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CleanupResolved 是 90 天滚动清理（单语句，天然幂等）：resolved 且
// resolved_at < cutoff → DELETE（active 永不删，TTL 兜底先转 resolved 才可被清理）。
func (r *Repository) CleanupResolved(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM alerts WHERE status = 'resolved' AND resolved_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// rowScanner 兼容 *sql.Row 与 *sql.Rows。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanAlert(row rowScanner) (*Alert, error) {
	a := &Alert{}
	var resolvedAt sql.NullTime
	err := row.Scan(
		&a.ID, &a.Level, &a.Source, &a.Title, &a.Detail, &a.Status, &a.OccurrenceCount,
		&a.FirstSeen, &a.LastSeen, &resolvedAt, &a.ResolvedBy, &a.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if resolvedAt.Valid {
		t := resolvedAt.Time
		a.ResolvedAt = &t
	}
	return a, nil
}
