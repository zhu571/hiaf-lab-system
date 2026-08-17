package logs

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
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

var (
	ErrReportNotFound    = errors.New("日报不存在")
	ErrNotReportOwner    = errors.New("只能操作自己的日报")
	ErrAlreadySubmitted  = errors.New("日报已提交")
	ErrEmptyRawText      = errors.New("日报不能为空")
	ErrNoLogEntries      = errors.New("至少需要一条工作记录")
	ErrLogProjectMissing = errors.New("工作记录的所属项目不能为空")
	ErrLogVoided         = errors.New("已废弃的记录不能提交")
	ErrLogNotDraft       = errors.New("只能修改草稿状态下的记录")
	ErrPerPageTooLarge   = errors.New("每页最大100条")

	ErrLogNotFound             = errors.New("工作记录不存在")
	ErrProjectNotFound         = errors.New("项目不存在")
	ErrForbidden               = errors.New("当前用户无权访问该项目")
	ErrInvalidInput            = errors.New("请求参数无效")
	ErrReportWarning           = errors.New("日报存在警告")
	ErrInvalidTimeZone         = errors.New("时区配置无效")
	ErrLogOwnerMismatch        = errors.New("只能修改自己的工作记录")
	ErrProjectLifecycleBlocked = errors.New("项目当前状态不允许修改工作记录")

	// ErrUpstream 复制自 steptemplates 包同名错误的用法模式（见 steptemplates/service.go）。
	// 有意不提取公共包：logs 与 steptemplates 是平行业务模块，互相 import 私有运行时
	// 实现会造成耦合；若第三个模块出现同需求，再提取到 common。
	ErrUpstream      = errors.New("py-agent 上游服务错误")
	ErrRateLimited   = errors.New("操作过于频繁，请稍后再试")
	ErrAiParseFailed = errors.New("解析失败，请修改描述后重试")
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	ProjectStatus(projectID string) (string, error)
	HasProjectPermission(projectID, userID string, perm middleware.Permission) (bool, error)
	ListProjectsWithPermission(userID string, perm middleware.Permission) ([]middleware.ProjectSummary, error)
}

type logRepository interface {
	GetOrCreateTodayReport(authorID, reportDate string) (*DailyReport, error)
	GetReportByID(id string) (*DailyReport, error)
	GetReportByDate(authorID, reportDate string) (*DailyReport, error)
	GetLatestReportBefore(authorID, beforeDate string) (*DailyReport, error)
	ListReports(params ReportListParams) ([]DailyReport, int, error)
	UpdateReport(id, rawText string) error
	SubmitReport(id, qualityStatus string) (*DailyReport, error)
	CreateLog(projectID, authorID string, req CreateLogRequest, occurredAt time.Time) (*Log, error)
	GetByID(id string) (*Log, error)
	List(projectID string, params LogListParams) ([]Log, int, error)
	UpdateLog(id string, req UpdateLogRequest, occurredAt *time.Time) (*Log, error)
	LinkLogToReport(reportID, logID string) error
	GetLogsByReport(reportID string) ([]Log, error)
}

type Service struct {
	repo     logRepository
	timezone string
	access   ProjectAccessChecker

	client      *http.Client
	parserURL   string
	parserToken string
	rlMu        sync.Mutex
	rlCalls     map[string][]time.Time
}

func NewService(repo logRepository, timezone string, access ProjectAccessChecker) *Service {
	return &Service{
		repo:     repo,
		timezone: timezone,
		access:   access,
		client:   &http.Client{Timeout: 60 * time.Second},
		rlCalls:  map[string][]time.Time{},
	}
}

func (s *Service) ConfigureParser(url, token string) {
	s.parserURL = strings.TrimRight(url, "/")
	s.parserToken = token
}

// AutoConfigure 复用 steptemplates 的同名模式：从 PY_AGENT_INTERPRET_URL 与
// PY_AGENT_INTERNAL_TOKEN_FILE 读取 py-agent 上游配置。
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
		s.parserURL = url
		s.parserToken = token
	}
	return nil
}

func (s *Service) GetOrCreateTodayReport(userID string) (*DailyReport, error) {
	loc, err := time.LoadLocation(defaultString(s.timezone, "Asia/Shanghai"))
	if err != nil {
		return nil, ErrInvalidTimeZone
	}
	report, err := s.repo.GetOrCreateTodayReport(userID, time.Now().In(loc).Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	return s.withReportLogs(report)
}

// GetReportByDateLatest 取指定日期（空则默认今天）的日报；latest=true 时向前回溯取最近一份
// 非空日报（跨周末，周一取周五），零日报用户返回 ErrReportNotFound。
func (s *Service) GetReportByDateLatest(userID, reportDate string, latest bool) (*DailyReport, error) {
	loc, err := time.LoadLocation(defaultString(s.timezone, "Asia/Shanghai"))
	if err != nil {
		return nil, ErrInvalidTimeZone
	}
	if strings.TrimSpace(reportDate) == "" {
		reportDate = time.Now().In(loc).Format(time.DateOnly)
	}
	if _, err := time.Parse(time.DateOnly, reportDate); err != nil {
		return nil, ErrInvalidInput
	}
	var report *DailyReport
	if latest {
		report, err = s.repo.GetLatestReportBefore(userID, reportDate)
	} else {
		report, err = s.repo.GetReportByDate(userID, reportDate)
	}
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, ErrReportNotFound
	}
	return s.withReportLogs(report)
}

func (s *Service) GetReportByID(id, userID, userRole string) (*DailyReport, error) {
	report, err := s.repo.GetReportByID(id)
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, ErrReportNotFound
	}
	if report.AuthorID != userID && userRole != "admin" {
		return nil, ErrNotReportOwner
	}
	return s.withReportLogs(report)
}

func (s *Service) UpdateReportRawText(id, userID, rawText string) (*DailyReport, error) {
	report, err := s.repo.GetReportByID(id)
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, ErrReportNotFound
	}
	if report.AuthorID != userID {
		return nil, ErrNotReportOwner
	}
	if report.ContentStatus != ReportStatusDraft {
		return nil, ErrAlreadySubmitted
	}
	if err := s.repo.UpdateReport(id, rawText); err != nil {
		return nil, err
	}
	updated, err := s.repo.GetReportByID(id)
	if err != nil {
		return nil, err
	}
	return s.withReportLogs(updated)
}

func (s *Service) ListReports(params ReportListParams) ([]DailyReport, int, error) {
	params.AuthorID = strings.TrimSpace(params.AuthorID)
	params.Status = strings.TrimSpace(params.Status)
	params.Keyword = strings.TrimSpace(params.Keyword)
	params.Date = strings.TrimSpace(params.Date)
	if params.PerPage > 100 {
		params.PerPage = 100
	}
	if !validOptionalReportStatus(params.Status) {
		return nil, 0, ErrInvalidInput
	}
	if params.Date != "" {
		if _, err := time.Parse(time.DateOnly, params.Date); err != nil {
			return nil, 0, ErrInvalidInput
		}
	}
	return s.repo.ListReports(params)
}

func (s *Service) CreateLog(projectID, userID, userRole string, req CreateLogRequest) (*Log, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(req.Content) == "" {
		return nil, ErrInvalidInput
	}
	if !validCategory(defaultString(req.Category, CategoryGeneral)) || !validSource(defaultString(req.Source, SourceManual)) {
		return nil, ErrInvalidInput
	}
	exists, err := s.access.ProjectExists(projectID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrProjectNotFound
	}
	ok, err := s.access.HasProjectPermission(projectID, userID, middleware.PermCreateLog)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}

	occurredAt := time.Now()
	if req.OccurredAt != nil && strings.TrimSpace(*req.OccurredAt) != "" {
		occurredAt, err = time.Parse(time.RFC3339, strings.TrimSpace(*req.OccurredAt))
		if err != nil {
			return nil, ErrInvalidInput
		}
	}

	var report *DailyReport
	if req.DailyReportID != nil && strings.TrimSpace(*req.DailyReportID) != "" {
		report, err = s.repo.GetReportByID(strings.TrimSpace(*req.DailyReportID))
		if err != nil {
			return nil, err
		}
		if report == nil {
			return nil, ErrReportNotFound
		}
		if report.AuthorID != userID {
			return nil, ErrNotReportOwner
		}
	}

	item, err := s.repo.CreateLog(projectID, userID, req, occurredAt)
	if err != nil {
		return nil, err
	}
	if report != nil {
		if err := s.repo.LinkLogToReport(report.ID, item.ID); err != nil {
			return nil, err
		}
	}
	return item, nil
}

func (s *Service) SubmitReport(id, userID, userRole string, force bool) (*SubmitResult, error) {
	report, err := s.repo.GetReportByID(id)
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, ErrReportNotFound
	}
	if report.AuthorID != userID {
		return nil, ErrNotReportOwner
	}
	if report.ContentStatus != ReportStatusDraft {
		return nil, ErrAlreadySubmitted
	}
	if strings.TrimSpace(report.RawText) == "" {
		return nil, ErrEmptyRawText
	}

	items, err := s.repo.GetLogsByReport(report.ID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNoLogEntries
	}
	for _, item := range items {
		if strings.TrimSpace(item.ProjectID) == "" {
			return nil, ErrLogProjectMissing
		}
		if item.ContentStatus == LogStatusVoided {
			return nil, ErrLogVoided
		}
		ok, err := s.access.HasProjectPermission(item.ProjectID, userID, middleware.PermRead)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrForbidden
		}
	}

	warnings := submitWarnings(*report, items)
	if len(warnings) > 0 && !force {
		report.Logs = items
		return &SubmitResult{Report: *report, Warnings: warnings, Blocked: true}, nil
	}

	qualityStatus := QualityPassed
	if len(warnings) > 0 {
		qualityStatus = QualityWarnings
	}
	updated, err := s.repo.SubmitReport(report.ID, qualityStatus)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrAlreadySubmitted
	}
	updated.Logs = items
	return &SubmitResult{Report: *updated, Warnings: warnings, Blocked: false}, nil
}

func (s *Service) ListLogs(projectID, userID, userRole string, params LogListParams) (*LogListResult, error) {
	if params.PerPage > 100 {
		params.PerPage = 100
	}
	if !validOptionalStatus(params.Status) || !validOptionalCategory(params.Category) {
		return nil, ErrInvalidInput
	}
	if err := validateOptionalRFC3339(params.DateFrom); err != nil {
		return nil, err
	}
	if err := validateOptionalRFC3339(params.DateTo); err != nil {
		return nil, err
	}
	ok, err := s.access.HasProjectPermission(projectID, userID, middleware.PermRead)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	items, total, err := s.repo.List(projectID, params)
	if err != nil {
		return nil, err
	}
	page := params.Page
	if page < 1 {
		page = 1
	}
	return &LogListResult{Items: items, Total: total, Page: page}, nil
}

func (s *Service) GetLog(id, userID, userRole string) (*Log, error) {
	item, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrLogNotFound
	}
	ok, err := s.access.HasProjectPermission(item.ProjectID, userID, middleware.PermRead)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	return item, nil
}

func (s *Service) UpdateLog(id, userID, userRole string, req UpdateLogRequest) (*Log, error) {
	item, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrLogNotFound
	}
	if item.ContentStatus != LogStatusDraft {
		return nil, ErrLogNotDraft
	}
	status, err := s.access.ProjectStatus(item.ProjectID)
	if err != nil {
		return nil, err
	}
	if status != projects.StatusActive {
		return nil, ErrProjectLifecycleBlocked
	}
	canAny, err := s.access.HasProjectPermission(item.ProjectID, userID, middleware.PermUpdateAnyLog)
	if err != nil {
		return nil, err
	}
	canOwn, err := s.access.HasProjectPermission(item.ProjectID, userID, middleware.PermUpdateOwnLog)
	if err != nil {
		return nil, err
	}
	if !canAny && !canOwn {
		return nil, ErrForbidden
	}
	if !canAny && item.AuthorID != userID {
		return nil, ErrLogOwnerMismatch
	}
	if req.Category != nil && !validCategory(*req.Category) {
		return nil, ErrInvalidInput
	}
	if req.Content != nil && strings.TrimSpace(*req.Content) == "" {
		return nil, ErrInvalidInput
	}
	if req.ContentStatus != nil && *req.ContentStatus != LogStatusConfirmed {
		return nil, ErrInvalidInput
	}

	var occurredAt *time.Time
	if req.OccurredAt != nil && strings.TrimSpace(*req.OccurredAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.OccurredAt))
		if err != nil {
			return nil, ErrInvalidInput
		}
		occurredAt = &parsed
	}
	updated, err := s.repo.UpdateLog(id, req, occurredAt)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrLogNotDraft
	}
	return updated, nil
}

func (s *Service) withReportLogs(report *DailyReport) (*DailyReport, error) {
	if report == nil {
		return nil, nil
	}
	items, err := s.repo.GetLogsByReport(report.ID)
	if err != nil {
		return nil, err
	}
	report.Logs = items
	return report, nil
}

// AiParse 把日报 raw_text 转发给 py-agent 整理为结构化日志草稿（不落库）。
// projects 由服务端按当前用户 PermCreateLog 权限注入，不透传前端。
func (s *Service) AiParse(ctx context.Context, id, userID, userRole string) (*AiParseResult, error) {
	report, err := s.GetReportByID(id, userID, userRole)
	if err != nil {
		return nil, err
	}
	if report.ContentStatus != ReportStatusDraft {
		return nil, fmt.Errorf("%w，不可再整理", ErrAlreadySubmitted)
	}
	rawText := strings.TrimSpace(report.RawText)
	if rawText == "" {
		return nil, ErrEmptyRawText
	}
	if utf8.RuneCountInString(report.RawText) > 4000 {
		return nil, fmt.Errorf("%w: 日报内容过长（上限 4000 字符）", ErrInvalidInput)
	}

	allowed, err := s.access.ListProjectsWithPermission(userID, middleware.PermCreateLog)
	if err != nil {
		return nil, err
	}
	if !s.allowOne(userID) {
		return nil, ErrRateLimited
	}
	if s.parserURL == "" || s.parserToken == "" {
		return nil, fmt.Errorf("%w: AI 整理服务未配置", ErrUpstream)
	}

	payload, err := json.Marshal(map[string]any{
		"raw_text":    report.RawText,
		"projects":    allowed,
		"report_date": report.ReportDate,
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.parserURL+"/v1/daily-parse", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.parserToken)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: py-agent 请求失败: %w", ErrUpstream, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnprocessableEntity:
		// py-agent 422 = 模型输出未通过校验，归为用户可重试的解析失败。
		return nil, ErrAiParseFailed
	case http.StatusBadRequest:
		return nil, fmt.Errorf("%w: 请求参数错误", ErrInvalidInput)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%w: py-agent 返回 %d: %s", ErrUpstream, resp.StatusCode, string(body))
	}

	var result AiParseResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: 解码 AI 响应失败: %w", ErrUpstream, err)
	}
	if result.Status != "ok" && result.Status != "clarify" && result.Status != "rejected" {
		return nil, fmt.Errorf("%w: AI 返回无效状态: %s", ErrUpstream, result.Status)
	}
	if result.Status == "ok" {
		if err := validateAiParseLogs(result.Logs, allowed); err != nil {
			return nil, err
		}
	} else if result.Status == "clarify" && (result.Question == nil || strings.TrimSpace(*result.Question) == "") {
		return nil, ErrAiParseFailed
	} else if result.Status == "rejected" && (result.Reason == nil || strings.TrimSpace(*result.Reason) == "") {
		return nil, ErrAiParseFailed
	}
	return &result, nil
}

// validateAiParseLogs 对 py-agent 响应做二次校验（防御上游被绕过/配置错误，fail closed）。
func validateAiParseLogs(items []AiParseLogEntry, allowed []middleware.ProjectSummary) error {
	if len(items) < 1 || len(items) > 20 {
		return ErrAiParseFailed
	}
	allowedIDs := make(map[string]bool, len(allowed))
	for _, p := range allowed {
		allowedIDs[p.ID] = true
	}
	for _, item := range items {
		if !validCategory(item.Category) || !allowedIDs[item.ProjectID] {
			return ErrAiParseFailed
		}
		if n := utf8.RuneCountInString(strings.TrimSpace(item.Content)); n < 1 || n > 2000 {
			return ErrAiParseFailed
		}
		if _, err := time.Parse(time.RFC3339, strings.TrimSpace(item.OccurredAt)); err != nil {
			return ErrAiParseFailed
		}
	}
	return nil
}

// allowOne 复制自 steptemplates.Service.allowOne（每用户 10 次/分钟，内存计数）。
// 有意不提取公共包：logs 与 steptemplates 是平行业务模块，互相 import 私有运行时
// 实现会造成耦合；若第三个模块出现同需求，再提取到 common。
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

func submitWarnings(report DailyReport, items []Log) []SubmitWarning {
	var warnings []SubmitWarning
	for _, item := range items {
		if item.ContentStatus == LogStatusDraft {
			warnings = append(warnings, SubmitWarning{
				Code:    "log_still_draft",
				Message: "关联工作记录仍是草稿",
				LogID:   item.ID,
			})
		}
		if item.OccurredAt.Format(time.DateOnly) != report.ReportDate {
			warnings = append(warnings, SubmitWarning{
				Code:    "date_mismatch",
				Message: "工作记录日期与日报日期不一致",
				LogID:   item.ID,
			})
		}
	}
	if strings.TrimSpace(report.RawText) != "" && !rawTextHasMatchingLog(report.RawText, items) {
		warnings = append(warnings, SubmitWarning{
			Code:    "raw_text_without_matching_log",
			Message: "日报原文有内容未能匹配到对应工作记录",
		})
	}
	if strings.TrimSpace(report.Summary) == "" {
		warnings = append(warnings, SubmitWarning{
			Code:    "summary_empty",
			Message: "日报摘要为空",
		})
	}
	return warnings
}

func rawTextHasMatchingLog(rawText string, items []Log) bool {
	normalized := strings.ToLower(rawText)
	for _, item := range items {
		content := strings.TrimSpace(item.Content)
		if content != "" && strings.Contains(normalized, strings.ToLower(content)) {
			return true
		}
	}
	return false
}

func validateOptionalRFC3339(v string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(v)); err != nil {
		return ErrInvalidInput
	}
	return nil
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func validOptionalCategory(v string) bool {
	return strings.TrimSpace(v) == "" || validCategory(v)
}

func validCategory(v string) bool {
	switch strings.TrimSpace(v) {
	case CategoryGeneral, CategoryAssembly, CategoryTest, CategoryCryo, CategoryRF, CategoryVacuum, CategoryBeam, CategoryDataAnalysis:
		return true
	default:
		return false
	}
}

func validSource(v string) bool {
	switch strings.TrimSpace(v) {
	case SourceManual, SourceAgent, SourceImport, SourceWechat:
		return true
	default:
		return false
	}
}

func validOptionalStatus(v string) bool {
	switch strings.TrimSpace(v) {
	case "", LogStatusDraft, LogStatusConfirmed, LogStatusLocked, LogStatusVoided:
		return true
	default:
		return false
	}
}

func validOptionalReportStatus(v string) bool {
	switch strings.TrimSpace(v) {
	case "", ReportStatusDraft, ReportStatusSubmitted, ReportStatusConfirmed, ReportStatusLocked:
		return true
	default:
		return false
	}
}

type ProjectAccessAdapter struct {
	DB   *sql.DB
	Repo interface {
		GetByID(id string) (*projects.Project, error)
	}
}

func (a ProjectAccessAdapter) ProjectExists(projectID string) (bool, error) {
	project, err := a.Repo.GetByID(projectID)
	return project != nil, err
}

func (a ProjectAccessAdapter) ProjectStatus(projectID string) (string, error) {
	project, err := a.Repo.GetByID(projectID)
	if err != nil || project == nil {
		return "", err
	}
	return project.Status, nil
}

func (a ProjectAccessAdapter) HasProjectPermission(projectID, userID string, perm middleware.Permission) (bool, error) {
	return middleware.HasPermission(a.DB, projectID, userID, perm)
}

func (a ProjectAccessAdapter) ListProjectsWithPermission(userID string, perm middleware.Permission) ([]middleware.ProjectSummary, error) {
	return middleware.ListProjectsWithPermission(a.DB, userID, perm)
}
