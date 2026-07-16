package issues

import (
	"errors"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

func TestCreateValidatesRelatedLogCountAndProject(t *testing.T) {
	tests := []struct {
		name         string
		allLogCount  int
		projectCount int
		want         error
	}{
		{
			name:         "all related logs exist and belong to project",
			allLogCount:  2,
			projectCount: 2,
		},
		{
			name:        "missing related log",
			allLogCount: 1,
			want:        ErrRelatedLogNotFound,
		},
		{
			name:         "related log from another project",
			allLogCount:  2,
			projectCount: 1,
			want:         ErrRelatedLogProjectMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeIssueRepo()
			repo.allLogCount = tt.allLogCount
			repo.projectLogCount = tt.projectCount
			svc := NewService(repo, fakeProjectAccess{status: projects.StatusActive, roles: map[string]string{"usr_1": projects.RoleMember}})

			issue, err := svc.Create("prj_1", "usr_1", auth.RoleMember, CreateIssueRequest{
				Title:         "RF reflected power spike",
				RelatedLogIDs: []string{"log_1", "log_2"},
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Create error = %v, want %v", err, tt.want)
			}
			if tt.want == nil && issue == nil {
				t.Fatal("Create returned nil issue")
			}
			if tt.want == nil && len(repo.created.RelatedLogIDs) != 2 {
				t.Fatalf("created related log count = %d, want 2", len(repo.created.RelatedLogIDs))
			}
		})
	}
}

func TestCreateAdminStillRespectsProjectLifecycle(t *testing.T) {
	repo := newFakeIssueRepo()
	svc := NewService(repo, fakeProjectAccess{status: projects.StatusDraft})

	_, err := svc.Create("prj_1", "admin_1", auth.RoleAdmin, CreateIssueRequest{Title: "draft project issue"})
	if !errors.Is(err, ErrProjectLifecycleBlocked) {
		t.Fatalf("Create error = %v, want %v", err, ErrProjectLifecycleBlocked)
	}
}

func TestUpdateRejectsClosedIssue(t *testing.T) {
	repo := newFakeIssueRepo()
	closed := testIssue("iss_1", StatusClosed, "usr_1", nil)
	repo.issues["iss_1"] = cloneIssue(closed)
	svc := NewService(repo, fakeProjectAccess{status: projects.StatusActive, roles: map[string]string{"usr_1": projects.RoleOwner}})
	title := "updated title"

	_, err := svc.Update("iss_1", "usr_1", auth.RoleMember, UpdateIssueRequest{Title: &title})
	if !errors.Is(err, ErrIssueClosed) {
		t.Fatalf("Update error = %v, want %v", err, ErrIssueClosed)
	}
}

func TestTransitionPermissionMatrix(t *testing.T) {
	assignee := "usr_assignee"
	tests := []struct {
		name   string
		issue  Issue
		userID string
		role   string
		target string
		roles  map[string]string
		want   error
	}{
		{
			name:   "open to in_progress allows assignee",
			issue:  testIssue("iss_1", StatusOpen, "usr_author", &assignee),
			userID: assignee,
			role:   auth.RoleMember,
			target: StatusInProgress,
		},
		{
			name:   "open to in_progress rejects viewer",
			issue:  testIssue("iss_1", StatusOpen, "usr_author", &assignee),
			userID: "usr_viewer",
			role:   auth.RoleMember,
			target: StatusInProgress,
			roles:  map[string]string{"usr_viewer": projects.RoleViewer},
			want:   ErrTransitionForbidden,
		},
		{
			name:   "in_progress to resolved allows assignee",
			issue:  testIssue("iss_1", StatusInProgress, "usr_author", &assignee),
			userID: assignee,
			role:   auth.RoleMember,
			target: StatusResolved,
		},
		{
			name:   "in_progress to resolved allows maintainer",
			issue:  testIssue("iss_1", StatusInProgress, "usr_author", &assignee),
			userID: "usr_maintainer",
			role:   auth.RoleMember,
			target: StatusResolved,
			roles:  map[string]string{"usr_maintainer": projects.RoleMaintainer},
		},
		{
			name:   "resolved to closed allows author",
			issue:  testIssue("iss_1", StatusResolved, "usr_author", &assignee),
			userID: "usr_author",
			role:   auth.RoleMember,
			target: StatusClosed,
		},
		{
			name:   "resolved to open requires owner",
			issue:  testIssue("iss_1", StatusResolved, "usr_author", &assignee),
			userID: "usr_owner",
			role:   auth.RoleMember,
			target: StatusOpen,
			roles:  map[string]string{"usr_owner": projects.RoleOwner},
		},
		{
			name:   "closed to open allows admin",
			issue:  testIssue("iss_1", StatusClosed, "usr_author", &assignee),
			userID: "admin_1",
			role:   auth.RoleAdmin,
			target: StatusOpen,
		},
		{
			name:   "open to closed requires owner",
			issue:  testIssue("iss_1", StatusOpen, "usr_author", &assignee),
			userID: "usr_owner",
			role:   auth.RoleMember,
			target: StatusClosed,
			roles:  map[string]string{"usr_owner": projects.RoleOwner},
		},
		{
			name:   "open to closed allows author with update_issue",
			issue:  testIssue("iss_1", StatusOpen, "usr_author", &assignee),
			userID: "usr_author",
			role:   auth.RoleMember,
			target: StatusClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeIssueRepo()
			repo.issues[tt.issue.ID] = cloneIssue(tt.issue)
			roles := map[string]string{
				tt.issue.AuthorID: projects.RoleMember,
				assignee:          projects.RoleMember,
			}
			for userID, role := range tt.roles {
				roles[userID] = role
			}
			svc := NewService(repo, fakeProjectAccess{status: projects.StatusActive, roles: roles})

			_, err := svc.Transition(tt.issue.ID, tt.userID, tt.role, TransitionRequest{
				TargetStatus: tt.target,
				Reason:       reasonFor(tt.issue.Status, tt.target),
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Transition error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestTransitionSetsAndClearsResolvedAt(t *testing.T) {
	assignee := "usr_assignee"
	repo := newFakeIssueRepo()
	inProgress := testIssue("iss_1", StatusInProgress, "usr_author", &assignee)
	repo.issues["iss_1"] = cloneIssue(inProgress)
	svc := NewService(repo, fakeProjectAccess{status: projects.StatusActive, roles: map[string]string{
		assignee:    projects.RoleMember,
		"usr_owner": projects.RoleOwner,
	}})

	resolved, err := svc.Transition("iss_1", assignee, auth.RoleMember, TransitionRequest{TargetStatus: StatusResolved})
	if err != nil {
		t.Fatalf("resolve returned error: %v", err)
	}
	if resolved.ResolvedAt == nil {
		t.Fatal("resolved_at is nil after transition to resolved")
	}
	if time.Since(*resolved.ResolvedAt) > time.Minute {
		t.Fatalf("resolved_at = %s, want recent time", resolved.ResolvedAt.Format(time.RFC3339))
	}

	reopened, err := svc.Transition("iss_1", "usr_owner", auth.RoleMember, TransitionRequest{TargetStatus: StatusOpen, Reason: "needs another check"})
	if err != nil {
		t.Fatalf("reopen returned error: %v", err)
	}
	if reopened.ResolvedAt != nil {
		t.Fatalf("resolved_at = %s, want nil after reopen", reopened.ResolvedAt.Format(time.RFC3339))
	}
}

func TestTransitionRequiresReasonWhenReopening(t *testing.T) {
	repo := newFakeIssueRepo()
	resolved := testIssue("iss_1", StatusResolved, "usr_author", nil)
	repo.issues["iss_1"] = cloneIssue(resolved)
	svc := NewService(repo, fakeProjectAccess{status: projects.StatusActive, roles: map[string]string{"usr_owner": projects.RoleOwner}})

	_, err := svc.Transition("iss_1", "usr_owner", auth.RoleMember, TransitionRequest{TargetStatus: StatusOpen})
	if !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("Transition error = %v, want %v", err, ErrReasonRequired)
	}
}

func testIssue(id, status, authorID string, assigneeID *string) Issue {
	now := time.Now()
	return Issue{
		ID:          id,
		ProjectID:   "prj_1",
		Title:       "issue title",
		Description: "issue description",
		Status:      status,
		Severity:    SeverityMedium,
		AuthorID:    authorID,
		AssigneeID:  assigneeID,
		ReportDate:  now.Format(time.DateOnly),
		OccurredAt:  now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func reasonFor(from, to string) string {
	if (from == StatusResolved || from == StatusClosed) && to == StatusOpen {
		return "needs another check"
	}
	return ""
}

type fakeProjectAccess struct {
	status        string
	roles         map[string]string
	exists        bool
	commentPolicy string
}

func (f fakeProjectAccess) ProjectExists(projectID string) (bool, error) {
	if f.exists {
		return true, nil
	}
	return f.status != "", nil
}

func (f fakeProjectAccess) ProjectStatus(projectID string) (string, error) {
	return f.status, nil
}

func (f fakeProjectAccess) ProjectCommentPolicy(projectID string) (string, error) {
	if f.commentPolicy != "" {
		return f.commentPolicy, nil
	}
	return projects.CommentPolicyMembers, nil
}

func (f fakeProjectAccess) HasProjectPermission(projectID, userID string, perm middleware.Permission) (bool, error) {
	if userID == "admin_1" {
		return true, nil
	}
	role, ok := f.roles[userID]
	if !ok {
		return false, nil
	}
	return fakeRoleHasPermission(role, perm), nil
}

func fakeRoleHasPermission(role string, perm middleware.Permission) bool {
	switch role {
	case projects.RoleOwner:
		return true
	case projects.RoleMaintainer:
		return perm != middleware.PermManageMembers
	case projects.RoleMember:
		switch perm {
		case middleware.PermRead,
			middleware.PermCreateLog,
			middleware.PermUpdateOwnLog,
			middleware.PermCreateIssue,
			middleware.PermUpdateIssue,
			middleware.PermCreateExperience:
			return true
		default:
			return false
		}
	case projects.RoleViewer:
		return perm == middleware.PermRead
	default:
		return false
	}
}

type fakeIssueRepo struct {
	issues          map[string]*Issue
	comments        map[string][]Comment
	allLogCount     int
	projectLogCount int
	created         CreateIssueRequest
}

func newFakeIssueRepo() *fakeIssueRepo {
	return &fakeIssueRepo{
		issues:          map[string]*Issue{},
		comments:        map[string][]Comment{},
		allLogCount:     0,
		projectLogCount: 0,
	}
}

func (f *fakeIssueRepo) Create(projectID, authorID string, req CreateIssueRequest, occurredAt time.Time, reportDate string) (*Issue, error) {
	f.created = req
	issue := Issue{
		ID:          "iss_new",
		ProjectID:   projectID,
		Title:       req.Title,
		Description: req.Description,
		Status:      StatusOpen,
		Severity:    defaultSeverity(req.Severity),
		AuthorID:    authorID,
		AssigneeID:  req.AssigneeID,
		ReportDate:  reportDate,
		OccurredAt:  occurredAt,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	f.issues[issue.ID] = cloneIssue(issue)
	return cloneIssue(issue), nil
}

func (f *fakeIssueRepo) GetByID(id string) (*Issue, error) {
	issue := f.issues[id]
	if issue == nil {
		return nil, nil
	}
	return cloneIssue(*issue), nil
}

func (f *fakeIssueRepo) List(projectID string, params IssueListParams) ([]Issue, int, error) {
	var out []Issue
	for _, issue := range f.issues {
		if issue.ProjectID == projectID {
			out = append(out, *cloneIssue(*issue))
		}
	}
	return out, len(out), nil
}

func (f *fakeIssueRepo) Update(id string, req UpdateIssueRequest) (*Issue, error) {
	issue := f.issues[id]
	if issue == nil {
		return nil, nil
	}
	if req.Title != nil {
		issue.Title = *req.Title
	}
	if req.Description != nil {
		issue.Description = *req.Description
	}
	if req.Severity != nil {
		issue.Severity = *req.Severity
	}
	if req.AssigneeID != nil {
		issue.AssigneeID = req.AssigneeID
	}
	issue.UpdatedAt = time.Now()
	return cloneIssue(*issue), nil
}

func (f *fakeIssueRepo) TransitionStatus(id, targetStatus, userID, comment string, addComment bool) (*Issue, error) {
	issue := f.issues[id]
	if issue == nil {
		return nil, nil
	}
	issue.Status = targetStatus
	now := time.Now()
	switch targetStatus {
	case StatusResolved:
		issue.ResolvedAt = &now
	case StatusOpen:
		issue.ResolvedAt = nil
	}
	issue.UpdatedAt = now
	if addComment {
		_, _ = f.AddComment(id, userID, comment)
	}
	return cloneIssue(*issue), nil
}

func (f *fakeIssueRepo) AddComment(issueID, authorID, content string) (*Comment, error) {
	comment := Comment{ID: "cmt_new", IssueID: issueID, AuthorID: authorID, Content: content, CreatedAt: time.Now()}
	f.comments[issueID] = append(f.comments[issueID], comment)
	return &comment, nil
}

func (f *fakeIssueRepo) GetComments(issueID string, page, perPage int) ([]Comment, error) {
	return append([]Comment(nil), f.comments[issueID]...), nil
}

func (f *fakeIssueRepo) CountRelatedLogs(projectID string, logIDs []string) (int, error) {
	return f.projectLogCount, nil
}

func (f *fakeIssueRepo) CountLogsByIDs(logIDs []string) (int, error) {
	return f.allLogCount, nil
}

func cloneIssue(issue Issue) *Issue {
	out := issue
	if issue.AssigneeID != nil {
		assignee := *issue.AssigneeID
		out.AssigneeID = &assignee
	}
	if issue.ResolvedAt != nil {
		resolvedAt := *issue.ResolvedAt
		out.ResolvedAt = &resolvedAt
	}
	out.Comments = append([]Comment(nil), issue.Comments...)
	return &out
}
