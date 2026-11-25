package common

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func openRateTestDB(t *testing.T) *sql.DB {
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
	return db
}

func TestRateLimitStoreAllow(t *testing.T) {
	db := openRateTestDB(t)
	scope := "test_" + time.Now().Format("150405.000000")
	subject := "user-a"
	store := NewRateLimitStore(db, scope, 2, time.Minute)
	t.Cleanup(func() { db.Exec(`DELETE FROM rate_limit_counters WHERE scope = $1`, scope) })

	if !store.Allow(context.Background(), subject) {
		t.Fatal("first call must be allowed")
	}
	if !store.Allow(context.Background(), subject) {
		t.Fatal("second call must be allowed")
	}
	if store.Allow(context.Background(), subject) {
		t.Fatal("third call must be limited")
	}
	if !store.Allow(context.Background(), "user-b") {
		t.Fatal("different subject must have its own window")
	}
}

func TestClaimScheduledRun(t *testing.T) {
	db := openRateTestDB(t)
	job := "test_job_" + time.Now().Format("150405.000000")
	key := "2026-11-25"
	t.Cleanup(func() { db.Exec(`DELETE FROM scheduler_claims WHERE job = $1`, job) })

	ok, err := ClaimScheduledRun(context.Background(), db, job, key)
	if err != nil || !ok {
		t.Fatalf("first claim = %v, %v; want true, nil", ok, err)
	}
	ok, err = ClaimScheduledRun(context.Background(), db, job, key)
	if err != nil || ok {
		t.Fatalf("second claim = %v, %v; want false, nil", ok, err)
	}
}
