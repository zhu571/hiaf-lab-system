package testdata

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

const (
	projectUUID = "b0000000-0000-4000-8000-000000000001"
	runUUID     = "70000000-0000-4000-8000-000000000001"
	dataUUID    = "d0000000-0000-4000-8000-000000000001"
)

func TestServiceCreateAndMarkInvalid(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleMember}, fakeRuns{exists: true})
	runID := runUUID
	td, err := svc.Create(projectUUID, "creator", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypeCryo, RunID: &runID, Measurement: " temperature ", Value: 79.6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if td.Source != SourceManual || td.Quality != QualityNormal || td.Measurement != "temperature" {
		t.Fatalf("defaults/normalization = %#v", td)
	}

	if err := svc.MarkInvalid(td.ID, "other", auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("MarkInvalid other error = %v", err)
	}
	ownerService := NewService(repo, fakeAccess{role: projects.RoleOwner}, fakeRuns{exists: true})
	if err := ownerService.MarkInvalid(td.ID, "owner", auth.RoleMember); err != nil {
		t.Fatalf("MarkInvalid owner error = %v", err)
	}
	if err := svc.MarkInvalid(td.ID, "creator", auth.RoleMember); err != nil || repo.item.Quality != QualityInvalid {
		t.Fatalf("MarkInvalid creator error = %v, quality = %q", err, repo.item.Quality)
	}
}

func TestCreateRejectsMissingRunAndUpdateRejectsDataType(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, fakeAccess{role: projects.RoleOwner}, fakeRuns{})
	runID := runUUID
	_, err := svc.Create(projectUUID, "owner", auth.RoleMember, nil, CreateTestDataRequest{
		DataType: DataTypePressure, RunID: &runID, Measurement: "pressure",
	})
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("Create missing run error = %v", err)
	}
	repo.item = &TestData{ID: dataUUID, ProjectID: projectUUID, DataType: DataTypePressure, RecordedBy: stringPointer("creator")}
	dataType := DataTypeCryo
	_, err = svc.Update(dataUUID, "owner", auth.RoleMember, UpdateTestDataRequest{DataType: &dataType})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Update data_type error = %v", err)
	}
}

func TestHTTPRunValidator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/experiment-runs/"+runUUID || r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("request = %s, authorization = %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	headers := http.Header{"Authorization": {"Bearer token"}}
	exists, err := NewHTTPRunValidator(server.URL).Exists(runUUID, headers)
	if err != nil || !exists {
		t.Fatalf("Exists = %v, %v", exists, err)
	}
}

type fakeRepository struct{ item *TestData }

func (f *fakeRepository) Create(td *TestData) error {
	td.ID, td.CreatedAt, td.UpdatedAt = dataUUID, time.Now(), time.Now()
	f.item = td
	return nil
}
func (f *fakeRepository) GetByID(id string) (*TestData, error) { return f.item, nil }
func (f *fakeRepository) List(ListParams) ([]TestData, int, error) {
	return nil, 0, nil
}
func (f *fakeRepository) Update(string, UpdateTestDataRequest) error { return nil }
func (f *fakeRepository) MarkInvalid(string, string) error {
	f.item.Quality = QualityInvalid
	return nil
}

type fakeAccess struct{ role string }

func (f fakeAccess) ProjectExists(string) (bool, error) { return true, nil }
func (f fakeAccess) CanAccessProject(_, _, _ string, minRole string) (bool, error) {
	rank := map[string]int{projects.RoleViewer: 1, projects.RoleMember: 2, projects.RoleMaintainer: 3, projects.RoleOwner: 4}
	return rank[f.role] >= rank[minRole], nil
}
func (f fakeAccess) ProjectRole(_, _, _ string) (string, error) { return f.role, nil }

type fakeRuns struct{ exists bool }

func (f fakeRuns) Exists(string, http.Header) (bool, error) { return f.exists, nil }

func stringPointer(value string) *string { return &value }
