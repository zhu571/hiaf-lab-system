package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// service 集成测试：需要 TEST_DATABASE_URL（CI/本地按 scripts/test-go.sh 应用全量迁移）。
// auth.Service 的 repo 是具体 *Repository，无法注入 mock，走真实 PostgreSQL（同 projects
// service_db_test 模式）；固定 UUID 种子（ON CONFLICT DO UPDATE 重置可变状态：
// 密码/锁定/停用/language，保证重复运行幂等）+ t.Cleanup 清理，
// CI 以 -p 1 串行跑避免撞种子。本文件覆盖登录/锁定/改密/refresh 轮换/profile/管理员三条链。
// 注意：迁移 009 种子了 admin 用户 a0000000-...-000001（haofan），CountActiveAdmins 会把
// 它计入；「最后一个管理员」用例会临时停用 haofan，t.Cleanup 恢复。

const (
	authDBAdmin1ID = "00000000-0000-0000-0000-00000000b501"
	authDBAdmin2ID = "00000000-0000-0000-0000-00000000b502"
	authDBUserID   = "00000000-0000-0000-0000-00000000b503"
	authDBUser2ID  = "00000000-0000-0000-0000-00000000b504"
)

const authTestJWTSecret = "auth-service-db-test-secret"

func openAuthSvcDB(t *testing.T) *sql.DB {
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

	for _, u := range []struct {
		id       string
		username string
		role     string
	}{
		{authDBAdmin1ID, "auth_db_admin1", RoleAdmin},
		{authDBAdmin2ID, "auth_db_admin2", RoleAdmin},
		{authDBUserID, "auth_db_user1", RoleMember},
		{authDBUser2ID, "auth_db_user2", RoleMember},
	} {
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, 'x', 'Auth DB Test', $3, false, false)
			 ON CONFLICT (id) DO UPDATE SET
			   username = EXCLUDED.username, password_hash = EXCLUDED.password_hash,
			   display_name = EXCLUDED.display_name, role = EXCLUDED.role,
			   must_change_pw = false, disabled = false,
			   failed_attempts = 0, locked_until = NULL, language = 'zh'`, u.id, u.username, u.role); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM refresh_tokens WHERE user_id IN ($1,$2,$3,$4)`,
			authDBAdmin1ID, authDBAdmin2ID, authDBUserID, authDBUser2ID)
		db.Exec(`DELETE FROM revoked_tokens WHERE user_id IN ($1,$2,$3,$4)`,
			authDBAdmin1ID, authDBAdmin2ID, authDBUserID, authDBUser2ID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3,$4)`,
			authDBAdmin1ID, authDBAdmin2ID, authDBUserID, authDBUser2ID)
	})
	return db
}

// registerUser 用真实 Register 流程创建用户（argon2 哈希 + must_change_pw=true），返回用户名。
func registerUser(t *testing.T, svc *Service) string {
	t.Helper()
	username := fmt.Sprintf("auth_rt_%d", time.Now().UnixNano())
	user, err := svc.Register(username, "Test1234abcd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = svc.repo.db.Exec(`DELETE FROM users WHERE id = $1`, user.ID) })
	return username
}

func TestDBSvcRegister(t *testing.T) {
	db := openAuthSvcDB(t)
	svc := NewService(NewRepository(db), []byte(authTestJWTSecret))

	// 弱密码 → ErrPasswordTooShort（不触库）
	if _, err := svc.Register("weak_user_1", "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("weak password: got %v, want ErrPasswordTooShort", err)
	}

	// 成功：member 角色 + must_change_pw=true + display_name 默认空
	username := registerUser(t, svc)
	user, err := svc.repo.GetByUsername(username)
	if err != nil || user == nil {
		t.Fatalf("get registered user: %v %v", user, err)
	}
	if user.Role != RoleMember || !user.MustChangePW || user.DisplayName != "" {
		t.Fatalf("registered defaults: %+v", user)
	}
	// 哈希格式 salt:hash
	if !strings.Contains(user.PasswordHash, ":") || user.PasswordHash == "x" {
		t.Fatalf("password hash not persisted: %q", user.PasswordHash)
	}

	// 重复用户名 → ErrUsernameTaken
	if _, err := svc.Register(username, "Test1234abcd"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate username: got %v, want ErrUsernameTaken", err)
	}
}

func TestDBSvcLogin(t *testing.T) {
	db := openAuthSvcDB(t)
	svc := NewService(NewRepository(db), []byte(authTestJWTSecret))
	username := registerUser(t, svc)
	password := "Test1234abcd"

	// 未知用户名 → ErrInvalidCredentials（防枚举路径）
	if _, _, err := svc.Login("no_such_user_x", password, "10.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user: got %v, want ErrInvalidCredentials", err)
	}

	// 首次登录：newIP=true（last_login_ip 为空视为新 IP），tokens 齐全
	resp, newIP, err := svc.Login(username, password, "10.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if !newIP {
		t.Fatal("first login from new IP must report newIP=true")
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" || resp.ExpiresIn <= 0 || resp.RefreshExpiresIn <= 0 {
		t.Fatalf("token pair incomplete: %+v", resp)
	}
	if !resp.MustChangePassword {
		t.Fatal("registered user must carry must_change_password=true")
	}
	if resp.User == nil || resp.User.Username != username || resp.User.Role != RoleMember {
		t.Fatalf("login response user: %+v", resp.User)
	}
	// last_login_ip 由 Audit 中间件落库（S5，与审计行同事务）；service 层只读不写。
	// 模拟中间件持久化后：同 IP 再次登录 → newIP=false；换 IP → newIP=true。
	if _, err := db.Exec(`UPDATE users SET last_login_ip = '10.1.1.1' WHERE username = $1`, username); err != nil {
		t.Fatal(err)
	}
	if _, newIP, err = svc.Login(username, password, "10.1.1.1"); err != nil || newIP {
		t.Fatalf("same IP relogin: err=%v newIP=%v", err, newIP)
	}
	// 换 IP → newIP=true
	if _, newIP, err = svc.Login(username, password, "10.1.1.2"); err != nil || !newIP {
		t.Fatalf("different IP relogin: err=%v newIP=%v", err, newIP)
	}
	// clientIP 为空 → 恒 false
	if _, newIP, err = svc.Login(username, password, ""); err != nil || newIP {
		t.Fatalf("empty clientIP: err=%v newIP=%v", err, newIP)
	}

	// 密码错误：1-4 次 → ErrInvalidCredentials，第 5 次触发锁定 → ErrAccountLocked
	for i := 1; i <= 4; i++ {
		if _, _, err := svc.Login(username, "wrong-pass", "10.2.2.2"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: got %v, want ErrInvalidCredentials", i, err)
		}
	}
	if _, _, err := svc.Login(username, "wrong-pass", "10.2.2.2"); !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("5th attempt: got %v, want ErrAccountLocked", err)
	}
	// 锁定期间即使密码正确也拒绝
	if _, _, err := svc.Login(username, password, "10.2.2.3"); !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("locked user: got %v, want ErrAccountLocked", err)
	}
	// failed_attempts 落库为 5
	locked, err := svc.repo.GetByUsername(username)
	if err != nil || locked == nil || locked.FailedAttempts != 5 || locked.LockedUntil == nil {
		t.Fatalf("lock state: %+v err=%v", locked, err)
	}
	// 锁定到期自动解除：locked_until 已过期 → 正确密码可登录（成功登录重置计数）
	if _, err := db.Exec(`UPDATE users SET locked_until = now() - interval '1 minute' WHERE username = $1`, username); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login(username, password, "10.2.2.3"); err != nil {
		t.Fatalf("login after lock expiry: %v", err)
	}
	reset, err := svc.repo.GetByUsername(username)
	if err != nil || reset == nil || reset.FailedAttempts != 0 || reset.LockedUntil != nil {
		t.Fatalf("reset after expiry login: %+v err=%v", reset, err)
	}

	// 停用用户 → ErrAccountDisabled
	if _, err := svc.repo.UpdateUser(authDBUser2ID, nil, nil, boolPtr(true)); err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Login("auth_db_user2", "whatever", "10.3.3.3")
	if !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("disabled user: got %v, want ErrAccountDisabled", err)
	}
	if _, err := svc.repo.UpdateUser(authDBUser2ID, nil, nil, boolPtr(false)); err != nil {
		t.Fatal(err)
	}
}

func TestDBSvcChangePassword(t *testing.T) {
	db := openAuthSvcDB(t)
	repo := NewRepository(db)
	svc := NewService(repo, []byte(authTestJWTSecret))
	username := registerUser(t, svc)
	password := "Test1234abcd"

	// 弱新密码 / 错误旧密码
	if err := svc.ChangePassword(authDBUserID, "x", "weak1"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("weak new password: got %v", err)
	}
	if err := svc.ChangePassword(authDBUserID, "wrong-old", "NewPass1234"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong old password: got %v", err)
	}
	// 用户不存在 → ErrInvalidCredentials
	if err := svc.ChangePassword("00000000-0000-0000-0000-000000009999", "x", "NewPass1234"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("missing user: got %v", err)
	}

	// 改密成功：token_version +1、must_change_pw 清零、旧 refresh 全撤销
	user, err := repo.GetByUsername(username)
	if err != nil || user == nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login(username, password, "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(user.ID, password, "NewPass5678"); err != nil {
		t.Fatal(err)
	}
	after, err := repo.GetByID(user.ID)
	if err != nil || after == nil {
		t.Fatal(err)
	}
	if after.TokenVersion != user.TokenVersion+1 {
		t.Fatalf("token_version = %d, want %d", after.TokenVersion, user.TokenVersion+1)
	}
	if after.MustChangePW {
		t.Fatal("must_change_pw must be cleared after change")
	}
	// 旧密码不可再用，新密码可登录
	if _, _, err := svc.Login(username, password, "10.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password: got %v, want ErrInvalidCredentials", err)
	}
	if _, _, err := svc.Login(username, "NewPass5678", "10.0.0.1"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
}

func TestDBSvcRefreshRotation(t *testing.T) {
	db := openAuthSvcDB(t)
	repo := NewRepository(db)
	svc := NewService(repo, []byte(authTestJWTSecret))
	username := registerUser(t, svc)

	resp, _, err := svc.Login(username, "Test1234abcd", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	oldToken := resp.RefreshToken

	// 轮换：旧 token 撤销、新 pair 可用
	rotated, err := svc.RefreshAccessToken(oldToken)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.AccessToken == "" || rotated.RefreshToken == "" || rotated.RefreshToken == oldToken {
		t.Fatalf("rotated pair: %+v", rotated)
	}
	// 旧 token 重放 → ErrInvalidCredentials，且命中真复用检测
	if _, err := svc.RefreshAccessToken(oldToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("replayed old token: got %v, want ErrInvalidCredentials", err)
	}
	if reuse, err := svc.IsRefreshTokenReuse(oldToken); err != nil || !reuse {
		t.Fatalf("IsRefreshTokenReuse(old) = %v, %v, want true", reuse, err)
	}
	// 新 token 可再轮换
	if _, err := svc.RefreshAccessToken(rotated.RefreshToken); err != nil {
		t.Fatalf("rotate new token: %v", err)
	}
	// 未知 token
	if _, err := svc.RefreshAccessToken("unknown-token"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown token: got %v", err)
	}

	// 停用用户 refresh → ErrAccountDisabled（auth_db_user2 密码为 'x'，改用真实注册用户）
	user2name := registerUser(t, svc)
	u2, err := repo.GetByUsername(user2name)
	if err != nil || u2 == nil {
		t.Fatal(err)
	}
	resp2, _, err := svc.Login(user2name, "Test1234abcd", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	// 停用用户 refresh：UpdateUser 停用会同时撤销全部 refresh token（repo 语义），
	// 因此刷新直接查不到 → ErrInvalidCredentials；仅当停用绕过 repo（直改 SQL）时
	// 才命中 RefreshAccessToken 的 ErrAccountDisabled 纵深防御分支。
	if _, err := repo.UpdateUser(u2.ID, nil, nil, boolPtr(true)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefreshAccessToken(resp2.RefreshToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled+revoked refresh: got %v, want ErrInvalidCredentials", err)
	}
	// 重新启用拿新 token，再绕过 repo 直改 SQL 停用（不撤销 token）→ 命中 ErrAccountDisabled 分支
	if _, err := db.Exec(`UPDATE users SET disabled = FALSE WHERE id = $1`, u2.ID); err != nil {
		t.Fatal(err)
	}
	resp2b, _, err := svc.Login(user2name, "Test1234abcd", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET disabled = TRUE WHERE id = $1`, u2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefreshAccessToken(resp2b.RefreshToken); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("disabled user refresh: got %v, want ErrAccountDisabled", err)
	}
	if _, err := repo.UpdateUser(u2.ID, nil, nil, boolPtr(false)); err != nil {
		t.Fatal(err)
	}
}

func TestDBSvcLogout(t *testing.T) {
	db := openAuthSvcDB(t)
	repo := NewRepository(db)
	svc := NewService(repo, []byte(authTestJWTSecret))
	username := registerUser(t, svc)

	// 空 token / 未知 token → 静默成功
	if err := svc.Logout(""); err != nil {
		t.Fatalf("empty token logout: %v", err)
	}
	if err := svc.Logout("unknown-token"); err != nil {
		t.Fatalf("unknown token logout: %v", err)
	}

	resp, _, err := svc.Login(username, "Test1234abcd", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(resp.RefreshToken); err != nil {
		t.Fatal(err)
	}
	// 退出后 token 已撤销 → refresh 拒绝，且命中真复用重放检测（Logout 写黑名单）
	if _, err := svc.RefreshAccessToken(resp.RefreshToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("refresh after logout: got %v, want ErrInvalidCredentials", err)
	}
	if reuse, err := svc.IsRefreshTokenReuse(resp.RefreshToken); err != nil || !reuse {
		t.Fatalf("IsRefreshTokenReuse after logout = %v, %v, want true", reuse, err)
	}
}

func TestDBSvcUpdateProfile(t *testing.T) {
	db := openAuthSvcDB(t)
	repo := NewRepository(db)
	svc := NewService(repo, []byte(authTestJWTSecret))

	// 非法语言
	if _, err := svc.UpdateProfile(authDBUserID, UpdateProfileRequest{Language: "fr"}); !errors.Is(err, ErrInvalidLanguage) {
		t.Fatalf("invalid language: got %v, want ErrInvalidLanguage", err)
	}
	// 用户不存在
	if _, err := svc.UpdateProfile("00000000-0000-0000-0000-000000009999", UpdateProfileRequest{Language: LanguageEN}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("missing user: got %v, want ErrInvalidCredentials", err)
	}
	// 成功：切 en，返回 UserInfo
	info, err := svc.UpdateProfile(authDBUserID, UpdateProfileRequest{Language: LanguageEN})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.Language != LanguageEN || info.Username != "auth_db_user1" {
		t.Fatalf("profile: %+v", info)
	}
	user, err := repo.GetByID(authDBUserID)
	if err != nil || user == nil || user.Language != LanguageEN {
		t.Fatalf("persisted language: %+v err=%v", user, err)
	}
	// 切回 zh 不影响其他字段
	if _, err := svc.UpdateProfile(authDBUserID, UpdateProfileRequest{Language: LanguageZH}); err != nil {
		t.Fatal(err)
	}
}

func TestDBSvcAdminUsers(t *testing.T) {
	db := openAuthSvcDB(t)
	repo := NewRepository(db)
	svc := NewService(repo, []byte(authTestJWTSecret))

	// AdminCreateUser：非法角色 / 弱密码 / 用户名重复
	if _, _, err := svc.AdminCreateUser(AdminCreateUserRequest{Username: "x1", Role: "boss", Password: "Test1234abcd"}); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("bad role: got %v, want ErrInvalidRole", err)
	}
	if _, _, err := svc.AdminCreateUser(AdminCreateUserRequest{Username: "x2", Password: "weak"}); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("weak password: got %v", err)
	}
	if _, _, err := svc.AdminCreateUser(AdminCreateUserRequest{Username: "auth_db_user1", Password: "Test1234abcd"}); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("taken username: got %v", err)
	}

	// 显式密码创建（maintainer）
	created, createdInfo, err := svc.AdminCreateUser(AdminCreateUserRequest{Username: "admin_created_1", DisplayName: "管理员开号", Role: RoleMaintainer, Password: "Test1234abcd"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE username = 'admin_created_1'`) })
	if createdInfo.Username != "admin_created_1" || createdInfo.Role != RoleMaintainer || created.TemporaryPassword != "Test1234abcd" {
		t.Fatalf("created: %+v %+v", created, createdInfo)
	}
	// 生成的临时密码（18 位 hex）→ must_change_pw=true
	auto, autoInfo, err := svc.AdminCreateUser(AdminCreateUserRequest{Username: "admin_created_2"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE username = 'admin_created_2'`) })
	if len(auto.TemporaryPassword) != 18 {
		t.Fatalf("temporary password length = %d, want 18", len(auto.TemporaryPassword))
	}
	got, err := repo.GetByID(autoInfo.ID)
	if err != nil || got == nil || !got.MustChangePW {
		t.Fatalf("auto-created user: %+v err=%v", got, err)
	}
	// 临时密码可登录
	if _, _, err := svc.Login("admin_created_2", auto.TemporaryPassword, "10.0.0.1"); err != nil {
		t.Fatalf("temporary password login: %v", err)
	}

	// AdminUpdateUser：改名 + 升角色
	name := "改名用户"
	role := RoleMaintainer
	updated, err := svc.AdminUpdateUser(authDBAdmin1ID, authDBUserID, AdminUpdateUserRequest{DisplayName: &name, Role: &role})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != name || updated.Role != RoleMaintainer {
		t.Fatalf("updated: %+v", updated)
	}
	// 不能改自己
	if _, err := svc.AdminUpdateUser(authDBAdmin1ID, authDBAdmin1ID, AdminUpdateUserRequest{}); !errors.Is(err, ErrCannotModifySelf) {
		t.Fatalf("self update: got %v, want ErrCannotModifySelf", err)
	}
	// 目标不存在
	if _, err := svc.AdminUpdateUser(authDBAdmin1ID, "00000000-0000-0000-0000-000000009999", AdminUpdateUserRequest{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("missing target: got %v", err)
	}
	// 非法角色
	badRole := "boss"
	if _, err := svc.AdminUpdateUser(authDBAdmin1ID, authDBUserID, AdminUpdateUserRequest{Role: &badRole}); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("bad role update: got %v", err)
	}

	// 停用用户 → 登录被拒
	disabled := true
	if _, err := svc.AdminUpdateUser(authDBAdmin1ID, authDBUser2ID, AdminUpdateUserRequest{Disabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login("auth_db_user2", "whatever", "10.0.0.1"); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("disabled login: got %v", err)
	}
	if _, err := svc.AdminUpdateUser(authDBAdmin1ID, authDBUser2ID, AdminUpdateUserRequest{Disabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}

	// 最后一个活跃管理员保护：临时停用其他全部 admin（含迁移 009 的 haofan），
	// 再降级 b501 → ErrLastActiveAdmin；恢复原状。
	var restore []string
	rows, err := db.Query(`SELECT id FROM users WHERE role = 'admin' AND disabled = FALSE AND id NOT IN ($1,$2)`,
		authDBAdmin1ID, authDBAdmin2ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		restore = append(restore, id)
	}
	rows.Close()
	t.Cleanup(func() {
		for _, id := range restore {
			db.Exec(`UPDATE users SET role = 'admin', disabled = FALSE WHERE id = $1`, id)
		}
	})
	for _, id := range restore {
		if _, err := db.Exec(`UPDATE users SET disabled = TRUE WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
	// 此时活跃 admin 仅 b501、b502：降级 b502 成功（仍剩 b501）
	demote := RoleMember
	if _, err := svc.AdminUpdateUser(authDBAdmin1ID, authDBAdmin2ID, AdminUpdateUserRequest{Role: &demote}); err != nil {
		t.Fatalf("demote when 2 admins left: %v", err)
	}
	// 恢复 b502 角色（本用例降级它后不再重置，种子 DO UPDATE 只在下次用例开头兜底）
	t.Cleanup(func() {
		db.Exec(`UPDATE users SET role = 'admin' WHERE id = $1`, authDBAdmin2ID)
	})
	// 再降级 b501 → 最后一个 → ErrLastActiveAdmin
	if _, err := svc.AdminUpdateUser(authDBAdmin1ID, authDBAdmin1ID, AdminUpdateUserRequest{Role: &demote}); !errors.Is(err, ErrCannotModifySelf) {
		t.Fatalf("self demote: got %v, want ErrCannotModifySelf", err)
	}
	adminID2 := authDBAdmin2ID
	if _, err := svc.AdminUpdateUser(adminID2, authDBAdmin1ID, AdminUpdateUserRequest{Role: &demote}); !errors.Is(err, ErrLastActiveAdmin) {
		t.Fatalf("last admin demote: got %v, want ErrLastActiveAdmin", err)
	}
	// 停用最后一个管理员同样被拒
	if _, err := svc.AdminUpdateUser(adminID2, authDBAdmin1ID, AdminUpdateUserRequest{Disabled: boolPtr(true)}); !errors.Is(err, ErrLastActiveAdmin) {
		t.Fatalf("last admin disable: got %v, want ErrLastActiveAdmin", err)
	}
}

func TestDBSvcAdminResetPassword(t *testing.T) {
	db := openAuthSvcDB(t)
	repo := NewRepository(db)
	svc := NewService(repo, []byte(authTestJWTSecret))

	// 目标不存在 / 弱密码
	if _, err := svc.AdminResetPassword("00000000-0000-0000-0000-000000009999", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("missing user: got %v", err)
	}
	if _, err := svc.AdminResetPassword(authDBUserID, "weak1"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("weak password: got %v", err)
	}
	// 显式新密码
	resp, err := svc.AdminResetPassword(authDBUserID, "ResetPass123")
	if err != nil {
		t.Fatal(err)
	}
	if resp.TemporaryPassword != "ResetPass123" {
		t.Fatalf("explicit reset: %+v", resp)
	}
	user, err := repo.GetByID(authDBUserID)
	if err != nil || user == nil || user.MustChangePW {
		t.Fatalf("after reset: %+v err=%v (must_change_pw 应保持由 UpdatePassword 清零)", user, err)
	}
	if _, _, err := svc.Login("auth_db_user1", "ResetPass123", "10.0.0.1"); err != nil {
		t.Fatalf("login with reset password: %v", err)
	}
	// 自动生成
	auto, err := svc.AdminResetPassword(authDBUser2ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(auto.TemporaryPassword) != 18 {
		t.Fatalf("auto temp password length = %d, want 18", len(auto.TemporaryPassword))
	}
}

func TestDBSvcGetUserAndListUsers(t *testing.T) {
	db := openAuthSvcDB(t)
	svc := NewService(NewRepository(db), []byte(authTestJWTSecret))

	user, err := svc.GetUser(authDBUserID)
	if err != nil || user == nil || user.Username != "auth_db_user1" {
		t.Fatalf("get user: %+v err=%v", user, err)
	}
	if user, err := svc.GetUser("00000000-0000-0000-0000-000000009999"); err != nil || user != nil {
		t.Fatalf("missing user: %+v err=%v", user, err)
	}
	infos, err := svc.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) < 4 {
		t.Fatalf("list users too short: %d", len(infos))
	}
	found := false
	for _, info := range infos {
		if info.Username == "auth_db_user2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("list missing seeded user: %+v", infos)
	}
}

func boolPtr(b bool) *bool { return &b }
