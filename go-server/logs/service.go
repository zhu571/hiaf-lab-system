package logs

import (
	"errors"
	"strings"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
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

	ErrLogNotFound      = errors.New("工作记录不存在")
	ErrProjectNotFound  = errors.New("项目不存在")
	ErrForbidden        = errors.New("当前用户无权访问该项目")
	ErrInvalidInput     = errors.New("请求参数无效")
	ErrReportWarning    = errors.New("日报存在警告")
	ErrInvalidTimeZone  = errors.New("时区配置无效")
	ErrLogOwnerMismatch = errors.New("只能修改自己的工作记录")
)

type ProjectAccessChecker interface {
	ProjectExists(projectID string) (bool, error)
	CanAccessProject(projectID, userID, userRole, minRole string) (bool, error)
}

type logRepository interface {
	GetOrCreateTodayReport(authorID, reportDate string) (*DailyReport, error)
	GetReportByID(id string) (*DailyReport, error)
	GetReportByDate(authorID, reportDate string) (*DailyReport, error)
	ListReports(authorID string, page, perPage int) ([]DailyReport, int, error)
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
}

func NewService(repo logRepository, timezone string, access ProjectAccessChecker) *Service {
	return &Service{repo: repo, timezone: timezone, access: access}
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

func (s *Service) GetReportByDate(userID, reportDate string) (*DailyReport, error) {
	if _, err := time.Parse(time.DateOnly, reportDate); err != nil {
		return nil, ErrInvalidInput
	}
	report, err := s.repo.GetReportByDate(userID, reportDate)
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, ErrReportNotFound
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

func (s *Service) ListReports(userID string, page, perPage int) ([]DailyReport, int, error) {
	if perPage > 100 {
		perPage = 100
	}
	return s.repo.ListReports(userID, page, perPage)
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
	ok, err := s.access.CanAccessProject(projectID, userID, userRole, projects.RoleMember)
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

	item, err := s.repo.CreateLog(projectID, userID, req, occurredAt)
	if err != nil {
		return nil, err
	}
	if req.DailyReportID != nil && strings.TrimSpace(*req.DailyReportID) != "" {
		report, err := s.repo.GetReportByID(strings.TrimSpace(*req.DailyReportID))
		if err != nil {
			return nil, err
		}
		if report == nil {
			return nil, ErrReportNotFound
		}
		if report.AuthorID != userID {
			return nil, ErrNotReportOwner
		}
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
		ok, err := s.access.CanAccessProject(item.ProjectID, userID, userRole, projects.RoleViewer)
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
	ok, err := s.access.CanAccessProject(projectID, userID, userRole, projects.RoleViewer)
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
	ok, err := s.access.CanAccessProject(item.ProjectID, userID, userRole, projects.RoleViewer)
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
	if item.AuthorID != userID {
		return nil, ErrLogOwnerMismatch
	}
	if item.ContentStatus != LogStatusDraft {
		return nil, ErrLogNotDraft
	}
	ok, err := s.access.CanAccessProject(item.ProjectID, userID, userRole, projects.RoleMember)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrForbidden
	}
	if req.Category != nil && !validCategory(*req.Category) {
		return nil, ErrInvalidInput
	}
	if req.Content != nil && strings.TrimSpace(*req.Content) == "" {
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
	return member != nil && member.Status == projects.MemberStatusActive && middleware.ProjectRoleRank(member.Role) >= middleware.ProjectRoleRank(minRole), nil
}
