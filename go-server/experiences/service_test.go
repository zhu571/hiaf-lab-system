package experiences

import (
	"errors"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

func TestCreateNormalizesTagsAndRequiresAdminForGlobal(t *testing.T) {
	repo := newFakeExperienceRepo()
	svc := NewService(repo, fakeProjectAccess{roles: map[string]string{"usr_1": projects.RoleMember}})

	_, err := svc.Create("usr_1", auth.RoleMember, CreateExperienceRequest{
		Title:   "Global",
		Content: "global content",
		Tags:    []string{" RF ", "rf", "", "Matching"},
	})
	if !errors.Is(err, ErrGlobalExperienceAdminOnly) {
		t.Fatalf("Create global error = %v, want %v", err, ErrGlobalExperienceAdminOnly)
	}

	projectID := "prj_1"
	exp, err := svc.Create("usr_1", auth.RoleMember, CreateExperienceRequest{
		ProjectID: &projectID,
		Title:     "Project",
		Content:   "project content",
		Tags:      []string{" RF ", "rf", "", "Matching"},
	})
	if err != nil {
		t.Fatalf("Create project returned error: %v", err)
	}
	want := []string{"rf", "matching"}
	if len(exp.Tags) != len(want) || exp.Tags[0] != want[0] || exp.Tags[1] != want[1] {
		t.Fatalf("tags = %#v, want %#v", exp.Tags, want)
	}
}

func TestCandidateListFiltersOrdinaryUserToOwnItems(t *testing.T) {
	repo := newFakeExperienceRepo()
	prj := "prj_1"
	repo.experiences["exp_1"] = testExperience("exp_1", &prj, StatusCandidate, "usr_1")
	repo.experiences["exp_2"] = testExperience("exp_2", &prj, StatusCandidate, "usr_2")
	svc := NewService(repo, fakeProjectAccess{roles: map[string]string{"usr_1": projects.RoleMember}})

	_, err := svc.List("usr_1", auth.RoleMember, ExperienceListParams{ProjectID: prj, Status: StatusCandidate})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if repo.lastList.CandidateAuthorID != "usr_1" {
		t.Fatalf("CandidateAuthorID = %q, want usr_1", repo.lastList.CandidateAuthorID)
	}
}

func TestCandidateListAllowsMaintainerProjectItems(t *testing.T) {
	repo := newFakeExperienceRepo()
	svc := NewService(repo, fakeProjectAccess{roles: map[string]string{"usr_1": projects.RoleMaintainer}})

	_, err := svc.List("usr_1", auth.RoleMember, ExperienceListParams{ProjectID: "prj_1", Status: StatusCandidate})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if repo.lastList.CandidateAuthorID != "" {
		t.Fatalf("CandidateAuthorID = %q, want empty for maintainer", repo.lastList.CandidateAuthorID)
	}
}

func TestUpdateRejectsPublishedExperience(t *testing.T) {
	repo := newFakeExperienceRepo()
	prj := "prj_1"
	repo.experiences["exp_1"] = testExperience("exp_1", &prj, StatusPublished, "usr_1")
	svc := NewService(repo, fakeProjectAccess{roles: map[string]string{"usr_1": projects.RoleOwner}})
	title := "updated"

	_, err := svc.Update("exp_1", "usr_1", auth.RoleMember, UpdateExperienceRequest{Title: &title})
	if !errors.Is(err, ErrNotCandidate) {
		t.Fatalf("Update error = %v, want %v", err, ErrNotCandidate)
	}
}

func TestArchiveRequiresPublishedAndOwner(t *testing.T) {
	repo := newFakeExperienceRepo()
	prj := "prj_1"
	repo.experiences["exp_1"] = testExperience("exp_1", &prj, StatusCandidate, "usr_1")
	svc := NewService(repo, fakeProjectAccess{roles: map[string]string{"usr_1": projects.RoleOwner}})

	_, err := svc.Archive("exp_1", "usr_1", auth.RoleMember)
	if !errors.Is(err, ErrNotPublished) {
		t.Fatalf("Archive error = %v, want %v", err, ErrNotPublished)
	}

	repo.experiences["exp_1"].Status = StatusPublished
	_, err = svc.Archive("exp_1", "usr_1", auth.RoleMember)
	if err != nil {
		t.Fatalf("Archive returned error: %v", err)
	}
	if repo.experiences["exp_1"].Status != StatusArchived {
		t.Fatalf("status = %q, want %q", repo.experiences["exp_1"].Status, StatusArchived)
	}
}

func TestArchiveGlobalExperienceRequiresAdmin(t *testing.T) {
	repo := newFakeExperienceRepo()
	repo.experiences["exp_1"] = testExperience("exp_1", nil, StatusPublished, "usr_1")
	svc := NewService(repo, fakeProjectAccess{})

	_, err := svc.Archive("exp_1", "usr_1", auth.RoleMember)
	if !errors.Is(err, ErrGlobalExperienceAdminOnly) {
		t.Fatalf("Archive global error = %v, want %v", err, ErrGlobalExperienceAdminOnly)
	}

	_, err = svc.Archive("exp_1", "usr_admin", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("Archive global as admin returned error: %v", err)
	}
}

type fakeProjectAccess struct {
	roles map[string]string
}

func (f fakeProjectAccess) ProjectExists(projectID string) (bool, error) {
	return projectID == "prj_1" || projectID == "prj_2", nil
}

func (f fakeProjectAccess) CanAccessProject(projectID, userID, userRole, minRole string) (bool, error) {
	if userRole == auth.RoleAdmin {
		return true, nil
	}
	role, ok := f.roles[userID]
	if !ok {
		return false, nil
	}
	return projectRoleRank(role) >= projectRoleRank(minRole), nil
}

func (f fakeProjectAccess) ProjectRole(projectID, userID, userRole string) (string, error) {
	if userRole == auth.RoleAdmin {
		return projects.RoleOwner, nil
	}
	return f.roles[userID], nil
}

func projectRoleRank(role string) int {
	switch role {
	case projects.RoleOwner:
		return 40
	case projects.RoleMaintainer:
		return 30
	case projects.RoleMember:
		return 20
	case projects.RoleViewer:
		return 10
	default:
		return 0
	}
}

type fakeExperienceRepo struct {
	experiences map[string]*Experience
	lastList    ExperienceListParams
}

func newFakeExperienceRepo() *fakeExperienceRepo {
	return &fakeExperienceRepo{experiences: map[string]*Experience{}}
}

func (f *fakeExperienceRepo) Create(authorID string, req CreateExperienceRequest) (*Experience, error) {
	exp := Experience{
		ID:        "exp_new",
		ProjectID: req.ProjectID,
		Title:     req.Title,
		Content:   req.Content,
		Tags:      append([]string(nil), req.Tags...),
		Status:    StatusCandidate,
		AuthorID:  authorID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	f.experiences[exp.ID] = cloneExperience(exp)
	return cloneExperience(exp), nil
}

func (f *fakeExperienceRepo) GetByID(id string) (*Experience, error) {
	return cloneExperiencePtr(f.experiences[id]), nil
}

func (f *fakeExperienceRepo) List(params ExperienceListParams) ([]Experience, int, error) {
	f.lastList = params
	var out []Experience
	for _, exp := range f.experiences {
		if params.CandidateAuthorID != "" && exp.AuthorID != params.CandidateAuthorID {
			continue
		}
		out = append(out, *cloneExperience(*exp))
	}
	return out, len(out), nil
}

func (f *fakeExperienceRepo) Update(id string, req UpdateExperienceRequest) (*Experience, error) {
	exp := f.experiences[id]
	if exp == nil {
		return nil, nil
	}
	if exp.Status != StatusCandidate {
		return nil, ErrNotCandidate
	}
	if req.Title != nil {
		exp.Title = *req.Title
	}
	if req.Content != nil {
		exp.Content = *req.Content
	}
	if req.Tags != nil {
		exp.Tags = append([]string(nil), req.Tags...)
	}
	return cloneExperience(*exp), nil
}

func (f *fakeExperienceRepo) Publish(id, reviewerID string) (*Experience, error) {
	exp := f.experiences[id]
	if exp == nil {
		return nil, nil
	}
	if exp.Status != StatusCandidate {
		return nil, ErrNotCandidate
	}
	exp.Status = StatusPublished
	exp.ReviewerID = &reviewerID
	now := time.Now()
	exp.PublishedAt = &now
	return cloneExperience(*exp), nil
}

func (f *fakeExperienceRepo) Archive(id string) (*Experience, error) {
	exp := f.experiences[id]
	if exp == nil {
		return nil, nil
	}
	if exp.Status != StatusPublished {
		return nil, ErrNotPublished
	}
	exp.Status = StatusArchived
	return cloneExperience(*exp), nil
}

func testExperience(id string, projectID *string, status, authorID string) *Experience {
	now := time.Now()
	return &Experience{
		ID:        id,
		ProjectID: projectID,
		Title:     "title",
		Content:   "content",
		Tags:      []string{"rf"},
		Status:    status,
		AuthorID:  authorID,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func cloneExperiencePtr(exp *Experience) *Experience {
	if exp == nil {
		return nil
	}
	return cloneExperience(*exp)
}

func cloneExperience(exp Experience) *Experience {
	out := exp
	if exp.ProjectID != nil {
		projectID := *exp.ProjectID
		out.ProjectID = &projectID
	}
	if exp.ReviewerID != nil {
		reviewerID := *exp.ReviewerID
		out.ReviewerID = &reviewerID
	}
	if exp.PublishedAt != nil {
		publishedAt := *exp.PublishedAt
		out.PublishedAt = &publishedAt
	}
	out.Tags = append([]string(nil), exp.Tags...)
	out.LinkedProjects = append([]ExperienceProjectLink(nil), exp.LinkedProjects...)
	return &out
}
