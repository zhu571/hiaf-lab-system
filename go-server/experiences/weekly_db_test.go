package experiences

import (
	"database/sql"
	"os"
	"testing"
)

// FindWeeklySummary / CreateWeeklySummary 周报落库 db 测试（AI-1）：
// global（project_id NULL）+ published + ai_generated + tags 含 weekly_summary，
// 每周幂等查重按 title 精确 + tag 过滤。固定 UUID 段 c6xx（用户）。
// 需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移）。

const (
	wkExpAuthorID = "00000000-0000-0000-0000-00000000c601"
)

func openExperiencesWeeklyTestDB(t *testing.T) *sql.DB {
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
	if _, err := db.Exec(
		`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
		 VALUES ($1, 'wk_exp_user', 'x', '周报经验测试', 'member', false, false)
		 ON CONFLICT (id) DO NOTHING`, wkExpAuthorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM experiences WHERE author_id = $1`, wkExpAuthorID)
		db.Exec(`DELETE FROM users WHERE id = $1`, wkExpAuthorID)
	})
	return db
}

func TestDBCreateAndFindWeeklySummary(t *testing.T) {
	db := openExperiencesWeeklyTestDB(t)
	repo := NewRepository(db)

	created, err := repo.CreateWeeklySummary(wkExpAuthorID, "周报 2026-09-07 ~ 2026-09-13", "## 本周进展\n完成匹配电路装配。")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Title != "周报 2026-09-07 ~ 2026-09-13" ||
		created.Content != "## 本周进展\n完成匹配电路装配。" {
		t.Fatalf("created: %+v", created)
	}
	if created.Status != StatusPublished || created.ProjectID != nil || !created.AiGenerated {
		t.Fatalf("weekly summary row: status=%q project=%v ai=%v", created.Status, created.ProjectID, created.AiGenerated)
	}
	if created.PublishedAt == nil || created.CreatedAt.IsZero() {
		t.Fatalf("weekly summary timestamps: published=%v created=%v", created.PublishedAt, created.CreatedAt)
	}
	found := false
	for _, tag := range created.Tags {
		if tag == weeklySummaryTag {
			found = true
		}
	}
	if !found {
		t.Fatalf("tags missing weekly_summary: %v", created.Tags)
	}

	// 幂等查重：按 title 精确命中
	hit, err := repo.FindWeeklySummary("周报 2026-09-07 ~ 2026-09-13")
	if err != nil {
		t.Fatal(err)
	}
	if hit == nil || hit.ID != created.ID {
		t.Fatalf("FindWeeklySummary hit: %+v, want id=%s", hit, created.ID)
	}

	// 未生成过的周 → nil、无错误
	miss, err := repo.FindWeeklySummary("周报 2026-09-14 ~ 2026-09-20")
	if err != nil {
		t.Fatal(err)
	}
	if miss != nil {
		t.Fatalf("unexpected hit: %+v", miss)
	}
}

func TestDBFindWeeklySummaryIgnoresPlainExperience(t *testing.T) {
	db := openExperiencesWeeklyTestDB(t)
	repo := NewRepository(db)

	// 同标题但无 weekly_summary tag 的普通经验：必须不命中（title 非唯一，tag 兜底）
	title := "周报 2026-09-14 ~ 2026-09-20"
	if _, err := db.Exec(
		`INSERT INTO experiences (title, content, tags_json, status, author_id)
		 VALUES ($1, '普通候选经验', '[]'::jsonb, 'candidate', $2)`,
		title, wkExpAuthorID); err != nil {
		t.Fatal(err)
	}
	miss, err := repo.FindWeeklySummary(title)
	if err != nil {
		t.Fatal(err)
	}
	if miss != nil {
		t.Fatalf("plain experience must not match: %+v", miss)
	}
}
