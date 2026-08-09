package automation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrRuleNotFound    = errors.New("automation rule not found")
	ErrInvalidName     = errors.New("invalid rule name")
	ErrInvalidTrigger  = errors.New("invalid trigger_event")
	ErrInvalidAction   = errors.New("invalid action")
	ErrNothingToUpdate = errors.New("nothing to update")
)

// Service 承载规则校验（一期白名单与 DB CHECK 一致，service 层提前给出 400 而非 500）。
type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context) ([]Rule, error) {
	return s.repo.List(ctx)
}

func (s *Service) Create(ctx context.Context, createdBy string, req CreateRuleRequest) (Rule, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		return Rule{}, ErrInvalidName
	}
	if req.TriggerEvent != TriggerDailyReportSubmitted {
		return Rule{}, ErrInvalidTrigger
	}
	if err := validateAction(req.Action); err != nil {
		return Rule{}, err
	}
	rule := Rule{Name: name, TriggerEvent: req.TriggerEvent, Action: req.Action}
	if createdBy != "" {
		rule.CreatedBy = &createdBy
	}
	return s.repo.Create(ctx, rule)
}

// SetEnabled 是一期唯一允许的修改（不改 trigger_event/action，避免无 schema 校验的写穿）。
func (s *Service) SetEnabled(ctx context.Context, id string, req UpdateRuleRequest) (Rule, error) {
	if req.Enabled == nil {
		return Rule{}, ErrNothingToUpdate
	}
	rule, err := s.repo.SetEnabled(ctx, id, *req.Enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, ErrRuleNotFound
	}
	return rule, err
}

func (s *Service) Delete(ctx context.Context, id string) error {
	err := s.repo.Delete(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRuleNotFound
	}
	return err
}

// validateAction 校验 action JSON schema：必须是对象且 type 在一期白名单内。
func validateAction(raw json.RawMessage) error {
	var action struct {
		Type string `json:"type"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &action) != nil {
		return ErrInvalidAction
	}
	if action.Type != ActionEnqueueAgentTask {
		return ErrInvalidAction
	}
	return nil
}
