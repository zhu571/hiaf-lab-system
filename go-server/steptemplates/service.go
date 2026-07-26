package steptemplates

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrTemplateNotFound = errors.New("模板不存在")
	ErrInvalidInput     = errors.New("请求参数无效")
	ErrForbidden        = errors.New("当前用户无权执行此操作")
	ErrAgentRejected    = errors.New("agent 角色不允许执行此操作")
	ErrDuplicateItems   = errors.New("步骤序号重复")
	ErrDependencyInvalid = errors.New("依赖步骤序号无效")
)

type stepRepo interface {
	Create(template *StepTemplate, items []StepTemplateItem) (*StepTemplate, error)
	GetByID(id string) (*StepTemplate, error)
	GetTemplateWithItems(id string) (*StepTemplate, []StepTemplateItem, error)
	List(kind, query string, page, perPage int) ([]StepTemplate, int, error)
	Update(id string, req UpdateTemplateRequest) (*StepTemplate, error)
	ReplaceItems(templateID string, items []StepTemplateItem) error
	SoftDelete(id string) error
}

type Service struct {
	repo            stepRepo
	db              *sql.DB
	client          *http.Client
	plannerURL      string
	plannerToken    string
	rlMu            sync.Mutex
	rlCalls         map[string][]time.Time
}

func NewService(repo stepRepo, db *sql.DB) *Service {
	svc := &Service{
		repo:    repo,
		db:      db,
		client:  &http.Client{Timeout: 60 * time.Second},
		rlCalls: map[string][]time.Time{},
	}
	return svc
}

func (s *Service) ConfigurePlanner(url, token string) {
	s.plannerURL = strings.TrimRight(url, "/")
	s.plannerToken = token
}

func (s *Service) AutoConfigure() error {
	url := strings.TrimRight(os.Getenv("PY_AGENT_INTERPRET_URL"), "/")
	tokenPath := os.Getenv("PY_AGENT_INTERNAL_TOKEN_FILE")
	var token string
	if tokenPath != "" {
		data, err := os.ReadFile(filepath.Clean(tokenPath))
		if err != nil {
			return fmt.Errorf("read py-agent token: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	if url != "" && token != "" {
		s.plannerURL = url
		s.plannerToken = token
	}
	return nil
}

func (s *Service) Generate(ctx context.Context, userID, userRole string, req GenerateRequest) (*GenerateResponseData, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	if !s.allowOne(userID) {
		return nil, fmt.Errorf("AI 生成请求过于频繁，请稍后再试")
	}
	if s.plannerURL == "" || s.plannerToken == "" {
		return nil, fmt.Errorf("AI 生成服务未配置")
	}
	kind := strings.TrimSpace(req.Kind)
	if kind != "assembly" && kind != "experiment" {
		return nil, fmt.Errorf("kind 必须为 assembly 或 experiment")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" || len(prompt) > 4000 {
		return nil, ErrInvalidInput
	}

	payload, err := json.Marshal(map[string]any{
		"kind":    kind,
		"prompt":  prompt,
		"context": req.Context,
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.plannerURL+"/v1/step-plan", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.plannerToken)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("py-agent 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("py-agent 返回 %d: %s", resp.StatusCode, string(body))
	}

	var result GenerateResponseData
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("解码 AI 响应失败: %w", err)
	}

	if result.Status != "ok" && result.Status != "clarify" && result.Status != "rejected" {
		return nil, fmt.Errorf("AI 返回无效状态: %s", result.Status)
	}

	if result.Status == "ok" {
		if err := validateSteps(result.Steps); err != nil {
			return nil, fmt.Errorf("AI 生成的步骤校验失败: %w", err)
		}
		result.Steps = reorderSteps(result.Steps)
	}

	return &result, nil
}

func (s *Service) Create(userID, userRole string, req CreateTemplateRequest) (*StepTemplate, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	if err := s.requireWriteAccess(userRole, userID, nil); err != nil {
		return nil, err
	}
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}
	items := reorderAndNormalizeItems(req.Items)
	template := &StepTemplate{
		Name:         strings.TrimSpace(req.Name),
		Kind:         strings.TrimSpace(req.Kind),
		Description:  strings.TrimSpace(req.Description),
		SourcePrompt: strings.TrimSpace(req.SourcePrompt),
		AIGenerated:  req.AIGenerated,
		CreatedBy:    &userID,
	}
	return s.repo.Create(template, items)
}

func (s *Service) GetByID(id, userID, userRole string) (*StepTemplate, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	template, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, ErrTemplateNotFound
	}
	return template, nil
}

func (s *Service) List(userRole string, kind, query string, page, perPage int) (*ListResult, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
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
	items, total, err := s.repo.List(kind, query, page, perPage)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []StepTemplate{}
	}
	return &ListResult{Items: items, Total: total, Page: page, PerPage: perPage}, nil
}

func (s *Service) Update(id, userID, userRole string, req UpdateTemplateRequest) (*StepTemplate, error) {
	if userRole == auth.RoleAgent {
		return nil, ErrAgentRejected
	}
	template, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, ErrTemplateNotFound
	}
	if err := s.requireWriteAccess(userRole, userID, template); err != nil {
		return nil, err
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 256 {
			return nil, ErrInvalidInput
		}
		req.Name = &name
	}
	updated, err := s.repo.Update(id, req)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrTemplateNotFound
	}
	return updated, nil
}

func (s *Service) ReplaceItems(id, userID, userRole string, req ReplaceItemsRequest) error {
	if userRole == auth.RoleAgent {
		return ErrAgentRejected
	}
	template, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if template == nil {
		return ErrTemplateNotFound
	}
	if err := s.requireWriteAccess(userRole, userID, template); err != nil {
		return err
	}
	items := reorderAndNormalizeItems(req.Items)
	return s.repo.ReplaceItems(id, items)
}

func (s *Service) SoftDelete(id, userID, userRole string) error {
	if userRole == auth.RoleAgent {
		return ErrAgentRejected
	}
	template, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if template == nil {
		return ErrTemplateNotFound
	}
	if err := s.requireWriteAccess(userRole, userID, template); err != nil {
		return err
	}
	return s.repo.SoftDelete(id)
}

func (s *Service) HasAnyProjectRole(userID string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM project_members
		 WHERE user_id=$1 AND status='active' AND role IN ('member','maintainer','owner'))`,
		userID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check project membership: %w", err)
	}
	return exists, nil
}

func (s *Service) requireWriteAccess(userRole, userID string, template *StepTemplate) error {
	if userRole == auth.RoleAdmin {
		return nil
	}
	if userRole == auth.RoleMaintainer {
		return nil
	}
	if template != nil && template.CreatedBy != nil && *template.CreatedBy == userID {
		return nil
	}
	return ErrForbidden
}

func (s *Service) requireGenerateAccess(userID string) error {
	exists, err := s.HasAnyProjectRole(userID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrForbidden
	}
	return nil
}

func (s *Service) allowOne(userID string) bool {
	now, cutoff := time.Now(), time.Now().Add(-time.Minute)
	s.rlMu.Lock()
	defer s.rlMu.Unlock()
	calls := s.rlCalls[userID][:0]
	for _, call := range s.rlCalls[userID] {
		if call.After(cutoff) {
			calls = append(calls, call)
		}
	}
	if len(calls) >= 10 {
		s.rlCalls[userID] = calls
		return false
	}
	s.rlCalls[userID] = append(calls, now)
	return true
}

func validateCreateRequest(req CreateTemplateRequest) error {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 256 {
		return fmt.Errorf("模板名称需 1-256 字符")
	}
	kind := strings.TrimSpace(req.Kind)
	if kind != "assembly" && kind != "experiment" {
		return fmt.Errorf("kind 必须为 assembly 或 experiment")
	}
	if len(req.Items) < MinItems || len(req.Items) > MaxItems {
		return fmt.Errorf("步骤数需在 %d-%d 之间", MinItems, MaxItems)
	}
	return nil
}

func validateSteps(steps []StepCandidate) error {
	if len(steps) < MinItems || len(steps) > MaxItems {
		return fmt.Errorf("步骤数需在 %d-%d 之间，实际 %d", MinItems, MaxItems, len(steps))
	}
	orders := make(map[int]bool, len(steps))
	for _, s := range steps {
		if s.StepOrder <= 0 {
			return fmt.Errorf("step_order 必须 > 0")
		}
		orders[s.StepOrder] = true
	}
	if len(orders) != len(steps) {
		return fmt.Errorf("step_order 重复")
	}
	sorted := make([]StepCandidate, len(steps))
	copy(sorted, steps)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StepOrder < sorted[j].StepOrder })
	ordinals := make(map[int]int, len(sorted))
	for i, s := range sorted {
		ordinals[s.StepOrder] = i + 1
	}
	for _, s := range steps {
		if s.DependsOnOrder != nil {
			if _, ok := ordinals[*s.DependsOnOrder]; !ok {
				return fmt.Errorf("depends_on_order %d 指向不存在的步骤", *s.DependsOnOrder)
			}
			if ordinals[*s.DependsOnOrder] >= ordinals[s.StepOrder] {
				return fmt.Errorf("depends_on_order %d 必须小于当前 step_order %d", *s.DependsOnOrder, s.StepOrder)
			}
		}
	}
	return nil
}

func reorderSteps(steps []StepCandidate) []StepCandidate {
	sorted := make([]StepCandidate, len(steps))
	copy(sorted, steps)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StepOrder < sorted[j].StepOrder })
	remap := make(map[int]int, len(sorted))
	for i := range sorted {
		remap[sorted[i].StepOrder] = i + 1
	}
	result := make([]StepCandidate, len(sorted))
	for i := range sorted {
		s := sorted[i]
		s.StepOrder = i + 1
		if s.DependsOnOrder != nil {
			newDep := remap[*s.DependsOnOrder]
			s.DependsOnOrder = &newDep
		}
		result[i] = s
	}
	return result
}

func reorderAndNormalizeItems(items []ItemDef) []StepTemplateItem {
	sorted := make([]ItemDef, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StepOrder < sorted[j].StepOrder })
	remap := make(map[int]int, len(sorted))
	for i := range sorted {
		remap[sorted[i].StepOrder] = i + 1
	}
	result := make([]StepTemplateItem, len(sorted))
	for i := range sorted {
		s := sorted[i]
		newOrder := i + 1
		var newDep *int
		if s.DependsOnOrder != nil {
			mapped := remap[*s.DependsOnOrder]
			newDep = &mapped
		}
		result[i] = StepTemplateItem{
			Name:           strings.TrimSpace(s.Name),
			Description:    strings.TrimSpace(s.Description),
			StepOrder:      newOrder,
			DependsOnOrder: newDep,
		}
	}
	return result
}

type projectMemberChecker interface {
	GetMember(projectID, userID string) (*projects.ProjectMember, error)
}
