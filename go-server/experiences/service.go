package experiences

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrExperienceNotFound        = errors.New("经验不存在")
	ErrNotCandidate              = errors.New("只有候选经验可以修改或发布")
	ErrNotPublished              = errors.New("只有已发布经验可以归档")
	ErrPublishForbidden          = errors.New("当前用户无权发布经验")
	ErrGlobalExperienceAdminOnly = errors.New("全局经验仅管理员可创建、发布或归档")
	ErrInvalidInput              = errors.New("请求参数无效")
	ErrProjectNotFound           = errors.New("项目不存在")
	ErrForbidden                 = errors.New("当前用户无权访问该项目")
	ErrExtractNotConfigured      = errors.New("AI 经验提取服务未配置")
	ErrExtractUpstream           = errors.New("py-agent 上游服务错误")
	ErrInvalidLLMOutput          = errors.New("经验提取模型输出无效")
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	CanAccessProject(projectID, userID, userRole, minRole string) (bool, error)
	ProjectRole(projectID, userID, userRole string) (string, error)
}

type AgentTaskValidator interface {
	ValidateAgentTask(taskID, actingUserID string) (bool, error)
}

// issueSource 由 main.go 以 issues 模块仓储装配注入：经验提取（AI-2）的 issue 数据源，
// experiences 不 SELECT issues 表（模块单向依赖，见 AGENTS.md §5）。
type issueSource interface {
	ResolvedIssuesSince(ctx context.Context, since time.Time, limit int) ([]ResolvedIssue, error)
}

// extractLLM 由 main.go 以 HTTP client 装配注入：调 py-agent /v1/experience-extract。
type extractLLM interface {
	Extract(ctx context.Context, req ExtractLLMRequest) (*ExtractLLMResponse, error)
}

type experienceRepository interface {
	Create(authorID string, req CreateExperienceRequest) (*Experience, error)
	GetByID(id string) (*Experience, error)
	List(params ExperienceListParams) ([]Experience, int, error)
	Update(id string, req UpdateExperienceRequest) (*Experience, error)
	Publish(id, reviewerID string) (*Experience, error)
	Archive(id string) (*Experience, error)
}

type Service struct {
	repo      experienceRepository
	access    ProjectAccessChecker
	validator AgentTaskValidator
	source    issueSource
	extractor extractLLM
	now       func() time.Time
}

func NewService(repo experienceRepository, access ProjectAccessChecker, validators ...AgentTaskValidator) *Service {
	s := &Service{repo: repo, access: access, now: time.Now}
	if len(validators) > 0 {
		s.validator = validators[0]
	}
	return s
}

// SetIssueSource / SetExtractor 由 main.go 构造期注入（AI-2 经验提取依赖；
// 对齐 agent 模块 SetExecutor 注入化先例，见 AGENTS.md §5）。
func (s *Service) SetIssueSource(source issueSource) { s.source = source }

func (s *Service) SetExtractor(extractor extractLLM) { s.extractor = extractor }

func (s *Service) Create(userID, userRole string, req CreateExperienceRequest) (*Experience, error) {
	if err := s.validateAgentFields(userID, userRole, req.AiGenerated, req.AgentTaskID, req.CandidateID); err != nil {
		return nil, ErrInvalidInput
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	if req.Title == "" || len(req.Title) > 256 || req.Content == "" {
		return nil, ErrInvalidInput
	}
	req.Tags = normalizeTags(req.Tags)
	links, err := s.normalizeLinks(req.LinkedProjects)
	if err != nil {
		return nil, err
	}
	req.LinkedProjects = links
	if req.ProjectID != nil {
		projectID := strings.TrimSpace(*req.ProjectID)
		if projectID == "" {
			req.ProjectID = nil
		} else {
			req.ProjectID = &projectID
		}
	}

	if req.ProjectID == nil {
		if userRole != auth.RoleAdmin {
			return nil, ErrGlobalExperienceAdminOnly
		}
		return s.repo.Create(userID, req)
	}

	if err := s.requireProject(*req.ProjectID); err != nil {
		return nil, err
	}
	ok, err := s.access.CanAccessProject(*req.ProjectID, userID, userRole, projects.RoleMember)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	return s.repo.Create(userID, req)
}

func (s *Service) validateAgentFields(userID, userRole string, aiGenerated bool, taskID, candidateID *string) error {
	if userRole != auth.RoleAgent {
		if aiGenerated || taskID != nil || candidateID != nil {
			return ErrInvalidInput
		}
		return nil
	}
	if !aiGenerated || taskID == nil || strings.TrimSpace(*taskID) == "" || s.validator == nil {
		return ErrInvalidInput
	}
	valid, err := s.validator.ValidateAgentTask(strings.TrimSpace(*taskID), userID)
	if err != nil {
		return err
	}
	if !valid {
		return ErrInvalidInput
	}
	return nil
}

func (s *Service) List(userID, userRole string, params ExperienceListParams) (*ExperienceListResult, error) {
	params.Status = strings.TrimSpace(params.Status)
	if params.Status == "" {
		params.Status = StatusPublished
	}
	if !validStatus(params.Status) {
		return nil, ErrInvalidInput
	}
	params.Tags = normalizeTags(params.Tags)
	params.Keyword = strings.TrimSpace(params.Keyword)
	params.UserRole = userRole
	if params.PerPage > 100 {
		params.PerPage = 100
	}
	if strings.TrimSpace(params.ProjectID) != "" {
		params.ProjectID = strings.TrimSpace(params.ProjectID)
		ok, err := s.access.CanAccessProject(params.ProjectID, userID, userRole, projects.RoleViewer)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrForbidden
		}
		role, err := s.access.ProjectRole(params.ProjectID, userID, userRole)
		if err != nil {
			return nil, err
		}
		params.ProjectRole = role
	}
	if params.Status == StatusCandidate && userRole != auth.RoleAdmin && !canReviewProjectRole(params.ProjectRole) {
		params.CandidateAuthorID = userID
	}

	items, total, err := s.repo.List(params)
	if err != nil {
		return nil, err
	}
	page, perPage := normalizePage(params.Page, params.PerPage)
	if perPage > 100 {
		perPage = 100
	}
	return &ExperienceListResult{Items: items, Total: total, Page: page, PerPage: perPage}, nil
}

func (s *Service) GetByID(id, userID, userRole string) (*Experience, error) {
	exp, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if exp == nil {
		return nil, ErrExperienceNotFound
	}
	if !s.canRead(*exp, userID, userRole) {
		return nil, ErrForbidden
	}
	return exp, nil
}

func (s *Service) Update(id, userID, userRole string, req UpdateExperienceRequest) (*Experience, error) {
	exp, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if exp == nil {
		return nil, ErrExperienceNotFound
	}
	if exp.Status != StatusCandidate {
		return nil, ErrNotCandidate
	}
	if !s.canUpdate(*exp, userID, userRole) {
		return nil, ErrForbidden
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" || len(title) > 256 {
			return nil, ErrInvalidInput
		}
		req.Title = &title
	}
	if req.Content != nil {
		content := strings.TrimSpace(*req.Content)
		if content == "" {
			return nil, ErrInvalidInput
		}
		req.Content = &content
	}
	if req.Tags != nil {
		req.Tags = normalizeTags(req.Tags)
	}
	if req.LinkedProjects != nil {
		links, err := s.normalizeLinks(req.LinkedProjects)
		if err != nil {
			return nil, err
		}
		req.LinkedProjects = links
	}
	updated, err := s.repo.Update(id, req)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) Publish(id, userID, userRole string) (*Experience, error) {
	exp, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if exp == nil {
		return nil, ErrExperienceNotFound
	}
	if exp.Status != StatusCandidate {
		return nil, ErrNotCandidate
	}
	if exp.ProjectID == nil {
		if userRole != auth.RoleAdmin {
			return nil, ErrGlobalExperienceAdminOnly
		}
		return s.repo.Publish(id, userID)
	}
	ok, err := s.access.CanAccessProject(*exp.ProjectID, userID, userRole, projects.RoleMaintainer)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrPublishForbidden
	}
	return s.repo.Publish(id, userID)
}

func (s *Service) Archive(id, userID, userRole string) (*Experience, error) {
	exp, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if exp == nil {
		return nil, ErrExperienceNotFound
	}
	if exp.Status != StatusPublished {
		return nil, ErrNotPublished
	}
	if exp.ProjectID == nil {
		if userRole != auth.RoleAdmin {
			return nil, ErrGlobalExperienceAdminOnly
		}
		return s.repo.Archive(id)
	}
	ok, err := s.access.CanAccessProject(*exp.ProjectID, userID, userRole, projects.RoleOwner)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	return s.repo.Archive(id)
}

// ExtractCandidates 经验候选提取（AI-2）：maintainer+ 手动触发，取最近 days 天
// （默认 7，限 1-30）resolved/closed 的 issue → LLM 提炼 0-N 条经验条目 →
// 校验后落库为 ai_generated=true + candidate 草稿（tags 追加 ai_extracted 标记），
// 由现有 experiences 审核流程（Update/Publish）人工审核发布。
func (s *Service) ExtractCandidates(ctx context.Context, userID, userRole string, days int) (*ExtractCandidatesResult, error) {
	if userRole != auth.RoleAdmin && userRole != auth.RoleMaintainer {
		return nil, ErrForbidden
	}
	if s.source == nil || s.extractor == nil {
		return nil, ErrExtractNotConfigured
	}
	if days <= 0 {
		days = 7
	}
	if days > 30 {
		days = 30
	}
	since := s.now().AddDate(0, 0, -days)
	issues, err := s.source.ResolvedIssuesSince(ctx, since, 50)
	if err != nil {
		return nil, err
	}
	if len(issues) == 0 {
		return &ExtractCandidatesResult{Items: []ExtractedItem{}, Total: 0}, nil
	}

	input := make([]ExtractIssueInput, 0, len(issues))
	issueProjects := make(map[string]string, len(issues))
	for _, issue := range issues {
		input = append(input, ExtractIssueInput{
			ID:          issue.ID,
			ProjectID:   issue.ProjectID,
			Title:       issue.Title,
			Description: issue.Description,
			Comments:    issue.Comments,
			RunID:       stringPtrValue(issue.RunID),
		})
		issueProjects[issue.ID] = issue.ProjectID
	}
	resp, err := s.extractor.Extract(ctx, ExtractLLMRequest{Issues: input})
	if err != nil {
		return nil, err
	}
	if err := validateExtractResponse(resp, issueProjects); err != nil {
		return nil, err
	}

	items := make([]ExtractedItem, 0, len(resp.Entries))
	for _, entry := range resp.Entries {
		projectID := issueProjects[entry.IssueID]
		tags := normalizeTags(append(entry.Tags, aiExtractedTag))
		exp, err := s.repo.Create(userID, CreateExperienceRequest{
			ProjectID:   &projectID,
			Title:       entry.Title,
			Content:     entry.Content,
			Tags:        tags,
			AiGenerated: true,
		})
		if err != nil {
			return nil, err
		}
		items = append(items, ExtractedItem{Experience: *exp, IssueID: entry.IssueID, Confidence: entry.Confidence})
	}
	return &ExtractCandidatesResult{Items: items, Total: len(items)}, nil
}

// validateExtractResponse 纵深校验 LLM 提取输出（对齐 py-agent validate_experience_candidates，
// 防越界内容落库）：status=ok、0-10 条、issue_id 必须在输入集内、字段长度/置信度边界。
func validateExtractResponse(resp *ExtractLLMResponse, issueProjects map[string]string) error {
	if resp == nil || resp.Status != "ok" {
		return ErrInvalidLLMOutput
	}
	if len(resp.Entries) > 10 {
		return ErrInvalidLLMOutput
	}
	for _, entry := range resp.Entries {
		title := strings.TrimSpace(entry.Title)
		content := strings.TrimSpace(entry.Content)
		if _, ok := issueProjects[entry.IssueID]; !ok {
			return ErrInvalidLLMOutput
		}
		if title == "" || len([]rune(title)) > 256 || content == "" || len([]rune(content)) > 2000 {
			return ErrInvalidLLMOutput
		}
		if entry.Confidence < 0 || entry.Confidence > 1 {
			return ErrInvalidLLMOutput
		}
		if len(entry.Tags) > 10 {
			return ErrInvalidLLMOutput
		}
		for _, tag := range entry.Tags {
			if tag = strings.TrimSpace(tag); tag == "" || len([]rune(tag)) > 32 {
				return ErrInvalidLLMOutput
			}
		}
	}
	return nil
}

func (s *Service) canRead(exp Experience, userID, userRole string) bool {
	if userRole == auth.RoleAdmin {
		return true
	}
	if exp.Status == StatusCandidate {
		if exp.AuthorID == userID {
			return true
		}
		if exp.ProjectID == nil {
			return false
		}
		return s.canAccess(*exp.ProjectID, userID, userRole, projects.RoleMaintainer)
	}
	if exp.ProjectID == nil {
		return true
	}
	return s.canAccess(*exp.ProjectID, userID, userRole, projects.RoleViewer)
}

func (s *Service) canUpdate(exp Experience, userID, userRole string) bool {
	if userRole == auth.RoleAdmin || exp.AuthorID == userID {
		return true
	}
	if exp.ProjectID == nil {
		return false
	}
	return s.canAccess(*exp.ProjectID, userID, userRole, projects.RoleMaintainer)
}

func (s *Service) canAccess(projectID, userID, userRole, minRole string) bool {
	ok, err := s.access.CanAccessProject(projectID, userID, userRole, minRole)
	return err == nil && ok
}

func (s *Service) requireProject(projectID string) error {
	exists, err := s.access.ProjectExists(projectID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrProjectNotFound
	}
	return nil
}

func (s *Service) normalizeLinks(in []ExperienceProjectLink) ([]ExperienceProjectLink, error) {
	seen := map[string]bool{}
	out := make([]ExperienceProjectLink, 0, len(in))
	for _, link := range in {
		projectID := strings.TrimSpace(link.ProjectID)
		if projectID == "" || seen[projectID] {
			continue
		}
		if err := s.requireProject(projectID); err != nil {
			return nil, err
		}
		relation := strings.TrimSpace(link.Relation)
		if relation == "" {
			relation = RelationApplicable
		}
		if !validRelation(relation) {
			return nil, ErrInvalidInput
		}
		seen[projectID] = true
		out = append(out, ExperienceProjectLink{ProjectID: projectID, Relation: relation})
	}
	return out, nil
}

func normalizeTags(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, tag := range in {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

func validStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case StatusCandidate, StatusPublished, StatusArchived:
		return true
	default:
		return false
	}
}

func validRelation(relation string) bool {
	switch relation {
	case RelationPrimary, RelationApplicable, RelationDerivedFrom:
		return true
	default:
		return false
	}
}

func canReviewProjectRole(role string) bool {
	switch role {
	case "maintainer", "owner":
		return true
	default:
		return false
	}
}

type ProjectAccessAdapter struct {
	Repo interface {
		GetByID(id string) (*projects.Project, error)
		GetMember(projectID, userID string) (*projects.ProjectMember, error)
	}
}

func (a ProjectAccessAdapter) ProjectExists(projectID string) (bool, error) {
	project, err := a.Repo.GetByID(projectID)
	return project != nil, err
}

func (a ProjectAccessAdapter) CanAccessProject(projectID, userID, userRole, minRole string) (bool, error) {
	if userRole == auth.RoleAdmin {
		return true, nil
	}
	member, err := a.Repo.GetMember(projectID, userID)
	if err != nil {
		return false, err
	}
	roleRank := map[string]int{"viewer": 1, "member": 2, "maintainer": 3, "owner": 4}
	return member != nil && member.Status == projects.MemberStatusActive && roleRank[member.Role] >= roleRank[minRole], nil
}

func (a ProjectAccessAdapter) ProjectRole(projectID, userID, userRole string) (string, error) {
	if userRole == auth.RoleAdmin {
		return projects.RoleOwner, nil
	}
	member, err := a.Repo.GetMember(projectID, userID)
	if err != nil || member == nil || member.Status != projects.MemberStatusActive {
		return "", err
	}
	return member.Role, nil
}
