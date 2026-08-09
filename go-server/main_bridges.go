package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zhu571/hiaf-lab-system/go-server/agent"
	"github.com/zhu571/hiaf-lab-system/go-server/assembly"
	"github.com/zhu571/hiaf-lab-system/go-server/audit"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/experiences"
	"github.com/zhu571/hiaf-lab-system/go-server/issues"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
	"github.com/zhu571/hiaf-lab-system/go-server/runs"
	"github.com/zhu571/hiaf-lab-system/go-server/steptemplates"
)

// main_bridges.go：main.go 装配用的桥接适配器（纯组织性拆分，P2-5）。
// agent 模块的候选执行与 trace 读取、assembly/runs 的模板读取都经这些适配器
// 在 main.go 构造期注入，业务模块间不跨模块读表。本文件不含装配逻辑。

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
