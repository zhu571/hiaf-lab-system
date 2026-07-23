package common

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

// OpenDB opens a PostgreSQL connection pool using environment variables or Docker secrets.
func OpenDB() (*sql.DB, error) {
	host := envOrDefault("DB_HOST", "localhost")
	port := envOrDefault("DB_PORT", "5432")
	user := envOrDefault("DB_USER", "lab")
	dbname := envOrDefault("DB_NAME", "lab")

	password, err := ReadSecret("/run/secrets/db_password", "DB_PASSWORD")
	if err != nil {
		return nil, fmt.Errorf("read db password: %w", err)
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return db, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
