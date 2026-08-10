package alert

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// Service 编排告警中心：字段校验 → 聚合去重（Repository.Report）→ 按 level
// 策略经注入的 Sender 发送 → TTL 兜底 + 90 天滚动清理维护任务。
type Service struct {
	repo     *Repository
	sender   Sender
	db       *sql.DB
	now      func() time.Time
	clickURL string
	// maintenanceMu 防维护任务重入（与 ask retention 互斥模式一致）。
	maintenanceMu chan struct{}
}

// NewService 构造告警服务。now 注入时钟（默认 time.Now），单测可覆盖。
func NewService(repo *Repository, sender Sender, db *sql.DB, now ...func() time.Time) *Service {
	s := &Service{
		repo:          repo,
		sender:        sender,
		db:            db,
		now:           time.Now,
		maintenanceMu: make(chan struct{}, 1),
	}
	if len(now) > 0 && now[0] != nil {
		s.now = now[0]
	}
	return s
}

// SetClickURL 注入告警通知点击落地地址（main.go 接 notify.WebURL+"/alerts"，
// 避免 alert 模块硬编码部署地址）。
func (s *Service) SetClickURL(url string) { s.clickURL = url }

// sendPolicy 是 level → (priority, tags, 是否双通道) 的发送策略映射（方案 §3，
// 对齐现状通道，不降低送达率：critical/error ntfy+MeoW，warning/info 仅 ntfy）。
func sendPolicy(level string) (priority string, tags []string, both bool) {
	switch level {
	case LevelCritical:
		return "urgent", []string{"rotating_light"}, true
	case LevelError:
		return "high", []string{"warning"}, true
	case LevelWarning:
		return "high", []string{"warning"}, false
	default: // info
		return "default", nil, false
	}
}

// Report 上报一条告警：字段级校验（title ≤256、detail ≤2000、枚举校验，
// 防洪，方案 §4）→ 聚合去重事务 → 窗口外/新行按 level 策略发送。
func (s *Service) Report(ctx context.Context, level, source, title, detail string) (*ReportResult, error) {
	level = strings.TrimSpace(level)
	source = strings.TrimSpace(source)
	title = strings.TrimSpace(title)
	if !validLevels[level] {
		return nil, errors.Join(ErrInvalidInput, errors.New("level 非法"))
	}
	if !validSources[source] {
		return nil, errors.Join(ErrInvalidInput, errors.New("source 非法"))
	}
	if title == "" {
		return nil, errors.Join(ErrInvalidInput, errors.New("title 不能为空"))
	}
	if utf8.RuneCountInString(title) > maxTitleLen {
		return nil, errors.Join(ErrInvalidInput, errors.New("title 超过 256 字符"))
	}
	if utf8.RuneCountInString(detail) > maxDetailLen {
		return nil, errors.Join(ErrInvalidInput, errors.New("detail 超过 2000 字符"))
	}

	res, err := s.repo.Report(ctx, level, source, title, detail, s.now())
	if err != nil {
		return nil, err
	}
	if !res.Deduplicated {
		priority, tags, both := sendPolicy(level)
		var sendErr error
		if both {
			sendErr = s.sender.SendBoth(Topic, title, detail, s.clickURL, priority, tags)
		} else {
			sendErr = s.sender.Send(Topic, title, detail, s.clickURL, priority, tags)
		}
		if sendErr != nil {
			// 发送失败不影响落库/去重（聚合窗口与唯一索引仍生效）。
			slog.Error("alert send failed", "error", sendErr, "source", source, "title", title)
		}
	}
	return res, nil
}

// ResolveByID 按 id 解除 active 告警（前端 admin/maintainer 手动 resolve；
// 由 handler 传入操作者 username）。不匹配 active 行 → 幂等成功，不报错。
func (s *Service) ResolveByID(ctx context.Context, id, resolvedBy string) (bool, error) {
	return s.repo.ResolveByID(ctx, id, resolvedBy)
}

// ResolveBySource 按 source+title 解除 active 告警（内部恢复上报，resolvedBy 传
// ResolvedBySystem；不匹配幂等 success）。
func (s *Service) ResolveBySource(ctx context.Context, source, title, resolvedBy string) error {
	_, err := s.ResolveBySourceMatched(ctx, source, title, resolvedBy)
	return err
}

// ResolveBySourceMatched 同 ResolveBySource，额外返回是否实际命中并解除
// active 行（handler 写入审计 detail 用）。
func (s *Service) ResolveBySourceMatched(ctx context.Context, source, title, resolvedBy string) (bool, error) {
	source = strings.TrimSpace(source)
	title = strings.TrimSpace(title)
	if source == "" || title == "" {
		return false, errors.Join(ErrInvalidInput, errors.New("source 与 title 不能为空"))
	}
	if !validSources[source] {
		return false, errors.Join(ErrInvalidInput, errors.New("source 非法"))
	}
	if utf8.RuneCountInString(title) > maxTitleLen {
		return false, errors.Join(ErrInvalidInput, errors.New("title 超过 256 字符"))
	}
	return s.repo.ResolveBySource(ctx, source, title, resolvedBy)
}

// List 按可选 status 过滤分页（limit 默认 50，上限 200）。
func (s *Service) List(ctx context.Context, status string, limit, offset int) ([]Alert, int, error) {
	if status != "" && !validStatuses[status] {
		return nil, 0, errors.Join(ErrInvalidInput, errors.New("status 非法"))
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.List(ctx, status, limit, offset)
}

// Get 按 id 查询单条记录（不存在 → ErrNotFound）。
func (s *Service) Get(ctx context.Context, id string) (*Alert, error) {
	return s.repo.Get(ctx, id)
}

// RunTTLOnce 执行一轮 TTL 兜底扫描（启动立即 + 每小时）：
// active 且 last_seen < now-24h → 置 resolved（resolved_by='ttl'）。
// 单语句天然幂等；仅影响行数 >0 时写审计（避免每小时刷审计日志，先例：
// todos scheduler 的 WriteSystemAudit，detail 带 count）。返回影响行数。
func (s *Service) RunTTLOnce(ctx context.Context, now time.Time) (int64, error) {
	n, err := s.repo.ResolveByTTL(ctx, now.Add(-ttlAge))
	if err != nil {
		return 0, err
	}
	if n > 0 {
		if auditErr := middleware.WriteSystemAudit(ctx, s.db, "alerts.ttl",
			map[string]any{"count": n, "cutoff": now.Add(-ttlAge).Format(time.RFC3339)}); auditErr != nil {
			slog.Error("alerts.ttl audit write failed", "error", auditErr)
		}
		slog.Info("alerts ttl resolved", "count", n)
	}
	return n, nil
}

// RunCleanupOnce 执行一轮 90 天滚动清理（每日 04:00）：
// resolved 且 resolved_at < now-90d → DELETE（active 永不删）。
// 幂等；仅影响行数 >0 时写审计。返回影响行数。
func (s *Service) RunCleanupOnce(ctx context.Context, now time.Time) (int64, error) {
	n, err := s.repo.CleanupResolved(ctx, now.Add(-cleanupAge))
	if err != nil {
		return 0, err
	}
	if n > 0 {
		if auditErr := middleware.WriteSystemAudit(ctx, s.db, "alerts.cleanup",
			map[string]any{"count": n, "cutoff": now.Add(-cleanupAge).Format(time.RFC3339)}); auditErr != nil {
			slog.Error("alerts.cleanup audit write failed", "error", auditErr)
		}
		slog.Info("alerts cleanup deleted", "count", n)
	}
	return n, nil
}

// StartMaintenance 启动维护任务（main.go 启动 goroutine，随进程退出）：
//   - TTL 扫描：启动时立即执行一次（恢复服务重启期间过期的现场）+ 每小时；
//   - 滚动清理：每日 04:00（Asia/Shanghai，独立 ticker 对齐整点，与 TTL 分时）。
func (s *Service) StartMaintenance(ctx context.Context) {
	// 先执行启动即 TTL（失败仅日志，不阻塞后续任务）。
	if _, err := s.RunTTLOnce(ctx, s.now()); err != nil && ctx.Err() == nil {
		slog.Warn("alerts ttl startup run failed", "error", err)
	}

	ttlTicker := time.NewTicker(time.Hour)
	defer ttlTicker.Stop()
	cleanupTicker := time.NewTicker(s.nextCleanupDelay())
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ttlTicker.C:
			s.runMaintenance(s.RunTTLOnce, ctx, s.now())
		case <-cleanupTicker.C:
			s.runMaintenance(s.RunCleanupOnce, ctx, s.now())
			cleanupTicker.Reset(s.nextCleanupDelay())
		}
	}
}

// runMaintenance 互斥执行一轮维护任务（防重入）。
func (s *Service) runMaintenance(run func(context.Context, time.Time) (int64, error), ctx context.Context, now time.Time) {
	select {
	case s.maintenanceMu <- struct{}{}:
		defer func() { <-s.maintenanceMu }()
	default:
		return
	}
	if _, err := run(ctx, now); err != nil && ctx.Err() == nil {
		slog.Warn("alerts maintenance run failed", "error", err)
	}
}

// nextCleanupDelay 计算距下一个 Asia/Shanghai 04:00 的等待时长（对齐整点，
// 避免每小时 ticker 起点漂移导致 04:00 判定失效）。
func (s *Service) nextCleanupDelay() time.Duration {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		slog.Warn("alerts maintenance: Asia/Shanghai unavailable, fallback to UTC", "error", err)
		loc = time.UTC
	}
	n := s.now().In(loc)
	next := time.Date(n.Year(), n.Month(), n.Day(), 4, 0, 0, 0, loc)
	if !next.After(n) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Sub(n)
}
