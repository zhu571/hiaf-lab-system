package auth

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// 集成测试：需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移 001-038）。
// 覆盖 P2-4：FindRevokedRefreshToken / IsRefreshTokenReuse（真复用重放检测）。
// 本测试写真实 refresh_tokens / revoked_tokens 表，结束后清理所有本用例创建的行。

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
		db.Exec(`DELETE FROM revoked_tokens WHERE user_id = $1`, authDBTestUserID)
		db.Exec(`DELETE FROM users WHERE id = $1`, authDBTestUserID)
	})
	return db
}

// insertRefreshToken 插入一条 refresh token（crypt 同生产 StoreRefreshToken）。
func insertRefreshToken(t *testing.T, db *sql.DB, raw string, expiresIn time.Duration) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO refresh_tokens (user_id, token_hash, family, expires_at)
		 VALUES ($1, crypt($2, gen_salt('bf')), gen_random_uuid(), now() + $3)`,
		authDBTestUserID, raw, expiresIn.String(),
	)
	if err != nil {
		t.Fatal(err)
	}
}

// insertBlacklistRow 直接向 revoked_tokens 插入一条黑名单行（构造过期/边界场景，
// token_lookup 计算与生产 RevokeRefreshToken 一致）。
func insertBlacklistRow(t *testing.T, db *sql.DB, raw string, expiresIn time.Duration) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO revoked_tokens (token_lookup, user_id, expires_at)
		 VALUES (encode(digest($1, 'sha256'), 'hex'), $2, now() + $3)`,
		raw, authDBTestUserID, expiresIn.String(),
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
	insertRefreshToken(t, db, raw, 30*24*time.Hour)
	rec, err := r.FindRefreshToken(raw)
	if err != nil || rec == nil {
		t.Fatalf("live token should be found: rec=%v err=%v", rec, err)
	}
	rev, err := r.FindRevokedRefreshToken(raw)
	if err != nil || rev != nil {
		t.Fatalf("live token must not match revoked lookup: rev=%v err=%v", rev, err)
	}

	// 撤销后 → 主表行物理删除、黑名单写入一行、FindRefreshToken 不再命中、
	// FindRevokedRefreshToken 命中（真复用）
	if err := r.RevokeRefreshToken(rec.ID, raw, rec.UserID, rec.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	var mainRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM refresh_tokens WHERE id = $1`, rec.ID).Scan(&mainRows); err != nil {
		t.Fatal(err)
	}
	if mainRows != 0 {
		t.Fatalf("refresh_tokens row must be physically deleted, got %d rows", mainRows)
	}
	var blackRows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM revoked_tokens WHERE token_lookup = encode(digest($1, 'sha256'), 'hex')`,
		raw,
	).Scan(&blackRows); err != nil {
		t.Fatal(err)
	}
	if blackRows != 1 {
		t.Fatalf("revoked_tokens must contain exactly one blacklist row, got %d", blackRows)
	}
	live, err := r.FindRefreshToken(raw)
	if err != nil || live != nil {
		t.Fatalf("revoked token must not be found by FindRefreshToken: rec=%v err=%v", live, err)
	}
	rev, err = r.FindRevokedRefreshToken(raw)
	if err != nil || rev == nil {
		t.Fatalf("revoked token should be found by FindRevokedRefreshToken: rev=%v err=%v", rev, err)
	}
	if err := r.RevokeRefreshToken(rec.ID, raw, "00000000-0000-0000-0000-00000000c002", rec.ExpiresAt); err == nil {
		t.Fatal("another user must not revoke this token")
	}

	// 同一 token 重复撤销 → 幂等不报错（主表行已删、黑名单唯一索引 ON CONFLICT DO NOTHING）
	if err := r.RevokeRefreshToken(rec.ID, raw, rec.UserID, rec.ExpiresAt); err != nil {
		t.Fatalf("revoke twice should be idempotent: %v", err)
	}

	// 过期黑名单行 → 查不到
	expired := fmt.Sprintf("rt-expired-%d", time.Now().UnixNano())
	insertBlacklistRow(t, db, expired, -24*time.Hour)
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
	insertRefreshToken(t, db, raw, 30*24*time.Hour)
	reuse, err := svc.IsRefreshTokenReuse(raw)
	if err != nil || reuse {
		t.Fatalf("live token must not be reuse: reuse=%v err=%v", reuse, err)
	}

	// 撤销后 → true
	rec, err := r.FindRefreshToken(raw)
	if err != nil || rec == nil {
		t.Fatal(err)
	}
	if err := r.RevokeRefreshToken(rec.ID, raw, rec.UserID, rec.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	reuse, err = svc.IsRefreshTokenReuse(raw)
	if err != nil || !reuse {
		t.Fatalf("revoked token must be reuse: reuse=%v err=%v", reuse, err)
	}

	// 过期黑名单行 → false
	expired := fmt.Sprintf("rt-reuse-expired-%d", time.Now().UnixNano())
	insertBlacklistRow(t, db, expired, -24*time.Hour)
	reuse, err = svc.IsRefreshTokenReuse(expired)
	if err != nil || reuse {
		t.Fatalf("expired revoked token must not be reuse: reuse=%v err=%v", reuse, err)
	}
}

// S1 用户名维度：5 次/15min/用户名账户锁定（第二道防线，独立于 IP 级限流）。
// 需要 TEST_DATABASE_URL（同文件其余用例）。
func TestIncrementFailedAttemptsLocksAfterFive(t *testing.T) {
	db := openAuthTestDB(t)
	r := NewRepository(db)
	username := fmt.Sprintf("locktest-%d", time.Now().UnixNano())
	_, err := db.Exec(`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
		VALUES (gen_random_uuid(), $1, 'x', 'Lock Test', 'member', false, false)`, username)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE username = $1`, username) })

	for i := 1; i <= 5; i++ {
		attempts, lockedUntil, err := r.IncrementFailedAttempts(username)
		if err != nil {
			t.Fatal(err)
		}
		if attempts != i {
			t.Fatalf("attempts = %d, want %d", attempts, i)
		}
		if i == 5 && lockedUntil == nil {
			t.Fatal("5th failure must set locked_until")
		}
	}
	user, err := r.GetByUsername(username)
	if err != nil || user == nil {
		t.Fatalf("get locked user: %v", err)
	}
	if user.LockedUntil == nil {
		t.Fatal("user must be locked after 5 failures")
	}
	if err := r.ResetFailedAttempts(username); err != nil {
		t.Fatal(err)
	}
	user2, err := r.GetByUsername(username)
	if err != nil || user2 == nil {
		t.Fatalf("get reset user: %v", err)
	}
	if user2.LockedUntil != nil {
		t.Fatal("reset must clear lock")
	}
}

// R6：改角色 → token_version +1，旧版本 claims 的 access token 被 TokenVersionValidator
// 拒绝（HTTP 层 401）；同值 role / 仅改 display_name 不递增。
// 验证器接线与 main.go:85-92 一致（version 匹配且未停用），AuthRequired 兜底返回 401。
func TestDBTokenVersionInvalidatedOnRoleChange(t *testing.T) {
	db := openAuthSvcDB(t)
	repo := NewRepository(db)

	user, err := repo.GetByID(authDBUserID)
	if err != nil || user == nil {
		t.Fatalf("get user: %v %v", user, err)
	}

	middleware.SetJWTSecret([]byte(authTestJWTSecret))
	t.Cleanup(func() { middleware.SetJWTSecret(nil) })
	middleware.TokenVersionValidator = func(userID string, version int) bool {
		u, err := repo.GetByID(userID)
		if err != nil || u == nil {
			return false
		}
		return u.TokenVersion == version && !u.Disabled
	}
	t.Cleanup(func() { middleware.TokenVersionValidator = nil })
	t.Cleanup(func() { _, _ = repo.UpdateUser(authDBUserID, nil, nil, boolPtr(false)) })

	// 基线：当前版本 token 通过 AuthRequired（200）。
	okToken := authTestToken(t, user.ID, user.Username, user.Role, user.TokenVersion)
	if got := authGuardStatus(okToken); got != http.StatusOK {
		t.Fatalf("baseline token = %d, want 200", got)
	}

	// 改角色 → token_version +1。
	newRole := RoleMaintainer
	updated, err := repo.UpdateUser(user.ID, nil, &newRole, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.TokenVersion != user.TokenVersion+1 {
		t.Fatalf("token_version = %d, want %d", updated.TokenVersion, user.TokenVersion+1)
	}
	if updated.Role != RoleMaintainer {
		t.Fatalf("role = %q, want %q", updated.Role, RoleMaintainer)
	}

	// 旧版本 claims 被 TokenVersionValidator 拒绝（直查 + HTTP 401）。
	if middleware.TokenVersionValidator(user.ID, user.TokenVersion) {
		t.Fatal("stale version must be rejected by validator")
	}
	if got := authGuardStatus(okToken); got != http.StatusUnauthorized {
		t.Fatalf("stale-version token = %d, want 401", got)
	}
	// 新版本 claims 通过（直查 + HTTP 200）。
	if !middleware.TokenVersionValidator(user.ID, updated.TokenVersion) {
		t.Fatal("current version must pass validator")
	}
	if got := authGuardStatus(authTestToken(t, user.ID, user.Username, updated.Role, updated.TokenVersion)); got != http.StatusOK {
		t.Fatalf("current-version token = %d, want 200", got)
	}

	// 同值 role → 不递增。
	sameRole := updated.Role
	same, err := repo.UpdateUser(user.ID, nil, &sameRole, nil)
	if err != nil {
		t.Fatal(err)
	}
	if same.TokenVersion != updated.TokenVersion {
		t.Fatalf("same-role update bumped version: %d → %d", updated.TokenVersion, same.TokenVersion)
	}

	// 仅改 display_name → 不递增。
	name := "改名不失效token"
	renamed, err := repo.UpdateUser(user.ID, &name, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.TokenVersion != updated.TokenVersion {
		t.Fatalf("display-name update bumped version: %d → %d", updated.TokenVersion, renamed.TokenVersion)
	}
}

// authTestToken 用固定测试密钥签发 access token（等价 middleware.GenerateToken 的直接用法）。
func authTestToken(t *testing.T, userID, username, role string, version int) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, version, []byte(authTestJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// authGuardStatus 经 AuthRequired 中间件做一次最小请求，返回状态码。
func authGuardStatus(token string) int {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	middleware.AuthRequired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
	return rec.Code
}
