package projects

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
)

// 集成测试：需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移）。
// projects.Service 的 repo 是具体 *Repository，无法注入 mock，故 service 层 CRUD/流转
// 走真实 PostgreSQL（与 auth/repository_db_test.go 同模式）；无 TEST_DATABASE_URL 时跳过。
// 固定 UUID 种子（ON CONFLICT DO NOTHING）+ t.Cleanup 清理，CI 以 -p 1 串行跑避免撞种子。

const (
	dbAdminUserID    = "00000000-0000-0000-0000-00000000e001"
	dbOwnerUserID    = "00000000-0000-0000-0000-00000000e002"
	dbMemberUserID   = "00000000-0000-0000-0000-00000000e003"
	dbOutsiderUserID = "00000000-0000-0000-0000-00000000e004"

	dbDraftProjectID     = "b0000000-0000-4000-8000-000000000201"
	dbCompletedProjectID = "b0000000-0000-4000-8000-000000000202"
	dbArchivedProjectID  = "b0000000-0000-4000-8000-000000000203"
)

func openProjectsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// 种子用户：admin / owner（可升 maintainer 语义用 member 行区分）/ member / 无成员关系的 outsider
	for _, u := range []struct {
		id       string
		username string
		role     string
	}{
		{dbAdminUserID, "projects_dbtest_admin", "admin"},
		{dbOwnerUserID, "projects_dbtest_owner", "member"},
		{dbMemberUserID, "projects_dbtest_member", "member"},
		{dbOutsiderUserID, "projects_dbtest_outsider", "member"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Projects DB Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}

	// 种子项目：draft（owner 为 e002）/ completed / archived（时间戳按真实流转语义补齐）
	for _, p := range []struct {
		id          string
		code        string
		status      string
		completedAt bool
		archivedAt  bool
	}{
		{dbDraftProjectID, "PRJ_DBTEST_DRAFT", StatusDraft, false, false},
		{dbCompletedProjectID, "PRJ_DBTEST_COMPLETED", StatusCompleted, true, false},
		{dbArchivedProjectID, "PRJ_DBTEST_ARCHIVED", StatusArchived, false, true},
	} {
		completedAt := "NULL"
		if p.completedAt {
			completedAt = "now()"
		}
		archivedAt := "NULL"
		if p.archivedAt {
			archivedAt = "now()"
		}
		if _, err := db.Exec(
			`INSERT INTO projects (id, code, name, status, owner_user_id, created_by, completed_at, archived_at)
			 VALUES ($1, $2, 'DB 种子项目', $3, $4, $4, `+completedAt+`, `+archivedAt+`)
			 ON CONFLICT (id) DO NOTHING`, p.id, p.code, p.status, dbOwnerUserID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 VALUES ($1, $2, 'owner', 'active', $2), ($1, $3, 'member', 'active', $2)
		 ON CONFLICT (project_id, user_id) DO NOTHING`, dbDraftProjectID, dbOwnerUserID, dbMemberUserID); err != nil {
		t.Fatal(err)
	}
	for _, projectID := range []string{dbCompletedProjectID, dbArchivedProjectID} {
		if _, err := db.Exec(
			`INSERT INTO project_members (project_id, user_id, role, status, added_by)
			 VALUES ($1, $2, 'owner', 'active', $2)
			 ON CONFLICT (project_id, user_id) DO NOTHING`, projectID, dbOwnerUserID); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM projects WHERE code LIKE 'PRJ_DBTEST_%'`)
		db.Exec(`DELETE FROM projects WHERE owner_user_id IN ($1,$2,$3,$4)`, dbAdminUserID, dbOwnerUserID, dbMemberUserID, dbOutsiderUserID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4)`, dbAdminUserID, dbOwnerUserID, dbMemberUserID, dbOutsiderUserID)
	})
	return db
}

func uniqueProjectCode(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("PRJ_DBTEST_%d", time.Now().UnixNano())
}

type fakeIssueCounter struct {
	open  int
	err   error
	calls int
}

func (f *fakeIssueCounter) CountOpenByProject(projectID string) (int, error) {
	f.calls++
	return f.open, f.err
}

func TestDBCreateProject(t *testing.T) {
	db := openProjectsTestDB(t)
	svc := NewService(NewRepository(db), nil, nil)

	// 空 code / name → ErrInvalidInput（maintainer 角色）
	for name, req := range map[string]CreateProjectRequest{
		"empty code": {Code: "  ", Name: "项目"},
		"empty name": {Code: "X1", Name: "  "},
	} {
		if _, err := svc.Create(req, dbOwnerUserID, auth.RoleMaintainer); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: got %v, want ErrInvalidInput", name, err)
		}
	}

	// 非法日期 → ErrInvalidInput
	badDate := "2026/01/01"
	if _, err := svc.Create(CreateProjectRequest{Code: uniqueProjectCode(t), Name: "项目", StartDate: &badDate}, dbOwnerUserID, auth.RoleMaintainer); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad date: got %v, want ErrInvalidInput", err)
	}

	// 非法 visibility → ErrInvalidInput
	if _, err := svc.Create(CreateProjectRequest{Code: uniqueProjectCode(t), Name: "项目", Visibility: "public"}, dbOwnerUserID, auth.RoleMaintainer); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad visibility: got %v, want ErrInvalidInput", err)
	}

	// code 已占用 → ErrCodeTaken
	if _, err := svc.Create(CreateProjectRequest{Code: "PRJ_DBTEST_DRAFT", Name: "重复"}, dbOwnerUserID, auth.RoleMaintainer); !errors.Is(err, ErrCodeTaken) {
		t.Fatalf("taken code: got %v, want ErrCodeTaken", err)
	}

	// 成功：默认 draft/restricted/members，owner 成员自动落库
	code := uniqueProjectCode(t)
	start := "2026-08-01"
	created, err := svc.Create(CreateProjectRequest{
		Code: code, Name: "  集成创建项目  ", ShortName: "集成", Description: "desc",
		StartDate: &start, Tags: []string{"a", "a", "b"},
	}, dbOwnerUserID, auth.RoleMaintainer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM projects WHERE id = $1`, created.ID) })
	if created.Status != StatusDraft || created.Visibility != VisibilityRestricted || created.CommentPolicy != CommentPolicyMembers {
		t.Fatalf("defaults: %+v", created)
	}
	if created.Name != "集成创建项目" {
		t.Fatalf("name trim: %q", created.Name)
	}
	member, err := NewRepository(db).GetMember(created.ID, dbOwnerUserID)
	if err != nil || member == nil || member.Role != RoleOwner {
		t.Fatalf("auto owner member: %+v err=%v", member, err)
	}
	stats, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.MemberCount != 1 || stats.OpenIssueCount != 0 || stats.LogCount != 0 {
		t.Fatalf("stats: %+v", stats)
	}
}

func TestDBGetByIDAndList(t *testing.T) {
	db := openProjectsTestDB(t)
	svc := NewService(NewRepository(db), nil, nil)

	// 404 语义：不存在 → ErrProjectNotFound
	if _, err := svc.GetByID("b0000000-0000-4000-8000-000000009999"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing project: got %v, want ErrProjectNotFound", err)
	}

	// 成员可见自己的项目
	got, err := svc.GetByID(dbDraftProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != dbDraftProjectID || got.Status != StatusDraft || got.MemberCount != 2 {
		t.Fatalf("get: %+v", got)
	}

	// List：member 只看到成员项目；admin 全量；非法 status 拒绝
	memberList, err := svc.List(dbMemberUserID, auth.RoleMember, "")
	if err != nil {
		t.Fatal(err)
	}
	foundDraft := false
	for _, p := range memberList {
		if p.ID == dbDraftProjectID {
			foundDraft = true
		}
		if p.ID == dbArchivedProjectID {
			t.Fatalf("member list must not include non-member project")
		}
	}
	if !foundDraft {
		t.Fatalf("member list missing draft project: %+v", memberList)
	}
	adminList, err := svc.List(dbAdminUserID, auth.RoleAdmin, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(adminList) < 3 {
		t.Fatalf("admin list too short: %d", len(adminList))
	}
	if _, err := svc.List(dbMemberUserID, auth.RoleMember, "bogus"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad status filter: got %v, want ErrInvalidInput", err)
	}
	completed, err := svc.List(dbAdminUserID, auth.RoleAdmin, StatusCompleted)
	if err != nil {
		t.Fatal(err)
	}
	if len(completed) != 1 || completed[0].ID != dbCompletedProjectID {
		t.Fatalf("completed filter: %+v", completed)
	}
}

func TestDBUpdateProject(t *testing.T) {
	db := openProjectsTestDB(t)
	svc := NewService(NewRepository(db), nil, nil)

	// 不存在 → ErrProjectNotFound
	if _, err := svc.Update("b0000000-0000-4000-8000-000000009999", UpdateProjectRequest{}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("update missing: got %v, want ErrProjectNotFound", err)
	}

	// 非法日期 → ErrInvalidInput
	badDate := "not-a-date"
	if _, err := svc.Update(dbDraftProjectID, UpdateProjectRequest{StartDate: &badDate}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad date: got %v, want ErrInvalidInput", err)
	}

	// 空 name / 非法 visibility / 非法 comment_policy → ErrInvalidInput
	emptyName := "  "
	if _, err := svc.Update(dbDraftProjectID, UpdateProjectRequest{Name: &emptyName}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty name: got %v, want ErrInvalidInput", err)
	}
	public := "public"
	if _, err := svc.Update(dbDraftProjectID, UpdateProjectRequest{Visibility: &public}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad visibility: got %v, want ErrInvalidInput", err)
	}
	policy := "none"
	if _, err := svc.Update(dbDraftProjectID, UpdateProjectRequest{CommentPolicy: &policy}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad comment policy: got %v, want ErrInvalidInput", err)
	}

	// 成功：改名 + 改 visibility + 清空日期
	newName := "集成改名"
	newVis := VisibilityWorkspace
	emptyDate := ""
	updated, err := svc.Update(dbDraftProjectID, UpdateProjectRequest{
		Name: &newName, Visibility: &newVis, StartDate: &emptyDate, Tags: []string{"t1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != newName || updated.Visibility != newVis || updated.StartDate != nil {
		t.Fatalf("updated: %+v", updated)
	}
}

func TestDBTransitionStatus(t *testing.T) {
	db := openProjectsTestDB(t)
	repo := NewRepository(db)
	svc := NewService(repo, nil, nil)

	// 不存在 → ErrProjectNotFound
	if _, _, err := svc.TransitionStatus("b0000000-0000-4000-8000-000000009999", StatusTransitionRequest{Action: "activate"}, dbOwnerUserID, auth.RoleMember); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing: got %v, want ErrProjectNotFound", err)
	}

	// 倒退操作 deactivate/reopen 仅 admin → 非 admin 403
	if _, _, err := svc.TransitionStatus(dbDraftProjectID, StatusTransitionRequest{Action: "deactivate"}, dbOwnerUserID, auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("deactivate as member: got %v, want ErrForbidden", err)
	}

	// 非 owner 成员 → 403（即便动作本身合法）
	if _, _, err := svc.TransitionStatus(dbDraftProjectID, StatusTransitionRequest{Action: "activate"}, dbMemberUserID, auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("activate as member: got %v, want ErrForbidden", err)
	}

	// owner 合法流转：draft → active → completed（无 open issues，无警告）
	activated, warnings, err := svc.TransitionStatus(dbDraftProjectID, StatusTransitionRequest{Action: "activate"}, dbOwnerUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if activated.Status != StatusActive || len(warnings) != 0 {
		t.Fatalf("activated: %+v warnings=%v", activated, warnings)
	}
	completed, _, err := svc.TransitionStatus(dbDraftProjectID, StatusTransitionRequest{Action: "complete"}, dbOwnerUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed: %+v", completed)
	}

	// completed → archive → reactivate（reactivate 需填 reason）
	archived, _, err := svc.TransitionStatus(dbCompletedProjectID, StatusTransitionRequest{Action: "archive"}, dbOwnerUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if archived.Status != StatusArchived || archived.ArchivedAt == nil {
		t.Fatalf("archived: %+v", archived)
	}
	if _, _, err := svc.TransitionStatus(dbArchivedProjectID, StatusTransitionRequest{Action: "reactivate"}, dbOwnerUserID, auth.RoleMember); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("reactivate without reason: got %v, want ErrInvalidInput", err)
	}
	reason := "重新启用"
	reactivated, _, err := svc.TransitionStatus(dbArchivedProjectID, StatusTransitionRequest{Action: "reactivate", Reason: reason}, dbOwnerUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if reactivated.Status != StatusActive {
		t.Fatalf("reactivated: %+v", reactivated)
	}

	// admin 倒退：active → draft（deactivate）
	draftAgain, _, err := svc.TransitionStatus(dbArchivedProjectID, StatusTransitionRequest{Action: "deactivate"}, dbAdminUserID, auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if draftAgain.Status != StatusDraft {
		t.Fatalf("deactivated: %+v", draftAgain)
	}

	// 非法流转：completed 上再 activate → ErrInvalidTransition
	if _, _, err := svc.TransitionStatus(dbCompletedProjectID, StatusTransitionRequest{Action: "activate"}, dbAdminUserID, auth.RoleAdmin); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid transition: got %v, want ErrInvalidTransition", err)
	}

	// active → completed 且有 open issues：默认 ErrTransitionWarning；ignore_warnings=true 放行
	issues := &fakeIssueCounter{open: 3}
	svcWithIssues := NewService(repo, issues, nil)
	activated2, _, err := svcWithIssues.TransitionStatus(dbArchivedProjectID, StatusTransitionRequest{Action: "activate"}, dbOwnerUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if activated2.Status != StatusActive {
		t.Fatalf("re-activate: %+v", activated2)
	}
	_, warnings, err = svcWithIssues.TransitionStatus(dbArchivedProjectID, StatusTransitionRequest{Action: "complete"}, dbOwnerUserID, auth.RoleMember)
	if !errors.Is(err, ErrTransitionWarning) || len(warnings) != 1 || warnings[0].Code != "open_issues" || warnings[0].Count != 3 {
		t.Fatalf("warning path: err=%v warnings=%v", err, warnings)
	}
	completed2, warnings2, err := svcWithIssues.TransitionStatus(dbArchivedProjectID, StatusTransitionRequest{Action: "complete", IgnoreWarnings: true}, dbOwnerUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if completed2.Status != StatusCompleted || len(warnings2) != 1 {
		t.Fatalf("ignore warnings: %+v warnings=%v", completed2, warnings2)
	}

	// issueCounter 出错 → 错误透传（用新创建的项目，保证处于 active 可 complete 的状态）
	issueErr := errors.New("counter boom")
	svcErr := NewService(repo, &fakeIssueCounter{err: issueErr}, nil)
	code := uniqueProjectCode(t)
	fresh, err := svcErr.Create(CreateProjectRequest{Code: code, Name: "计数器失败"}, dbOwnerUserID, auth.RoleMaintainer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM projects WHERE id = $1`, fresh.ID) })
	if _, _, err := svcErr.TransitionStatus(fresh.ID, StatusTransitionRequest{Action: "activate"}, dbOwnerUserID, auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svcErr.TransitionStatus(fresh.ID, StatusTransitionRequest{Action: "complete"}, dbOwnerUserID, auth.RoleMember); !errors.Is(err, issueErr) {
		t.Fatalf("counter error: got %v, want %v", err, issueErr)
	}
	if issues.calls == 0 {
		t.Fatal("fakeIssueCounter not called")
	}
}

func TestDBMembers(t *testing.T) {
	db := openProjectsTestDB(t)
	svc := NewService(NewRepository(db), nil, nil)

	// AddMember：非法输入 / 项目不存在 / 用户不存在
	if _, err := svc.AddMember(dbDraftProjectID, AddMemberRequest{UserID: "", Role: RoleMember}, dbOwnerUserID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty user: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.AddMember(dbDraftProjectID, AddMemberRequest{UserID: dbMemberUserID, Role: "superuser"}, dbOwnerUserID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad role: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.AddMember("b0000000-0000-4000-8000-000000009999", AddMemberRequest{UserID: dbMemberUserID, Role: RoleMember}, dbOwnerUserID); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing project: got %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.AddMember(dbDraftProjectID, AddMemberRequest{UserID: "b0000000-0000-4000-8000-000000009999", Role: RoleMember}, dbOwnerUserID); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing user: got %v, want ErrUserNotFound", err)
	}

	// 成功加成员：outsider 以 viewer 加入
	member, err := svc.AddMember(dbDraftProjectID, AddMemberRequest{UserID: dbOutsiderUserID, Role: RoleViewer}, dbOwnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != RoleViewer || member.Status != MemberStatusActive {
		t.Fatalf("added member: %+v", member)
	}

	// ListMembers：项目不存在 404；存在时返回含 owner/member/viewer
	if _, err := svc.ListMembers("b0000000-0000-4000-8000-000000009999"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("list members missing project: got %v", err)
	}
	all, err := svc.ListMembers(dbDraftProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("members: %+v", all)
	}

	// UpdateMemberRole：非法角色 / 不存在的成员 / 降级最后 owner 拒绝
	if _, err := svc.UpdateMemberRole(dbDraftProjectID, dbMemberUserID, UpdateMemberRequest{Role: "boss"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad role: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.UpdateMemberRole(dbDraftProjectID, "b0000000-0000-4000-8000-000000009999", UpdateMemberRequest{Role: RoleMember}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing member: got %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.UpdateMemberRole(dbDraftProjectID, dbOwnerUserID, UpdateMemberRequest{Role: RoleMember}); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("demote last owner: got %v, want ErrLastOwner", err)
	}

	// 正常升职 viewer → maintainer
	upgraded, err := svc.UpdateMemberRole(dbDraftProjectID, dbOutsiderUserID, UpdateMemberRequest{Role: RoleMaintainer})
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Role != RoleMaintainer {
		t.Fatalf("upgraded: %+v", upgraded)
	}

	// RemoveMember：不存在的成员 → ErrProjectNotFound；移除最后一个 owner → ErrLastOwner
	if err := svc.RemoveMember(dbDraftProjectID, "b0000000-0000-4000-8000-000000009999"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("remove missing member: got %v, want ErrProjectNotFound", err)
	}
	if err := svc.RemoveMember(dbDraftProjectID, dbOwnerUserID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("remove last owner: got %v, want ErrLastOwner", err)
	}
	if err := svc.RemoveMember(dbDraftProjectID, dbMemberUserID); err != nil {
		t.Fatal(err)
	}
	after, err := svc.ListMembers(dbDraftProjectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range after {
		if m.UserID == dbMemberUserID {
			t.Fatalf("member not removed: %+v", after)
		}
	}
}

// 软删除语义：projects 无 DELETE 端点，归档（archive）即最终态；校验归档后
// 不可再流转（除 reactivate），且 reactivate 后 completed_at/archived_at 保留。
func TestDBArchiveKeepsTimestamps(t *testing.T) {
	db := openProjectsTestDB(t)
	svc := NewService(NewRepository(db), nil, nil)

	archived, _, err := svc.TransitionStatus(dbCompletedProjectID, StatusTransitionRequest{Action: "archive"}, dbOwnerUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if archived.CompletedAt == nil || archived.ArchivedAt == nil {
		t.Fatalf("timestamps missing: %+v", archived)
	}
	// archived → draft 直接违法（仅 reactivate 合法）
	if _, _, err := svc.TransitionStatus(dbCompletedProjectID, StatusTransitionRequest{Action: "deactivate"}, dbAdminUserID, auth.RoleAdmin); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("archived deactivate: got %v, want ErrInvalidTransition", err)
	}
	reactivated, _, err := svc.TransitionStatus(dbCompletedProjectID, StatusTransitionRequest{Action: "reactivate", Reason: "重启"}, dbOwnerUserID, auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if reactivated.Status != StatusActive || reactivated.CompletedAt == nil {
		t.Fatalf("reactivated: %+v", reactivated)
	}
}
