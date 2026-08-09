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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := prepareSQL(tc.sql)
			if err == nil {
				t.Fatalf("expected rejection for %q", tc.sql)
			}
		})
	}
}

func TestPrepareSQL_Allowed(t *testing.T) {
	cases := []struct {
		name, sql, wantTable string
		wantCapped           bool // 无 LIMIT 补 200 / 已有 LIMIT n>200 改写
		wantUnchanged        bool // 已有 LIMIT ≤200：原样保留
	}{
		{"simple", "SELECT * FROM logs", "logs", true, false},
		{"lowercase", "select id from logs where project_id = 'x'", "logs", true, false},
		{"leading whitespace", " \n\t SELECT id FROM projects", "projects", true, false},
		{"quoted table", `SELECT * FROM "logs"`, "logs", true, false},
		{"existing limit 100", "SELECT * FROM daily_reports LIMIT 100", "daily_reports", false, true},
		{"explicit limit 200", "SELECT * FROM issues LIMIT 200", "issues", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, table, truncated, err := prepareSQL(tc.sql)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if table != tc.wantTable {
				t.Fatalf("table = %q, want %q", table, tc.wantTable)
			}
			if tc.wantCapped {
				if !strings.Contains(out, "LIMIT 200") {
					t.Fatalf("expected LIMIT 200 in %q", out)
				}
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

func TestPrepareSQL_LimitRewrite(t *testing.T) {
	out, _, truncated, err := prepareSQL("SELECT * FROM logs LIMIT 5000")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "LIMIT 200") || strings.Contains(out, "5000") {
		t.Fatalf("LIMIT 5000 not rewritten: %q", out)
	}
	if truncated {
		t.Fatal("LIMIT rewrite is not actual truncation, must not set truncated")
	}

	out, _, _, err = prepareSQL("SELECT * FROM logs LIMIT ALL")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "LIMIT 200") {
		t.Fatalf("LIMIT ALL not rewritten: %q", out)
	}

	out, _, _, err = prepareSQL("SELECT * FROM logs LIMIT 5000 OFFSET 10")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "LIMIT 200") || !strings.Contains(out, "OFFSET 10") {
		t.Fatalf("rewrite dropped OFFSET or kept 5000: %q", out)
	}
}

func TestPrepareSQL_LimitStringLiteralUntouched(t *testing.T) {
	out, _, _, err := prepareSQL(`SELECT * FROM logs WHERE content = 'limit 5000'`)
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
		if _, _, _, err := prepareSQL(sql); err != nil {
			t.Fatalf("string literal keyword falsely rejected %q: %v", sql, err)
		}
	}
}

func TestPrepareSQL_QuotedWhitelistOK(t *testing.T) {
	out, table, _, err := prepareSQL(`SELECT * FROM "logs"`)
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
