package runs

import (
	"errors"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func TestTargetTransition(t *testing.T) {
	tests := []struct {
		status, action, want string
		started, ended       bool
	}{
		{StatusPlanned, "start", StatusActive, true, false},
		{StatusPlanned, "abort", StatusAborted, false, true},
		{StatusActive, "pause", StatusPaused, true, false},
		{StatusActive, "complete", StatusCompleted, true, true},
		{StatusActive, "abort", StatusAborted, true, true},
		{StatusPaused, "resume", StatusActive, true, false},
		{StatusPaused, "abort", StatusAborted, true, true},
	}
	for _, tt := range tests {
		got, started, ended, err := targetTransition(tt.status, tt.action)
		if err != nil || got != tt.want || started != tt.started || ended != tt.ended {
			t.Errorf("targetTransition(%q, %q) = %q, %v, %v, %v", tt.status, tt.action, got, started, ended, err)
		}
	}
	if _, _, _, err := targetTransition(StatusCompleted, "resume"); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("invalid transition error = %v", err)
	}
}

const (
	testRunProjectID = "11111111-1111-1111-1111-111111111111"
	testRunID        = "44444444-4444-4444-4444-444444444444"
	testRunStepID    = "55555555-5555-5555-5555-555555555555"
	testTemplateID   = "66666666-6666-6666-6666-666666666666"
)

type fakeRepo struct {
	run          *ExperimentRun
	reportLinks  []string
	steps        map[string]*RunStep
	maxOrder     int
	sourcedSteps map[string]string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		run:          &ExperimentRun{ID: testRunID, ProjectID: testRunProjectID, Status: StatusPlanned},
		steps:        map[string]*RunStep{},
		sourcedSteps: map[string]string{},
	}
}

func (f *fakeRepo) Create(*ExperimentRun) error                           { return nil }
func (f *fakeRepo) List(RunListParams) ([]ExperimentRun, int, error)      { return nil, 0, nil }
func (f *fakeRepo) Update(string, UpdateRunRequest) error                 { return nil }
func (f *fakeRepo) SoftDelete(string) error                               { return nil }
func (f *fakeRepo) AddReportLink(string, string) error                    { return nil }
func (f *fakeRepo) RemoveReportLink(string, string) error                 { return nil }
func (f *fakeRepo) GetReportLinks(string) ([]string, error)               { return f.reportLinks, nil }
func (f *fakeRepo) UpdateStatus(string, string, string, bool, bool) error { return nil }
func (f *fakeRepo) Reorder(string, []ReorderItem) error                   { return nil }
func (f *fakeRepo) MaxStepOrder(string) (int, error)                      { return f.maxOrder, nil }
func (f *fakeRepo) GetByID(id string) (*ExperimentRun, error) {
	if f.run != nil && f.run.ID == id {
		return f.run, nil
	}
	return nil, nil
}
func (f *fakeRepo) ListSteps(runID string) ([]RunStep, error) {
	items := []RunStep{}
	for _, step := range f.steps {
		if step.RunID == runID {
			items = append(items, *step)
		}
	}
	return items, nil
}
func (f *fakeRepo) CreateStep(step *RunStep) error {
	step.ID = testRunStepID
	f.steps[step.ID] = step
	return nil
}
func (f *fakeRepo) CreateStepsMany(runID, userID string, defs []StepDef, startOrder int) ([]RunStep, error) {
	steps := make([]RunStep, len(defs))
	for i, def := range defs {
		steps[i] = RunStep{RunID: runID, Name: def.Name, Status: StepStatusPlanned, StepOrder: startOrder + i + 1}
		steps[i].ID = testRunStepID[:len(testRunStepID)-1] + string(rune('a'+i))
	}
	return steps, nil
}
func (f *fakeRepo) GetStepByID(id string) (*RunStep, error)    { return f.steps[id], nil }
func (f *fakeRepo) UpdateStep(string, UpdateStepRequest) error { return nil }
func (f *fakeRepo) UpdateStepStatus(id, from, to string, started, completed *time.Time) error {
	step := f.steps[id]
	if step == nil || step.Status != from {
		return ErrStepConflict
	}
	step.Status, step.StartedAt, step.CompletedAt = to, started, completed
	return nil
}
func (f *fakeRepo) SoftDeleteStep(id string) error {
	delete(f.steps, id)
	return nil
}
func (f *fakeRepo) SetSourceTemplateID(stepID, templateID string) error {
	f.sourcedSteps[stepID] = templateID
	return nil
}

type allowAccess struct{}

func (allowAccess) ProjectExists(string) (bool, error) { return true, nil }
func (allowAccess) CanAccessProject(string, string, string, string) (bool, error) {
	return true, nil
}

type fakeTemplateReader struct {
	tmpl  *SteptemplatesTemplate
	items []SteptemplatesItem
}

type fakeReportReader struct{ reports []LinkedDailyReport }

func (f fakeReportReader) GetReportSummaries([]string, string, string) ([]LinkedDailyReport, error) {
	return f.reports, nil
}

func TestGetByIDIncludesLinkedDailyReports(t *testing.T) {
	repo := newFakeRepo()
	repo.reportLinks = []string{"report-1"}
	svc := NewService(repo, allowAccess{})
	svc.ConfigureReports(fakeReportReader{reports: []LinkedDailyReport{{ID: "report-1", ReportDate: "2026-08-01", Summary: "降温完成"}}})

	run, err := svc.GetByID(testRunID, "user", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if len(run.DailyReports) != 1 || run.DailyReports[0].Summary != "降温完成" || run.DailyReports[0].ReportDate != "2026-08-01" {
		t.Fatalf("daily_reports = %+v", run.DailyReports)
	}
}

func (f fakeTemplateReader) GetTemplateWithItems(string) (*SteptemplatesTemplate, []SteptemplatesItem, error) {
	return f.tmpl, f.items, nil
}

func TestStepTransitionTarget(t *testing.T) {
	tests := []struct {
		status, transition, want string
		ok                       bool
	}{
		{StepStatusPlanned, StepTransitionStart, StepStatusInProgress, true},
		{StepStatusPlanned, StepTransitionCancel, StepStatusCancelled, true},
		{StepStatusInProgress, StepTransitionPause, StepStatusPaused, true},
		{StepStatusInProgress, StepTransitionComplete, StepStatusCompleted, true},
		{StepStatusInProgress, StepTransitionSkip, StepStatusSkipped, true},
		{StepStatusPaused, StepTransitionResume, StepStatusInProgress, true},
		{StepStatusSkipped, StepTransitionStart, StepStatusInProgress, true},
		{StepStatusPlanned, StepTransitionComplete, "", false},
		{StepStatusCompleted, StepTransitionStart, "", false},
		{StepStatusCancelled, StepTransitionResume, "", false},
	}
	for _, tt := range tests {
		got, ok := stepTransitionTarget(tt.status, tt.transition)
		if got != tt.want || ok != tt.ok {
			t.Errorf("stepTransitionTarget(%q, %q) = %q, %v; want %q, %v",
				tt.status, tt.transition, got, ok, tt.want, tt.ok)
		}
	}
}

func TestUpdateStepTransition(t *testing.T) {
	repo := newFakeRepo()
	repo.steps[testRunStepID] = &RunStep{ID: testRunStepID, RunID: testRunID, Status: StepStatusPlanned}
	svc := NewService(repo, allowAccess{})
	start := StepTransitionStart

	step, err := svc.UpdateStep(testRunStepID, "user", "member", UpdateStepRequest{Transition: &start})
	if err != nil {
		t.Fatal(err)
	}
	if step.Status != StepStatusInProgress || step.StartedAt == nil || step.CompletedAt != nil {
		t.Fatalf("unexpected transitioned step: %+v", step)
	}

	resume := StepTransitionResume
	if _, err := svc.UpdateStep(testRunStepID, "user", "member", UpdateStepRequest{Transition: &resume}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid transition error = %v", err)
	}
}

func TestApplyTemplateAppendsAfterExistingSteps(t *testing.T) {
	repo := newFakeRepo()
	repo.maxOrder = 2
	svc := NewService(repo, allowAccess{})
	svc.ConfigureTemplates(fakeTemplateReader{
		tmpl: &SteptemplatesTemplate{ID: testTemplateID, Kind: "experiment"},
		items: []SteptemplatesItem{
			{ID: "i1", Name: "降温", StepOrder: 1},
			{ID: "i2", Name: "充气", StepOrder: 2},
		},
	})

	steps, err := svc.ApplyTemplate(testRunID, "user", "member", ApplyTemplateRequest{TemplateID: strPtr(testTemplateID)})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].StepOrder != 3 || steps[1].StepOrder != 4 {
		t.Fatalf("unexpected step orders: %+v", steps)
	}
	if len(repo.sourcedSteps) != 2 {
		t.Fatalf("source_template_id not recorded: %+v", repo.sourcedSteps)
	}
	for _, tmplID := range repo.sourcedSteps {
		if tmplID != testTemplateID {
			t.Fatalf("unexpected source template id: %s", tmplID)
		}
	}
}

func TestApplyTemplateRejectsWrongKind(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, allowAccess{})
	svc.ConfigureTemplates(fakeTemplateReader{
		tmpl: &SteptemplatesTemplate{ID: testTemplateID, Kind: "assembly"},
	})

	_, err := svc.ApplyTemplate(testRunID, "user", "member", ApplyTemplateRequest{TemplateID: strPtr(testTemplateID)})
	var commonErr *common.Error
	if !errors.As(err, &commonErr) || commonErr.Code != "bad_request" {
		t.Fatalf("got %v, want bad_request common.Error", err)
	}
}

func TestApplyTemplateRejectsBothSources(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, allowAccess{})
	svc.ConfigureTemplates(fakeTemplateReader{})

	_, err := svc.ApplyTemplate(testRunID, "user", "member", ApplyTemplateRequest{
		TemplateID: strPtr(testTemplateID),
		Steps:      []StepDef{{Name: "x", StepOrder: 1}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("got %v, want %v", err, ErrInvalidInput)
	}
}

func strPtr(value string) *string { return &value }
