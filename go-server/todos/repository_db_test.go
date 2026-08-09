package todos

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// 集成测试：需要 TEST_DATABASE_URL（CI/本地按 AGENTS.md 起 postgres 并跑完 001-027 迁移）。
// 本测试写真实 todos 表，结束后清理所有本用例创建的行。

const (
	dbUserID    = "00000000-0000-0000-0000-00000000b001"
	dbUserID2   = "00000000-0000-0000-0000-00000000b002"
	dbProjectID = "00000000-0000-0000-0000-00000000b003"
	dbIssueID   = "00000000-0000-0000-0000-00000000b004"
)

func openTestDB(t *testing.T) *sql.DB {
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
	// 种子数据（幂等 ON CONFLICT）
	_, err = db.Exec(`
		INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
		VALUES ($1, 'todos_alice', 'x', 'Alice', 'member', false, false),
		       ($2, 'todos_bob', 'x', 'Bob', 'member', false, false)
		ON CONFLICT (id) DO NOTHING`, dbUserID, dbUserID2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO projects (id, code, name, status, visibility, owner_user_id, created_by)
		VALUES ($1, 'TST', 'Todo Test', 'active', 'restricted', $2, $2)
		ON CONFLICT (id) DO NOTHING`, dbProjectID, dbUserID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO issues (id, project_id, title, status, severity, author_id, assignee_id, report_date, occurred_at)
		VALUES ($1, $2, '真空度异常', 'open', 'high', $3, $3, CURRENT_DATE, now())
		ON CONFLICT (id) DO NOTHING`, dbIssueID, dbProjectID, dbUserID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM todos WHERE created_by IN ($1,$2)`, dbUserID, dbUserID2)
		db.Exec(`DELETE FROM issues WHERE id=$1`, dbIssueID)
		db.Exec(`DELETE FROM projects WHERE id=$1`, dbProjectID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, dbUserID, dbUserID2)
	})
	return db
}

func TestRepositoryStateGuards(t *testing.T) {
	db := openTestDB(t)
	r := NewRepository(db)

	todo, err := r.Create(&Todo{Title: "写日报", Priority: PriorityMedium, Status: StatusPending, Source: SourceManual, CreatedBy: dbUserID, CreatedFor: "2026-08-07"})
	if err != nil {
		t.Fatal(err)
	}

	// pending → done
	now := time.Now()
	ok, err := r.UpdateDone(todo.ID, dbUserID, now)
	if err != nil || !ok {
		t.Fatalf("done guard: ok=%v err=%v", ok, err)
	}
	got, _ := r.GetByID(todo.ID)
	if got.Status != StatusDone || got.CompletedBy == nil || *got.CompletedBy != dbUserID || got.CompletedAt == nil {
		t.Fatalf("unexpected done row: %+v", got)
	}
	// done 再 done → 0 rows
	ok, err = r.UpdateDone(todo.ID, dbUserID, now)
	if err != nil || ok {
		t.Fatalf("double done must be rejected: ok=%v err=%v", ok, err)
	}

	// 新 todo：pending → deferred
	todo2, _ := r.Create(&Todo{Title: "任务2", Priority: PriorityLow, Status: StatusPending, Source: SourceManual, CreatedBy: dbUserID, CreatedFor: "2026-08-07"})
	ok, err = r.UpdateDefer(todo2.ID, "2026-08-08", now)
	if err != nil || !ok {
		t.Fatalf("defer guard: ok=%v err=%v", ok, err)
	}
	got, _ = r.GetByID(todo2.ID)
	if got.Status != StatusDeferred || got.CreatedFor != "2026-08-08" {
		t.Fatalf("unexpected deferred row: %+v", got)
	}
	// deferred 再 defer → 0 rows
	ok, err = r.UpdateDefer(todo2.ID, "2026-08-09", now)
	if err != nil || ok {
		t.Fatalf("defer on deferred must be rejected: ok=%v err=%v", ok, err)
	}

	// edit 乐观锁
	stale := now.Add(-time.Hour)
	title := "改标题"
	ok, err = r.UpdateEdit(todo2.ID, stale, UpdateRequest{Title: &title}, now)
	if err != nil || ok {
		t.Fatalf("stale edit must be rejected: ok=%v err=%v", ok, err)
	}
	ok, err = r.UpdateEdit(todo2.ID, got.UpdatedAt, UpdateRequest{Title: &title, Priority: ptr(PriorityHigh)}, now)
	if err != nil || !ok {
		t.Fatalf("current edit: ok=%v err=%v", ok, err)
	}
	got, _ = r.GetByID(todo2.ID)
	if got.Title != "改标题" || got.Priority != PriorityHigh {
		t.Fatalf("edit not applied: %+v", got)
	}
}

// terminalIssueIDs 模拟注入的 issueStatusResolver：返回终态 issue id 集合（两段式语义）。
func terminalIssueIDs(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT id FROM issues WHERE status IN ('resolved','closed')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func TestRepositoryRolloverAndIssueSync(t *testing.T) {
	db := openTestDB(t)
	r := NewRepository(db)
	now := time.Now()
	issueID := dbIssueID

	expired, _ := r.Create(&Todo{Title: "过期", Priority: PriorityMedium, Status: StatusPending, Source: SourceManual, CreatedBy: dbUserID, CreatedFor: "2026-08-01"})
	deferredToday, _ := r.Create(&Todo{Title: "今日推迟", Priority: PriorityMedium, Status: StatusDeferred, Source: SourceManual, CreatedBy: dbUserID, CreatedFor: "2026-08-07"})
	doneOld, _ := r.Create(&Todo{Title: "旧完成", Priority: PriorityMedium, Status: StatusDone, Source: SourceManual, CreatedBy: dbUserID, CreatedFor: "2026-07-01"})

	// rollover：过期 pending + 今日 deferred → pending/today
	n, err := r.Rollover("2026-08-07", now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rollover, got %d", n)
	}
	for _, id := range []string{expired.ID, deferredToday.ID} {
		got, _ := r.GetByID(id)
		if got.Status != StatusPending || got.CreatedFor != "2026-08-07" {
			t.Fatalf("rollover failed for %s: %+v", id, got)
		}
	}

	// issue_sync：resolved issue → 在途待办 cancelled（completed_at 为空）
	issueTodo, err := r.Create(&Todo{Title: "真空度异常", Priority: PriorityHigh, Status: StatusPending, Source: SourceIssue, CreatedBy: dbUserID, CreatedFor: "2026-08-07", IssueID: &issueID})
	if err != nil {
		t.Fatal(err)
	}
	// 先验证部分唯一索引：同 issue 第二条在途插入被跳过
	ok, err := r.InsertGenerated(&Todo{Title: "重复", Priority: PriorityHigh, Status: StatusPending, Source: SourceIssue, CreatedBy: dbUserID, CreatedFor: "2026-08-07", IssueID: &issueID})
	if err != nil || ok {
		t.Fatalf("duplicate inflight issue insert must be skipped: ok=%v err=%v", ok, err)
	}
	// resolved 后唯一索引释放，可重新生成
	if _, err := db.Exec(`UPDATE issues SET status='resolved', resolved_at=now() WHERE id=$1`, dbIssueID); err != nil {
		t.Fatal(err)
	}
	got, _ := r.GetByID(issueTodo.ID)
	if got.Status != StatusPending {
		t.Fatalf("issue todo should be pending before sync: %+v", got)
	}
	// 两段式：先经 resolver 取终态 id 集合，再更新（todos 不直读 issues 表）
	ids, err := terminalIssueIDs(db)
	if err != nil {
		t.Fatal(err)
	}
	n, err = r.IssueSync(ids)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 issue sync, got %d", n)
	}
	got, _ = r.GetByID(issueTodo.ID)
	if got.Status != StatusCancelled || got.CompletedAt != nil || got.CompletedBy != nil {
		t.Fatalf("issue sync must cancel without completion fields: %+v", got)
	}
	// 幂等：重复执行结果一致（已 cancelled 不再计入）
	n, err = r.IssueSync(ids)
	if err != nil || n != 0 {
		t.Fatalf("issue sync must be idempotent: n=%d err=%v", n, err)
	}
	// 空集合 → 直接返回 0
	n, err = r.IssueSync(nil)
	if err != nil || n != 0 {
		t.Fatalf("empty terminal ids must be a no-op: n=%d err=%v", n, err)
	}
	// 释放后可重新生成
	ok, err = r.InsertGenerated(&Todo{Title: "重新生成", Priority: PriorityHigh, Status: StatusPending, Source: SourceIssue, CreatedBy: dbUserID, CreatedFor: "2026-08-08", IssueID: &issueID})
	if err != nil || !ok {
		t.Fatalf("re-insert after resolve must succeed: ok=%v err=%v", ok, err)
	}

	// cleanup：done/cancelled 按 created_for（< 30 天前），in-flight 按 created_at 兜底
	_, err = db.Exec(`UPDATE todos SET created_at = now() - interval '35 days' WHERE id = $1`, expired.ID)
	if err != nil {
		t.Fatal(err)
	}
	dc, inf, err := r.Cleanup("2026-07-08", now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if dc != 1 || inf != 1 {
		t.Fatalf("cleanup counts: dc=%d inf=%d", dc, inf)
	}
	gotOld, err := r.GetByID(doneOld.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotOld != nil {
		t.Fatalf("done old should be deleted, got %+v", gotOld)
	}
}

func TestRepositoryListAndOpenForUser(t *testing.T) {
	db := openTestDB(t)
	r := NewRepository(db)

	pid := dbProjectID
	r.Create(&Todo{Title: "我的", Priority: PriorityMedium, Status: StatusPending, Source: SourceManual, CreatedBy: dbUserID, CreatedFor: "2026-08-07"})
	shared, _ := r.Create(&Todo{Title: "共享", Priority: PriorityHigh, Status: StatusPending, Source: SourceManual, CreatedBy: dbUserID, CreatedFor: "2026-08-07", ProjectID: &pid})
	r.Create(&Todo{Title: "他人的", Priority: PriorityMedium, Status: StatusPending, Source: SourceManual, CreatedBy: dbUserID2, CreatedFor: "2026-08-07"})
	r.Create(&Todo{Title: "已完成", Priority: PriorityMedium, Status: StatusDone, Source: SourceManual, CreatedBy: dbUserID, CreatedFor: "2026-08-07"})

	// scope=mine：只看自己
	items, err := r.List(dbUserID, nil, ListParams{Date: "2026-08-07", Scope: ScopeMine, Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("scope=mine expected 3, got %d", len(items))
	}

	// scope=shared 非成员（projectIDs 空）→ 空
	items, err = r.List(dbUserID2, nil, ListParams{Date: "2026-08-07", Scope: ScopeShared, Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("non-member shared expected 0, got %d", len(items))
	}

	// scope=shared 成员（u2 在 p1）→ 只含共享项（不含自己的个人项）
	items, err = r.List(dbUserID2, []string{dbProjectID}, ListParams{Date: "2026-08-07", Scope: ScopeShared, Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "共享" {
		t.Fatalf("member shared expected only shared item, got %+v", items)
	}

	// 推送口径（OpenVisibleForUser）：成员可见自己个人项 + 项目内他人共享项
	vis, err := r.OpenVisibleForUser(dbUserID2, []string{dbProjectID}, "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	if len(vis) != 2 {
		t.Fatalf("member visible expected 2 (own + shared), got %+v", vis)
	}
	// 非成员（projectIDs 空）→ 只推自己的
	vis, err = r.OpenVisibleForUser(dbUserID2, nil, "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	if len(vis) != 1 || vis[0].Title != "他人的" {
		t.Fatalf("non-member visible expected only own item, got %+v", vis)
	}

	// status=open：排除 done/cancelled；deferred 移到明天后不再出现在今日 open
	r.UpdateDefer(shared.ID, "2026-08-08", time.Now())
	items, err = r.List(dbUserID, nil, ListParams{Date: "2026-08-07", Scope: ScopeMine, Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Status == StatusDone || it.Status == StatusCancelled {
			t.Fatalf("open filter leaked %s", it.Status)
		}
	}
	if len(items) != 1 || items[0].Title != "我的" {
		t.Fatalf("open expected only pending item, got %+v", items)
	}
	// 明日（08-08）open 含 deferred 项
	items, err = r.List(dbUserID, nil, ListParams{Date: "2026-08-08", Scope: ScopeMine, Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != StatusDeferred || items[0].Title != "共享" {
		t.Fatalf("tomorrow open should contain deferred item, got %+v", items)
	}

	// OpenForUserAndDate → OpenVisibleForUser：排除 done/cancelled
	open, err := r.OpenVisibleForUser(dbUserID, nil, "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range open {
		if it.Status == StatusDone || it.Status == StatusCancelled {
			t.Fatalf("open-for-user leaked %s", it.Status)
		}
	}
	if len(open) != 1 || open[0].Title != "我的" {
		t.Fatalf("open-for-user expected only pending item, got %+v", open)
	}

	// InflightIssueIDs
	inflight, err := r.InflightIssueIDs(dbUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(inflight) != 0 {
		t.Fatalf("expected no inflight issue ids, got %v", inflight)
	}
}

func TestRepositoryCleanupHintCandidates(t *testing.T) {
	db := openTestDB(t)
	r := NewRepository(db)

	r.Create(&Todo{Title: "近期完成", Priority: PriorityMedium, Status: StatusDone, Source: SourceManual, CreatedBy: dbUserID, CreatedFor: "2026-07-10"})
	r.Create(&Todo{Title: "很久以前完成", Priority: PriorityMedium, Status: StatusDone, Source: SourceManual, CreatedBy: dbUserID, CreatedFor: "2026-06-01"})

	candidates, err := r.CleanupHintCandidates(dbUserID, "2026-07-09")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Title != "近期完成" {
		t.Fatalf("expected 1 candidate, got %+v", candidates)
	}
}
