package todos

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// ---------- fakes ----------

var testLoc = time.FixedZone("CST", 8*3600)

func testNow() time.Time { return time.Date(2026, 8, 7, 9, 0, 0, 0, testLoc) }

type fakeRepo struct {
	mu    sync.Mutex
	todos map[string]*Todo
	seq   int

	getErr    error
	listErr   error
	createErr error

	// 状态守卫开关：默认按真实状态机执行，测试可覆盖。
	forceDoneOK  *bool
	forceDeferOK *bool
	forceEditOK  *bool

	inflightOverride map[string]bool
	hintCandidates   []Todo

	lastListParams ListParams
	lastProjectIDs []string

	doneCalls      int
	deferCalls     int
	editCalls      int
	deleteCalls    int
	rolloverCalls  int
	issueSyncCalls int
	cleanupCalls   int
	openCalls      int
	inflightCalls  int

	lastDoneBy            string
	lastDoneAt            time.Time
	lastDeferTomorrow     string
	lastEditOld           time.Time
	lastRolloverToday     string
	lastCleanupCreatedFor string
	lastCleanupCreatedAt  time.Time
	lastOpenUserID        string
	lastOpenDate          string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{todos: map[string]*Todo{}}
}

func (r *fakeRepo) copy(t *Todo) *Todo {
	if t == nil {
		return nil
	}
	c := *t
	if t.ProjectID != nil {
		p := *t.ProjectID
		c.ProjectID = &p
	}
	if t.IssueID != nil {
		p := *t.IssueID
		c.IssueID = &p
	}
	if t.CompletedAt != nil {
		p := *t.CompletedAt
		c.CompletedAt = &p
	}
	if t.CompletedBy != nil {
		p := *t.CompletedBy
		c.CompletedBy = &p
	}
	return &c
}

// countRollover 加锁读取 rollover 次数（scheduler goroutine 并发写）。
func (r *fakeRepo) countRollover() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rolloverCalls
}

func (r *fakeRepo) Create(t *Todo) (*Todo, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	now := testNow()
	t.ID = fmt.Sprintf("t%d", r.seq)
	t.CreatedAt = now
	t.UpdatedAt = now
	r.todos[t.ID] = r.copy(t)
	return r.copy(t), nil
}

func (r *fakeRepo) InsertGenerated(t *Todo) (bool, error) {
	// issue 在途唯一约束：同 issue 且 pending/deferred → 跳过
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.IssueID != nil {
		for _, ex := range r.todos {
			if ex.IssueID != nil && *ex.IssueID == *t.IssueID &&
				(ex.Status == StatusPending || ex.Status == StatusDeferred) {
				return false, nil
			}
		}
	}
	r.seq++
	now := testNow()
	t.ID = fmt.Sprintf("t%d", r.seq)
	t.CreatedAt = now
	t.UpdatedAt = now
	r.todos[t.ID] = r.copy(t)
	return true, nil
}

func (r *fakeRepo) GetByID(id string) (*Todo, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.copy(r.todos[id]), nil
}

func (r *fakeRepo) List(userID string, projectIDs []string, p ListParams) ([]Todo, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastListParams = p
	r.lastProjectIDs = projectIDs
	out := []Todo{}
	for _, t := range r.todos {
		if t.CreatedFor != p.Date {
			continue
		}
		if p.Scope == ScopeMine && t.CreatedBy != userID {
			continue
		}
		if p.Scope == ScopeShared {
			// shared = project_id IN 成员项目（不含个人项）
			if t.ProjectID == nil || !contains(projectIDs, *t.ProjectID) {
				continue
			}
		}
		if p.Scope == ScopeAll && t.CreatedBy != userID {
			if t.ProjectID == nil || !contains(projectIDs, *t.ProjectID) {
				continue
			}
		}
		switch p.Status {
		case "open":
			if t.Status != StatusPending && t.Status != StatusDeferred {
				continue
			}
		case StatusDone:
			if t.Status != StatusDone {
				continue
			}
		case StatusCancelled:
			if t.Status != StatusCancelled {
				continue
			}
		case "all":
		default:
			if t.Status == StatusDone || t.Status == StatusCancelled {
				continue
			}
		}
		out = append(out, *r.copy(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *fakeRepo) UpdateDone(id, completedBy string, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.doneCalls++
	r.lastDoneBy = completedBy
	r.lastDoneAt = now
	if r.forceDoneOK != nil {
		return *r.forceDoneOK, nil
	}
	t := r.todos[id]
	if t == nil || (t.Status != StatusPending && t.Status != StatusDeferred) {
		return false, nil
	}
	t.Status = StatusDone
	t.CompletedAt = &now
	t.CompletedBy = &completedBy
	t.UpdatedAt = now
	return true, nil
}

func (r *fakeRepo) UpdateDefer(id, tomorrow string, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deferCalls++
	r.lastDeferTomorrow = tomorrow
	if r.forceDeferOK != nil {
		return *r.forceDeferOK, nil
	}
	t := r.todos[id]
	if t == nil || t.Status != StatusPending {
		return false, nil
	}
	t.Status = StatusDeferred
	t.CreatedFor = tomorrow
	t.UpdatedAt = now
	return true, nil
}

func (r *fakeRepo) UpdateEdit(id string, oldUpdatedAt time.Time, req UpdateRequest, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.editCalls++
	r.lastEditOld = oldUpdatedAt
	if r.forceEditOK != nil {
		return *r.forceEditOK, nil
	}
	t := r.todos[id]
	if t == nil || !t.UpdatedAt.Equal(oldUpdatedAt) {
		return false, nil
	}
	if req.Title != nil {
		t.Title = *req.Title
	}
	if req.Priority != nil {
		t.Priority = *req.Priority
	}
	if req.ProjectID != nil {
		// 与真实 repository 对齐：空串 = 取消共享（置 NULL）
		if *req.ProjectID == "" {
			t.ProjectID = nil
		} else {
			t.ProjectID = req.ProjectID
		}
	}
	t.UpdatedAt = now
	return true, nil
}

func (r *fakeRepo) Delete(id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteCalls++
	if _, ok := r.todos[id]; !ok {
		return false, nil
	}
	delete(r.todos, id)
	return true, nil
}

func (r *fakeRepo) Rollover(today string, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rolloverCalls++
	r.lastRolloverToday = today
	var n int64
	for _, t := range r.todos {
		due := (t.CreatedFor < today && (t.Status == StatusPending || t.Status == StatusDeferred)) ||
			(t.CreatedFor == today && t.Status == StatusDeferred)
		if due {
			t.CreatedFor = today
			t.Status = StatusPending
			t.UpdatedAt = now
			n++
		}
	}
	return n, nil
}

// fakeResolver 模拟注入的 issueStatusResolver（终态 issue id 集合）。
type fakeResolver struct {
	terminalIDs map[string]bool
	err         error
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{terminalIDs: map[string]bool{}}
}

func (f *fakeResolver) TerminalIssueIDs(ctx context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	ids := make([]string, 0, len(f.terminalIDs))
	for id := range f.terminalIDs {
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *fakeRepo) IssueSync(terminalIDs []string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.issueSyncCalls++
	terminal := map[string]bool{}
	for _, id := range terminalIDs {
		terminal[id] = true
	}
	var n int64
	for _, t := range r.todos {
		if (t.Status == StatusPending || t.Status == StatusDeferred) && t.IssueID != nil && terminal[*t.IssueID] {
			t.Status = StatusCancelled
			t.UpdatedAt = testNow()
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) Cleanup(createdForCutoff string, createdAtCutoff time.Time) (int64, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupCalls++
	r.lastCleanupCreatedFor = createdForCutoff
	r.lastCleanupCreatedAt = createdAtCutoff
	var dc, inf int64
	for id, t := range r.todos {
		if (t.Status == StatusDone || t.Status == StatusCancelled) && t.CreatedFor < createdForCutoff {
			delete(r.todos, id)
			dc++
		} else if (t.Status == StatusPending || t.Status == StatusDeferred) && t.CreatedAt.Before(createdAtCutoff) {
			delete(r.todos, id)
			inf++
		}
	}
	return dc, inf, nil
}

func (r *fakeRepo) OpenVisibleForUser(userID string, projectIDs []string, date string) ([]Todo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.openCalls++
	r.lastOpenUserID = userID
	r.lastOpenDate = date
	out := []Todo{}
	for _, t := range r.todos {
		if t.CreatedFor != date || t.Status == StatusDone || t.Status == StatusCancelled {
			continue
		}
		if t.CreatedBy != userID && (t.ProjectID == nil || !contains(projectIDs, *t.ProjectID)) {
			continue // 他人个人项/非成员项目共享项不可见
		}
		out = append(out, *r.copy(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *fakeRepo) InflightIssueIDs(userID string) (map[string]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inflightCalls++
	if r.inflightOverride != nil {
		return r.inflightOverride, nil
	}
	out := map[string]bool{}
	for _, t := range r.todos {
		if t.CreatedBy == userID && t.IssueID != nil && (t.Status == StatusPending || t.Status == StatusDeferred) {
			out[*t.IssueID] = true
		}
	}
	return out, nil
}

func (r *fakeRepo) CleanupHintCandidates(userID, cutoffDate string) ([]Todo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Todo{}
	for _, t := range r.todos {
		if t.CreatedBy != userID {
			continue
		}
		if (t.Status == StatusDone || t.Status == StatusCancelled) && t.CreatedFor >= cutoffDate {
			out = append(out, *r.copy(t))
		} else if (t.Status == StatusPending || t.Status == StatusDeferred) && t.CreatedAt.Format(time.DateOnly) >= cutoffDate {
			out = append(out, *r.copy(t))
		}
	}
	return append(out, r.hintCandidates...), nil
}

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

type fakeSnap struct {
	users      []UserSnapshot
	userByID   map[string]*UserSnapshot
	issues     map[string][]IssueSnapshot
	issuesErr  map[string]error
	projectIDs map[string][]string
	roles      map[string]map[string]string // userID → projectID → role

	activeUsersErr error
}

func newFakeSnap() *fakeSnap {
	return &fakeSnap{
		userByID:   map[string]*UserSnapshot{},
		issues:     map[string][]IssueSnapshot{},
		projectIDs: map[string][]string{},
		roles:      map[string]map[string]string{},
	}
}

func (s *fakeSnap) ActiveUsers() ([]UserSnapshot, error) {
	if s.activeUsersErr != nil {
		return nil, s.activeUsersErr
	}
	return s.users, nil
}

func (s *fakeSnap) UserByID(userID string) (*UserSnapshot, error) {
	return s.userByID[userID], nil
}

func (s *fakeSnap) OpenIssuesForUser(userID string) ([]IssueSnapshot, error) {
	if err := s.issuesErr[userID]; err != nil {
		return nil, err
	}
	return s.issues[userID], nil
}

func (s *fakeSnap) MyProjectIDs(userID string) ([]string, error) {
	return s.projectIDs[userID], nil
}

func (s *fakeSnap) ProjectRole(userID, projectID string) (string, bool, error) {
	if m, ok := s.roles[userID]; ok {
		if role, ok := m[projectID]; ok {
			return role, true, nil
		}
	}
	return "", false, nil
}

type fakePerm struct {
	allowed map[string]map[string]bool // projectID → userID
}

func newFakePerm() *fakePerm { return &fakePerm{allowed: map[string]map[string]bool{}} }

func (p *fakePerm) HasPermission(projectID, userID string, perm middleware.Permission) (bool, error) {
	if m, ok := p.allowed[projectID]; ok {
		return m[userID], nil
	}
	return false, nil
}

type fakeAudit struct {
	mu      sync.Mutex
	actions []string
	details []map[string]any
	err     error
}

func newFakeAudit() *fakeAudit { return &fakeAudit{} }

func (a *fakeAudit) WriteSystemAudit(ctx context.Context, action string, detail map[string]any) error {
	if a.err != nil {
		return a.err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, action)
	a.details = append(a.details, detail)
	return nil
}

func (a *fakeAudit) lastDetail() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.details) == 0 {
		return nil
	}
	return a.details[len(a.details)-1]
}

type fakeLLM struct {
	mu           sync.Mutex
	addResp      *LLMParseResponse
	addErr       error
	dailyResp    []LLMItem
	dailyErr     error
	addCalls     int
	dailyCalls   int
	lastRaw      string
	lastUser     string
	lastReport   string
	lastIssues   []IssueSnapshot
	lastExisting []string
}

func (l *fakeLLM) ParseAdd(ctx context.Context, userID, rawText string) (*LLMParseResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.addCalls++
	l.lastUser = userID
	l.lastRaw = rawText
	return l.addResp, l.addErr
}

func (l *fakeLLM) GenerateDaily(ctx context.Context, userID, report string, issues []IssueSnapshot, existingTitles []string) ([]LLMItem, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dailyCalls++
	l.lastUser = userID
	l.lastReport = report
	l.lastIssues = issues
	l.lastExisting = existingTitles
	return l.dailyResp, l.dailyErr
}

type fakeReports struct {
	mu       sync.Mutex
	report   string
	err      error
	calls    int
	lastUser string
}

func (f *fakeReports) FetchLatestReport(ctx context.Context, userID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastUser = userID
	if f.err != nil {
		return "", f.err
	}
	return f.report, nil
}

type fakeNtfy struct {
	mu       sync.Mutex
	ensured  []string
	resets   []string
	accesses []string
	err      error
}

func (n *fakeNtfy) EnsureUser(name, password string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ensured = append(n.ensured, name)
	return n.err
}

func (n *fakeNtfy) ResetPassword(name, password string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.resets = append(n.resets, name+":"+password)
	return n.err
}

func (n *fakeNtfy) EnsureAccess(topic, user, perm string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.accesses = append(n.accesses, user+"@"+topic+":"+perm)
	return n.err
}

type fakePub struct {
	mu       sync.Mutex
	topics   []string
	titles   []string
	messages []string
	errs     []error // 逐次返回错误，用完返回最后一个
	calls    int
}

func (p *fakePub) Publish(topic, title, message string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.topics = append(p.topics, topic)
	p.titles = append(p.titles, title)
	p.messages = append(p.messages, message)
	if len(p.errs) > 0 {
		err := p.errs[min(p.calls-1, len(p.errs)-1)]
		return err
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type testDeps struct {
	repo     *fakeRepo
	resolver *fakeResolver
	snap     *fakeSnap
	perm     *fakePerm
	audit    *fakeAudit
	llm      *fakeLLM
	reports  *fakeReports
	ntfy     *fakeNtfy
	pub      *fakePub
}

func newTestDeps() *testDeps {
	return &testDeps{
		repo:     newFakeRepo(),
		resolver: newFakeResolver(),
		snap:     newFakeSnap(),
		perm:     newFakePerm(),
		audit:    newFakeAudit(),
		llm:      &fakeLLM{},
		reports:  &fakeReports{},
		ntfy:     &fakeNtfy{},
		pub:      &fakePub{},
	}
}

func (d *testDeps) service() *Service {
	svc := NewService(d.repo, d.resolver, d.snap, d.perm, d.audit, d.llm, d.reports, d.ntfy, d.pub, testLoc, testNow)
	svc.publishRetryDelay = 0
	return svc
}

func (d *testDeps) addUser(id, username string, role string) {
	d.snap.userByID[id] = &UserSnapshot{ID: id, Username: username, DisplayName: "显示名" + id}
	d.snap.users = append(d.snap.users, UserSnapshot{ID: id, Username: username})
}

// ---------- CRUD ----------

func TestCreate(t *testing.T) {
	d := newTestDeps()
	svc := d.service()

	item, err := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "  写日报  "})
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "写日报" || item.Priority != PriorityMedium || item.Status != StatusPending || item.Source != SourceManual || item.CreatedFor != "2026-08-07" {
		t.Fatalf("unexpected created todo: %+v", item)
	}
}

func TestCreateAgentRejected(t *testing.T) {
	d := newTestDeps()
	_, err := d.service().Create("u1", auth.RoleAgent, CreateRequest{Title: "x"})
	if !errors.Is(err, ErrAgentRejected) {
		t.Fatalf("expected ErrAgentRejected, got %v", err)
	}
}

func TestCreateInvalidTitle(t *testing.T) {
	d := newTestDeps()
	for _, title := range []string{"", "   ", "\n\t"} {
		if _, err := d.service().Create("u1", auth.RoleMember, CreateRequest{Title: title}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("title %q: expected ErrInvalidInput, got %v", title, err)
		}
	}
}

func TestCreateTitleCleaning(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	item, err := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "a\x00b\r\nc"})
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "abc" {
		t.Fatalf("control chars not stripped: %q", item.Title)
	}
}

func TestCreateProjectForbidden(t *testing.T) {
	d := newTestDeps()
	pid := "p1"
	d.perm.allowed["p1"] = map[string]bool{}
	_, err := d.service().Create("u1", auth.RoleMember, CreateRequest{Title: "x", ProjectID: &pid})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestCreateProjectAllowed(t *testing.T) {
	d := newTestDeps()
	d.perm.allowed["p1"] = map[string]bool{"u1": true}
	pid := "p1"
	item, err := d.service().Create("u1", auth.RoleMember, CreateRequest{Title: "x", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	if item.ProjectID == nil || *item.ProjectID != "p1" {
		t.Fatalf("expected project p1, got %+v", item.ProjectID)
	}
}

// ---------- 权限矩阵 ----------

// helper：u1 建共享待办（p1），返回 id
func (d *testDeps) sharedTodo(ownerID string) string {
	d.perm.allowed["p1"] = map[string]bool{ownerID: true}
	pid := "p1"
	item, err := d.service().Create(ownerID, auth.RoleMember, CreateRequest{Title: "共享待办", ProjectID: &pid})
	if err != nil {
		panic(err)
	}
	return item.ID
}

func TestPermissionMatrix(t *testing.T) {
	d := newTestDeps()
	d.addUser("u1", "alice", auth.RoleMember)
	d.addUser("u2", "bob", auth.RoleMember)
	d.addUser("u3", "carol", auth.RoleViewer)
	d.snap.roles["u2"] = map[string]string{"p1": auth.RoleMember}
	d.snap.roles["u3"] = map[string]string{"p1": auth.RoleViewer}
	id := d.sharedTodo("u1")

	svc := d.service()

	// 非成员 → 404（done/defer/edit/delete）
	if _, err := svc.Done(id, "u9", auth.RoleMember); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member done: expected 404, got %v", err)
	}
	if _, err := svc.Defer(id, "u9", auth.RoleMember); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member defer: expected 404, got %v", err)
	}
	if _, err := svc.Edit(id, "u9", auth.RoleMember, UpdateRequest{UpdatedAt: &time.Time{}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member edit: expected 404, got %v", err)
	}
	if err := svc.Delete(id, "u9", auth.RoleMember); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member delete: expected 404, got %v", err)
	}

	// viewer 只读：done 403、defer 403、edit 403、delete 403
	if _, err := svc.Done(id, "u3", auth.RoleViewer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer done: expected 403, got %v", err)
	}
	if _, err := svc.Defer(id, "u3", auth.RoleViewer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer defer: expected 403, got %v", err)
	}
	if _, err := svc.Edit(id, "u3", auth.RoleViewer, UpdateRequest{UpdatedAt: &time.Time{}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer edit: expected 403, got %v", err)
	}
	if err := svc.Delete(id, "u3", auth.RoleViewer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer delete: expected 403, got %v", err)
	}

	// 共享项成员：done 可以；defer/edit/delete 403（仅添加者）
	if _, err := svc.Done(id, "u2", auth.RoleMember); err != nil {
		t.Fatalf("member done: %v", err)
	}
	id2 := d.sharedTodo("u1")
	if _, err := svc.Defer(id2, "u2", auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member defer: expected 403, got %v", err)
	}
	if _, err := svc.Edit(id2, "u2", auth.RoleMember, UpdateRequest{UpdatedAt: &time.Time{}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member edit: expected 403, got %v", err)
	}
	if err := svc.Delete(id2, "u2", auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member delete: expected 403, got %v", err)
	}

	// 成员被移除后不可见（404）
	delete(d.snap.roles, "u2")
	if _, err := svc.Done(id2, "u2", auth.RoleMember); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed member done: expected 404, got %v", err)
	}
}

// ---------- 状态机 ----------

func TestDoneStateMachine(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	created, _ := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "x"})
	id := created.ID
	item, err := svc.Done(id, "u1", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != StatusDone || item.CompletedBy == nil || *item.CompletedBy != "u1" || item.CompletedAt == nil {
		t.Fatalf("unexpected done todo: %+v", item)
	}
	// 对 done 项再 done → 409 state_conflict
	if _, err := svc.Done(id, "u1", auth.RoleMember); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("double done: expected state_conflict, got %v", err)
	}
}

func TestDeferStateMachine(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	created, _ := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "x"})
	id := created.ID
	item, err := svc.Defer(id, "u1", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != StatusDeferred || item.CreatedFor != "2026-08-08" {
		t.Fatalf("unexpected deferred todo: %+v", item)
	}
	// deferred 不能再次 defer → 409
	if _, err := svc.Defer(id, "u1", auth.RoleMember); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("defer on deferred: expected state_conflict, got %v", err)
	}
	// 次日（08-08）rollover：created_for=today AND deferred → 归一 pending
	svc.now = func() time.Time { return time.Date(2026, 8, 8, 9, 0, 0, 0, testLoc) }
	if err := svc.RunRollover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d.repo.rolloverCalls != 1 {
		t.Fatalf("expected 1 rollover call, got %d", d.repo.rolloverCalls)
	}
	item, _ = d.repo.GetByID(id)
	if item.Status != StatusPending || item.CreatedFor != "2026-08-08" {
		t.Fatalf("next-day rollover failed: %+v", item)
	}
	// 过期 pending 也顺延
	created2, _ := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "y"})
	id2 := created2.ID
	d.repo.todos[id2].CreatedFor = "2026-08-05"
	if err := svc.RunRollover(context.Background()); err != nil {
		t.Fatal(err)
	}
	item, _ = d.repo.GetByID(id2)
	if item.Status != StatusPending || item.CreatedFor != "2026-08-08" {
		t.Fatalf("expired rollover failed: %+v", item)
	}
	// 已 done 不顺延
	created3, _ := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "z"})
	id3 := created3.ID
	d.repo.todos[id3].CreatedFor = "2026-08-01"
	d.repo.todos[id3].Status = StatusDone
	_ = svc.RunRollover(context.Background())
	item, _ = d.repo.GetByID(id3)
	if item.Status != StatusDone {
		t.Fatalf("done must not rollover, got %+v", item)
	}
}

func TestEditOptimisticLock(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	created, _ := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "旧标题", Priority: PriorityLow})
	id := created.ID
	item, _ := d.repo.GetByID(id)
	stale := item.UpdatedAt.Add(-time.Hour)

	// 旧 updated_at → 409 version_conflict
	title := "新标题"
	if _, err := svc.Edit(id, "u1", auth.RoleMember, UpdateRequest{UpdatedAt: &stale, Title: &title}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale updated_at: expected version_conflict, got %v", err)
	}
	// 当前 updated_at → 成功
	curr := item.UpdatedAt
	edited, err := svc.Edit(id, "u1", auth.RoleMember, UpdateRequest{UpdatedAt: &curr, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Title != "新标题" {
		t.Fatalf("edit failed: %+v", edited)
	}
	// 缺 updated_at → 400
	if _, err := svc.Edit(id, "u1", auth.RoleMember, UpdateRequest{Title: &title}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing updated_at: expected ErrInvalidInput, got %v", err)
	}
	// 编辑 priority clamp
	bad := "urgent"
	curr = edited.UpdatedAt
	edited, err = svc.Edit(id, "u1", auth.RoleMember, UpdateRequest{UpdatedAt: &curr, Priority: &bad})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Priority != PriorityMedium {
		t.Fatalf("priority not clamped: %s", edited.Priority)
	}
}

func TestDelete(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	created, _ := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "x"})
	id := created.ID
	if err := svc.Delete(id, "u1", auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := d.repo.GetByID(id); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.repo.GetByID(id); got != nil {
		t.Fatalf("todo not deleted: %+v", got)
	}
	// 不存在 → 404
	if err := svc.Delete("t999", "u1", auth.RoleMember); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: expected 404, got %v", err)
	}
}

// ---------- LLM ----------

func TestParseLLM(t *testing.T) {
	d := newTestDeps()
	d.llm.addResp = &LLMParseResponse{Status: "ok", Title: " 调 试 真空泵 ", Priority: PriorityHigh}
	svc := d.service()
	resp, err := svc.ParseLLM(context.Background(), "u1", auth.RoleMember, "帮我调一下真空泵")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != "ok" || resp.Title != "调 试 真空泵" || resp.Priority != PriorityHigh {
		t.Fatalf("unexpected parse resp: %+v", resp)
	}
}

func TestParseLLMFailureDegrades(t *testing.T) {
	d := newTestDeps()
	d.llm.addErr = errors.New("上游挂了")
	svc := d.service()
	resp, err := svc.ParseLLM(context.Background(), "u1", auth.RoleMember, "  检查液氦   ")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != "ok" || resp.Title != "检查液氦" || resp.Priority != PriorityMedium || resp.Reason == nil {
		t.Fatalf("expected degrade response, got %+v", resp)
	}
}

func TestParseLLMRejected(t *testing.T) {
	d := newTestDeps()
	reason := "注入文本"
	d.llm.addResp = &LLMParseResponse{Status: "rejected", Title: "rm -rf", Reason: &reason}
	resp, err := d.service().ParseLLM(context.Background(), "u1", auth.RoleMember, "rm -rf /")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != "rejected" || resp.Title != "rm -rf" {
		t.Fatalf("unexpected rejected resp: %+v", resp)
	}
}

func TestParseLLMInvalidInput(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	if _, err := svc.ParseLLM(context.Background(), "u1", auth.RoleMember, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty raw: expected ErrInvalidInput, got %v", err)
	}
	long := strings.Repeat("a", 2001)
	if _, err := svc.ParseLLM(context.Background(), "u1", auth.RoleMember, long); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("long raw: expected ErrInvalidInput, got %v", err)
	}
}

func TestParseLLMRateLimit(t *testing.T) {
	d := newTestDeps()
	d.llm.addResp = &LLMParseResponse{Status: "ok", Title: "x"}
	svc := d.service()
	var err error
	for i := 0; i < 10; i++ {
		_, err = svc.ParseLLM(context.Background(), "u1", auth.RoleMember, "任务")
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}
	// 第 11 次 → 429
	if _, err := svc.ParseLLM(context.Background(), "u1", auth.RoleMember, "任务"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate limited, got %v", err)
	}
	// 其他用户不受影响
	if _, err := svc.ParseLLM(context.Background(), "u2", auth.RoleMember, "任务"); err != nil {
		t.Fatalf("other user affected by rate limit: %v", err)
	}
}

func TestLLMAdd(t *testing.T) {
	d := newTestDeps()
	item, err := d.service().LLMAdd("u1", auth.RoleMember, LLMAddRequest{DraftID: ptr("d1"), Title: " 写 报告 ", Priority: PriorityHigh})
	if err != nil {
		t.Fatal(err)
	}
	if item.Source != SourceLLM || item.Title != "写 报告" || item.Priority != PriorityHigh {
		t.Fatalf("unexpected llm add: %+v", item)
	}
}

func TestLLMAddAgentRejected(t *testing.T) {
	d := newTestDeps()
	if _, err := d.service().LLMAdd("u1", auth.RoleAgent, LLMAddRequest{Title: "x"}); !errors.Is(err, ErrAgentRejected) {
		t.Fatalf("expected ErrAgentRejected, got %v", err)
	}
}

// ---------- List ----------

func TestListSemantics(t *testing.T) {
	d := newTestDeps()
	d.addUser("u1", "alice", auth.RoleMember)
	d.snap.projectIDs["u1"] = []string{"p1"}
	d.perm.allowed["p1"] = map[string]bool{"u1": true}
	svc := d.service()
	pid := "p1"
	// u1 私有 + p1 共享；u2 私有
	svc.Create("u1", auth.RoleMember, CreateRequest{Title: "私有一"})
	svc.Create("u1", auth.RoleMember, CreateRequest{Title: "共享一", ProjectID: &pid})
	svc.Create("u2", auth.RoleMember, CreateRequest{Title: "他人私有"})

	// scope=mine：只有自己创建
	items, err := svc.List("u1", ListParams{Date: "2026-08-07", Scope: ScopeMine, Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("scope=mine expected 2, got %d", len(items))
	}

	// scope=all：自己 + 共享可见
	items, err = svc.List("u1", ListParams{Date: "2026-08-07", Scope: ScopeAll, Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("scope=all expected 2, got %d", len(items))
	}

	// 非成员 scope=shared → 200 空列表（无 projectIDs → 恒假）
	d.snap.projectIDs["u9"] = nil
	items, err = svc.List("u9", ListParams{Date: "2026-08-07", Scope: ScopeShared, Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("non-member shared expected empty, got %d", len(items))
	}

	// 成员 scope=shared：只含共享项（u2 是 p1 成员且有个人项）
	d.snap.projectIDs["u2"] = []string{"p1"}
	items, err = svc.List("u2", ListParams{Date: "2026-08-07", Scope: ScopeShared, Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "共享一" {
		t.Fatalf("member shared expected only shared item, got %+v", items)
	}

	// 默认 date=today、默认 scope=all
	items, err = svc.List("u1", ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if d.repo.lastListParams.Date != "2026-08-07" || d.repo.lastListParams.Scope != ScopeAll {
		t.Fatalf("defaults not applied: %+v", d.repo.lastListParams)
	}

	// 非法输入
	if _, err := svc.List("u1", ListParams{Date: "not-a-date"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad date: expected ErrInvalidInput, got %v", err)
	}
	if _, err := svc.List("u1", ListParams{Scope: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad scope: expected ErrInvalidInput, got %v", err)
	}

	// nil 结果归一为空数组
	d.repo.listErr = nil
	d.repo.todos = map[string]*Todo{}
	items, err = svc.List("u1", ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("expected empty non-nil slice, got %#v", items)
	}
}

// ---------- NotificationTopic / Provision / Redeem ----------

func TestNotificationTopic(t *testing.T) {
	d := newTestDeps()
	topic, err := d.service().NotificationTopic("u1")
	if err != nil {
		t.Fatal(err)
	}
	want := topicForUser("u1")
	if topic.Topic != want || topic.SubscribeURL == "" {
		t.Fatalf("unexpected topic: %+v", topic)
	}
}

func TestProvisionRedeemFlow(t *testing.T) {
	d := newTestDeps()
	d.addUser("u1", "alice", auth.RoleMember)
	svc := d.service()

	prov, err := svc.Provision("u1", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if prov.ProvisionToken == "" || prov.ExpiresAt.IsZero() {
		t.Fatalf("unexpected provision: %+v", prov)
	}
	if len(d.ntfy.resets) != 1 || !strings.HasPrefix(d.ntfy.resets[0], "todo-alice:") {
		t.Fatalf("expected reset password for todo-alice, got %v", d.ntfy.resets)
	}

	redeem, err := svc.Redeem("u1", auth.RoleMember, prov.ProvisionToken)
	if err != nil {
		t.Fatal(err)
	}
	if redeem.Username != "todo-alice" || redeem.Password == "" || redeem.Topic != topicForUser("u1") {
		t.Fatalf("unexpected redeem: %+v", redeem)
	}

	// 兑换即作废：二次兑换 → invalid
	if _, err := svc.Redeem("u1", auth.RoleMember, prov.ProvisionToken); !errors.Is(err, ErrInvalidProvisionToken) {
		t.Fatalf("second redeem should fail, got %v", err)
	}

	// 再次 provision 作废旧 token
	prov2, err := svc.Provision("u1", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redeem("u1", auth.RoleMember, prov.ProvisionToken); err == nil {
		t.Fatal("old token should be invalid after re-provision")
	}
	if _, err := svc.Redeem("u1", auth.RoleMember, prov2.ProvisionToken); err != nil {
		t.Fatalf("new token should work: %v", err)
	}
}

func TestRedeemTokenBoundToUser(t *testing.T) {
	d := newTestDeps()
	d.addUser("u1", "alice", auth.RoleMember)
	d.addUser("u2", "bob", auth.RoleMember)
	svc := d.service()

	prov, err := svc.Provision("u1", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	// token 归属绑定：其他登录用户持 token 兑换 → 401（防泄漏后被冒用）
	if _, err := svc.Redeem("u2", auth.RoleMember, prov.ProvisionToken); !errors.Is(err, ErrInvalidProvisionToken) {
		t.Fatalf("cross-user redeem must fail, got %v", err)
	}
	// 一次性语义：冒用尝试即焚毁 token，归属用户需重新 provision
	if _, err := svc.Redeem("u1", auth.RoleMember, prov.ProvisionToken); !errors.Is(err, ErrInvalidProvisionToken) {
		t.Fatalf("burned token must fail, got %v", err)
	}
}

func TestEditClearSharing(t *testing.T) {
	d := newTestDeps()
	d.addUser("u1", "alice", auth.RoleMember)
	d.perm.allowed["p1"] = map[string]bool{"u1": true}
	svc := d.service()

	pid := "p1"
	item, err := svc.Create("u1", auth.RoleMember, CreateRequest{Title: "共享任务", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	if item.ProjectID == nil || *item.ProjectID != "p1" {
		t.Fatalf("expected shared todo, got %+v", item)
	}
	// 空串 = 取消共享（置 NULL）
	empty := ""
	updated, err := svc.Edit(item.ID, "u1", auth.RoleMember, UpdateRequest{UpdatedAt: &item.UpdatedAt, ProjectID: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ProjectID != nil {
		t.Fatalf("empty project_id must clear sharing, got %+v", updated.ProjectID)
	}
	// 字段缺席 = 不变
	updated2, err := svc.Edit(item.ID, "u1", auth.RoleMember, UpdateRequest{UpdatedAt: &updated.UpdatedAt, ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	title := "改标题"
	updated3, err := svc.Edit(item.ID, "u1", auth.RoleMember, UpdateRequest{UpdatedAt: &updated2.UpdatedAt, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if updated3.ProjectID == nil || *updated3.ProjectID != "p1" {
		t.Fatalf("absent project_id must keep sharing, got %+v", updated3.ProjectID)
	}
	if updated3.Title != "改标题" {
		t.Fatalf("title not updated: %+v", updated3)
	}
}

func TestListInvalidStatus(t *testing.T) {
	d := newTestDeps()
	if _, err := d.service().List("u1", ListParams{Status: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid status must be rejected, got %v", err)
	}
}

func TestProvisionUserNotFound(t *testing.T) {
	d := newTestDeps()
	if _, err := d.service().Provision("ghost", auth.RoleMember); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestProvisionRedeemRateLimit(t *testing.T) {
	d := newTestDeps()
	d.addUser("u1", "alice", auth.RoleMember)
	svc := d.service()
	times := make([]time.Time, 10)
	for i := range times {
		times[i] = testNow()
	}
	svc.rlCalls["u1"] = times
	if _, err := svc.Provision("u1", auth.RoleMember); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("provision expected rate limited, got %v", err)
	}
	if _, err := svc.Redeem("u1", auth.RoleMember, "token"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("redeem expected rate limited, got %v", err)
	}
}

func TestProvisionAgentRejected(t *testing.T) {
	d := newTestDeps()
	if _, err := d.service().Provision("u1", auth.RoleAgent); !errors.Is(err, ErrAgentRejected) {
		t.Fatalf("expected ErrAgentRejected, got %v", err)
	}
}

// ---------- 工具 ----------

func TestCleanTitle(t *testing.T) {
	long := strings.Repeat("长", 300)
	got := cleanTitle(long)
	// VARCHAR(256) 按字符计：按 rune 截断，且必须是合法 UTF-8（不允许切断多字节序列）
	if utf8.RuneCountInString(got) != 256 || !utf8.ValidString(got) {
		t.Fatalf("expected 256 valid-UTF8 runes, got %d runes (valid=%v)", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
	if got := cleanTitle("  a\x01b  "); got != "ab" {
		t.Fatalf("unexpected cleanTitle: %q", got)
	}
}

func TestClampPriority(t *testing.T) {
	if clampPriority("high") != PriorityHigh || clampPriority("LOW") != PriorityMedium || clampPriority("weird") != PriorityMedium {
		t.Fatal("clampPriority failed")
	}
}

func TestSeverityToPriority(t *testing.T) {
	cases := map[string]string{
		"critical": PriorityHigh, "high": PriorityHigh,
		"medium": PriorityMedium, "normal": PriorityMedium, "": PriorityMedium,
		"low": PriorityLow,
	}
	for sev, want := range cases {
		if got := severityToPriority(sev); got != want {
			t.Fatalf("severity %q: got %s want %s", sev, got, want)
		}
	}
}

func TestTopicForUserDeterministic(t *testing.T) {
	if topicForUser("u1") != topicForUser("u1") {
		t.Fatal("topic must be deterministic")
	}
	if !strings.HasPrefix(topicForUser("u1"), "lab-todos-") {
		t.Fatalf("unexpected topic: %s", topicForUser("u1"))
	}
}

func TestAllowOneWindow(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	// 窗口内有 10 次调用 → 拒绝
	times := make([]time.Time, 10)
	for i := range times {
		times[i] = testNow()
	}
	svc.rlCalls["u1"] = times
	if svc.allowOne("u1") {
		t.Fatal("expected rate limited inside window")
	}
	// 2 分钟后窗口过期 → 放行
	svc.now = func() time.Time { return testNow().Add(2 * time.Minute) }
	svc.rlCalls["u1"] = nil
	if !svc.allowOne("u1") {
		t.Fatal("should pass after window expires")
	}
}

func ptr(s string) *string { return &s }
