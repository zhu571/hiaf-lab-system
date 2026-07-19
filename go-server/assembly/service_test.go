package assembly

import (
	"errors"
	"testing"
	"time"
)

const (
	testProjectID    = "11111111-1111-1111-1111-111111111111"
	testStepID       = "22222222-2222-2222-2222-222222222222"
	testDependencyID = "33333333-3333-3333-3333-333333333333"
)

type fakeRepo struct{ steps map[string]*AssemblyStep }

func (f *fakeRepo) Create(*AssemblyStep) error                  { return nil }
func (f *fakeRepo) GetByProject(string) ([]AssemblyStep, error) { return nil, nil }
func (f *fakeRepo) Update(string, UpdateStepRequest) error      { return nil }
func (f *fakeRepo) SoftDelete(string) error                     { return nil }
func (f *fakeRepo) Reorder(string, []ReorderItem) error         { return nil }
func (f *fakeRepo) GetDependencyChain(string) ([]string, error) { return nil, nil }
func (f *fakeRepo) MaxStepOrder(string) (int, error)            { return 0, nil }
func (f *fakeRepo) GetByID(id string) (*AssemblyStep, error)    { return f.steps[id], nil }
func (f *fakeRepo) UpdateStatus(id, from, to string, started, completed *time.Time) error {
	step := f.steps[id]
	if step == nil || step.Status != from {
		return ErrStepConflict
	}
	step.Status, step.StartedAt, step.CompletedAt = to, started, completed
	return nil
}

type allowAccess struct{}

func (allowAccess) ProjectExists(string) (bool, error) { return true, nil }
func (allowAccess) CanAccessProject(string, string, string, string) (bool, error) {
	return true, nil
}

func TestCancelledDependencyNeedsAuditedOverride(t *testing.T) {
	repo := &fakeRepo{steps: map[string]*AssemblyStep{
		testStepID:       {ID: testStepID, ProjectID: testProjectID, Status: StatusPlanned, DependsOn: stringAddress(testDependencyID)},
		testDependencyID: {ID: testDependencyID, ProjectID: testProjectID, Status: StatusCancelled},
	}}
	svc := NewService(repo, allowAccess{})
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	svc.now = func() time.Time { return now }
	start := TransitionStart

	if _, err := svc.Update(testStepID, "user", "member", UpdateStepRequest{Transition: &start}); !errors.Is(err, ErrDependencyPending) {
		t.Fatalf("without override: got %v, want %v", err, ErrDependencyPending)
	}
	reason := "debug assembly"
	step, err := svc.Update(testStepID, "user", "member", UpdateStepRequest{Transition: &start, OverrideReason: &reason})
	if err != nil {
		t.Fatal(err)
	}
	if step.Status != StatusInProgress || step.StartedAt == nil || !step.StartedAt.Equal(now) || step.CompletedAt != nil {
		t.Fatalf("unexpected transitioned step: %+v", step)
	}
}

func stringAddress(value string) *string { return &value }
