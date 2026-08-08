package todos

import (
	"context"
	"sort"
	"time"
)

// RunCleanup 23:30 历史清理：done/cancelled 按 created_for（< 30 天前）、in-flight 按 created_at 兜底；
// 按批审计（含两类删除计数）。
func (s *Service) RunCleanup(ctx context.Context) error {
	loc := s.loc
	now := s.now().In(loc)
	createdForCutoff := now.AddDate(0, 0, -30).Format(time.DateOnly)
	createdAtCutoff := now.Add(-30 * 24 * time.Hour)
	doneCancelled, inflight, err := s.repo.Cleanup(createdForCutoff, createdAtCutoff)
	if err != nil {
		return err
	}
	return s.audit.WriteSystemAudit(ctx, ActionCleanup, map[string]any{
		"done_cancelled": doneCancelled, "inflight": inflight,
	})
}

// cleanupHintsFor 计算清理预警（推送模板用）：done/cancelled 按 created_for、in-flight 按 created_at
// 距今 ≥27 天 → 剩余清理天数 30-天数，按剩余天数分组。
// cutoff 取 today-30（含 daysLeft=0，即今晚 23:30 将被删除的项，方案 §4 "≥27 天"含第 30 天）。
func (s *Service) cleanupHintsFor(userID string) ([]CleanupHint, error) {
	today := s.todayStr()
	todayTime, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return nil, err
	}
	cutoffDate := todayTime.AddDate(0, 0, -30).Format(time.DateOnly)
	candidates, err := s.repo.CleanupHintCandidates(userID, cutoffDate)
	if err != nil {
		return nil, err
	}
	grouped := map[int]int{}
	for _, t := range candidates {
		var daysElapsed int
		if t.Status == StatusDone || t.Status == StatusCancelled {
			daysElapsed = daysBetween(todayTime, t.CreatedFor)
		} else {
			daysElapsed = daysBetween(todayTime, t.CreatedAt.In(s.loc).Format(time.DateOnly))
		}
		daysLeft := 30 - daysElapsed
		if daysLeft >= 0 && daysLeft <= 3 {
			grouped[daysLeft]++
		}
	}
	hints := make([]CleanupHint, 0, len(grouped))
	for daysLeft, count := range grouped {
		hints = append(hints, CleanupHint{DaysLeft: daysLeft, Count: count})
	}
	sort.Slice(hints, func(i, j int) bool { return hints[i].DaysLeft < hints[j].DaysLeft })
	return hints, nil
}

func daysBetween(today time.Time, date string) int {
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return 0
	}
	return int(today.Sub(d).Hours() / 24)
}
