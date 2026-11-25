package common

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

// DBOptions 是连接池参数（AR-12：由 config 解析后传入，避免 common 直接散布 env 读取）。
type DBOptions struct {
	Host         string
	Port         string
	User         string
	Name         string
	PasswordFile string
	PasswordEnv  string
	MaxOpen      int
	MaxIdle      int
}

func (o DBOptions) withDefaults() DBOptions {
	if o.Host == "" {
		o.Host = "localhost"
	}
	if o.Port == "" {
		o.Port = "5432"
	}
	if o.User == "" {
		o.User = "lab"
	}
	if o.Name == "" {
		o.Name = "lab"
	}
	if o.PasswordFile == "" {
		o.PasswordFile = "/run/secrets/db_password"
	}
	if o.PasswordEnv == "" {
		o.PasswordEnv = "DB_PASSWORD"
	}
	if o.MaxOpen <= 0 {
		o.MaxOpen = 25
	}
	if o.MaxIdle < 0 {
		o.MaxIdle = 5
	}
	if o.MaxIdle > o.MaxOpen {
		o.MaxIdle = o.MaxOpen
	}
	return o
}

// OpenDBWithOptions 按显式参数打开连接池；main 用 config.DB 构造，测试可注入。
func OpenDBWithOptions(o DBOptions) (*sql.DB, error) {
	o = o.withDefaults()

	password, err := ReadSecret(o.PasswordFile, o.PasswordEnv)
	if err != nil {
		return nil, fmt.Errorf("read db password: %w", err)
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		o.Host, o.Port, o.User, password, o.Name,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(o.MaxOpen)
	db.SetMaxIdleConns(o.MaxIdle)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return db, nil
}

// OpenDB 保留旧签名，供测试与未迁移调用方使用；新代码走 OpenDBWithOptions。
func OpenDB() (*sql.DB, error) {
	return OpenDBWithOptions(DBOptions{
		Host: envOrDefault("DB_HOST", "localhost"),
		Port: envOrDefault("DB_PORT", "5432"),
		User: envOrDefault("DB_USER", "lab"),
		Name: envOrDefault("DB_NAME", "lab"),
	})
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
