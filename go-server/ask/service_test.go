package ask

import (
	"strings"
	"testing"
	"time"
)

func TestPrepareSQL_Rejected(t *testing.T) {
	cases := []struct {
		name, sql string
	}{
		{"FROM users", "SELECT * FROM users"},
		{"quoted users", `SELECT * FROM "users"`},
		{"schema qualified", "SELECT * FROM public.users"},
		{"CTE body FROM", "WITH t AS (SELECT * FROM users) SELECT * FROM t"},
		{"WITH write CTE", "WITH t AS (DELETE FROM logs RETURNING *) SELECT * FROM t"},
		{"select-list subquery", "SELECT id, (SELECT count(*) FROM users) FROM logs"},
		{"multi-statement", "SELECT * FROM logs; DROP TABLE logs"},
		{"line comment", "SELECT * FROM logs -- comment"},
		{"block comment", "SELECT * FROM logs /* comment */"},
		{"insert", "INSERT INTO logs (content) VALUES ('x')"},
		{"update", "UPDATE logs SET content='x'"},
		{"delete", "DELETE FROM logs"},
		{"copy", "COPY logs TO STDOUT"},
		{"join", "SELECT * FROM logs JOIN projects ON logs.project_id = projects.id"},
		{"comma join", "SELECT * FROM logs, projects"},
		{"comma join with alias", "SELECT * FROM logs t1, projects t2 WHERE t1.id = t2.id"},
		{"comma join with as alias", "SELECT * FROM logs AS l, projects AS p"},
		{"comma join mixed alias", "SELECT * FROM logs AS l, projects p2"},
		{"comma join via schema", "SELECT * FROM public.logs, projects"},
		{"comma self join", "SELECT * FROM projects p1, projects p2"},
		{"comma self join where", "SELECT * FROM projects p1, projects p2 WHERE p1.id=p2.id"},
		{"comma projects issues", "SELECT * FROM projects p, issues i"},
		{"cross-table subquery", "SELECT * FROM logs WHERE id IN (SELECT id FROM projects)"},
		{"link table in FROM", "SELECT * FROM daily_report_log_links"},
		{"pg_catalog", "SELECT * FROM pg_catalog.pg_tables"},
		{"information_schema", "SELECT * FROM information_schema.tables"},
		{"current_setting", "SELECT current_setting('statement_timeout')"},
		{"set_config", "SELECT set_config('statement_timeout', '1000', false)"},
		{"current_user", "SELECT current_user"},
		{"version()", "SELECT version()"},
		{"into write", "SELECT * INTO backup_logs FROM logs"},
		{"for share", "SELECT * FROM logs FOR SHARE"},
		{"subquery single table", "SELECT id, (SELECT count(*) FROM logs) FROM logs"},
		{"subquery in where", "SELECT * FROM logs WHERE id IN (SELECT id FROM logs)"},
		{"union", "SELECT * FROM logs UNION SELECT * FROM issues"},
		{"intersect", "SELECT * FROM logs INTERSECT SELECT * FROM logs"},
		{"except", "SELECT * FROM logs EXCEPT SELECT * FROM logs"},
		{"window over", "SELECT row_number() OVER (ORDER BY id) FROM logs"},
		{"non-select leading", "DELETE FROM logs"},
		{"empty", "   "},
		// R2 白名单收缩：个人表/无 project_id 的内容表/跨项目敏感表移出白名单。
		{"daily_reports removed", "SELECT * FROM daily_reports"},
		{"issue_comments removed", "SELECT * FROM issue_comments"},
		{"run_steps removed", "SELECT * FROM run_steps"},
		{"todos removed", "SELECT * FROM todos"},
		{"attachments removed", "SELECT * FROM attachments"},
		{"project_members removed", "SELECT * FROM project_members"},
		{"automation_rules removed", "SELECT * FROM automation_rules"},
		{"instrument_results removed", "SELECT * FROM instrument_results"},
		{"dollar-quote unmatched quote hides union (R1)", "SELECT 1 FROM logs WHERE $a$ ' $a$ IS NULL UNION SELECT usename FROM pg_user"},
		{"dollar-quote unmatched quote hides subquery (R1)", "SELECT id FROM logs WHERE $q$ ' $q$ = 1 AND id IN (SELECT id FROM users)"},
		{"dollar-quote unmatched quote hides pg_catalog (R1)", "SELECT * FROM logs WHERE $x$ ' $x$ IS NULL UNION SELECT * FROM pg_catalog.pg_tables"},
		{"dollar-quote unmatched quote hides write (R1)", "SELECT * FROM logs WHERE $w$ ' $w$ IS NULL UNION SELECT 1 INTO tmp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := prepareSQL(tc.sql, nil)
			if err == nil {
				t.Fatalf("expected rejection for %q", tc.sql)
			}
		})
	}
}

// prepareSQL 行级隔离注入的可访问项目集合（R2 测试固定值）。
var r2TestProjectIDs = []string{"11111111-1111-4111-8111-111111111111"}

func TestPrepareSQL_Allowed(t *testing.T) {
	filter := "project_id IN ('11111111-1111-4111-8111-111111111111')"
	projectsFilter := "id IN ('11111111-1111-4111-8111-111111111111')"
	cases := []struct {
		name, sql, wantTable, wantFilter string
		wantCapped                       bool // 无 LIMIT 补 200 / 已有 LIMIT n>200 改写
		wantUnchanged                    bool // 全局表 + 已有 LIMIT ≤200：原样保留
		wantLimit                        string
	}{
		{"simple", "SELECT * FROM logs", "logs", filter, true, false, ""},
		{"lowercase", "select id from logs where project_id = 'x'", "logs", filter, true, false, ""},
		{"leading whitespace", " \n\t SELECT id FROM projects", "projects", projectsFilter, true, false, ""},
		{"quoted table", `SELECT * FROM "logs"`, "logs", filter, true, false, ""},
		// 全局模板表（全员可读）不注入行级过滤，LIMIT 语义原样保留。
		{"existing limit 100 global table", "SELECT * FROM step_templates LIMIT 100", "step_templates", "", false, true, ""},
		{"explicit limit 200 global table", "SELECT * FROM step_template_items LIMIT 200", "step_template_items", "", false, true, ""},
		// 项目表带小 LIMIT：注入过滤但不改写 LIMIT 本身。
		{"projects alias small limit", "SELECT * FROM projects p LIMIT 5", "projects", projectsFilter, false, false, "LIMIT 5"},
		// dollar-quote 字面量内的关键字（合法字符串值）不得误拒。
		{"dollar-quote literal keywords allowed", "SELECT * FROM logs WHERE content = $a$union select from users$a$", "logs", filter, true, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, table, truncated, err := prepareSQL(tc.sql, r2TestProjectIDs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if table != tc.wantTable {
				t.Fatalf("table = %q, want %q", table, tc.wantTable)
			}
			if tc.wantFilter != "" && !strings.Contains(out, tc.wantFilter) {
				t.Fatalf("row filter %q missing in %q", tc.wantFilter, out)
			}
			if tc.wantFilter == "" && strings.Contains(out, " IN (") {
				t.Fatalf("global table must not be filtered: %q", out)
			}
			if tc.wantCapped {
				if !strings.Contains(out, "LIMIT 200") {
					t.Fatalf("expected LIMIT 200 in %q", out)
				}
			}
			if tc.wantLimit != "" && !strings.Contains(out, tc.wantLimit) {
				t.Fatalf("expected %s preserved in %q", tc.wantLimit, out)
			}
			if tc.wantUnchanged {
				if out != strings.TrimSpace(tc.sql) {
					t.Fatalf("SQL should be unchanged, got %q", out)
				}
				if truncated {
					t.Fatal("existing LIMIT <=200 must not set truncated")
				}
			}
		})
	}
}

// R2：行级隔离注入位置——有 WHERE 用 AND 连接、无 WHERE 插在 ORDER BY/LIMIT 前、
// 空可访问集合注入 IN (NULL)（恒假 0 行）。
func TestPrepareSQL_RowFilterInjection(t *testing.T) {
	pids := []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"}
	two := "project_id IN ('11111111-1111-4111-8111-111111111111','22222222-2222-4222-8222-222222222222')"

	out, _, _, err := prepareSQL("SELECT id FROM logs WHERE content = 'x' ORDER BY id", pids)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "WHERE "+two+" AND ( content = 'x' ORDER BY id)") {
		t.Fatalf("WHERE must be extended with AND: %q", out)
	}

	out, _, _, err = prepareSQL("SELECT id FROM logs ORDER BY id LIMIT 10", pids)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "WHERE "+two+" ORDER BY id LIMIT 10") {
		t.Fatalf("filter must precede ORDER BY/LIMIT: %q", out)
	}

	// WHERE 出现在字符串字面量中：不得被误当作注入点。
	out, _, _, err = prepareSQL(`SELECT id FROM logs WHERE content = 'where x' ORDER BY id`, pids)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, two+" AND ( content = 'where x' ORDER BY id)") {
		t.Fatalf("literal 'where' must not hijack injection point: %q", out)
	}

	// 空可访问集合 → IN (NULL) 恒假。
	out, _, _, err = prepareSQL("SELECT * FROM logs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "WHERE project_id IN (NULL)") {
		t.Fatalf("empty accessible set must inject IN (NULL): %q", out)
	}

	// projects 表用 id 列过滤。
	out, _, _, err = prepareSQL("SELECT * FROM projects", pids)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "WHERE id IN (") {
		t.Fatalf("projects must filter by id: %q", out)
	}
}

func TestPrepareSQL_LimitRewrite(t *testing.T) {
	out, _, truncated, err := prepareSQL("SELECT * FROM logs LIMIT 5000", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "LIMIT 200") || strings.Contains(out, "5000") {
		t.Fatalf("LIMIT 5000 not rewritten: %q", out)
	}
	if truncated {
		t.Fatal("LIMIT rewrite is not actual truncation, must not set truncated")
	}

	out, _, _, err = prepareSQL("SELECT * FROM logs LIMIT ALL", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "LIMIT 200") {
		t.Fatalf("LIMIT ALL not rewritten: %q", out)
	}

	out, _, _, err = prepareSQL("SELECT * FROM logs LIMIT 5000 OFFSET 10", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "LIMIT 200") || !strings.Contains(out, "OFFSET 10") {
		t.Fatalf("rewrite dropped OFFSET or kept 5000: %q", out)
	}
}

func TestPrepareSQL_LimitStringLiteralUntouched(t *testing.T) {
	out, _, _, err := prepareSQL(`SELECT * FROM logs WHERE content = 'limit 5000'`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "'limit 5000'") {
		t.Fatalf("string literal must not be rewritten: %q", out)
	}
	if !strings.Contains(out, "LIMIT 200") {
		t.Fatalf("expected appended LIMIT 200: %q", out)
	}
}

func TestPrepareSQL_StringLiteralKeywordsAllowed(t *testing.T) {
	cases := []string{
		`SELECT * FROM logs WHERE content = 'from users'`,
		`SELECT * FROM logs WHERE content = 'into the woods'`,
		`SELECT * FROM logs WHERE content = 'delete me'`,
		`SELECT * FROM logs WHERE content = 'limit 5000'`,
	}
	for _, sql := range cases {
		if _, _, _, err := prepareSQL(sql, nil); err != nil {
			t.Fatalf("string literal keyword falsely rejected %q: %v", sql, err)
		}
	}
}

func TestPrepareSQL_QuotedWhitelistOK(t *testing.T) {
	out, table, _, err := prepareSQL(`SELECT * FROM "logs"`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if table != "logs" || !strings.Contains(out, `"logs"`) {
		t.Fatalf("quoted whitelist table failed: table=%q out=%q", table, out)
	}
}

func TestDedupColumns(t *testing.T) {
	out := dedupColumns([]string{"id", "id", "content"}, "logs")
	want := []string{"id", "logs.id", "content"}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("out[%d] = %q, want %q (all: %v)", i, out[i], want[i], out)
		}
	}
}

func TestNormalizeValue(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if got := normalizeValue(now, ""); got != "2026-08-09T12:00:00Z" {
		t.Fatalf("time: got %v", got)
	}
	if got := normalizeValue([]byte(`{"a":1}`), "jsonb"); got == nil {
		t.Fatal("valid JSONB should be preserved")
	}
	if got := normalizeValue([]byte{0x00, 0xff, 0x01}, "bytea"); got != "00ff01" {
		t.Fatalf("bytea should be hex, got %v", got)
	}
	// UUID 列：lib/pq 返回 16 字节 []byte，须格式化为标准 UUID 字符串。
	uuidBytes := []byte{0x3a, 0x60, 0x27, 0x02, 0x02, 0xb6, 0x49, 0x59, 0x95, 0x7c, 0xac, 0xd6, 0xec, 0xbd, 0x97, 0xca}
	if got := normalizeValue(uuidBytes, "uuid"); got != "3a602702-02b6-4959-957c-acd6ecbd97ca" {
		t.Fatalf("uuid: got %v", got)
	}
	// 无列类型上下文时按 16 字节兜底。
	if got := normalizeValue(uuidBytes, ""); got != "3a602702-02b6-4959-957c-acd6ecbd97ca" {
		t.Fatalf("uuid fallback: got %v", got)
	}
	if got := normalizeValue(3.14, ""); got != 3.14 {
		t.Fatalf("float: got %v", got)
	}
	if normalizeValue(nil, "") != nil {
		t.Fatal("nil should stay nil")
	}
}

func TestCapSnapshot(t *testing.T) {
	long := strings.Repeat("长", 2000)
	rows := []map[string]any{{"id": "1", "project_id": "p1", "content": long}}
	cols, out, truncated := capSnapshot([]string{"id", "project_id", "content"}, rows)
	if !truncated {
		t.Fatal("cell truncation should set truncated")
	}
	if got := out[0]["content"].(string); len([]rune(got)) != cellMaxRunes {
		t.Fatalf("cell not truncated to %d runes, got %d", cellMaxRunes, len([]rune(got)))
	}
	if len(cols) != 3 {
		t.Fatalf("small snapshot must not prune columns: %v", cols)
	}

	// 超出 256KB：优先保留 id/project_id，裁掉其余大列。
	big := strings.Repeat("x", 500)
	rows = make([]map[string]any, 200)
	for i := range rows {
		rows[i] = map[string]any{"id": "1", "project_id": "p1", "content": big, "meta": big, "extra": big, "extra2": big}
	}
	cols, out, truncated = capSnapshot([]string{"id", "project_id", "content", "meta", "extra", "extra2"}, rows)
	if !truncated {
		t.Fatal("budget cut should set truncated")
	}
	if len(cols) >= len([]string{"id", "project_id", "content", "meta", "extra", "extra2"}) {
		t.Fatalf("expected some columns pruned, got %v", cols)
	}
	if cols[0] != "id" || cols[1] != "project_id" {
		t.Fatalf("id/project_id must be kept first, got %v", cols)
	}
	if len(out) != 200 {
		t.Fatalf("200 rows should fit after column pruning, got %d", len(out))
	}
	if snapshotSize(out) > snapshotBudget {
		t.Fatalf("snapshot over budget: %d bytes", snapshotSize(out))
	}
}
