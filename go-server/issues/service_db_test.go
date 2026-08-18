package issues

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

// service 集成测试：需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移）。
// issues.Service 的 repo 是具体 *Repository 但 access/validator 是接口，故 service 层用
// 真实 repo + fake access/validator（同 projects service_db_test 模式），覆盖创建校验
// （关联日志/项目生命周期/agent 字段）、状态流转、评论策略与仓储级查询。
// 固定 UUID 种子（ON CONFLICT DO NOTHING）+ t.Cleanup 清理，CI 以 -p 1 串行跑。

const (
	issuesDBOwnerID    = "00000000-0000-0000-0000-00000000b701"
	issuesDBMemberID   = "00000000-0000-0000-0000-00000000b702"
	issuesDBViewerID   = "00000000-0000-0000-0000-00000000b703"
	issuesDBOutsiderID = "00000000-0000-0000-0000-00000000b704"
	issuesDBAdminID    = "00000000-0000-0000-0000-00000000b705"

	issuesDBProjectID    = "c0000000-0000-4000-8000-00000000c701"
	issuesDBDraftProject = "c0000000-0000-4000-8000-00000000c702"
	issuesDBLogID        = "c0000000-0000-4000-8000-00000000c703"
	issuesDBOtherLogID   = "c0000000-0000-4000-8000-00000000c704"
	issuesDBOtherProject = "c0000000-0000-4000-8000-00000000c705"
	issuesDBReportID     = "c0000000-0000-4000-8000-00000000c706"
	issuesDBTaskID       = "c0000000-0000-4000-8000-00000000c707"
	issuesDBCandidateID  = "c0000000-0000-4000-8000-00000000c708"
)

func openIssuesSvcDB(t *testing.T) *sql.DB {
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

	for _, u := range []struct {
		id       string
		username string
		role     string
	}{
		{issuesDBOwnerID, "issues_db_owner", "member"},
		{issuesDBMemberID, "issues_db_member", "member"},
		{issuesDBViewerID, "issues_db_viewer", "viewer"},
		{issuesDBOutsiderID, "issues_db_outsider", "member"},
		{issuesDBAdminID, "issues_db_admin", "admin"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Issues DB Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	// 种子项目：active（owner=issuesDBOwnerID）/ draft
	for _, p := range []struct {
		id     string
		code   string
		status string
	}{
		{issuesDBProjectID, "ISS_DBTEST_ACTIVE", projects.StatusActive},
		{issuesDBDraftProject, "ISS_DBTEST_DRAFT", projects.StatusDraft},
		{issuesDBOtherProject, "ISS_DBTEST_OTHER", projects.StatusActive},
	} {
		if _, err := db.Exec(
			`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
			 VALUES ($1, $2, 'DB 种子项目', $3, $4, $4)
			 ON CONFLICT (id) DO NOTHING`, p.id, p.code, p.status, issuesDBOwnerID); err != nil {
			t.Fatal(err)
		}
	}
	// 成员：owner / member / viewer（active 项目）
	for _, m := range []struct {
		userID string
		role   string
	}{
		{issuesDBOwnerID, projects.RoleOwner},
		{issuesDBMemberID, projects.RoleMember},
		{issuesDBViewerID, projects.RoleViewer},
	} {
		if _, err := db.Exec(
			`INSERT INTO project_members (project_id, user_id, role, status, added_by)
			 VALUES ($1, $2, $3, 'active', $2)
			 ON CONFLICT (project_id, user_id) DO NOTHING`, issuesDBProjectID, m.userID, m.role); err != nil {
			t.Fatal(err)
		}
	}
	// 关联日志：本项目 1 条 + 其他项目 1 条
	for _, l := range []struct {
		id        string
		projectID string
	}{
		{issuesDBLogID, issuesDBProjectID},
		{issuesDBOtherLogID, issuesDBOtherProject},
	} {
		if _, err := db.Exec(
			`INSERT INTO logs (id, project_id, author_id, category, content, source)
			 VALUES ($1, $2, $3, 'general', 'db seed log', 'manual')
			 ON CONFLICT (id) DO NOTHING`, l.id, l.projectID, issuesDBOwnerID); err != nil {
			t.Fatal(err)
		}
	}
	// agent 链路种子：日报 → pending_agent_tasks（processing）→ candidate action
	if _, err := db.Exec(
		`INSERT INTO daily_reports (id, report_date, author_id)
		 VALUES ($1, '2099-03-01', $2)
		 ON CONFLICT (id) DO NOTHING`, issuesDBReportID, issuesDBOwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO pending_agent_tasks (id, report_id, status, acting_user_id)
		 VALUES ($1, $2, 'processing', $3)
		 ON CONFLICT (id) DO NOTHING`, issuesDBTaskID, issuesDBReportID, issuesDBOwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO agent_candidate_actions (id, task_id, action_type, project_id, pool_action_key, payload)
		 VALUES ($1, $2, 'create_issue', $3, $4, '{"title":"t"}'::jsonb)
		 ON CONFLICT (id) DO NOTHING`,
		issuesDBCandidateID, issuesDBTaskID, issuesDBProjectID, issuesDBTaskID+":create_issue:0"); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id IN (SELECT id FROM issues WHERE project_id IN ($1,$2,$3))`,
			issuesDBProjectID, issuesDBDraftProject, issuesDBOtherProject)
		db.Exec(`DELETE FROM issue_log_links WHERE log_id IN ($1,$2)`, issuesDBLogID, issuesDBOtherLogID)
		db.Exec(`DELETE FROM issues WHERE project_id IN ($1,$2,$3)`,
			issuesDBProjectID, issuesDBDraftProject, issuesDBOtherProject)
		db.Exec(`DELETE FROM agent_candidate_actions WHERE id = $1`, issuesDBCandidateID)
		db.Exec(`DELETE FROM pending_agent_tasks WHERE id = $1`, issuesDBTaskID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, issuesDBReportID)
		db.Exec(`DELETE FROM logs WHERE id IN ($1,$2)`, issuesDBLogID, issuesDBOtherLogID)
		db.Exec(`DELETE FROM project_members WHERE project_id IN ($1,$2,$3)`,
			issuesDBProjectID, issuesDBDraftProject, issuesDBOtherProject)
		db.Exec(`DELETE FROM projects WHERE id IN ($1,$2,$3)`,
			issuesDBProjectID, issuesDBDraftProject, issuesDBOtherProject)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4,$5)`,
			issuesDBOwnerID, issuesDBMemberID, issuesDBViewerID, issuesDBOutsiderID, issuesDBAdminID)
	})
	return db
}

// issuesSvc 构造 service：真实 repo + fake access（角色表驱动权限）+ 可选 validator。
func issuesSvc(db *sql.DB, status string, roles map[string]string, validator AgentTaskValidator) *Service {
	access := fakeProjectAccess{status: status, roles: roles}
	if status == "" {
		access.status = projects.StatusActive
	}
	if validator == nil {
		return NewService(NewRepository(db), access)
	}
	return NewService(NewRepository(db), access, validator)
}

func issueRoles() map[string]string {
	return map[string]string{
		issuesDBOwnerID:  projects.RoleOwner,
		issuesDBMemberID: projects.RoleMember,
		issuesDBViewerID: projects.RoleViewer,
		issuesDBAdminID:  projects.RoleOwner,
	}
}

func TestDBIssueCreate(t *testing.T) {
	db := openIssuesSvcDB(t)
	svc := issuesSvc(db, projects.StatusActive, issueRoles(), nil)

	// 输入校验
	if _, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{Title: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty title: got %v, want ErrInvalidInput", err)
	}
	tooLong := make([]rune, 257)
	for i := range tooLong {
		tooLong[i] = 'x'
	}
	if _, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{Title: string(tooLong)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("long title: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{Title: "t", Severity: "urgent"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad severity: got %v, want ErrInvalidInput", err)
	}
	badOccurred := "2026/07/01"
	if _, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{Title: "t", OccurredAt: &badOccurred}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad occurred_at: got %v, want ErrInvalidInput", err)
	}
	badDate := "2026-99-99"
	if _, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{Title: "t", ReportDate: &badDate}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad report_date: got %v, want ErrInvalidInput", err)
	}

	// 项目维度：不存在 / draft 生命周期 / 无权限
	missingAccess := fakeProjectAccess{status: "", roles: issueRoles()}
	missingSvc := NewService(NewRepository(db), missingAccess)
	if _, err := missingSvc.Create("00000000-0000-0000-0000-000000009999", issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{Title: "t"}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("missing project: got %v, want ErrProjectNotFound", err)
	}
	draftSvc := issuesSvc(db, projects.StatusDraft, issueRoles(), nil)
	if _, err := draftSvc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{Title: "t"}); !errors.Is(err, ErrProjectLifecycleBlocked) {
		t.Fatalf("draft project: got %v, want ErrProjectLifecycleBlocked", err)
	}
	if _, err := svc.Create(issuesDBProjectID, issuesDBOutsiderID, auth.RoleMember, CreateIssueRequest{Title: "t"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider: got %v, want ErrForbidden", err)
	}

	// 关联日志：全部存在且属本项目 → 成功；缺失 / 跨项目 → 对应错误
	created, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{
		Title: "RF 反射功率异常", RelatedLogIDs: []string{issuesDBLogID, "  ", issuesDBLogID},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created.ID)
		db.Exec(`DELETE FROM issue_log_links WHERE issue_id = $1`, created.ID)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created.ID)
	})
	if created.Title != "RF 反射功率异常" || created.Status != StatusOpen || created.Severity != SeverityMedium || created.AuthorID != issuesDBOwnerID {
		t.Fatalf("created: %+v", created)
	}
	if created.AiGenerated {
		t.Fatal("manual issue must not be ai_generated")
	}
	var linkCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM issue_log_links WHERE issue_id = $1`, created.ID).Scan(&linkCount); err != nil {
		t.Fatal(err)
	}
	if linkCount != 1 {
		t.Fatalf("issue_log_links = %d, want 1 (dedupe + trim)", linkCount)
	}

	if _, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{
		Title: "t", RelatedLogIDs: []string{"00000000-0000-0000-0000-000000009999"},
	}); !errors.Is(err, ErrRelatedLogNotFound) {
		t.Fatalf("missing log: got %v, want ErrRelatedLogNotFound", err)
	}
	if _, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{
		Title: "t", RelatedLogIDs: []string{issuesDBOtherLogID},
	}); !errors.Is(err, ErrRelatedLogProjectMismatch) {
		t.Fatalf("cross project log: got %v, want ErrRelatedLogProjectMismatch", err)
	}
}

func TestDBIssueAgentCreate(t *testing.T) {
	db := openIssuesSvcDB(t)
	validator := &fakeAgentTaskValidator{valid: true}
	svc := issuesSvc(db, projects.StatusActive, issueRoles(), validator)

	// 非 agent 携带 AI 字段 → ErrInvalidInput
	taskID := issuesDBTaskID
	candID := issuesDBCandidateID
	if _, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleMember, CreateIssueRequest{
		Title: "t", AiGenerated: true, AgentTaskID: &taskID, CandidateID: &candID,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("member with ai fields: got %v, want ErrInvalidInput", err)
	}

	// agent 缺 task_id / 未注入 validator / task 校验失败 → ErrInvalidInput
	if _, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleAgent, CreateIssueRequest{
		Title: "t", AiGenerated: true,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("agent without task id: got %v, want ErrInvalidInput", err)
	}
	noValidator := issuesSvc(db, projects.StatusActive, issueRoles(), nil)
	if _, err := noValidator.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleAgent, CreateIssueRequest{
		Title: "t", AiGenerated: true, AgentTaskID: &taskID,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("agent without validator: got %v, want ErrInvalidInput", err)
	}
	validator.valid = false
	if _, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleAgent, CreateIssueRequest{
		Title: "t", AiGenerated: true, AgentTaskID: &taskID, CandidateID: &candID,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid task: got %v, want ErrInvalidInput", err)
	}

	// 合法 agent 创建：ai_generated + 来源字段落库
	validator.valid = true
	created, err := svc.Create(issuesDBProjectID, issuesDBOwnerID, auth.RoleAgent, CreateIssueRequest{
		Title: "AI 识别的真空异常", AiGenerated: true, AgentTaskID: &taskID, CandidateID: &candID,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created.ID)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created.ID)
	})
	if !created.AiGenerated || created.AgentTaskID == nil || *created.AgentTaskID != taskID ||
		created.CandidateID == nil || *created.CandidateID != candID {
		t.Fatalf("agent created: %+v", created)
	}
	// 反查链路：GetByCandidateID 命中
	byCand, err := NewRepository(db).GetByCandidateID(candID)
	if err != nil || byCand == nil || byCand.ID != created.ID {
		t.Fatalf("get by candidate: %+v err=%v", byCand, err)
	}
}

func TestDBIssueListGet(t *testing.T) {
	db := openIssuesSvcDB(t)
	svc := issuesSvc(db, projects.StatusActive, issueRoles(), nil)

	// 种子：四种状态，验证未传 status 时返回全部
	open := seedIssue(t, db, "open", "", "")
	inProg := seedIssue(t, db, "in_progress", "", issuesDBMemberID)
	resolved := seedIssue(t, db, "resolved", "", "")
	closed := seedIssue(t, db, "closed", "", "")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id IN ($1,$2,$3,$4)`, open, inProg, resolved, closed)
		db.Exec(`DELETE FROM issues WHERE id IN ($1,$2,$3,$4)`, open, inProg, resolved, closed)
	})

	// 未传 status：不按状态过滤
	list, err := svc.List(issuesDBProjectID, issuesDBMemberID, auth.RoleMember, IssueListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 4 {
		t.Fatalf("unfiltered list total = %d, want 4", list.Total)
	}
	// 各状态过滤 + search
	inProgList, err := svc.List(issuesDBProjectID, issuesDBMemberID, auth.RoleMember, IssueListParams{Status: StatusInProgress})
	if err != nil {
		t.Fatal(err)
	}
	if inProgList.Total != 1 || inProgList.Items[0].ID != inProg {
		t.Fatalf("in_progress filter: %+v", inProgList)
	}
	resolvedList, err := svc.List(issuesDBProjectID, issuesDBMemberID, auth.RoleMember, IssueListParams{Status: StatusResolved})
	if err != nil {
		t.Fatal(err)
	}
	if resolvedList.Total != 1 || resolvedList.Items[0].ID != resolved {
		t.Fatalf("resolved filter: %+v", resolvedList)
	}
	searchList, err := svc.List(issuesDBProjectID, issuesDBMemberID, auth.RoleMember, IssueListParams{Status: StatusOpen, Search: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	if searchList.Total != 1 {
		t.Fatalf("search filter: %+v", searchList)
	}
	// 非法参数
	if _, err := svc.List(issuesDBProjectID, issuesDBMemberID, auth.RoleMember, IssueListParams{Status: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad status: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.List(issuesDBProjectID, issuesDBMemberID, auth.RoleMember, IssueListParams{Severity: "urgent"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad severity: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.List(issuesDBProjectID, issuesDBMemberID, auth.RoleMember, IssueListParams{Sort: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad sort: got %v, want ErrInvalidInput", err)
	}
	// 越权：outsider → ErrForbidden
	if _, err := svc.List(issuesDBProjectID, issuesDBOutsiderID, auth.RoleMember, IssueListParams{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider list: got %v, want ErrForbidden", err)
	}

	// GetByID：命中（含 comments）/ 404 / 403
	got, err := svc.GetByID(open, issuesDBMemberID, auth.RoleMember)
	if err != nil || got == nil || got.ID != open {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	if _, err := svc.GetByID("00000000-0000-0000-0000-000000009999", issuesDBMemberID, auth.RoleMember); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("missing: got %v, want ErrIssueNotFound", err)
	}
	if _, err := svc.GetByID(open, issuesDBOutsiderID, auth.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider get: got %v, want ErrForbidden", err)
	}
}

func TestDBIssueUpdate(t *testing.T) {
	db := openIssuesSvcDB(t)
	svc := issuesSvc(db, projects.StatusActive, issueRoles(), nil)

	created := seedIssue(t, db, "open", "", "")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created)
	})

	// 权限/输入校验
	if _, err := svc.Update(created, issuesDBOutsiderID, auth.RoleMember, UpdateIssueRequest{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider update: got %v, want ErrForbidden", err)
	}
	emptyTitle := "  "
	if _, err := svc.Update(created, issuesDBMemberID, auth.RoleMember, UpdateIssueRequest{Title: &emptyTitle}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty title: got %v, want ErrInvalidInput", err)
	}
	badSeverity := "urgent"
	if _, err := svc.Update(created, issuesDBMemberID, auth.RoleMember, UpdateIssueRequest{Severity: &badSeverity}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad severity: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.Update("00000000-0000-0000-0000-000000009999", issuesDBMemberID, auth.RoleMember, UpdateIssueRequest{}); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("missing: got %v, want ErrIssueNotFound", err)
	}

	// 成功更新：标题 + 严重级别 + assignee
	newTitle := "更新后的标题"
	newSev := SeverityHigh
	assigneeID := issuesDBMemberID
	updated, err := svc.Update(created, issuesDBMemberID, auth.RoleMember, UpdateIssueRequest{Title: &newTitle, Severity: &newSev, AssigneeID: &assigneeID})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != newTitle || updated.Severity != newSev || updated.AssigneeID == nil || *updated.AssigneeID != issuesDBMemberID {
		t.Fatalf("updated: %+v", updated)
	}

	// closed 不可改
	closed := seedIssue(t, db, "closed", "", "")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, closed)
		db.Exec(`DELETE FROM issues WHERE id = $1`, closed)
	})
	if _, err := svc.Update(closed, issuesDBMemberID, auth.RoleMember, UpdateIssueRequest{Title: &newTitle}); !errors.Is(err, ErrIssueClosed) {
		t.Fatalf("closed update: got %v, want ErrIssueClosed", err)
	}
}

func TestDBIssueTransition(t *testing.T) {
	db := openIssuesSvcDB(t)
	svc := issuesSvc(db, projects.StatusActive, issueRoles(), nil)

	created := seedIssue(t, db, "open", "", issuesDBMemberID)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created)
	})

	// 输入/状态校验
	if _, err := svc.Transition(created, issuesDBMemberID, auth.RoleMember, TransitionRequest{TargetStatus: "bogus"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad target: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.Transition(created, issuesDBMemberID, auth.RoleMember, TransitionRequest{TargetStatus: StatusOpen}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("same status: got %v, want ErrInvalidTransition", err)
	}
	if _, err := svc.Transition(created, issuesDBViewerID, auth.RoleMember, TransitionRequest{TargetStatus: StatusInProgress}); !errors.Is(err, ErrTransitionForbidden) {
		t.Fatalf("viewer transition: got %v, want ErrTransitionForbidden", err)
	}
	if _, err := svc.Transition("00000000-0000-0000-0000-000000009999", issuesDBMemberID, auth.RoleMember, TransitionRequest{TargetStatus: StatusInProgress}); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("missing: got %v, want ErrIssueNotFound", err)
	}
	// AddComment 但无 reason
	if _, err := svc.Transition(created, issuesDBMemberID, auth.RoleMember, TransitionRequest{TargetStatus: StatusInProgress, AddComment: true}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("add comment without reason: got %v, want ErrInvalidInput", err)
	}

	// 合法流转 open → in_progress（带评论）
	transitioned, err := svc.Transition(created, issuesDBMemberID, auth.RoleMember, TransitionRequest{
		TargetStatus: StatusInProgress, Reason: "开始处理", AddComment: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transitioned.Status != StatusInProgress {
		t.Fatalf("status: %q", transitioned.Status)
	}
	var commentCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM issue_comments WHERE issue_id = $1`, created).Scan(&commentCount); err != nil {
		t.Fatal(err)
	}
	if commentCount != 1 {
		t.Fatalf("comments = %d, want 1", commentCount)
	}

	// in_progress → resolved：resolved_at 落库
	resolved, err := svc.Transition(created, issuesDBMemberID, auth.RoleMember, TransitionRequest{TargetStatus: StatusResolved})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != StatusResolved || resolved.ResolvedAt == nil {
		t.Fatalf("resolved: %+v", resolved)
	}
	// resolved → closed
	closed, err := svc.Transition(created, issuesDBMemberID, auth.RoleMember, TransitionRequest{TargetStatus: StatusClosed})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != StatusClosed {
		t.Fatalf("closed: %+v", closed)
	}
	// closed → open：owner 且必须 reason；无 reason → ErrReasonRequired
	if _, err := svc.Transition(created, issuesDBOwnerID, auth.RoleMember, TransitionRequest{TargetStatus: StatusOpen}); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("reopen without reason: got %v, want ErrReasonRequired", err)
	}
	reopened, err := svc.Transition(created, issuesDBOwnerID, auth.RoleMember, TransitionRequest{TargetStatus: StatusOpen, Reason: "重新验证"})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != StatusOpen || reopened.ResolvedAt != nil {
		t.Fatalf("reopened: %+v", reopened)
	}

	// 项目非 active → ErrProjectLifecycleBlocked（admin 也无法绕过）
	draftSvc := issuesSvc(db, projects.StatusDraft, issueRoles(), nil)
	if _, err := draftSvc.Transition(created, issuesDBAdminID, auth.RoleAdmin, TransitionRequest{TargetStatus: StatusInProgress}); !errors.Is(err, ErrProjectLifecycleBlocked) {
		t.Fatalf("draft project transition: got %v, want ErrProjectLifecycleBlocked", err)
	}
}

func TestDBIssueComments(t *testing.T) {
	db := openIssuesSvcDB(t)
	svc := issuesSvc(db, projects.StatusActive, issueRoles(), nil)

	created := seedIssue(t, db, "open", "", "")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id = $1`, created)
		db.Exec(`DELETE FROM issues WHERE id = $1`, created)
	})

	// 空内容 / 不存在的 issue
	if _, err := svc.AddComment(created, issuesDBMemberID, auth.RoleMember, AddCommentRequest{Content: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty content: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.AddComment("00000000-0000-0000-0000-000000009999", issuesDBMemberID, auth.RoleMember, AddCommentRequest{Content: "x"}); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("missing issue: got %v, want ErrIssueNotFound", err)
	}
	// members 策略：member 可评、outsider 拒
	comment, err := svc.AddComment(created, issuesDBMemberID, auth.RoleMember, AddCommentRequest{Content: "我来看看"})
	if err != nil {
		t.Fatal(err)
	}
	if comment.Content != "我来看看" || comment.IssueID != created {
		t.Fatalf("comment: %+v", comment)
	}
	if _, err := svc.AddComment(created, issuesDBOutsiderID, auth.RoleMember, AddCommentRequest{Content: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider comment: got %v, want ErrForbidden", err)
	}
	// everyone 策略：outsider 可评
	everyoneSvc := issuesSvc(db, projects.StatusActive, issueRoles(), nil)
	everyoneAccess := fakeProjectAccess{status: projects.StatusActive, roles: issueRoles(), commentPolicy: projects.CommentPolicyEveryone}
	everyoneSvc = NewService(NewRepository(db), everyoneAccess)
	if _, err := everyoneSvc.AddComment(created, issuesDBOutsiderID, auth.RoleMember, AddCommentRequest{Content: "外部评论"}); err != nil {
		t.Fatalf("everyone policy: %v", err)
	}
	// disabled 策略 → ErrCommentsDisabled
	disabledAccess := fakeProjectAccess{status: projects.StatusActive, roles: issueRoles(), commentPolicy: projects.CommentPolicyDisabled}
	disabledSvc := NewService(NewRepository(db), disabledAccess)
	if _, err := disabledSvc.AddComment(created, issuesDBMemberID, auth.RoleMember, AddCommentRequest{Content: "x"}); !errors.Is(err, ErrCommentsDisabled) {
		t.Fatalf("disabled policy: got %v, want ErrCommentsDisabled", err)
	}
	// 未知策略 → ErrInvalidInput
	bogusAccess := fakeProjectAccess{status: projects.StatusActive, roles: issueRoles(), commentPolicy: "bogus"}
	bogusSvc := NewService(NewRepository(db), bogusAccess)
	if _, err := bogusSvc.AddComment(created, issuesDBMemberID, auth.RoleMember, AddCommentRequest{Content: "x"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bogus policy: got %v, want ErrInvalidInput", err)
	}

	// GetComments 分页
	comments, err := svc.GetComments(created, issuesDBMemberID, auth.RoleMember, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(comments))
	}
}

func TestDBIssueRepositoryHelpers(t *testing.T) {
	db := openIssuesSvcDB(t)
	repo := NewRepository(db)

	open := seedIssue(t, db, "open", "", "")
	resolved := seedIssue(t, db, "resolved", "", "")
	closed := seedIssue(t, db, "closed", "", "")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id IN ($1,$2,$3)`, open, resolved, closed)
		db.Exec(`DELETE FROM issues WHERE id IN ($1,$2,$3)`, open, resolved, closed)
	})

	// CountOpenByProject：resolved/closed 不计
	count, err := repo.CountOpenByProject(issuesDBProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("open count = %d, want 1", count)
	}
	// TerminalIssueIDs：resolved + closed
	ids, err := repo.TerminalIssueIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got[resolved] || !got[closed] || got[open] {
		t.Fatalf("terminal ids: %v", got)
	}
	// CountRelatedLogs / CountLogsByIDs
	if n, err := repo.CountLogsByIDs([]string{issuesDBLogID, issuesDBOtherLogID}); err != nil || n != 2 {
		t.Fatalf("count logs by ids = %d, %v", n, err)
	}
	if n, err := repo.CountLogsByIDs(nil); err != nil || n != 0 {
		t.Fatalf("count logs by empty = %d, %v", n, err)
	}
	if n, err := repo.CountRelatedLogs(issuesDBProjectID, []string{issuesDBLogID}); err != nil || n != 1 {
		t.Fatalf("count related = %d, %v", n, err)
	}
	if n, err := repo.CountRelatedLogs(issuesDBProjectID, nil); err != nil || n != 0 {
		t.Fatalf("count related empty = %d, %v", n, err)
	}
}

// seedIssue 直插一条 issue 到 issuesDBProjectID（返回 id）。
func seedIssue(t *testing.T, db *sql.DB, status, severity, assignee string) string {
	t.Helper()
	return seedIssueIn(t, db, issuesDBProjectID, status, severity, assignee)
}

// seedIssueIn 直插一条 issue 到指定项目（返回 id）。
func seedIssueIn(t *testing.T, db *sql.DB, projectID, status, severity, assignee string) string {
	t.Helper()
	if severity == "" {
		severity = SeverityMedium
	}
	assigneeSQL := "NULL"
	args := []any{projectID, status, severity, issuesDBOwnerID}
	if assignee != "" {
		assigneeSQL = "$5::uuid"
		args = append(args, assignee)
	}
	var id string
	query := fmt.Sprintf(
		`INSERT INTO issues (project_id, title, description, status, severity, author_id, assignee_id, report_date)
		 VALUES ($1, 'seed issue', 'seed', $2, $3, $4, %s, '2099-01-01')
		 RETURNING id`, assigneeSQL)
	if err := db.QueryRow(query, args...).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO issue_project_links (issue_id, project_id, relation) VALUES ($1, $2, 'primary')`, id, projectID); err != nil {
		t.Fatal(err)
	}
	return id
}

type fakeAgentTaskValidator struct{ valid bool }

func (f *fakeAgentTaskValidator) ValidateAgentTask(taskID, actingUserID string) (bool, error) {
	return f.valid, nil
}
