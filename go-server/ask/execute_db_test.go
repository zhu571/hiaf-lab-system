package ask

import (
	"context"
	"errors"
	"testing"
)

// Execute 集成测试：需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移 001-036，
// ask_reader 角色由 033 迁移创建）。
// 覆盖 SET LOCAL ROLE ask_reader + 只读事务 + LIMIT 封顶 + 行集序列化全链路。
// askUserID 是迁移 009 种子 admin（haofan，三个种子项目的 owner）——行级隔离下
// 语义等价于修复前（全部 active 项目可见）。
func TestExecuteDB(t *testing.T) {
	db := openAskTestDB(t)
	defer db.Close()
	svc := NewService(NewRepository(db), db)

	resp, err := svc.Execute(context.Background(), askUserID, "SELECT id, content FROM logs LIMIT 5")
	if err != nil {
		t.Fatalf("Execute valid sql: %v", err)
	}
	if resp.TableName != "logs" || resp.RowCount > 5 || len(resp.Rows) != resp.RowCount {
		t.Fatalf("unexpected response: table=%q rows=%d", resp.TableName, resp.RowCount)
	}
	if len(resp.Columns) == 0 {
		t.Fatal("columns missing")
	}
	if resp.Truncated {
		t.Fatal("LIMIT 5 with 5 rows must not be truncated")
	}

	resp, err = svc.Execute(context.Background(), askUserID, "SELECT * FROM logs")
	if err != nil {
		t.Fatalf("Execute without limit: %v", err)
	}
	if resp.RowCount > maxRows {
		t.Fatalf("rows over cap: %d", resp.RowCount)
	}
	if resp.Truncated && resp.RowCount != maxRows {
		t.Fatalf("truncated must imply hitting row cap, got rows=%d truncated=%v", resp.RowCount, resp.Truncated)
	}

	if _, err := svc.Execute(context.Background(), askUserID, "SELECT * FROM users"); !errors.Is(err, ErrSQLRejected) {
		t.Fatalf("users must be rejected at parser layer, got %v", err)
	}

	// JSONB 保留结构（projects.tags_json）。
	resp, err = svc.Execute(context.Background(), askUserID, "SELECT id, tags_json FROM projects LIMIT 1")
	if err != nil {
		t.Fatalf("Execute jsonb: %v", err)
	}
	if len(resp.Rows) > 0 {
		if v, ok := resp.Rows[0]["tags_json"]; !ok || v == nil {
			t.Fatalf("tags_json should be preserved as JSON, got %v", resp.Rows[0])
		}
	}

	// UUID 列（test_data.id）应序列化为标准 UUID 字符串而非 null。
	resp, err = svc.Execute(context.Background(), askUserID, "SELECT id, project_id FROM test_data LIMIT 1")
	if err != nil {
		t.Fatalf("Execute uuid: %v", err)
	}
	if len(resp.Rows) > 0 {
		for _, c := range []string{"id", "project_id"} {
			v, ok := resp.Rows[0][c]
			s, isStr := v.(string)
			if !ok || !isStr || len(s) != 36 {
				t.Fatalf("%s should be a 36-char UUID string, got %v", c, v)
			}
		}
	}
}

// R2 行级隔离：Execute 按调用方可访问项目集合注入 project_id IN (...)——
// 普通用户查不到他人项目的数据；个人表（daily_reports）已移出白名单；
// 无用户上下文整体拒绝。固定 UUID 段 7axx 避开其他测试用段。
const (
	r2User1ID  = "7a000000-0000-4000-8000-000000000001"
	r2User2ID  = "7a000000-0000-4000-8000-000000000002"
	r2ViewrID  = "7a000000-0000-4000-8000-000000000003"
	r2Proj1ID  = "7b000000-0000-4000-8000-000000000001"
	r2Proj2ID  = "7b000000-0000-4000-8000-000000000002"
	r2Log1aID  = "7c000000-0000-4000-8000-000000000001"
	r2Log1bID  = "7c000000-0000-4000-8000-000000000002"
	r2Log2aID  = "7c000000-0000-4000-8000-000000000003"
	r2ReportID = "7d000000-0000-4000-8000-000000000001"
)

func TestExecuteDB_RowLevelIsolation(t *testing.T) {
	db := openAskTestDB(t)
	defer db.Close()
	svc := NewService(NewRepository(db), db)

	cleanup := func() {
		db.Exec(`DELETE FROM logs WHERE id IN ($1,$2,$3)`, r2Log1aID, r2Log1bID, r2Log2aID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, r2ReportID)
		db.Exec(`DELETE FROM project_members WHERE project_id IN ($1,$2)`, r2Proj1ID, r2Proj2ID)
		db.Exec(`DELETE FROM projects WHERE id IN ($1,$2)`, r2Proj1ID, r2Proj2ID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`, r2User1ID, r2User2ID, r2ViewrID)
	}
	cleanup()
	defer cleanup()

	if _, err := db.Exec(
		`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw)
		 VALUES ($1,'ask_r2_u1','x','R2 U1','member',false),
		        ($2,'ask_r2_u2','x','R2 U2','member',false),
		        ($3,'ask_r2_viewer','x','R2 Viewer','viewer',false)`,
		r2User1ID, r2User2ID, r2ViewrID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO projects (id, code, name, status, visibility, owner_user_id, created_by)
		 VALUES ($1,'ASK-R2-P1','R2 项目一','active','restricted',$3,$3),
		        ($2,'ASK-R2-P2','R2 项目二','active','restricted',$4,$4)`,
		r2Proj1ID, r2Proj2ID, r2User1ID, r2User2ID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 VALUES ($1,$2,'member','active',$2),($3,$4,'member','active',$4)`,
		r2Proj1ID, r2User1ID, r2Proj2ID, r2User2ID); err != nil {
		t.Fatal(err)
	}
	for _, l := range []struct{ id, project, author, content string }{
		{r2Log1aID, r2Proj1ID, r2User1ID, "项目一日志 A"},
		{r2Log1bID, r2Proj1ID, r2User1ID, "项目一日志 B"},
		{r2Log2aID, r2Proj2ID, r2User2ID, "项目二日志 A"},
	} {
		if _, err := db.Exec(
			`INSERT INTO logs (id, project_id, author_id, category, content, source, content_status)
			 VALUES ($1,$2,$3,'general',$4,'manual','draft')`, l.id, l.project, l.author, l.content); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO daily_reports (id, report_date, author_id, raw_text, summary, content_status)
		 VALUES ($1, CURRENT_DATE, $2, '他人日报全文', '', 'confirmed')`, r2ReportID, r2User2ID); err != nil {
		t.Fatal(err)
	}

	// 用户 1（仅项目一成员）：只能看到项目一的日志。
	resp, err := svc.Execute(context.Background(), r2User1ID, "SELECT id, project_id, content FROM logs")
	if err != nil {
		t.Fatalf("u1 execute: %v", err)
	}
	if resp.RowCount != 2 {
		t.Fatalf("u1 must see exactly 2 rows (project 1 only), got %d", resp.RowCount)
	}
	for _, row := range resp.Rows {
		if row["project_id"] != r2Proj1ID {
			t.Fatalf("u1 leaked foreign project row: %v", row)
		}
	}

	// 用户 2（仅项目二成员）：只能看到项目二。
	resp, err = svc.Execute(context.Background(), r2User2ID, "SELECT id, project_id FROM logs")
	if err != nil {
		t.Fatalf("u2 execute: %v", err)
	}
	if resp.RowCount != 1 || resp.Rows[0]["project_id"] != r2Proj2ID {
		t.Fatalf("u2 must see exactly project-2 rows, got %+v", resp.Rows)
	}

	// 无任何项目成员关系的 viewer：0 行（IN (NULL) 恒假）。
	resp, err = svc.Execute(context.Background(), r2ViewrID, "SELECT * FROM logs")
	if err != nil {
		t.Fatalf("viewer execute: %v", err)
	}
	if resp.RowCount != 0 {
		t.Fatalf("viewer without membership must see 0 rows, got %d", resp.RowCount)
	}

	// 项目表查询附带 WHERE 时注入为 AND（不破坏原语义）。
	resp, err = svc.Execute(context.Background(), r2User1ID, "SELECT id FROM logs WHERE content = '项目一日志 A'")
	if err != nil {
		t.Fatalf("u1 where-clause execute: %v", err)
	}
	if resp.RowCount != 1 {
		t.Fatalf("u1 where-clause must match 1 row, got %d", resp.RowCount)
	}

	// 个人表已移出白名单：不能经 ask 读到他人日报。
	if _, err := svc.Execute(context.Background(), r2User1ID, "SELECT * FROM daily_reports LIMIT 10"); !errors.Is(err, ErrSQLRejected) {
		t.Fatalf("daily_reports must be rejected (personal table), got %v", err)
	}

	// 无用户上下文：整体拒绝（fail-closed）。
	if _, err := svc.Execute(context.Background(), "", "SELECT * FROM logs"); !errors.Is(err, ErrSQLRejected) {
		t.Fatalf("missing user context must be rejected, got %v", err)
	}
	if _, err := svc.Execute(context.Background(), "   ", "SELECT * FROM logs"); !errors.Is(err, ErrSQLRejected) {
		t.Fatalf("blank user context must be rejected, got %v", err)
	}
}
