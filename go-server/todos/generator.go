package todos

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// 生成上限：issue ≤ 8 + LLM ≤ 4；LLM 并发上限 4。
const (
	maxIssueItems    = 8
	maxLLMItems      = 4
	generateParallel = 4
)

// RunIssueSync issue 联动：resolved/closed → 在途待办 cancelled（23:00 生成前置，0 条也写审计）。
func (s *Service) RunIssueSync(ctx context.Context) error {
	count, err := s.repo.IssueSync()
	if err != nil {
		return err
	}
	return s.audit.WriteSystemAudit(ctx, ActionIssueSync, map[string]any{"count": count})
}

// RunRollover 顺延归一：过期 pending/deferred → 今天 pending（幂等可补跑），按批审计。
func (s *Service) RunRollover(ctx context.Context) error {
	today := s.todayStr()
	count, err := s.repo.Rollover(today, s.now())
	if err != nil {
		return err
	}
	return s.audit.WriteSystemAudit(ctx, ActionRollover, map[string]any{"count": count, "date": today})
}

// GenerateForDate 生成 targetDate（明日）的待办：逐用户聚合 issue + LLM 日报延续。
// 并发上限 4；单用户失败独立跳过（不阻塞整批、不计入失败告警）；按批审计一行。
func (s *Service) GenerateForDate(ctx context.Context, targetDate string) error {
	users, err := s.snap.ActiveUsers()
	if err != nil {
		return err
	}
	sem := make(chan struct{}, generateParallel)
	var wg sync.WaitGroup
	var created, succeeded int64
	for _, u := range users {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(u UserSnapshot) {
			defer wg.Done()
			defer func() { <-sem }()
			n, err := s.generateForUser(ctx, u, targetDate)
			if err != nil {
				// 单用户 LLM 超时/上游失败：只跳过该用户，静默处理。
				logError("generate user "+u.Username, err)
				return
			}
			atomic.AddInt64(&created, int64(n))
			atomic.AddInt64(&succeeded, 1)
		}(u)
	}
	wg.Wait()
	return s.audit.WriteSystemAudit(ctx, ActionGenerate, map[string]any{
		"date": targetDate, "users": len(users), "succeeded": succeeded, "created": created,
	})
}

func (s *Service) generateForUser(ctx context.Context, u UserSnapshot, targetDate string) (int, error) {
	issues, err := s.snap.OpenIssuesForUser(u.ID)
	if err != nil {
		return 0, err
	}
	inflight, err := s.repo.InflightIssueIDs(u.ID)
	if err != nil {
		return 0, err
	}
	selected := selectIssues(issues, inflight)

	// existing_titles = issue 标题 + 本批已生成项标题（归一化去重）。
	existing := map[string]bool{}
	for _, iss := range selected {
		existing[normalizeTitle(iss.Title)] = true
	}

	report, err := s.reports.FetchLatestReport(ctx, u.ID)
	if err != nil {
		// 日报端点异常不阻塞 issue 聚合；视为无日报。
		logError("fetch report user "+u.Username, err)
		report = ""
	}

	var llmItems []LLMItem
	if len(selected) == 0 && strings.TrimSpace(report) == "" {
		// 两源皆空跳过 LLM（省成本与超时面）。
		return 0, nil
	}
	llmItems, err = s.llm.GenerateDaily(ctx, u.ID, report, selected, sortedTitles(existing))
	if err != nil {
		// LLM 失败：静默跳过该用户延续部分（issue 规则项不受影响）。
		logError("llm generate user "+u.Username, err)
		llmItems = nil
	}

	created := 0
	for _, iss := range selected {
		id := iss.ID
		ok, err := s.repo.InsertGenerated(&Todo{
			Title: cleanTitle(iss.Title), Priority: severityToPriority(iss.Severity),
			Status: StatusPending, Source: SourceIssue, CreatedBy: u.ID,
			CreatedFor: targetDate, IssueID: &id,
		})
		if err != nil {
			logError("insert issue todo user "+u.Username, err)
			continue
		}
		if ok {
			created++
		}
	}
	llmCreated := 0
	for _, item := range llmItems {
		if llmCreated >= maxLLMItems {
			break // LLM 补充项独立上限 4（方案 §2：issue ≤ 8 + LLM ≤ 4）
		}
		title := cleanTitle(item.Title)
		key := normalizeTitle(title)
		if title == "" || existing[key] {
			continue // 与 issue/本批已生成项重复 → 跳过
		}
		existing[key] = true
		ok, err := s.repo.InsertGenerated(&Todo{
			Title: title, Priority: clampPriority(item.Priority), Status: StatusPending,
			Source: SourceDailyLLM, CreatedBy: u.ID, CreatedFor: targetDate,
		})
		if err != nil {
			logError("insert llm todo user "+u.Username, err)
			continue
		}
		if ok {
			created++
			llmCreated++
		}
	}
	return created, nil
}

// selectIssues 取舍：过滤已在途 issue；按 severity 降序（high>medium>low）、同 severity 按 occurred_at 新→旧；取前 8。
func selectIssues(issues []IssueSnapshot, inflight map[string]bool) []IssueSnapshot {
	out := make([]IssueSnapshot, 0, len(issues))
	for _, iss := range issues {
		if inflight[iss.ID] {
			continue
		}
		out = append(out, iss)
	}
	sort.SliceStable(out, func(i, j int) bool {
		wi, wj := severityWeight(out[i].Severity), severityWeight(out[j].Severity)
		if wi != wj {
			return wi > wj
		}
		return out[i].OccurredAt.After(out[j].OccurredAt)
	})
	if len(out) > maxIssueItems {
		out = out[:maxIssueItems]
	}
	return out
}

func severityWeight(severity string) int {
	switch severity {
	case "critical", "high":
		return 3
	case "low":
		return 1
	default:
		return 2
	}
}

func sortedTitles(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
