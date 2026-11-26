package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

var (
	ErrNotFound           = errors.New("assistant session not found")
	ErrForbidden          = errors.New("assistant session forbidden")
	ErrInvalid            = errors.New("invalid assistant request")
	ErrProjectUnsupported = errors.New("project sessions are not enabled yet")
	ErrUpstream           = errors.New("assistant upstream unavailable")
)

// ProjectAccessChecker 由 main 注入，assistant 不直接读项目表。
type ProjectAccessChecker interface {
	CanReadProject(ctx context.Context, userID, projectID string) (bool, error)
}

const (
	statusActive   = "active"
	statusArchived = "archived"
	maxContentLen  = 20000
)

type Service struct {
	repo     *Repository
	replier  Replier
	projects ProjectAccessChecker
}

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

// SetReplier 注入真实 LLM 客户端；未注入时 StartRun 返回明确的占位回复。
func (s *Service) SetReplier(r Replier) { s.replier = r }

// SetProjectAccessChecker 注入项目成员校验；未注入时项目会话不可用。
func (s *Service) SetProjectAccessChecker(c ProjectAccessChecker) { s.projects = c }

func (s *Service) Create(ctx context.Context, userID string, req CreateSessionRequest) (*Session, error) {
	var projectID *string
	if id := strings.TrimSpace(req.ProjectID); id != "" {
		if s.projects == nil {
			return nil, ErrProjectUnsupported
		}
		ok, err := s.projects.CanReadProject(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrForbidden
		}
		projectID = &id
	}
	title := strings.TrimSpace(req.Title)
	if len([]rune(title)) > 200 {
		return nil, ErrInvalid
	}
	if title == "" {
		title = "新会话"
	}
	sess := &Session{UserID: userID, Title: title, ProjectID: projectID}
	if err := s.repo.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Service) List(ctx context.Context, userID, status string, limit int) ([]Session, error) {
	if status != "" && status != statusActive && status != statusArchived {
		return nil, ErrInvalid
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	items, err := s.repo.ListVisibleSessions(ctx, userID, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(items))
	for _, sess := range items {
		if sess.UserID == userID {
			out = append(out, sess)
			continue
		}
		if sess.ProjectID == nil || s.projects == nil {
			continue
		}
		ok, err := s.projects.CanReadProject(ctx, userID, *sess.ProjectID)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, sess)
		}
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, userID, id string) (*Session, error) {
	sess, err := s.repo.GetSessionAny(ctx, id)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, ErrNotFound
	}
	if sess.UserID == userID {
		return sess, nil
	}
	if sess.ProjectID != nil && s.projects != nil {
		ok, err := s.projects.CanReadProject(ctx, userID, *sess.ProjectID)
		if err != nil {
			return nil, err
		}
		if ok {
			return sess, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Service) Update(ctx context.Context, userID, id string, req UpdateSessionRequest) (*Session, error) {
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" || len([]rune(title)) > 200 {
			return nil, ErrInvalid
		}
		req.Title = &title
	}
	if req.Status != nil && *req.Status != statusActive && *req.Status != statusArchived {
		return nil, ErrInvalid
	}
	sess, err := s.repo.UpdateSession(ctx, userID, id, req.Title, req.Status)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, ErrNotFound
	}
	return sess, nil
}

func (s *Service) Archive(ctx context.Context, userID, id string) error {
	archived := statusArchived
	sess, err := s.repo.UpdateSession(ctx, userID, id, nil, &archived)
	if err != nil {
		return err
	}
	if sess == nil {
		return ErrNotFound
	}
	return nil
}

func (s *Service) Messages(ctx context.Context, userID, id string) ([]Message, error) {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return nil, err
	}
	return s.repo.ListMessagesBySession(ctx, id)
}

func (s *Service) AddMessage(ctx context.Context, userID, id, content string) (*Message, error) {
	content = strings.TrimSpace(content)
	if content == "" || len([]rune(content)) > maxContentLen {
		return nil, ErrInvalid
	}
	m, err := s.repo.AddUserMessage(ctx, userID, id, content)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrNotFound
	}
	return m, nil
}

// StartRun 记录一次用户提问并返回一条明确的占位 assistant 回复。
// 真实 LLM/tool 循环接入后替换本方法内部实现，API 形状保持不变。
func (s *Service) StartRun(ctx context.Context, userID, sessionID, content string) (*RunResult, error) {
	sess, err := s.Get(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.Status != statusActive {
		return nil, ErrInvalid
	}
	// 项目会话对其他成员只读；继续对话仍由创建者发起。
	if sess.UserID != userID {
		return nil, ErrForbidden
	}
	userMsg, err := s.AddMessage(ctx, userID, sessionID, content)
	if err != nil {
		return nil, err
	}
	run := &Run{SessionID: sessionID, UserID: userID, Model: "placeholder", PromptVersion: "assistant-placeholder-v1"}
	if err := s.repo.CreateRun(ctx, run); err != nil {
		return nil, err
	}
	reply := "AI 运行时尚未接入，你的问题已记录；接入后会在这里回答。"
	var meta []byte
	if s.replier != nil {
		history, err := s.repo.ListMessages(ctx, userID, sessionID)
		if err != nil {
			_ = s.repo.FinishRun(ctx, run.ID, "failed", err.Error())
			return nil, err
		}
		if len(history) > 20 {
			history = history[len(history)-20:]
		}
		answer, err := s.replier.Reply(ctx, history)
		if err != nil {
			_ = s.repo.FinishRun(ctx, run.ID, "failed", err.Error())
			return nil, ErrUpstream
		}
		reply = answer
		run.Model = "py-agent"
		run.PromptVersion = "assistant-chat-v1"
	}
	if s.replier == nil {
		meta, _ = json.Marshal(map[string]any{"placeholder": true})
	}
	assistantMsg, err := s.repo.AddAssistantMessage(ctx, sessionID, run.ID, reply, meta)
	if err != nil {
		_ = s.repo.FinishRun(ctx, run.ID, "failed", err.Error())
		return nil, err
	}
	if err := s.repo.FinishRun(ctx, run.ID, "done", ""); err != nil {
		return nil, err
	}
	finished := assistantMsg.CreatedAt
	run.Status = "done"
	run.FinishedAt = &finished
	return &RunResult{Run: *run, Messages: []Message{*userMsg, *assistantMsg}}, nil
}
