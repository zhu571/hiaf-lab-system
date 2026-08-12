package experiences

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

// 集成测试：经验 CRUD、AI 候选审核（approve→入库、reject）、搜索/筛选、软删
// （archive 即终态）、candidate_id 反查。固定 UUID 段 d3xx（用户）+ d0000000-…d3xx（项目）。

const (
	expAdminID      = "00000000-0000-0000-0000-00000000d301"
	expOwnerID      = "00000000-0000-0000-0000-00000000d302"
	expMaintainerID = "00000000-0000-0000-0000-00000000d303"
	expMemberID     = "00000000-0000-0000-0000-00000000d304"
	expOutsiderID   = "00000000-0000-0000-0000-00000000d305"

	expProjectID    = "d0000000-0000-4000-8000-00000000d301"
	expProjectID2   = "d0000000-0000-4000-8000-00000000d302"
	expCandidateID  = "d0000000-0000-4000-8000-00000000d303"
	expCandidateID2 = "d0000000-0000-4000-8000-00000000d304"
	expReportID     = "d0000000-0000-4000-8000-00000000d305"
	expTaskID       = "d0000000-0000-4000-8000-00000000d306"
)

func openExperiencesTestDB(t *testing.T) *sql.DB {
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
		{expAdminID, "exp_dbtest_admin", "admin"},
		{expOwnerID, "exp_dbtest_owner", "member"},
		{expMaintainerID, "exp_dbtest_maintainer", "member"},
		{expMemberID, "exp_dbtest_member", "member"},
		{expOutsiderID, "exp_dbtest_outsider", "member"},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Experience DB Test', $3, false, false)
			 ON CONFLICT (id) DO NOTHING`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []struct {
		id   string
		code string
	}{
		{expProjectID, "PRJ_EXP_DBTEST_1"},
		{expProjectID2, "PRJ_EXP_DBTEST_2"},
	} {
		if _, err := db.Exec(
			`INSERT INTO projects (id, code, name, status, owner_user_id, created_by)
			 VALUES ($1, $2, '经验集成测试项目', 'draft', $3, $3)
			 ON CONFLICT (id) DO NOTHING`, p.id, p.code, expOwnerID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 VALUES ($1, $2, 'owner', 'active', $2), ($1, $3, 'maintainer', 'active', $2), ($1, $4, 'member', 'active', $2)
		 ON CONFLICT (project_id, user_id) DO NOTHING`,
		expProjectID, expOwnerID, expMaintainerID, expMemberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 VALUES ($1, $2, 'owner', 'active', $2)
		 ON CONFLICT (project_id, user_id) DO NOTHING`, expProjectID2, expOwnerID); err != nil {
		t.Fatal(err)
	}
	// agent 候选链路种子：daily_reports → pending_agent_tasks → agent_candidate_actions
	// （experiences.candidate_id FK 指向 agent_candidate_actions，见迁移 030）
	if _, err := db.Exec(
		`INSERT INTO daily_reports (id, report_date, author_id)
		 VALUES ($1, DATE '2026-08-01', $2)
		 ON CONFLICT (id) DO NOTHING`, expReportID, expMemberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO pending_agent_tasks (id, report_id, status)
		 VALUES ($1, $2, 'processing')
		 ON CONFLICT (id) DO NOTHING`, expTaskID, expReportID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO agent_candidate_actions (id, task_id, action_type, project_id, pool_action_key, payload)
		 VALUES ($1, $2, 'create_experience', $3, 'exp-dbtest-cand-1', '{}'::jsonb)
		 ON CONFLICT (id) DO NOTHING`, expCandidateID, expTaskID, expProjectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM agent_candidate_actions WHERE id = $1`, expCandidateID)
		db.Exec(`DELETE FROM pending_agent_tasks WHERE id = $1`, expTaskID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, expReportID)
	})
	t.Cleanup(func() {
		db.Exec(`DELETE FROM experiences WHERE author_id IN ($1,$2,$3,$4,$5)`, expAdminID, expOwnerID, expMaintainerID, expMemberID, expOutsiderID)
		db.Exec(`DELETE FROM projects WHERE id IN ($1,$2)`, expProjectID, expProjectID2)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4,$5)`, expAdminID, expOwnerID, expMaintainerID, expMemberID, expOutsiderID)
	})
	return db
}

func expTestService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	db := openExperiencesTestDB(t)
	return NewService(NewRepository(db), ProjectAccessAdapter{Repo: projects.NewRepository(db)}), db
}

// fakeAgentValidator：模拟 pending_agent_tasks 校验；valid=false 模拟任务无效/过期。
type fakeAgentValidator struct{ valid bool }

func (f *fakeAgentValidator) ValidateAgentTask(taskID, actingUserID string) (bool, error) {
	return f.valid, nil
}

func expPtr(v string) *string { return &v }

func containsExperience(items []Experience, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func expUniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("exp-h-%d", time.Now().UnixNano())
}

// ---------- service 层 ----------

func TestDBCreateExperience(t *testing.T) {
	svc, db := expTestService(t)

	// 全局经验仅 admin：member → ErrGlobalExperienceAdminOnly
	if _, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		Title: "全局", Content: "内容",
	}); err != ErrGlobalExperienceAdminOnly {
		t.Fatalf("global by member: got %v", err)
	}
	// 项目不存在 → ErrProjectNotFound
	missing := "d0000000-0000-4000-8000-000000009999"
	if _, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: &missing, Title: "T", Content: "C",
	}); err != ErrProjectNotFound {
		t.Fatalf("missing project: got %v", err)
	}
	// 无成员关系 → ErrForbidden
	if _, err := svc.Create(expOutsiderID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "T", Content: "C",
	}); err != ErrForbidden {
		t.Fatalf("outsider: got %v", err)
	}
	// 空 title / 超长 title / 空 content → ErrInvalidInput
	if _, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "  ", Content: "C",
	}); err != ErrInvalidInput {
		t.Fatalf("empty title: got %v", err)
	}
	if _, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: strings.Repeat("长", 257), Content: "C",
	}); err != ErrInvalidInput {
		t.Fatalf("long title: got %v", err)
	}
	if _, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "T", Content: "  ",
	}); err != ErrInvalidInput {
		t.Fatalf("empty content: got %v", err)
	}
	// 非 agent 带 ai_generated / agent_task_id / candidate_id → ErrInvalidInput
	taskID := "task-1"
	if _, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "T", Content: "C", AiGenerated: true,
	}); err != ErrInvalidInput {
		t.Fatalf("ai_generated by user: got %v", err)
	}
	if _, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "T", Content: "C", AgentTaskID: &taskID,
	}); err != ErrInvalidInput {
		t.Fatalf("task id by user: got %v", err)
	}
	if _, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "T", Content: "C", CandidateID: expPtr(expCandidateID),
	}); err != ErrInvalidInput {
		t.Fatalf("candidate id by user: got %v", err)
	}
	// agent 无 validator / 任务无效 → ErrInvalidInput
	agentNoValidator := NewService(NewRepository(db), ProjectAccessAdapter{Repo: projects.NewRepository(db)})
	if _, err := agentNoValidator.Create(expMemberID, auth.RoleAgent, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "T", Content: "C", AiGenerated: true, AgentTaskID: &taskID,
	}); err != ErrInvalidInput {
		t.Fatalf("agent no validator: got %v", err)
	}
	agentBadTask := NewService(NewRepository(db), ProjectAccessAdapter{Repo: projects.NewRepository(db)},
		&fakeAgentValidator{valid: false})
	if _, err := agentBadTask.Create(expMemberID, auth.RoleAgent, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "T", Content: "C", AiGenerated: true, AgentTaskID: &taskID,
	}); err != ErrInvalidInput {
		t.Fatalf("agent bad task: got %v", err)
	}
	// 成功创建（member，项目内）：tags 归一化 + candidate_id 反查 + 链接项目
	created, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "  RF 匹配经验  ", Content: "  内容  ",
		Tags: []string{" RF ", "rf", "Matching"},
		LinkedProjects: []ExperienceProjectLink{
			{ProjectID: expProjectID2, Relation: RelationApplicable},
			{ProjectID: expProjectID2, Relation: RelationApplicable}, // 去重
			{ProjectID: expProjectID2, Relation: "bogus"},            // 覆盖：去重先命中
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiences WHERE id = $1`, created.ID) })
	if created.Status != StatusCandidate || created.Title != "RF 匹配经验" || created.Content != "内容" ||
		created.AuthorID != expMemberID || len(created.Tags) != 2 || created.Tags[0] != "rf" || created.Tags[1] != "matching" {
		t.Fatalf("created: %+v", created)
	}
	if len(created.LinkedProjects) != 1 || created.LinkedProjects[0].ProjectID != expProjectID2 {
		t.Fatalf("links: %+v", created.LinkedProjects)
	}

	// agent 候选入库（approve→入库）：带 candidate_id + task 有效
	agentOK := NewService(NewRepository(db), ProjectAccessAdapter{Repo: projects.NewRepository(db)},
		&fakeAgentValidator{valid: true})
	cand, err := agentOK.Create(expOwnerID, auth.RoleAgent, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "候选经验", Content: "候选内容",
		AiGenerated: true, AgentTaskID: &taskID, CandidateID: expPtr(expCandidateID),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiences WHERE id = $1`, cand.ID) })
	if !cand.AiGenerated || cand.CandidateID == nil || *cand.CandidateID != expCandidateID ||
		cand.AgentTaskID == nil || *cand.AgentTaskID != taskID {
		t.Fatalf("agent candidate: %+v", cand)
	}
	// candidate_id 反查
	byCandidate, err := NewRepository(db).GetByCandidateID(expCandidateID)
	if err != nil || byCandidate == nil || byCandidate.ID != cand.ID {
		t.Fatalf("GetByCandidateID: %+v err=%v", byCandidate, err)
	}
	// 全局 admin 创建
	global, err := svc.Create(expAdminID, auth.RoleAdmin, CreateExperienceRequest{Title: "全局经验", Content: "g"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiences WHERE id = $1`, global.ID) })
	if global.ProjectID != nil {
		t.Fatalf("global should have nil project: %+v", global)
	}
	// 非法 relation（新项目，未命中去重）
	if _, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "T2", Content: "C2",
		LinkedProjects: []ExperienceProjectLink{{ProjectID: expProjectID2, Relation: "bogus"}},
	}); err != ErrInvalidInput {
		t.Fatalf("bad relation: got %v", err)
	}
}

func TestDBListExperience(t *testing.T) {
	svc, db := expTestService(t)

	// 种子：published 项目经验（member 作者）+ published 全局 + candidate（member 作者）+ archived
	published, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "发布经验", Content: "发布内容", Tags: []string{"cryo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(published.ID, expMaintainerID, auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiences WHERE id = $1`, published.ID) })

	global, err := svc.Create(expAdminID, auth.RoleAdmin, CreateExperienceRequest{
		Title: "全局发布", Content: "g", Tags: []string{"global"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(global.ID, expAdminID, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiences WHERE id = $1`, global.ID) })

	candidate, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "待审经验", Content: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiences WHERE id = $1`, candidate.ID) })

	// 默认列表：published（含全局 + 项目内）；注意迁移 009 有一条全局种子经验，断言按 ID 包含而非精确计数
	list, err := svc.List(expMemberID, auth.RoleMember, ExperienceListParams{ProjectID: expProjectID})
	if err != nil {
		t.Fatal(err)
	}
	if !containsExperience(list.Items, published.ID) || !containsExperience(list.Items, global.ID) {
		t.Fatalf("published list missing items: %+v", list)
	}
	// 空 project_id → 只返回全局（含 009 种子）
	globalOnly, err := svc.List(expMemberID, auth.RoleMember, ExperienceListParams{})
	if err != nil || !containsExperience(globalOnly.Items, global.ID) {
		t.Fatalf("global only: %+v err=%v", globalOnly, err)
	}
	for _, item := range globalOnly.Items {
		if item.ProjectID != nil {
			t.Fatalf("global-only list has project item: %+v", item)
		}
	}
	// 非法 status → ErrInvalidInput
	if _, err := svc.List(expMemberID, auth.RoleMember, ExperienceListParams{Status: "bogus"}); err != ErrInvalidInput {
		t.Fatalf("bad status: got %v", err)
	}
	// candidate：member 只看自己的；maintainer 看全部
	ownCands, err := svc.List(expMemberID, auth.RoleMember, ExperienceListParams{ProjectID: expProjectID, Status: StatusCandidate})
	if err != nil || ownCands.Total != 1 || ownCands.Items[0].ID != candidate.ID {
		t.Fatalf("own candidates: %+v err=%v", ownCands, err)
	}
	maintainerCands, err := svc.List(expMaintainerID, auth.RoleMember, ExperienceListParams{ProjectID: expProjectID, Status: StatusCandidate})
	if err != nil || maintainerCands.Total != 1 {
		t.Fatalf("maintainer candidates: %+v err=%v", maintainerCands, err)
	}
	// tags 过滤 / keyword 过滤
	tagged, err := svc.List(expMemberID, auth.RoleMember, ExperienceListParams{ProjectID: expProjectID, Tags: []string{"cryo"}})
	if err != nil || tagged.Total != 1 || tagged.Items[0].ID != published.ID {
		t.Fatalf("tags filter: %+v err=%v", tagged, err)
	}
	keyword, err := svc.List(expMemberID, auth.RoleMember, ExperienceListParams{ProjectID: expProjectID, Keyword: "发布内容"})
	if err != nil || keyword.Total != 1 || keyword.Items[0].ID != published.ID {
		t.Fatalf("keyword filter: %+v err=%v", keyword, err)
	}
	// per_page > 100 收敛
	capped, err := svc.List(expMemberID, auth.RoleMember, ExperienceListParams{ProjectID: expProjectID, Page: 1, PerPage: 500})
	if err != nil || capped.PerPage != 100 {
		t.Fatalf("per page cap: %+v err=%v", capped, err)
	}
	// 无权限项目 → ErrForbidden
	if _, err := svc.List(expOutsiderID, auth.RoleMember, ExperienceListParams{ProjectID: expProjectID}); err != ErrForbidden {
		t.Fatalf("outsider list: got %v", err)
	}
}

func TestDBGetUpdatePublishArchive(t *testing.T) {
	svc, db := expTestService(t)

	// GetByID：不存在 → ErrExperienceNotFound
	if _, err := svc.GetByID("d0000000-0000-4000-8000-000000009999", expMemberID, auth.RoleMember); err != ErrExperienceNotFound {
		t.Fatalf("missing: got %v", err)
	}
	// candidate：作者本人可读；owner 角色（≥maintainer）可读；无成员关系 → ErrForbidden
	cand, err := svc.Create(expMemberID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "待审", Content: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiences WHERE id = $1`, cand.ID) })
	if _, err := svc.GetByID(cand.ID, expMemberID, auth.RoleMember); err != nil {
		t.Fatalf("author read: %v", err)
	}
	if _, err := svc.GetByID(cand.ID, expOwnerID, auth.RoleMember); err != nil {
		t.Fatalf("owner read candidate: %v", err)
	}
	if _, err := svc.GetByID(cand.ID, expOutsiderID, auth.RoleMember); err != ErrForbidden {
		t.Fatalf("outsider read candidate: got %v", err)
	}
	// maintainer 可读候选（canRead 走 maintainer 通道）
	if _, err := svc.GetByID(cand.ID, expMaintainerID, auth.RoleMember); err != nil {
		t.Fatalf("maintainer read candidate: %v", err)
	}
	// 全局 published：任何人可读
	global, err := svc.Create(expAdminID, auth.RoleAdmin, CreateExperienceRequest{Title: "全局", Content: "g"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(global.ID, expAdminID, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiences WHERE id = $1`, global.ID) })
	if _, err := svc.GetByID(global.ID, expOutsiderID, auth.RoleMember); err != nil {
		t.Fatalf("outsider read global published: %v", err)
	}

	// Update：author 修改候选；published → ErrNotCandidate；空 title → ErrInvalidInput
	title := " 改题  "
	updated, err := svc.Update(cand.ID, expMemberID, auth.RoleMember, UpdateExperienceRequest{Title: &title})
	if err != nil || updated.Title != "改题" {
		t.Fatalf("update: %+v err=%v", updated, err)
	}
	if _, err := svc.Update(global.ID, expAdminID, auth.RoleAdmin, UpdateExperienceRequest{Title: &title}); err != ErrNotCandidate {
		t.Fatalf("update published: got %v", err)
	}
	emptyTitle := "  "
	if _, err := svc.Update(cand.ID, expMemberID, auth.RoleMember, UpdateExperienceRequest{Title: &emptyTitle}); err != ErrInvalidInput {
		t.Fatalf("empty title update: got %v", err)
	}
	emptyContent := ""
	if _, err := svc.Update(cand.ID, expMemberID, auth.RoleMember, UpdateExperienceRequest{Content: &emptyContent}); err != ErrInvalidInput {
		t.Fatalf("empty content update: got %v", err)
	}
	// 他人候选：member 改 → forbidden；maintainer 改 → OK（canUpdate maintainer 通道）
	if _, err := svc.Update(cand.ID, expMemberID, auth.RoleMember, UpdateExperienceRequest{Content: expPtr("m")}); err != nil {
		t.Fatalf("author content update: %v", err)
	}
	otherCand, err := svc.Create(expOwnerID, auth.RoleMember, CreateExperienceRequest{
		ProjectID: expPtr(expProjectID), Title: "owner候选", Content: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiences WHERE id = $1`, otherCand.ID) })
	if _, err := svc.Update(otherCand.ID, expMemberID, auth.RoleMember, UpdateExperienceRequest{Content: expPtr("x")}); err != ErrForbidden {
		t.Fatalf("member update others: got %v", err)
	}
	if _, err := svc.Update(otherCand.ID, expMaintainerID, auth.RoleMember, UpdateExperienceRequest{Content: expPtr("x")}); err != nil {
		t.Fatalf("maintainer update others: %v", err)
	}

	// Publish：maintainer 通过（approve→入库）；member → ErrPublishForbidden；已发布 → ErrNotCandidate
	if _, err := svc.Publish(otherCand.ID, expMemberID, auth.RoleMember); err != ErrPublishForbidden {
		t.Fatalf("member publish: got %v", err)
	}
	published, err := svc.Publish(otherCand.ID, expMaintainerID, auth.RoleMember)
	if err != nil || published.Status != StatusPublished || published.ReviewerID == nil || *published.ReviewerID != expMaintainerID || published.PublishedAt == nil {
		t.Fatalf("publish: %+v err=%v", published, err)
	}
	if _, err := svc.Publish(otherCand.ID, expMaintainerID, auth.RoleMember); err != ErrNotCandidate {
		t.Fatalf("republish: got %v", err)
	}
	globalCand, err := svc.Create(expAdminID, auth.RoleAdmin, CreateExperienceRequest{Title: "全局候选", Content: "c"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM experiences WHERE id = $1`, globalCand.ID) })
	// 全局 publish 非 admin → ErrGlobalExperienceAdminOnly
	if _, err := svc.Publish(globalCand.ID, expOwnerID, auth.RoleMember); err != ErrGlobalExperienceAdminOnly {
		t.Fatalf("non-admin publish global: got %v", err)
	}
	if _, err := svc.Publish(globalCand.ID, expAdminID, auth.RoleAdmin); err != nil {
		t.Fatalf("admin publish global: %v", err)
	}

	// Archive：owner 可归档；member → ErrForbidden；candidate → ErrNotPublished
	if _, err := svc.Archive(otherCand.ID, expMemberID, auth.RoleMember); err != ErrForbidden {
		t.Fatalf("member archive: got %v", err)
	}
	archived, err := svc.Archive(otherCand.ID, expOwnerID, auth.RoleMember)
	if err != nil || archived.Status != StatusArchived {
		t.Fatalf("archive: %+v err=%v", archived, err)
	}
	if _, err := svc.Archive(otherCand.ID, expOwnerID, auth.RoleMember); err != ErrNotPublished {
		t.Fatalf("rearchive: got %v", err)
	}
	if _, err := svc.Archive(cand.ID, expOwnerID, auth.RoleMember); err != ErrNotPublished {
		t.Fatalf("archive candidate: got %v", err)
	}
	// 归档后从 published 列表消失
	after, err := svc.List(expMemberID, auth.RoleMember, ExperienceListParams{ProjectID: expProjectID})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range after.Items {
		if item.ID == otherCand.ID {
			t.Fatalf("archived item still listed: %+v", after.Items)
		}
	}
	// 全局归档非 admin → ErrGlobalExperienceAdminOnly
	if _, err := svc.Archive(global.ID, expOwnerID, auth.RoleMember); err != ErrGlobalExperienceAdminOnly {
		t.Fatalf("non-admin archive global: got %v", err)
	}
	if _, err := svc.Archive(global.ID, expAdminID, auth.RoleAdmin); err != nil {
		t.Fatalf("admin archive global: %v", err)
	}
}

// ---------- handler 层 ----------

const expHandlerTestSecret = "experiences-handler-test-secret"

func newExperiencesTestRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(expHandlerTestSecret))
	svc := NewService(NewRepository(db), ProjectAccessAdapter{Repo: projects.NewRepository(db)})
	h := NewHandler(svc)
	router := chi.NewRouter()
	router.Route("/api/v1/experiences", func(r chi.Router) {
		r.Use(middleware.RequestID)
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Post("/candidates", h.Create)
		r.Post("/extract-candidates", h.ExtractCandidates)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetByID)
			r.Patch("/", h.Update)
			r.Post("/publish", h.Publish)
			r.Post("/archive", h.Archive)
		})
	})
	return router
}

func expToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(expHandlerTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func expRequest(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func expErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error body: %s, err=%v", rec.Body.String(), err)
	}
	return envelope.Error.Code
}

func assertExpAudit(t *testing.T, db *sql.DB, requestID, action string) {
	t.Helper()
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE request_id = $1 AND action = $2`, requestID, action,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit_log rows = %d, want 1 (request_id=%s action=%s)", count, requestID, action)
	}
}

func TestHandlerExperiences(t *testing.T) {
	db := openExperiencesTestDB(t)
	router := newExperiencesTestRouter(t, db)
	maintainer := expToken(t, expMaintainerID, "maintainer", auth.RoleMember)
	member := expToken(t, expMemberID, "member", auth.RoleMember)
	outsider := expToken(t, expOutsiderID, "outsider", auth.RoleMember)

	// 400：缺 Idempotency-Key；401：无 token
	rec := expRequest(t, router, http.MethodPost, "/api/v1/experiences", maintainer, "",
		`{"project_id":"`+expProjectID+`","title":"t","content":"c"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idempotency key = %d", rec.Code)
	}
	rec = expRequest(t, router, http.MethodPost, "/api/v1/experiences", "", expUniqueKey(t),
		`{"project_id":"`+expProjectID+`","title":"t","content":"c"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", rec.Code)
	}
	// 400：请求体解析失败
	rec = expRequest(t, router, http.MethodPost, "/api/v1/experiences", maintainer, expUniqueKey(t), `{bad`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", rec.Code)
	}
	// 403：outsider 创建项目经验
	rec = expRequest(t, router, http.MethodPost, "/api/v1/experiences", outsider, expUniqueKey(t),
		`{"project_id":"`+expProjectID+`","title":"t","content":"c"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider create = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 404：项目不存在
	rec = expRequest(t, router, http.MethodPost, "/api/v1/experiences", maintainer, expUniqueKey(t),
		`{"project_id":"d0000000-0000-4000-8000-000000009999","title":"t","content":"c"}`)
	if rec.Code != http.StatusNotFound || expErrorCode(t, rec) != "project_not_found" {
		t.Fatalf("missing project = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 403：member 创建全局经验 → 403（admin only）
	rec = expRequest(t, router, http.MethodPost, "/api/v1/experiences", member, expUniqueKey(t),
		`{"title":"全局","content":"c"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member global create = %d", rec.Code)
	}
	// 201：maintainer 创建成功
	rec = expRequest(t, router, http.MethodPost, "/api/v1/experiences", maintainer, expUniqueKey(t),
		`{"project_id":"`+expProjectID+`","title":"处理经验","content":"内容","tags":["rf"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == "" || envelope.Data.Status != StatusCandidate {
		t.Fatalf("create response: %+v", envelope.Data)
	}
	expPath := "/api/v1/experiences/" + envelope.Data.ID
	// 审计：POST 根路径派生 action = experiences
	assertExpAudit(t, db, envelope.RequestID, "experiences")

	// GET 列表：200
	rec = expRequest(t, router, http.MethodGet, "/api/v1/experiences?project_id="+expProjectID, maintainer, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 400：非法 status
	rec = expRequest(t, router, http.MethodGet, "/api/v1/experiences?project_id="+expProjectID+"&status=bogus", maintainer, "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status = %d", rec.Code)
	}

	// GET by id：200；member 看别人的 candidate → 403；404
	rec = expRequest(t, router, http.MethodGet, expPath, maintainer, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}
	rec = expRequest(t, router, http.MethodGet, expPath, member, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member get others candidate = %d", rec.Code)
	}
	rec = expRequest(t, router, http.MethodGet, "/api/v1/experiences/d0000000-0000-4000-8000-000000009999", maintainer, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get = %d", rec.Code)
	}

	// PATCH：200；不可修改字段（ai_generated）→ 400
	rec = expRequest(t, router, http.MethodPatch, expPath, maintainer, expUniqueKey(t),
		`{"title":"改名"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = expRequest(t, router, http.MethodPatch, expPath, maintainer, expUniqueKey(t),
		`{"ai_generated":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("immutable ai_generated = %d", rec.Code)
	}
	// PATCH：member 改别人的 candidate → 403
	rec = expRequest(t, router, http.MethodPatch, expPath, member, expUniqueKey(t), `{"title":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member update = %d", rec.Code)
	}

	// POST publish：200；member → 403
	rec = expRequest(t, router, http.MethodPost, expPath+"/publish", member, expUniqueKey(t), "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member publish = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = expRequest(t, router, http.MethodPost, expPath+"/publish", maintainer, expUniqueKey(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("publish = %d, body=%s", rec.Code, rec.Body.String())
	}
	var pubEnv struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pubEnv); err != nil {
		t.Fatal(err)
	}
	if pubEnv.Data.Status != StatusPublished {
		t.Fatalf("publish status = %q", pubEnv.Data.Status)
	}
	assertExpAudit(t, db, pubEnv.RequestID, "experiences."+envelope.Data.ID+".publish")
	// 重复 publish → 400 not_candidate
	rec = expRequest(t, router, http.MethodPost, expPath+"/publish", maintainer, expUniqueKey(t), "")
	if rec.Code != http.StatusBadRequest || expErrorCode(t, rec) != "not_candidate" {
		t.Fatalf("republish = %d, body=%s", rec.Code, rec.Body.String())
	}

	// POST archive：member → 403（owner 才能归档）；owner → 200
	rec = expRequest(t, router, http.MethodPost, expPath+"/archive", member, expUniqueKey(t), "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member archive = %d", rec.Code)
	}
	owner := expToken(t, expOwnerID, "owner", auth.RoleMember)
	rec = expRequest(t, router, http.MethodPost, expPath+"/archive", owner, expUniqueKey(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("archive = %d, body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pubEnv)
	assertExpAudit(t, db, pubEnv.RequestID, "experiences."+envelope.Data.ID+".archive")
	// 归档后再归档 → 400 not_published
	rec = expRequest(t, router, http.MethodPost, expPath+"/archive", owner, expUniqueKey(t), "")
	if rec.Code != http.StatusBadRequest || expErrorCode(t, rec) != "not_published" {
		t.Fatalf("rearchive = %d, body=%s", rec.Code, rec.Body.String())
	}

	// /candidates 别名：201（agent 语义路径；普通成员创建走同样校验）
	rec = expRequest(t, router, http.MethodPost, "/api/v1/experiences/candidates", maintainer, expUniqueKey(t),
		`{"project_id":"`+expProjectID+`","title":"别名创建","content":"c"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("candidates alias = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerExtractCandidatesPermissionAndUpstream(t *testing.T) {
	db := openExperiencesTestDB(t)
	router := newExperiencesTestRouter(t, db)
	member := expToken(t, expMemberID, "member", auth.RoleMember)
	admin := expToken(t, expAdminID, "admin", auth.RoleAdmin)

	// 未注入 issueSource/extractor → 502 upstream_error（not_configured 归入上游类）
	rec := expRequest(t, router, http.MethodPost, "/api/v1/experiences/extract-candidates", admin,
		expUniqueKey(t), `{"days":7}`)
	if rec.Code != http.StatusBadGateway || expErrorCode(t, rec) != "upstream_error" {
		t.Fatalf("unconfigured = %d, body=%s", rec.Code, rec.Body.String())
	}

	// 缺 Idempotency-Key → 400
	rec = expRequest(t, router, http.MethodPost, "/api/v1/experiences/extract-candidates", admin, "", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idempotency key = %d", rec.Code)
	}

	// 无 token → 401
	rec = expRequest(t, router, http.MethodPost, "/api/v1/experiences/extract-candidates", "",
		expUniqueKey(t), `{}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", rec.Code)
	}
	_ = member
}
