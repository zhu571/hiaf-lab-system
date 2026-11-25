package scans

import (
	"errors"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

type fakeRepo struct {
	scan     Scan
	existing *Scan
}

func (f *fakeRepo) List(ListParams) ([]Scan, int, error) { return []Scan{f.scan}, 1, nil }
func (f *fakeRepo) GetByID(string) (*Scan, error) {
	copy := f.scan
	return &copy, nil
}
func (f *fakeRepo) FindByUniqueKey(string, string, string, string) (*Scan, error) {
	return f.existing, nil
}
func (f *fakeRepo) Create(item *Scan) error {
	item.CreatedAt = time.Now()
	item.UpdatedAt = item.CreatedAt
	f.scan = *item
	return nil
}
func (f *fakeRepo) AttachPNG(string, string, string, int64, *string) error { return nil }
func (f *fakeRepo) Tags(string) ([]TagCount, error)                        { return []TagCount{{Tag: "qpig", Count: 1}}, nil }
func (f *fakeRepo) UpdateAnnotation(_ string, title, description string, tags []string, annotatedAt *time.Time) error {
	f.scan.Title = title
	f.scan.Description = description
	f.scan.Tags = tags
	f.scan.AnnotatedAt = annotatedAt
	return nil
}
func (f *fakeRepo) MarkVoid(_ string, userID, reason string) error {
	f.scan.IsVoid = true
	f.scan.VoidedBy = &userID
	f.scan.VoidReason = &reason
	return nil
}

type fakeAccess struct{ allowed bool }

func (f fakeAccess) ProjectExists(string) (bool, error) { return true, nil }
func (f fakeAccess) CanAccessProject(string, string, string, string) (bool, error) {
	return f.allowed, nil
}
func (f fakeAccess) ProjectRole(string, string, string) (string, error) {
	return projects.RoleMember, nil
}

func newTestService() (*Service, *fakeRepo) {
	repo := &fakeRepo{scan: Scan{ID: "scan-1", ProjectID: "11111111-1111-1111-1111-111111111111"}}
	return NewService(repo, fakeAccess{allowed: true}), repo
}

func TestUpdateAnnotationSetsAndClears(t *testing.T) {
	svc, repo := newTestService()
	title := "QPIG ref sweep"
	item, err := svc.Update("scan-1", "u1", projects.RoleMember, UpdateScanRequest{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if item.AnnotatedAt == nil || repo.scan.Title != title {
		t.Fatalf("annotation not set: %+v", item)
	}

	empty := ""
	item, err = svc.Update("scan-1", "u1", projects.RoleMember, UpdateScanRequest{Title: &empty, Description: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if item.AnnotatedAt != nil {
		t.Fatalf("annotation must clear when title/description empty: %+v", item)
	}
}

func TestVoidRejectsAgentAndEmptyReason(t *testing.T) {
	svc, _ := newTestService()
	if _, err := svc.Void("scan-1", "agent-1", auth.RoleAgent, "duplicate"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent void error = %v", err)
	}
	if _, err := svc.Void("scan-1", "u1", projects.RoleMember, " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty reason error = %v", err)
	}
}

func TestListRequiresProjectAccess(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, fakeAccess{allowed: false})
	_, err := svc.List("11111111-1111-1111-1111-111111111111", "u1", projects.RoleViewer, ListParams{})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want forbidden", err)
	}
}

func TestNormalizeTags(t *testing.T) {
	got, err := normalizeTags([]string{" QPIG ", "qpig", "RFQ", "中文"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"qpig", "rfq", "中文"}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tags = %v, want %v", got, want)
		}
	}
}
