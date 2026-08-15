package ask

import (
	"strings"
	"testing"
	"time"
)

func TestStripStrings(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// 掩码按字节原位替换：引号连同串内字符逐字节变空格，串外空格保留。
		{"SELECT 'a' FROM logs", "SELECT     FROM logs"},
		{"SELECT 'it''s' FROM logs", "SELECT         FROM logs"},
		{`SELECT "id" FROM logs`, "SELECT      FROM logs"},
		{`SELECT * FROM "logs" WHERE x='y'`, "SELECT * FROM        WHERE x=   "},
		{"SELECT 'unclosed FROM logs", "SELECT                    "},
		{"SELECT * FROM logs", "SELECT * FROM logs"},
	}
	for _, tc := range cases {
		if got := stripStrings(tc.in); got != tc.want {
			t.Errorf("stripStrings(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractFromTables(t *testing.T) {
	cases := []struct {
		sql  string
		want []string
	}{
		{"SELECT * FROM logs", []string{"logs"}},
		{"SELECT * FROM logs l WHERE l.id=1", []string{"logs"}},
		{"SELECT * FROM logs AS l", []string{"logs"}},
		{"SELECT * FROM public.projects", []string{"projects"}},
		{`SELECT * FROM "logs"`, []string{"logs"}},
		{`SELECT * FROM public."projects"`, []string{"projects"}},
		{"SELECT * FROM logs, projects", []string{"logs", "projects"}},
		// JOIN 是 FROM 子句终止关键字：extractFromTables 只解析出前一张表，
		// JOIN 本身的拦截由 joinRe 在 prepareSQL 内完成。
		{"SELECT * FROM logs JOIN projects ON ...", []string{"logs"}},
		{"SELECT * FROM logs WHERE content='from x'", []string{"logs"}},
		{"SELECT 1", nil},
		{"SELECT * FROM logs ORDER BY id", []string{"logs"}},
		{"SELECT * FROM logs LIMIT 5", []string{"logs"}},
	}
	for _, tc := range cases {
		got, err := extractFromTables(tc.sql)
		if err != nil {
			t.Errorf("extractFromTables(%q) error: %v", tc.sql, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("extractFromTables(%q) = %v, want %v", tc.sql, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("extractFromTables(%q) = %v, want %v", tc.sql, got, tc.want)
				break
			}
		}
	}
}

func TestReadTableRef(t *testing.T) {
	name, after, ok := readTableRef(`"quoted" rest`)
	if !ok || name != "quoted" || after != " rest" {
		t.Fatalf("quoted: %q %q %v", name, after, ok)
	}
	name, after, ok = readTableRef("logs l")
	if !ok || name != "logs" || after != " l" {
		t.Fatalf("plain: %q %q %v", name, after, ok)
	}
	name, after, ok = readTableRef("public.projects x")
	if !ok || name != "projects" || after != " x" {
		t.Fatalf("schema: %q %q %v", name, after, ok)
	}
	name, after, ok = readTableRef(`public."projects"`)
	if !ok || name != "projects" {
		t.Fatalf("schema quoted: %q %q %v", name, after, ok)
	}
	_, _, ok = readTableRef("(SELECT 1)")
	if ok {
		t.Fatal("paren should not be a table ref")
	}
}

func TestSkipAlias(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"AS l WHERE", " WHERE"},
		{"l WHERE", " WHERE"},
		{"WHERE", "WHERE"},      // 终止关键字：原样返回
		{"LIMIT", "LIMIT"},      // 终止关键字：原样返回
		{`"l" WHERE`, " WHERE"}, // 引号别名
		{"l,", ","},             // 逗号前保留
		{"l(", "("},             // 函数起点
		{"", ""},
	}
	for _, tc := range cases {
		if got := skipAlias(tc.in); got != tc.want {
			t.Errorf("skipAlias(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatUUID(t *testing.T) {
	b := []byte{0x3a, 0x60, 0x27, 0x02, 0x02, 0xb6, 0x49, 0x59, 0x95, 0x7c, 0xac, 0xd6, 0xec, 0xbd, 0x97, 0xca}
	if got := formatUUID(b); got != "3a602702-02b6-4959-957c-acd6ecbd97ca" {
		t.Fatalf("formatUUID = %q", got)
	}
}

func TestAllowOne(t *testing.T) {
	svc := &Service{rlCalls: map[string][]time.Time{}}

	// 10 次以内放行，第 11 次拒绝
	for i := 0; i < 10; i++ {
		if !svc.allowOne("u-rate") {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}
	if svc.allowOne("u-rate") {
		t.Fatal("11th call must be rejected")
	}

	// 其他用户不受影响
	if !svc.allowOne("u-other") {
		t.Fatal("other user must be allowed")
	}

	// 其他用户的过期 key（>1min 无调用）被顺手清理
	svc.rlCalls["u-stale"] = []time.Time{time.Now().Add(-2 * time.Minute)}
	if !svc.allowOne("u-fresh") {
		t.Fatal("fresh user must be allowed")
	}
	if _, ok := svc.rlCalls["u-stale"]; ok {
		t.Fatalf("stale key should be cleaned, got %v", svc.rlCalls)
	}

	// 自己的过期调用被裁剪：满 10 次旧记录 → 视为新窗口，放行且只留 1 条
	svc.rlCalls["u-own"] = make([]time.Time, 10)
	for i := range svc.rlCalls["u-own"] {
		svc.rlCalls["u-own"][i] = time.Now().Add(-2 * time.Minute)
	}
	if !svc.allowOne("u-own") {
		t.Fatal("expired calls must be trimmed and allowed")
	}
	if len(svc.rlCalls["u-own"]) != 1 {
		t.Fatalf("u-own calls should be trimmed to 1, got %d", len(svc.rlCalls["u-own"]))
	}
}

func TestPrepareSQL_MixedCaseLimitRewrite(t *testing.T) {
	// 全局表（无行级过滤注入）验证 LIMIT 改写的原文对齐。
	out, _, _, err := prepareSQL("select * from step_templates limit 5000", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "select * from step_templates LIMIT 200" {
		t.Fatalf("rewrite = %q", out)
	}
	out, _, _, err = prepareSQL("SELECT * FROM step_template_items LiMiT 300", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "SELECT * FROM step_template_items LIMIT 200" {
		t.Fatalf("mixed case rewrite = %q", out)
	}
}

// R1：dollar-quote 掩蔽——$tag$...$tag$（含未闭合）整体变空格、字节等长、
// 定界符内容中的引号不再影响 stripStrings 的引号扫描。
func TestMaskDollarQuotes(t *testing.T) {
	spaces := func(n int) string { return strings.Repeat(" ", n) }
	cases := []struct{ in, want string }{
		// 审查报告样例：dollar-quote 内未配对单引号整体掩蔽，尾部 UNION 暴露给黑名单。
		{"$a$ ' $a$ IS NULL UNION", spaces(9) + " IS NULL UNION"},
		// 空 tag（$$）与带数字 tag。
		{"$$x$$", spaces(5)},
		{"$42$ab$42$", spaces(10)},
		// 未闭合：掩到结尾（PG 侧同为语法错误）。
		{"WHERE x = $a$ UNION SELECT", "WHERE x = " + spaces(16)},
		// 普通 $ 字面量与 $1 参数占位不受影响（不构成 $...$ 定界对）。
		{"price > $5", "price > $5"},
		{"WHERE id = $1", "WHERE id = $1"},
		// 无 dollar-quote 原样返回。
		{"SELECT 'a' FROM logs", "SELECT 'a' FROM logs"},
		// 定界符内的嵌套不同 tag：整段一并掩蔽（与 PG 语义一致，内层是字面量）。
		{"$a$ $b$ inner $b$ $a$ tail", spaces(21) + " tail"},
	}
	for _, tc := range cases {
		if got := maskDollarQuotes(tc.in); got != tc.want {
			t.Errorf("maskDollarQuotes(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if len(maskDollarQuotes(tc.in)) != len(tc.in) {
			t.Errorf("maskDollarQuotes(%q) must preserve byte length", tc.in)
		}
	}
}
