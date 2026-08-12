package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/agent"
	"github.com/zhu571/hiaf-lab-system/go-server/alert"
	"github.com/zhu571/hiaf-lab-system/go-server/assembly"
	"github.com/zhu571/hiaf-lab-system/go-server/audit"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/experiences"
	"github.com/zhu571/hiaf-lab-system/go-server/issues"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
	"github.com/zhu571/hiaf-lab-system/go-server/notify"
	"github.com/zhu571/hiaf-lab-system/go-server/runs"
	"github.com/zhu571/hiaf-lab-system/go-server/steptemplates"
	"github.com/zhu571/hiaf-lab-system/go-server/weekly"
)

// main_bridges.go：main.go 装配用的桥接适配器（纯组织性拆分，P2-5）。
// agent 模块的候选执行与 trace 读取、assembly/runs 的模板读取都经这些适配器
// 在 main.go 构造期注入，业务模块间不跨模块读表。本文件不含装配逻辑。

// alertNotifySender 是 alert 模块 Sender 窄接口的 notify 适配器：
// 主题固定 lab-alerts（告警中心统一主题），点击落地告警中心页 /alerts；
// SendBoth 走 ntfy + MeoW 双通道（critical/error，等价现状 SecurityAlert/
// InstrumentEmergency 的 sendBoth，不降低送达率）。alert 模块自身不 import notify。
type alertNotifySender struct{}

func (alertNotifySender) Send(topic, title, msg, clickURL, priority string, tags []string) error {
	// 双通道：ntfy + MeoW（用户要求告警也走 MeoW 并写明具体错误；msg=detail 即错误详情）
	return notify.SendBoth(alert.Topic, title, msg, notify.WebURL+"/alerts", priority, tags)
}

func (alertNotifySender) SendBoth(topic, title, msg, clickURL, priority string, tags []string) error {
	return notify.SendBoth(alert.Topic, title, msg, notify.WebURL+"/alerts", priority, tags)
}

type candidateExecutor struct {
	issues      *issues.Service
	experiences *experiences.Service
}

// reportReaderBridge 经 logs 模块 service 读日报当前值（trace 用）。
// 无权限/不存在时降级为 nil（trace 容忍 report 段为空），其他错误上抛。
type reportReaderBridge struct {
	svc *logs.Service
}

func (b reportReaderBridge) GetReportCurrent(reportID, userID, userRole string) (*agent.TraceReport, error) {
	report, err := b.svc.GetReportByID(reportID, userID, userRole)
	if err != nil {
		if errors.Is(err, logs.ErrReportNotFound) || errors.Is(err, logs.ErrNotReportOwner) {
			return nil, nil
		}
		return nil, err
	}
	return &agent.TraceReport{ID: report.ID, ReportDate: report.ReportDate, RawText: report.RawText}, nil
}

// auditReaderBridge 经 audit 模块只读接口取任务相关审计行（trace 用）。
type auditReaderBridge struct {
	svc *audit.Service
}

func (b auditReaderBridge) ListByAgentTaskID(taskID string) ([]agent.AuditEvent, error) {
	records, err := b.svc.ListByAgentTaskID(taskID)
	if err != nil {
		return nil, err
	}
	events := make([]agent.AuditEvent, 0, len(records))
	for _, rec := range records {
		events = append(events, agent.AuditEvent{
			ID:          rec.ID,
			RequestID:   rec.RequestID,
			Username:    rec.Username,
			Method:      rec.Method,
			Path:        rec.Path,
			Action:      rec.Action,
			StatusCode:  rec.Status,
			ActorType:   rec.ActorType,
			AgentTaskID: rec.AgentTaskID,
			Detail:      rec.Detail,
			CreatedAt:   rec.CreatedAt,
		})
	}
	return events, nil
}

// resultResolverBridge 按 candidate_id 反查执行产物（trace 用），
// 查询落在 issues/experiences 各自仓储（本表列），不跨模块 join。
type resultResolverBridge struct {
	issues      *issues.Repository
	experiences *experiences.Repository
}

func (b resultResolverBridge) IssueByCandidateID(candidateID string) (*agent.TraceResult, error) {
	issue, err := b.issues.GetByCandidateID(candidateID)
	if err != nil || issue == nil {
		return nil, err
	}
	return &agent.TraceResult{
		IssueID: &issue.ID,
		Title:   issue.Title,
		URL:     "/projects/" + issue.ProjectID + "/issues/" + issue.ID,
	}, nil
}

func (b resultResolverBridge) ExperienceByCandidateID(candidateID string) (*agent.TraceResult, error) {
	exp, err := b.experiences.GetByCandidateID(candidateID)
	if err != nil || exp == nil {
		return nil, err
	}
	return &agent.TraceResult{
		ExperienceID: &exp.ID,
		Title:        exp.Title,
		URL:          "/experiences",
	}, nil
}

func (e candidateExecutor) Execute(candidate agent.AgentCandidateAction, actingUserID string) error {
	switch candidate.ActionType {
	case "create_issue":
		if candidate.ProjectID == nil {
			return fmt.Errorf("create_issue candidate has no project_id")
		}
		var req issues.CreateIssueRequest
		if err := json.Unmarshal(candidate.Payload, &req); err != nil {
			return err
		}
		req.AiGenerated = true
		req.AgentTaskID = &candidate.TaskID
		req.CandidateID = &candidate.ID
		_, err := e.issues.Create(*candidate.ProjectID, actingUserID, auth.RoleAgent, req)
		return err
	case "add_comment":
		var req struct {
			IssueID string `json:"issue_id"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(candidate.Payload, &req); err != nil {
			return err
		}
		_, err := e.issues.AddComment(req.IssueID, actingUserID, auth.RoleAgent, issues.AddCommentRequest{Content: req.Content})
		return err
	case "create_experience":
		if candidate.ProjectID == nil {
			return fmt.Errorf("create_experience candidate has no project_id")
		}
		var req experiences.CreateExperienceRequest
		if err := json.Unmarshal(candidate.Payload, &req); err != nil {
			return err
		}
		req.ProjectID = candidate.ProjectID
		req.AiGenerated = true
		req.AgentTaskID = &candidate.TaskID
		req.CandidateID = &candidate.ID
		_, err := e.experiences.Create(actingUserID, auth.RoleAgent, req)
		return err
	default:
		return fmt.Errorf("unsupported candidate action %q", candidate.ActionType)
	}
}

type templateReaderBridge struct {
	repo *steptemplates.Repository
}

func (b templateReaderBridge) GetTemplateWithItems(id string) (*assembly.SteptemplatesTemplate, []assembly.SteptemplatesItem, error) {
	tmpl, items, err := b.repo.GetTemplateWithItems(id)
	if err != nil {
		return nil, nil, err
	}
	if tmpl == nil {
		return nil, nil, nil
	}
	t := &assembly.SteptemplatesTemplate{
		ID:   tmpl.ID,
		Name: tmpl.Name,
		Kind: tmpl.Kind,
	}
	assemblyItems := make([]assembly.SteptemplatesItem, len(items))
	for i, item := range items {
		assemblyItems[i] = assembly.SteptemplatesItem{
			ID:             item.ID,
			Name:           item.Name,
			Description:    item.Description,
			StepOrder:      item.StepOrder,
			DependsOnOrder: item.DependsOnOrder,
		}
	}
	return t, assemblyItems, nil
}

type runTemplateReaderBridge struct {
	repo *steptemplates.Repository
}

func (b runTemplateReaderBridge) GetTemplateWithItems(id string) (*runs.SteptemplatesTemplate, []runs.SteptemplatesItem, error) {
	tmpl, items, err := b.repo.GetTemplateWithItems(id)
	if err != nil {
		return nil, nil, err
	}
	if tmpl == nil {
		return nil, nil, nil
	}
	t := &runs.SteptemplatesTemplate{
		ID:   tmpl.ID,
		Name: tmpl.Name,
		Kind: tmpl.Kind,
	}
	runItems := make([]runs.SteptemplatesItem, len(items))
	for i, item := range items {
		runItems[i] = runs.SteptemplatesItem{
			ID:             item.ID,
			Name:           item.Name,
			Description:    item.Description,
			StepOrder:      item.StepOrder,
			DependsOnOrder: item.DependsOnOrder,
		}
	}
	return t, runItems, nil
}

// ---- weekly 模块桥接（AI-1）：窄接口注入，weekly 不直读 daily_reports/issues/experiences ----

// weeklyReportReaderBridge 经 logs 模块仓储读本周日报（SQL 在 logs 包内，自有表）。
type weeklyReportReaderBridge struct {
	repo *logs.Repository
}

func (b weeklyReportReaderBridge) WeeklyReports(ctx context.Context, from, to string) ([]weekly.ReportEntry, error) {
	entries, err := b.repo.WeeklyReports(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]weekly.ReportEntry, len(entries))
	for i, e := range entries {
		out[i] = weekly.ReportEntry{ReportDate: e.ReportDate, AuthorName: e.AuthorName, RawText: e.RawText, Summary: e.Summary}
	}
	return out, nil
}

// weeklyIssueStatsBridge 经 issues 模块仓储读本周 issue 统计（SQL 在 issues 包内，自有表）。
type weeklyIssueStatsBridge struct {
	repo *issues.Repository
}

func (b weeklyIssueStatsBridge) WeeklyIssueStats(ctx context.Context, from, to string) (weekly.IssueStats, error) {
	stats, err := b.repo.WeeklyIssueStats(ctx, from, to)
	if err != nil {
		return weekly.IssueStats{}, err
	}
	return weekly.IssueStats{Created: stats.Created, Resolved: stats.Resolved, OpenHighCritical: stats.OpenHighCritical}, nil
}

// weeklyExperienceBridge 经 experiences 模块仓储落库/查重周报（SQL 在 experiences 包内，自有表）。
type weeklyExperienceBridge struct {
	repo *experiences.Repository
}

func (b weeklyExperienceBridge) FindWeeklySummary(title string) (*weekly.SavedSummary, error) {
	exp, err := b.repo.FindWeeklySummary(title)
	if err != nil || exp == nil {
		return nil, err
	}
	return &weekly.SavedSummary{ID: exp.ID, Title: exp.Title, Markdown: exp.Content, CreatedAt: exp.CreatedAt}, nil
}

func (b weeklyExperienceBridge) SaveWeeklySummary(authorID, title, content string) (*weekly.SavedSummary, error) {
	exp, err := b.repo.CreateWeeklySummary(authorID, title, content)
	if err != nil {
		return nil, err
	}
	return &weekly.SavedSummary{ID: exp.ID, Title: exp.Title, Markdown: exp.Content, CreatedAt: exp.CreatedAt}, nil
}

// weeklyNotifier 是 weekly 模块 notifier 窄接口的 notify 适配器：
// 主题固定 lab-weekly（周报频道），点击落地经验库页 /experiences；复用 notify.Send。
type weeklyNotifier struct{}

func (weeklyNotifier) Send(topic, title, msg, clickURL, priority string, tags []string) error {
	return notify.Send("lab-weekly", title, msg, notify.WebURL+"/experiences", priority, tags)
}

// experienceIssueBridge 经 issues 模块仓储读最近 resolved/closed 的 issue（AI-2
// 经验提取数据源；SQL 在 issues 包内只访问本模块表，见 AGENTS.md §5）。
type experienceIssueBridge struct {
	repo *issues.Repository
}

func (b experienceIssueBridge) ResolvedIssuesSince(ctx context.Context, since time.Time, limit int) ([]experiences.ResolvedIssue, error) {
	items, err := b.repo.ResolvedIssuesSince(ctx, since, limit)
	if err != nil {
		return nil, err
	}
	out := make([]experiences.ResolvedIssue, len(items))
	for i, item := range items {
		out[i] = experiences.ResolvedIssue{
			ID:          item.ID,
			ProjectID:   item.ProjectID,
			Title:       item.Title,
			Description: item.Description,
			Comments:    item.Comments,
			RunID:       item.RunID,
		}
	}
	return out, nil
}
