package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrTaskNotFound        = errors.New("Agent 任务不存在")
	ErrInvalidLease        = errors.New("Agent 任务租约无效或已过期")
	ErrInvalidInput        = errors.New("请求参数无效")
	ErrCandidateNotFound   = errors.New("候选动作不存在")
	ErrCandidateNotPending = errors.New("候选动作已审核")
)

type CandidateExecutor interface {
	Execute(candidate AgentCandidateAction, actingUserID string) error
}

// ReportReader 由 main.go 以 logs 模块 service 装配注入：trace 读取日报当前值，
// agent 模块不 SELECT daily_reports（模块单向依赖）。
type ReportReader interface {
	GetReportCurrent(reportID, userID, userRole string) (*TraceReport, error)
}

// AuditReader 由 main.go 以 audit 模块装配注入：trace 读取任务相关审计行，
// agent 模块不 SELECT audit_log（模块单向依赖）。
type AuditReader interface {
	ListByAgentTaskID(taskID string) ([]AuditEvent, error)
}

// CandidateResultResolver 由 main.go 以 issues/experiences 仓储装配注入：
// trace 按 candidate_id 反查执行产物，agent 模块不 SELECT issues/experiences。
type CandidateResultResolver interface {
	IssueByCandidateID(candidateID string) (*TraceResult, error)
	ExperienceByCandidateID(candidateID string) (*TraceResult, error)
}

type Service struct {
	repo           *Repository
	executor       CandidateExecutor
	reportReader   ReportReader
	auditReader    AuditReader
	resultResolver CandidateResultResolver
}

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) SetExecutor(executor CandidateExecutor) { s.executor = executor }

func (s *Service) SetReportReader(reader ReportReader) { s.reportReader = reader }

func (s *Service) SetAuditReader(reader AuditReader) { s.auditReader = reader }

func (s *Service) SetResultResolver(resolver CandidateResultResolver) { s.resultResolver = resolver }

func (s *Service) ValidateAgentTask(taskID, actingUserID string) (bool, error) {
	return s.repo.ValidateTask(taskID, actingUserID)
}

// QueueDepth 返回 agent 队列深度（pending + processing 任务数，P2-2 指标用）。
// 由 main.go 经 middleware.SetAgentQueueProvider 注入 /metrics，agent 不对外暴露表访问。
func (s *Service) QueueDepth(ctx context.Context) (int, error) {
	return s.repo.CountInQueue(ctx)
}

func (s *Service) Claim(leaseSeconds int) (*PendingAgentTask, error) {
	if leaseSeconds == 0 {
		leaseSeconds = 300
	}
	if leaseSeconds < 30 || leaseSeconds > 3600 {
		return nil, ErrInvalidInput
	}
	return s.repo.Claim(leaseSeconds)
}

// Renew 校验并延长任务租约（R8）：worker 周期续约，租约长度不再需要覆盖
// 「前置 HTTP 链 + LLM + fail 开销」的全链路最坏时间。
func (s *Service) Renew(taskID string, req RenewTaskRequest) (*PendingAgentTask, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(req.ClaimToken) == "" {
		return nil, ErrInvalidInput
	}
	leaseSeconds := req.LeaseSeconds
	if leaseSeconds == 0 {
		leaseSeconds = 300
	}
	if leaseSeconds < 30 || leaseSeconds > 3600 {
		return nil, ErrInvalidInput
	}
	return s.repo.Renew(taskID, req.ClaimToken, leaseSeconds)
}

func (s *Service) Complete(taskID string, req CompleteTaskRequest) (*PendingAgentTask, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(req.Model) == "" ||
		strings.TrimSpace(req.PromptVersion) == "" || !json.Valid(req.Result) {
		return nil, ErrInvalidInput
	}
	for _, candidate := range req.Candidates {
		if !validActionType(candidate.ActionType) || !json.Valid(candidate.Payload) {
			return nil, ErrInvalidInput
		}
	}
	if req.ReportDate != "" {
		if _, err := time.Parse(time.DateOnly, strings.TrimSpace(req.ReportDate)); err != nil {
			return nil, ErrInvalidInput
		}
	}
	return s.repo.Complete(taskID, req)
}

func (s *Service) Fail(taskID, detail, claimToken string) (*PendingAgentTask, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(detail) == "" {
		return nil, ErrInvalidInput
	}
	return s.repo.Fail(taskID, sanitizeError(detail), 3, claimToken)
}

func (s *Service) ListCandidates(status string, page, perPage int) (*CandidateListResult, error) {
	status = strings.TrimSpace(status)
	if status != "" && !validCandidateStatus(status) {
		return nil, ErrInvalidInput
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	items, total, err := s.repo.ListCandidates(status, page, perPage)
	if err != nil {
		return nil, err
	}
	return &CandidateListResult{Items: items, Total: total, Page: page, PerPage: perPage}, nil
}

func (s *Service) ApproveCandidate(id, reviewerID string) (*AgentCandidateAction, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(reviewerID) == "" || s.executor == nil {
		return nil, ErrInvalidInput
	}
	item, actingUserID, err := s.repo.ApproveCandidate(id, reviewerID)
	if err != nil || item.Status == CandidateExecuted {
		return item, err
	}
	// ponytail: approved is the durable no-duplicate fence; add a reconciler if crash recovery between execution and final status becomes necessary.
	if err := s.executor.Execute(*item, actingUserID); err != nil {
		return s.repo.MarkCandidateFailed(item.ID, sanitizeError(err.Error()))
	}
	return s.repo.MarkCandidateExecuted(item.ID)
}

func (s *Service) RejectCandidate(id, reviewerID, reason string) (*AgentCandidateAction, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(reviewerID) == "" || strings.TrimSpace(reason) == "" {
		return nil, ErrInvalidInput
	}
	return s.repo.RejectCandidate(id, reviewerID, strings.TrimSpace(reason))
}

// GetCandidateTrace 组装候选全链路溯源（C8）：candidate + task 快照 + 日报当前值
// + 执行产物 + 相关审计行。各外部数据均经注入接口获取，agent 不跨模块读表。
func (s *Service) GetCandidateTrace(candidateID, userID, userRole string) (*CandidateTrace, error) {
	if strings.TrimSpace(candidateID) == "" {
		return nil, ErrInvalidInput
	}
	candidate, _, err := s.repo.GetCandidate(candidateID)
	if err != nil {
		return nil, err
	}
	task, err := s.repo.GetTask(candidate.TaskID)
	if err != nil {
		return nil, err
	}
	trace := &CandidateTrace{
		Candidate: candidate,
		Task: TraceTask{
			ID:              task.ID,
			Status:          task.Status,
			Model:           task.Model,
			PromptVersion:   task.PromptVersion,
			AgentConfidence: task.AgentConfidence,
			RawTextSnapshot: task.RawTextSnapshot,
			RawTextSHA256:   task.RawTextSHA256,
			ReportDate:      task.ReportDate,
		},
		Audit: []AuditEvent{},
	}
	if s.reportReader != nil {
		report, err := s.reportReader.GetReportCurrent(task.ReportID, userID, userRole)
		if err != nil {
			return nil, err
		}
		trace.Report = report
	}
	if s.resultResolver != nil {
		var result *TraceResult
		switch candidate.ActionType {
		case "create_issue":
			result, err = s.resultResolver.IssueByCandidateID(candidate.ID)
		case "create_experience":
			result, err = s.resultResolver.ExperienceByCandidateID(candidate.ID)
		}
		if err != nil {
			return nil, err
		}
		trace.Result = result
	}
	if s.auditReader != nil {
		events, err := s.auditReader.ListByAgentTaskID(task.ID)
		if err != nil {
			return nil, err
		}
		trace.Audit = events
	}
	return trace, nil
}

func validActionType(v string) bool {
	switch v {
	case "create_issue", "add_comment", "create_experience":
		return true
	default:
		return false
	}
}

func validCandidateStatus(v string) bool {
	switch v {
	case CandidatePending, CandidateApproved, CandidateRejected, CandidateExecuted, CandidateExecutionFailed:
		return true
	default:
		return false
	}
}

func sanitizeError(v string) string {
	v = strings.TrimSpace(v)
	lower := strings.ToLower(v)
	for _, marker := range []string{"bearer ", "api_key", "api key", "token", "password"} {
		if strings.Contains(lower, marker) {
			return "agent task failed (sensitive detail redacted)"
		}
	}
	if utf8.RuneCountInString(v) <= 512 {
		return v
	}
	return string([]rune(v)[:512])
}
