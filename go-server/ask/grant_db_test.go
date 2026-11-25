package ask

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// TestAskReaderGrantsMainTables 保证迁移 033 的 ask_reader GRANT 与 mainTables 一致（AR-09）。
// DB 权限是 ask 全库只读例外的最后一道防线；漏 GRANT 会让生产查询 500。
func TestAskReaderGrantsMainTables(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, table := range mainTables {
		var allowed bool
		if err := db.QueryRow(
			`SELECT has_table_privilege('ask_reader', $1, 'SELECT')`, table,
		).Scan(&allowed); err != nil {
			t.Fatalf("check grant for %s: %v", table, err)
		}
		if !allowed {
			t.Errorf("ask_reader 缺少表 %s 的 SELECT 权限；同步迁移 033 的 GRANT", table)
		}
	}
}
