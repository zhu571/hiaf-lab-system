package rfmatch

import (
	"errors"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

const (
	projectUUID = "b0000000-0000-4000-8000-000000000001"
	recordUUID  = "d0000000-0000-4000-8000-000000000001"
)

func TestCreateRequiresStatusAndOnlyCreatorOrOwnerCanVoid(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember})
	if _, err := svc.Create(projectUUID, "creator", auth.RoleMember, CreateRFMatchingRequest{Device: DeviceRFQ, FrequencyMHz: 3}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create without status error = %v", err)
	}
	status := StatusPass
	record, err := svc.Create(projectUUID, "creator", auth.RoleMember, CreateRFMatchingRequest{Device: DeviceRFQ, FrequencyMHz: 3, Status: &status})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkVoid(record.ID, "other", auth.RoleMember, "bad reading"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("MarkVoid other error = %v", err)
	}
	if err := svc.MarkVoid(record.ID, "creator", auth.RoleMember, "bad reading"); err != nil || !repo.voided {
		t.Fatalf("MarkVoid creator error = %v, voided = %v", err, repo.voided)
	}
}

type fakeRepository struct {
	item   *RFMatchingRecord
	voided bool
}

func (f *fakeRepository) Create(record *RFMatchingRecord) error {
	record.ID, record.CreatedAt, record.UpdatedAt = recordUUID, time.Now(), time.Now()
	f.item = record
	return nil
}
func (f *fakeRepository) GetByID(string) (*RFMatchingRecord, error)        { return f.item, nil }
func (f *fakeRepository) List(ListParams) ([]RFMatchingRecord, int, error) { return nil, 0, nil }
func (f *fakeRepository) Update(string, UpdateRFMatchingRequest) error     { return nil }
func (f *fakeRepository) MarkVoid(string, string, string) error {
	f.voided = true
	return nil
}

type fakeAccess struct{ role string }

func (f fakeAccess) ProjectExists(string) (bool, error) { return true, nil }
func (f fakeAccess) CanAccessProject(_, _, _ string, minRole string) (bool, error) {
	rank := map[string]int{projects.RoleViewer: 1, projects.RoleMember: 2, projects.RoleMaintainer: 3, projects.RoleOwner: 4}
	return rank[f.role] >= rank[minRole], nil
}
func (f fakeAccess) ProjectRole(_, _, _ string) (string, error) { return f.role, nil }
