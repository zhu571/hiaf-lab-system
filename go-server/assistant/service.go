package assistant

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrNotFound           = errors.New("assistant session not found")
	ErrInvalid            = errors.New("invalid assistant request")
	ErrProjectUnsupported = errors.New("project sessions are not enabled yet")
)

const (
	statusActive   = "active"
	statusArchived = "archived"
	maxContentLen  = 20000
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) Create(ctx context.Context, userID string, req CreateSessionRequest) (*Session, error) {
	if strings.TrimSpace(req.ProjectID) != "" {
		return nil, ErrProjectUnsupported
	}
	title := strings.TrimSpace(req.Title)
	if len([]rune(title)) > 200 {
		return nil, ErrInvalid
	}
	if title == "" {
		title = "新会话"
	}
	sess := &Session{UserID: userID, Title: title}
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
	return s.repo.ListSessions(ctx, userID, status, limit)
}

func (s *Service) Get(ctx context.Context, userID, id string) (*Session, error) {
	sess, err := s.repo.GetSession(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, ErrNotFound
	}
	return sess, nil
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
	return s.repo.ListMessages(ctx, userID, id)
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
