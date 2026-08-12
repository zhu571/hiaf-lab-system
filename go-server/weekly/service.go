package weekly

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

var (
	ErrInvalidWeekStart = errors.New("week_start 必须是周一且格式为 YYYY-MM-DD")
	ErrNoReports        = errors.New("本周没有日报，无法生成周报")
	ErrNotConfigured    = errors.New("AI 周报服务未配置")
	ErrUpstream         = errors.New("py-agent 上游服务错误")
	ErrInvalidLLMOutput = errors.New("周报模型输出无效")
)

// 跨模块窄接口：全部由 main.go 构造期注入（AGENTS.md §5 注入化原则），
// weekly 不直读 daily_reports/issues/experiences 任何表。
type (
	reportReader interface {
		WeeklyReports(ctx context.Context, from, to string) ([]ReportEntry, error)
	}
	issueStatsReader interface {
		WeeklyIssueStats(ctx context.Context, from, to string) (IssueStats, error)
	}
	experienceStore interface {
		FindWeeklySummary(title string) (*SavedSummary, error)
		SaveWeeklySummary(authorID, title, content string) (*SavedSummary, error)
	}
	llmClient interface {
		Summarize(ctx context.Context, req LLMRequest) (*LLMResponse, error)
	}
	notifier interface {
		Send(topic, title, msg, clickURL, priority string, tags []string) error
	}
)

type Service struct {
	reports  reportReader
	issues   issueStatsReader
	store    experienceStore
	llm      llmClient
	notify   notifier
	loc      *time.Location
	now      func() time.Time
	notifyOn bool // 推送开关（scheduler 未注入 notifier 时关闭）
}

func NewService(reports reportReader, issues issueStatsReader, store experienceStore,
	llm llmClient, notifier notifier, loc *time.Location, now func() time.Time) *Service {
	return &Service{reports: reports, issues: issues, store: store, llm: llm,
		notify: notifier, loc: loc, now: now, notifyOn: notifier != nil}
}

// Generate 生成/复用本周周报：解析周范围 → 幂等查重 → 取数据 → LLM → 落库 → 推送。
// weekStart 空 = 本周一（loc 时区）；authorID 为落库作者（手动端点取当前用户，
// 定时调度取 WEEKLY_SUMMARY_AUTHOR_ID 配置用户）。
func (s *Service) Generate(ctx context.Context, authorID, weekStart string, notify bool) (*SummaryResult, error) {
	start, err := s.resolveWeekStart(weekStart)
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 0, 6)
	from, to := start.Format(time.DateOnly), end.Format(time.DateOnly)
	title := fmt.Sprintf("周报 %s ~ %s", from, to)

	// 每周幂等：同一周已生成过 → 直接复用（不重复调用 LLM、不重复落库）。
	if existing, err := s.store.FindWeeklySummary(title); err != nil {
		return nil, err
	} else if existing != nil {
		slog.Info("weekly summary reused", "title", title, "id", existing.ID)
		return &SummaryResult{
			ID: existing.ID, Title: existing.Title,
			Markdown: existing.Markdown, WeekStart: from, WeekEnd: to, Reused: true,
		}, nil
	}

	reports, err := s.reports.WeeklyReports(ctx, from, to)
	if err != nil {
		return nil, err
	}
	if len(reports) == 0 {
		return nil, ErrNoReports
	}
	reports = limitReports(reports)
	stats, err := s.issues.WeeklyIssueStats(ctx, from, to)
	if err != nil {
		return nil, err
	}
	resp, err := s.llm.Summarize(ctx, LLMRequest{WeekStart: from, WeekEnd: to, Reports: reports, IssueStats: stats})
	if err != nil {
		return nil, err
	}
	if err := validateLLMResponse(resp); err != nil {
		return nil, err
	}

	saved, err := s.store.SaveWeeklySummary(authorID, resp.Title, resp.Markdown)
	if err != nil {
		return nil, err
	}

	if notify && s.notifyOn && s.notify != nil {
		// 推送失败只告警不阻断：周报已落库，ntfy 抖动可事后补看。
		if err := s.notify.Send(DefaultTopic, saved.Title, resp.Summary, "", "default", []string{"chart_with_upwards_trend"}); err != nil {
			slog.Warn("weekly summary ntfy push failed", "error", err)
		}
	}
	return &SummaryResult{
		ID: saved.ID, Title: saved.Title, Summary: resp.Summary, Markdown: saved.Markdown,
		Highlights: resp.Highlights, Problems: resp.Problems, DataPoints: resp.DataPoints,
		WeekStart: from, WeekEnd: to,
	}, nil
}

// resolveWeekStart 解析周起始：空 → 本周一；显式 → 必须是周一（YYYY-MM-DD）。
func (s *Service) resolveWeekStart(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return weekStartOf(s.now().In(s.loc)), nil
	}
	t, err := time.ParseInLocation(time.DateOnly, raw, s.loc)
	if err != nil || t.Weekday() != time.Monday {
		return time.Time{}, ErrInvalidWeekStart
	}
	return t, nil
}

// weekStartOf 返回 t 所在周的周一 00:00（周一为一周起点）。
func weekStartOf(t time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return day.AddDate(0, 0, -(int(t.Weekday())+6)%7)
}

// LLM 载荷预算（对齐 py-agent /v1/weekly-summary 校验契约，防 400 静默失败）：
// 单字段上限 8000/3000/128 字符、单批 ≤100 条、整批 ≤480KB（py 请求预算 512KB，含转义余量）。
// daily_reports.raw_text/summary 为无界 TEXT，必须在此收紧；超限截断/丢最旧并日志告警。
const (
	maxWeeklyReports    = 100
	maxWeeklyRawText    = 8000
	maxWeeklySummary    = 3000
	maxWeeklyAuthorName = 128
	maxWeeklyPayload    = 480_000
)

// limitReports 收紧周报 LLM 载荷：字段截断 → 条数上限 → 总量预算。
// WeeklyReports 按日期升序返回，超限时保留末尾连续段（丢最旧保最新——周报优先收尾事实），
// 顺序仍为升序且至少保留 1 条。
func limitReports(entries []ReportEntry) []ReportEntry {
	for i := range entries {
		entries[i].AuthorName = truncateRunes(entries[i].AuthorName, maxWeeklyAuthorName)
		entries[i].RawText = truncateRunes(entries[i].RawText, maxWeeklyRawText)
		entries[i].Summary = truncateRunes(entries[i].Summary, maxWeeklySummary)
	}
	if len(entries) > maxWeeklyReports {
		slog.Warn("weekly reports beyond count cap, oldest dropped", "count", len(entries), "max", maxWeeklyReports)
		entries = entries[len(entries)-maxWeeklyReports:]
	}
	budget, kept := 0, 0
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		// +64 每条约占 JSON 结构/转义开销。
		cost := len([]rune(e.AuthorName)) + len([]rune(e.RawText)) + len([]rune(e.Summary)) + 64
		if kept > 0 && budget+cost > maxWeeklyPayload {
			break
		}
		budget += cost
		kept++
	}
	if kept < len(entries) {
		slog.Warn("weekly reports beyond payload budget, oldest dropped", "kept", kept, "max_chars", maxWeeklyPayload)
		entries = entries[len(entries)-kept:]
	}
	return entries
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// validateLLMResponse 纵深校验（与 py-agent validate_weekly_summary 对齐，防越界内容落库）。
func validateLLMResponse(resp *LLMResponse) error {
	if resp.Status != "ok" {
		return fmt.Errorf("%w: status=%q", ErrInvalidLLMOutput, resp.Status)
	}
	title := strings.TrimSpace(resp.Title)
	summary := strings.TrimSpace(resp.Summary)
	markdown := strings.TrimSpace(resp.Markdown)
	if title == "" || len([]rune(title)) > 256 || summary == "" || len([]rune(summary)) > 1000 ||
		markdown == "" || len([]rune(markdown)) > 30000 {
		return ErrInvalidLLMOutput
	}
	resp.Title, resp.Summary, resp.Markdown = title, summary, markdown
	for _, bullets := range [][]string{resp.Highlights, resp.Problems, resp.DataPoints} {
		if len(bullets) > 25 {
			return ErrInvalidLLMOutput
		}
		for _, b := range bullets {
			if strings.TrimSpace(b) == "" || len([]rune(b)) > 1000 {
				return ErrInvalidLLMOutput
			}
		}
	}
	return nil
}
