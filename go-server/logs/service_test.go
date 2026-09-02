package logs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

func TestSubmitReportHardBlocks(t *testing.T) {
	tests := []struct {
		name   string
		report DailyReport
		logs   []Log
		access bool
		want   error
	}{
		{
			name:   "non owner",
			report: testReport("usr_other", ReportStatusDraft, "worked"),
			logs:   []Log{testLog("log_1", "prj_1", LogStatusConfirmed, "work", "2026-07-14T10:00:00+08:00")},
			access: true,
			want:   ErrNotReportOwner,
		},
		{
			name:   "not draft",
			report: testReport("usr_1", ReportStatusSubmitted, "worked"),
			logs:   []Log{testLog("log_1", "prj_1", LogStatusConfirmed, "work", "2026-07-14T10:00:00+08:00")},
			access: true,
			want:   ErrAlreadySubmitted,
		},
		{
			name:   "empty raw text",
			report: testReport("usr_1", ReportStatusDraft, " "),
			logs:   []Log{testLog("log_1", "prj_1", LogStatusConfirmed, "work", "2026-07-14T10:00:00+08:00")},
			access: true,
			want:   ErrEmptyRawText,
		},
		{
			name:   "no linked logs",
			report: testReport("usr_1", ReportStatusDraft, "worked"),
			access: true,
			want:   ErrNoLogEntries,
		},
		{
			name:   "missing project id",
			report: testReport("usr_1", ReportStatusDraft, "worked"),
			logs:   []Log{testLog("log_1", "", LogStatusConfirmed, "work", "2026-07-14T10:00:00+08:00")},
			access: true,
			want:   ErrLogProjectMissing,
		},
		{
			name:   "voided log",
			report: testReport("usr_1", ReportStatusDraft, "worked"),
			logs:   []Log{testLog("log_1", "prj_1", LogStatusVoided, "work", "2026-07-14T10:00:00+08:00")},
			access: true,
			want:   ErrLogVoided,
		},
		{
			name:   "no project access",
			report: testReport("usr_1", ReportStatusDraft, "worked"),
			logs:   []Log{testLog("log_1", "prj_1", LogStatusConfirmed, "work", "2026-07-14T10:00:00+08:00")},
			access: false,
			want:   ErrForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := testService(tt.report, tt.logs, tt.access)
			_, err := svc.SubmitReport(tt.report.ID, "usr_1", projects.RoleMember, false)
			if !errors.Is(err, tt.want) {
				t.Fatalf("SubmitReport error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestGetReportByIDUsesOwnerBoundary(t *testing.T) {
	svc := testService(testReport("usr_1", ReportStatusSubmitted, "worked"), nil, true)
	if _, err := svc.GetReportByID("report_1", "usr_2", projects.RoleMember); !errors.Is(err, ErrNotReportOwner) {
		t.Fatalf("GetReportByID error = %v, want %v", err, ErrNotReportOwner)
	}
}

func TestListReportsRejectsInvertedDateRange(t *testing.T) {
	svc := testService(testReport("usr_1", ReportStatusDraft, "worked"), nil, true)
	_, _, err := svc.ListReports(ReportListParams{AuthorID: "usr_1", DateFrom: "2026-08-02", DateTo: "2026-08-01"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListReports error = %v, want %v", err, ErrInvalidInput)
	}
}

func TestSubmitReportWarningsBlockWithoutForce(t *testing.T) {
	report := testReport("usr_1", ReportStatusDraft, "raw text has no log content")
	item := testLog("log_1", "prj_1", LogStatusDraft, "actual log content", "2026-07-13T10:00:00+08:00")
	svc := testService(report, []Log{item}, true)

	result, err := svc.SubmitReport(report.ID, "usr_1", projects.RoleMember, false)
	if err != nil {
		t.Fatalf("SubmitReport returned error: %v", err)
	}
	if !result.Blocked {
		t.Fatal("SubmitReport should block on warnings without force")
	}
	wantCodes := map[string]bool{
		"log_still_draft":               false,
		"date_mismatch":                 false,
		"raw_text_without_matching_log": false,
		"summary_empty":                 false,
	}
	for _, warning := range result.Warnings {
		if _, ok := wantCodes[warning.Code]; ok {
			wantCodes[warning.Code] = true
		}
	}
	for code, seen := range wantCodes {
		if !seen {
			t.Fatalf("missing warning %q in %#v", code, result.Warnings)
		}
	}
}

func TestSubmitReportForceSubmitsWithWarnings(t *testing.T) {
	report := testReport("usr_1", ReportStatusDraft, "raw text has no log content")
	item := testLog("log_1", "prj_1", LogStatusDraft, "actual log content", "2026-07-13T10:00:00+08:00")
	repo := newFakeRepo(report, []Log{item})
	svc := NewService(repo, "Asia/Shanghai", fakeAccess{canAccess: true})

	result, err := svc.SubmitReport(report.ID, "usr_1", projects.RoleMember, true)
	if err != nil {
		t.Fatalf("SubmitReport returned error: %v", err)
	}
	if result.Blocked {
		t.Fatal("SubmitReport should not block when force=true")
	}
	if result.Report.ContentStatus != ReportStatusSubmitted {
		t.Fatalf("status = %q, want %q", result.Report.ContentStatus, ReportStatusSubmitted)
	}
	if result.Report.QualityStatus != QualityWarnings {
		t.Fatalf("quality = %q, want %q", result.Report.QualityStatus, QualityWarnings)
	}
	if repo.reports[report.ID].ContentStatus != ReportStatusSubmitted {
		t.Fatal("repository report was not submitted")
	}
}

func TestUpdateLogRequiresCurrentProjectAccess(t *testing.T) {
	report := testReport("usr_1", ReportStatusDraft, "worked")
	item := testLog("log_1", "prj_1", LogStatusDraft, "old", "2026-07-14T10:00:00+08:00")
	svc := testService(report, []Log{item}, false)

	content := "new content"
	_, err := svc.UpdateLog(item.ID, "usr_1", projects.RoleMember, UpdateLogRequest{Content: &content})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("UpdateLog error = %v, want %v", err, ErrForbidden)
	}
}

func TestListMineAddsProjectProjection(t *testing.T) {
	repo := newFakeRepo(DailyReport{ID: "r1"}, []Log{testLog("l1", "prj_1", LogStatusConfirmed, "mine", "2026-09-01T00:00:00Z")})
	svc := NewService(repo, "Asia/Shanghai", fakeAccess{projects: []middleware.ProjectSummary{{ID: "prj_1", Code: "P1", Name: "Project One"}}})
	result, err := svc.ListMine("usr_1", LogListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].ProjectCode != "P1" || result.Items[0].ProjectName != "Project One" {
		t.Fatalf("unexpected mine projection: %+v", result.Items)
	}
}

func TestTeamReportRequiresPermissionAndReturnsProjection(t *testing.T) {
	repo := newFakeRepo(DailyReport{ID: "r1"}, nil)
	repo.team = []TeamDailyReportProjection{{ID: "r1", ProjectLogCount: 2}}
	denied := NewService(repo, "Asia/Shanghai", fakeAccess{})
	if _, err := denied.ListTeamReports("prj_1", "usr_1", TeamReportListParams{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("denied err=%v", err)
	}
	allowed := NewService(repo, "Asia/Shanghai", fakeAccess{canAccess: true})
	result, err := allowed.ListTeamReports("prj_1", "usr_1", TeamReportListParams{})
	if err != nil || len(result.Items) != 1 || result.Items[0].ProjectLogCount != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestUpdateLogConfirmsDraft(t *testing.T) {
	report := testReport("usr_1", ReportStatusDraft, "worked")
	item := testLog("log_1", "prj_1", LogStatusDraft, "old", "2026-07-14T10:00:00+08:00")
	svc := testService(report, []Log{item}, true)
	confirmed := LogStatusConfirmed

	updated, err := svc.UpdateLog(item.ID, "usr_1", projects.RoleMember, UpdateLogRequest{ContentStatus: &confirmed})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ContentStatus != LogStatusConfirmed {
		t.Fatalf("status = %q, want %q", updated.ContentStatus, LogStatusConfirmed)
	}
}

func testService(report DailyReport, logs []Log, canAccess bool) *Service {
	return NewService(newFakeRepo(report, logs), "Asia/Shanghai", fakeAccess{canAccess: canAccess})
}

func testReport(authorID, status, rawText string) DailyReport {
	return DailyReport{
		ID:            "report_1",
		ReportDate:    "2026-07-14",
		AuthorID:      authorID,
		RawText:       rawText,
		ContentStatus: status,
		QualityStatus: QualityUnchecked,
	}
}

func testLog(id, projectID, status, content, occurredAt string) Log {
	parsed, err := time.Parse(time.RFC3339, occurredAt)
	if err != nil {
		panic(err)
	}
	return Log{
		ID:            id,
		ProjectID:     projectID,
		AuthorID:      "usr_1",
		OccurredAt:    parsed,
		Category:      CategoryGeneral,
		Content:       content,
		Source:        SourceManual,
		ContentStatus: status,
	}
}

type fakeAccess struct {
	canAccess bool
	projects  []middleware.ProjectSummary
}

func (f fakeAccess) ProjectExists(projectID string) (bool, error) {
	return true, nil
}

func (f fakeAccess) ProjectStatus(projectID string) (string, error) {
	return projects.StatusActive, nil
}

func (f fakeAccess) HasProjectPermission(projectID, userID string, perm middleware.Permission) (bool, error) {
	return f.canAccess, nil
}

func (f fakeAccess) ListProjectsWithPermission(userID string, perm middleware.Permission) ([]middleware.ProjectSummary, error) {
	return f.projects, nil
}

func (f fakeAccess) ListAllProjectsWithPermission(userID string, perm middleware.Permission) ([]middleware.ProjectSummary, error) {
	return f.ListProjectsWithPermission(userID, perm)
}

func (f fakeAccess) IsActiveProjectMember(projectID, userID string) (bool, error) {
	return f.canAccess, nil
}

type fakeRepo struct {
	reports map[string]*DailyReport
	logs    map[string]*Log
	links   map[string][]string
	team    []TeamDailyReportProjection
}

func newFakeRepo(report DailyReport, items []Log) *fakeRepo {
	reports := map[string]*DailyReport{report.ID: cloneReport(report)}
	logs := make(map[string]*Log, len(items))
	links := map[string][]string{report.ID: make([]string, 0, len(items))}
	for _, item := range items {
		logs[item.ID] = cloneLog(item)
		links[report.ID] = append(links[report.ID], item.ID)
	}
	return &fakeRepo{reports: reports, logs: logs, links: links}
}

func (f *fakeRepo) GetOrCreateTodayReport(authorID, reportDate string) (*DailyReport, error) {
	for _, report := range f.reports {
		if report.AuthorID == authorID && report.ReportDate == reportDate {
			return cloneReport(*report), nil
		}
	}
	report := DailyReport{ID: "report_today", ReportDate: reportDate, AuthorID: authorID, ContentStatus: ReportStatusDraft, QualityStatus: QualityUnchecked}
	f.reports[report.ID] = cloneReport(report)
	return cloneReport(report), nil
}

func (f *fakeRepo) GetReportByID(id string) (*DailyReport, error) {
	report := f.reports[id]
	if report == nil {
		return nil, nil
	}
	return cloneReport(*report), nil
}

func (f *fakeRepo) GetReportsByLog(logID string) ([]DailyReport, error) {
	out := []DailyReport{}
	for reportID, logIDs := range f.links {
		for _, id := range logIDs {
			if id == logID && f.reports[reportID] != nil {
				out = append(out, *cloneReport(*f.reports[reportID]))
			}
		}
	}
	return out, nil
}

func (f *fakeRepo) ListTeamReports(projectID string, params TeamReportListParams) ([]TeamDailyReportProjection, int, error) {
	return f.team, len(f.team), nil
}

func (f *fakeRepo) GetTeamReport(projectID, reportID string) (*TeamDailyReportProjection, error) {
	for i := range f.team {
		if f.team[i].ID == reportID {
			return &f.team[i], nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) GetReportByDate(authorID, reportDate string) (*DailyReport, error) {
	for _, report := range f.reports {
		if report.AuthorID == authorID && report.ReportDate == reportDate {
			return cloneReport(*report), nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) GetLatestReportBefore(authorID, beforeDate string) (*DailyReport, error) {
	var latest *DailyReport
	for _, report := range f.reports {
		if report.AuthorID != authorID || report.ReportDate > beforeDate {
			continue
		}
		if strings.TrimSpace(report.RawText) == "" && strings.TrimSpace(report.Summary) == "" {
			continue
		}
		if latest == nil || report.ReportDate > latest.ReportDate {
			latest = report
		}
	}
	if latest == nil {
		return nil, nil
	}
	return cloneReport(*latest), nil
}

func (f *fakeRepo) ListReports(params ReportListParams) ([]DailyReport, int, error) {
	var out []DailyReport
	for _, report := range f.reports {
		if params.AuthorID != "" && report.AuthorID != params.AuthorID {
			continue
		}
		if params.Status != "" && report.ContentStatus != params.Status {
			continue
		}
		if params.Keyword != "" && !strings.Contains(report.Summary, params.Keyword) && !strings.Contains(report.RawText, params.Keyword) {
			continue
		}
		if params.Date != "" && report.ReportDate != params.Date {
			continue
		}
		if params.DateFrom != "" && report.ReportDate < params.DateFrom {
			continue
		}
		if params.DateTo != "" && report.ReportDate > params.DateTo {
			continue
		}
		out = append(out, *cloneReport(*report))
	}
	return out, len(out), nil
}

func (f *fakeRepo) UpdateReport(id, rawText string) error {
	report := f.reports[id]
	if report == nil || report.ContentStatus != ReportStatusDraft {
		return errors.New("not found")
	}
	report.RawText = rawText
	return nil
}

func (f *fakeRepo) UpdateReportFields(id string, rawText, summary *string) error {
	report := f.reports[id]
	if report == nil || report.ContentStatus != ReportStatusDraft {
		return errors.New("not found")
	}
	if rawText != nil {
		report.RawText = *rawText
	}
	if summary != nil {
		report.Summary = *summary
	}
	return nil
}

func (f *fakeRepo) SubmitReport(id, qualityStatus string) (*DailyReport, error) {
	report := f.reports[id]
	if report == nil || report.ContentStatus != ReportStatusDraft {
		return nil, nil
	}
	report.ContentStatus = ReportStatusSubmitted
	report.QualityStatus = qualityStatus
	return cloneReport(*report), nil
}

func (f *fakeRepo) CreateLog(projectID, authorID string, req CreateLogRequest, occurredAt time.Time) (*Log, error) {
	item := Log{ID: "log_new", ProjectID: projectID, AuthorID: authorID, OccurredAt: occurredAt, Category: CategoryGeneral, Content: req.Content, RawSnippet: req.RawSnippet, Source: SourceManual, ContentStatus: LogStatusDraft}
	f.logs[item.ID] = cloneLog(item)
	return cloneLog(item), nil
}

func (f *fakeRepo) GetByID(id string) (*Log, error) {
	item := f.logs[id]
	if item == nil {
		return nil, nil
	}
	return cloneLog(*item), nil
}

func (f *fakeRepo) List(projectID string, params LogListParams) ([]Log, int, error) {
	var out []Log
	for _, item := range f.logs {
		if item.ProjectID == projectID && (params.Keyword == "" || strings.Contains(item.Content, params.Keyword)) {
			out = append(out, *cloneLog(*item))
		}
	}
	return out, len(out), nil
}

func (f *fakeRepo) ListMine(authorID string, projectIDs []string, params LogListParams) ([]Log, int, error) {
	return f.List("prj_1", params)
}

func (f *fakeRepo) UpdateLog(id string, req UpdateLogRequest, occurredAt *time.Time) (*Log, error) {
	item := f.logs[id]
	if item == nil || item.ContentStatus != LogStatusDraft {
		return nil, nil
	}
	if req.Content != nil {
		item.Content = *req.Content
	}
	if req.Category != nil {
		item.Category = *req.Category
	}
	if occurredAt != nil {
		item.OccurredAt = *occurredAt
	}
	if req.ContentStatus != nil {
		item.ContentStatus = *req.ContentStatus
	}
	return cloneLog(*item), nil
}

func (f *fakeRepo) LinkLogToReport(reportID, logID string) error {
	f.links[reportID] = append(f.links[reportID], logID)
	return nil
}

func (f *fakeRepo) GetLogsByReport(reportID string) ([]Log, error) {
	var out []Log
	for _, logID := range f.links[reportID] {
		if item := f.logs[logID]; item != nil {
			out = append(out, *cloneLog(*item))
		}
	}
	return out, nil
}

func cloneReport(report DailyReport) *DailyReport {
	copied := report
	copied.Logs = append([]Log(nil), report.Logs...)
	return &copied
}

func cloneLog(item Log) *Log {
	copied := item
	return &copied
}
