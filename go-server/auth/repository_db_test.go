package auth

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// 集成测试：需要 TEST_DATABASE_URL（CI/本地按 AGENTS.md 起 postgres 并跑完 001-033 迁移）。
// 覆盖 P2-4：FindRevokedRefreshToken / IsRefreshTokenReuse（真复用重放检测）。
// 本测试写真实 refresh_tokens 表，结束后清理所有本用例创建的行。

const authDBTestUserID = "00000000-0000-0000-0000-00000000c001"

func openAuthTestDB(t *testing.T) *sql.DB {
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
	_, err = db.Exec(`
		INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
		VALUES ($1, 'auth_dbtest', 'x', 'Auth DB Test', 'member', false, false)
		ON CONFLICT (id) DO NOTHING`, authDBTestUserID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM refresh_tokens WHERE user_id = $1`, authDBTestUserID)
		db.Exec(`DELETE FROM users WHERE id = $1`, authDBTestUserID)
	})
	return db
}

// insertRefreshToken 插入一条 refresh token（crypt 同生产 StoreRefreshToken）。
func insertRefreshToken(t *testing.T, db *sql.DB, raw string, revoked bool, expiresIn time.Duration) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO refresh_tokens (user_id, token_hash, family, expires_at, revoked)
		 VALUES ($1, crypt($2, gen_salt('bf')), gen_random_uuid(), now() + $3, $4)`,
		authDBTestUserID, raw, expiresIn.String(), revoked,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFindRevokedRefreshToken(t *testing.T) {
	db := openAuthTestDB(t)
	r := NewRepository(db)

	// 未撤销 → FindRefreshToken 命中、FindRevokedRefreshToken 不命中
	raw := fmt.Sprintf("rt-live-%d", time.Now().UnixNano())
	insertRefreshToken(t, db, raw, false, 30*24*time.Hour)
	rec, err := r.FindRefreshToken(raw)
	if err != nil || rec == nil {
		t.Fatalf("live token should be found: rec=%v err=%v", rec, err)
	}
	rev, err := r.FindRevokedRefreshToken(raw)
	if err != nil || rev != nil {
		t.Fatalf("live token must not match revoked lookup: rev=%v err=%v", rev, err)
	}

	// 撤销后 → FindRefreshToken 不再命中、FindRevokedRefreshToken 命中（真复用）
	if err := r.RevokeRefreshToken(rec.ID); err != nil {
		t.Fatal(err)
	}
	live, err := r.FindRefreshToken(raw)
	if err != nil || live != nil {
		t.Fatalf("revoked token must not be found by FindRefreshToken: rec=%v err=%v", live, err)
	}
	rev, err = r.FindRevokedRefreshToken(raw)
	if err != nil || rev == nil || rev.ID != rec.ID {
		t.Fatalf("revoked token should be found by FindRevokedRefreshToken: rev=%v err=%v", rev, err)
	}

	// 过期撤销 token → 查不到
	expired := fmt.Sprintf("rt-expired-%d", time.Now().UnixNano())
	insertRefreshToken(t, db, expired, true, -24*time.Hour)
	rev, err = r.FindRevokedRefreshToken(expired)
	if err != nil || rev != nil {
		t.Fatalf("expired revoked token must not be found: rev=%v err=%v", rev, err)
	}

	// 未知 token → 查不到（nil 无错误）
	rev, err = r.FindRevokedRefreshToken("rt-unknown-token")
	if err != nil || rev != nil {
		t.Fatalf("unknown token: rev=%v err=%v", rev, err)
	}
}

func TestIsRefreshTokenReuse(t *testing.T) {
	db := openAuthTestDB(t)
	r := NewRepository(db)
	svc := NewService(r, nil) // jwtKey 本用例不使用

	// 未撤销 → false
	raw := fmt.Sprintf("rt-reuse-%d", time.Now().UnixNano())
	insertRefreshToken(t, db, raw, false, 30*24*time.Hour)
	reuse, err := svc.IsRefreshTokenReuse(raw)
	if err != nil || reuse {
		t.Fatalf("live token must not be reuse: reuse=%v err=%v", reuse, err)
	}

	// 撤销后 → true
	rec, err := r.FindRefreshToken(raw)
	if err != nil || rec == nil {
		t.Fatal(err)
	}
	if err := r.RevokeRefreshToken(rec.ID); err != nil {
		t.Fatal(err)
	}
	reuse, err = svc.IsRefreshTokenReuse(raw)
	if err != nil || !reuse {
		t.Fatalf("revoked token must be reuse: reuse=%v err=%v", reuse, err)
	}

	// 过期撤销 → false
	expired := fmt.Sprintf("rt-reuse-expired-%d", time.Now().UnixNano())
	insertRefreshToken(t, db, expired, true, -24*time.Hour)
	reuse, err = svc.IsRefreshTokenReuse(expired)
	if err != nil || reuse {
		t.Fatalf("expired revoked token must not be reuse: reuse=%v err=%v", reuse, err)
	}
}
