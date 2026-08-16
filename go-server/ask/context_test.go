package ask

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestChatPayload AI-3：context 非空时注入 payload，为空时省略 context 键（零行为变化）；
// user_id（R2）始终随载荷下发，py-agent 在 execute 回调时回传做行级隔离。
func TestChatPayload(t *testing.T) {
	payload := chatPayload("问题", "schema", "", "u1")
	if _, ok := payload["context"]; ok {
		t.Fatal("empty context must be omitted from payload")
	}
	if payload["user_id"] != "u1" {
		t.Fatalf("user_id must be sent to py-agent, got: %v", payload["user_id"])
	}

	const ctxText = "最近 7 天日报摘要：\n- 2026-08-11: 完成预冷"
	payload = chatPayload("问题", "schema", ctxText, "u2")
	if payload["context"] != ctxText {
		t.Fatalf("context not injected: %v", payload["context"])
	}
	if payload["question"] != "问题" || payload["schema"] != "schema" || payload["user_id"] != "u2" {
		t.Fatalf("question/schema/user_id dropped: %v", payload)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatal(err)
	}
	if round["context"] != ctxText {
		t.Fatalf("round-trip context mismatch: %v", round)
	}
}

// TestBuildContext AI-3：组装最近 7 天日报（summary 优先、空 summary 回退 raw_text、
// 单条 200 字截断）+ 最近项目；总量 <4K 字符。
func TestBuildContext(t *testing.T) {
	db := openAskTestDB(t)
	defer db.Close()
	ensureAskFixture(t, db)
	svc := NewService(NewRepository(db), db)

	cleanup := func() {
		db.Exec(`DELETE FROM daily_reports WHERE author_id = $1 AND report_date >= CURRENT_DATE - 10`, askUserID)
		// 只清本用例项目的成员行——fixture 项目（ASK-FIXTURE）的成员关系归 ensureAskFixture 维护。
		db.Exec(`DELETE FROM project_members WHERE user_id = $1
		          AND project_id IN (SELECT id FROM projects WHERE code = 'AI3-CTX-TEST')`, askUserID)
		db.Exec(`DELETE FROM projects WHERE code = 'AI3-CTX-TEST'`)
	}
	cleanup()
	defer cleanup()

	longSummary := strings.Repeat("长", 500)
	if _, err := db.Exec(
		`INSERT INTO daily_reports (report_date, author_id, raw_text, summary, content_status)
		 VALUES (CURRENT_DATE, $1, 'raw body', $2, 'confirmed')`, askUserID, longSummary); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO daily_reports (report_date, author_id, raw_text, summary, content_status)
		 VALUES (CURRENT_DATE - 2, $1, $2, '', 'confirmed')`, askUserID, "纯 raw_text 日报"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO projects (code, name, status, visibility, owner_user_id, created_by)
		 VALUES ('AI3-CTX-TEST', 'AI3 测试项目', 'active', 'restricted', $1, $1)`,
		askUserID); err != nil {
		t.Fatal(err)
	}
	// R2：上下文项目节只取本人 active 成员项目，须补成员关系。
	if _, err := db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 SELECT id, $1, 'member', 'active', $1 FROM projects WHERE code = 'AI3-CTX-TEST'`,
		askUserID); err != nil {
		t.Fatal(err)
	}

	got, err := svc.buildContext(context.Background(), askUserID)
	if err != nil {
		t.Fatalf("buildContext: %v", err)
	}
	if !strings.HasPrefix(got, "最近 7 天日报摘要：") {
		t.Fatalf("context must start with report section: %.120s", got)
	}
	if !strings.Contains(got, "AI3 测试项目") || !strings.Contains(got, "AI3-CTX-TEST") {
		t.Fatalf("project section missing: %.240s", got)
	}
	if !strings.Contains(got, "纯 raw_text 日报") {
		t.Fatalf("raw_text fallback missing: %.240s", got)
	}
	if strings.Contains(got, strings.Repeat("长", 201)) {
		t.Fatal("long summary must be truncated to 200 runes per entry")
	}
	if n := len([]rune(got)); n >= contextBudgetRunes {
		t.Fatalf("context over budget: %d runes", n)
	}
}
