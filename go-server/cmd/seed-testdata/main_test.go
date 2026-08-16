package main

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// 集成测试：依赖 TEST_DATABASE_URL（scripts/test-go.sh / CI 在全量迁移后先执行
// seed 命令再跑测试，故正常路径下 run() 应走「已存在跳过」分支；两种分支都断言
// 种子数据完备）。审计行完整性由 audit.VerifyChain 的既有测试链路覆盖，此处只查行数。
func TestSeedTestdataIdempotent(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skip("TEST_DATABASE_URL unreachable")
	}

	// 第一次：可能真正插入（干净库）或跳过（seed 已由脚本执行过）。
	if err := run(db); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// 第二次：必须幂等跳过，不得报主键冲突。
	if err := run(db); err != nil {
		t.Fatalf("second run must be idempotent: %v", err)
	}

	var role string
	if err := db.QueryRow(`SELECT role FROM users WHERE username = 'haofan'`).Scan(&role); err != nil {
		t.Fatalf("seed user haofan missing: %v", err)
	}
	if role != "admin" {
		t.Fatalf("haofan role = %q, want admin", role)
	}

	var users, projects, auditRows int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE id::text LIKE 'a0000000-%'`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 5 {
		t.Fatalf("seed users = %d, want 5", users)
	}
	if err := db.QueryRow(`SELECT count(*) FROM projects WHERE id::text LIKE 'b0000000-%'`).Scan(&projects); err != nil {
		t.Fatal(err)
	}
	if projects != 3 {
		t.Fatalf("seed projects = %d, want 3", projects)
	}
	if err := db.QueryRow(`SELECT count(*) FROM audit_log WHERE request_id LIKE 'req_202606%' OR request_id LIKE 'req_202607%'`).Scan(&auditRows); err != nil {
		t.Fatal(err)
	}
	if auditRows < 5 {
		t.Fatalf("seed audit rows = %d, want >= 5", auditRows)
	}
}
