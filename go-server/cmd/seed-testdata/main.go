// seed-testdata — 开发/测试环境种子数据集工具（R7）。
//
// 由来：原 migrations/009_test_data.up.sql 会随迁移链在全新部署自动执行，
// 等于给新环境发放已知口令（Test1234!）的 admin 账号，且其开头全量 DELETE
// 业务表——误用于已有库是清库灾难。009 已改空操作，数据集移到本命令显式执行。
//
// 行为（参照 cmd/seed-agent 的幂等模式）：
//   * 库中已存在用户 haofan（含历史 009 环境遗留）→ 打印提示直接返回；
//   * 否则在单事务内执行 seed.sql（固定 UUID 数据集，见文件头注释）；
//   * 审计行走 029 的 BEFORE INSERT hash 链触发器自动接链，无需特殊处理。
//
// 严禁对生产库执行：种子密码公开已知（Test1234!）。
//
// 连接方式：DATABASE_URL（URL 形 DSN，scripts/test-go.sh 等测试脚本使用）
// 优先；缺省走 common.OpenDB()（DB_* 环境变量 / docker secrets，与 server 同源）。
package main

import (
	"database/sql"
	"embed"
	"fmt"
	"os"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

//go:embed seed.sql
var seedFS embed.FS

func main() {
	db, err := openDB()
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	if err := run(db); err != nil {
		fatal(err)
	}
}

func openDB() (*sql.DB, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, err
		}
		if err := db.Ping(); err != nil {
			return nil, fmt.Errorf("ping db: %w", err)
		}
		return db, nil
	}
	return common.OpenDB()
}

func run(db *sql.DB) error {
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM users WHERE username = 'haofan')`).Scan(&exists); err != nil {
		return fmt.Errorf("check seed state: %w", err)
	}
	if exists {
		fmt.Println("seed data already present (user haofan exists), skip")
		return nil
	}

	script, err := seedFS.ReadFile("seed.sql")
	if err != nil {
		return fmt.Errorf("read seed.sql: %w", err)
	}
	if _, err := db.Exec(string(script)); err != nil {
		return fmt.Errorf("exec seed.sql: %w", err)
	}
	fmt.Println("seed data inserted (users: haofan/lisi/zhangsan/wangwu/zhaoliu, password Test1234! — dev/test only)")
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
