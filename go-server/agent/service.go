package agent

import (
	"encoding/json"
	"errors"
	"strings"
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

type Service struct {
	repo     *Repository
	executor CandidateExecutor
}

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) SetExecutor(executor CandidateExecutor) { s.executor = executor }

func (s *Service) ValidateAgentTask(taskID, actingUserID string) (bool, error) {
	return s.repo.ValidateTask(taskID, actingUserID)
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
	return s.repo.Complete(taskID, req)
}

func (s *Service) Fail(taskID, detail string) (*PendingAgentTask, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(detail) == "" {
		return nil, ErrInvalidInput
	}
	return s.repo.Fail(taskID, sanitizeError(detail), 3)
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
