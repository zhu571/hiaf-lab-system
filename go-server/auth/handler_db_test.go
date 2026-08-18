package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// handler 集成测试：与 main.go 一致挂 AuthRequired + RequireRole + Audit + RequestID
// 中间件（真实 DB，TEST_DATABASE_URL 空则跳过）。handler 层只断言状态码 + 响应体；
// 审计写入由 Audit 中间件负责，此处顺带校验 audit_log 落库。
// 注意：auth handler 目前没有任何 SetAuditAction 调用，审计 action 全部为 Audit 中间件
// 按路径派生（auth.login / auth.refresh / auth.register / auth.change-password /
// auth.profile / admin.users 等）；本文件按派生值断言，语义化 action 缺失另见报告。

const authHandlerTestSecret = "auth-handler-test-secret"

const (
	authHandlerAdminID = "00000000-0000-0000-0000-00000000b601"
	authHandlerUserID  = "00000000-0000-0000-0000-00000000b602"
	authHandlerUser2ID = "00000000-0000-0000-0000-00000000b603"
)

func newAuthTestRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	middleware.SetJWTSecret([]byte(authHandlerTestSecret))
	svc := NewService(NewRepository(db), []byte(authHandlerTestSecret))
	h := NewHandler(svc)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Mount("/api/v1/auth", h.Routes(middleware.Audit(db)))
	r.Route("/api/v1/admin/users", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.RequireRole(RoleAdmin))
		r.Use(middleware.Audit(db))
		r.Get("/", h.AdminListUsers)
		r.Post("/", h.AdminCreateUser)
		r.Patch("/{id}", h.AdminUpdateUser)
		r.Post("/{id}/reset-password", h.AdminResetPassword)
	})
	r.Route("/api/v1/admin/invitation-codes", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.RequireRole(RoleAdmin))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", h.AdminListInvitationCodes)
		r.Post("/", h.AdminCreateInvitationCode)
		r.Post("/{id}/revoke", h.AdminRevokeInvitationCode)
	})
	return r
}

// openAuthHandlerDB 种子：admin（b601）+ 普通用户（b602/b603），密码 Test1234abcd。
func openAuthHandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openAuthSvcDB(t)
	// b501/b502/b503/b504 由 openAuthSvcDB 种子；再补 b601/b602/b603
	for _, u := range []struct {
		id       string
		username string
		role     string
	}{
		{authHandlerAdminID, "auth_h_admin", RoleAdmin},
		{authHandlerUserID, "auth_h_user1", RoleMember},
		{authHandlerUser2ID, "auth_h_user2", RoleMember},
	} {
		hash, err := hashPassword("Test1234abcd")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(
			`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
			 VALUES ($1, $2, $3, 'Auth Handler Test', $4, false, false)
			 ON CONFLICT (id) DO UPDATE SET
			   username = EXCLUDED.username, password_hash = EXCLUDED.password_hash,
			   display_name = EXCLUDED.display_name, role = EXCLUDED.role,
			   must_change_pw = false, disabled = false,
			   failed_attempts = 0, locked_until = NULL, language = 'zh'`, u.id, u.username, hash, u.role); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		// 注意：audit_log.user_id 引用 users(id)（无级联），登录/改密等请求产生的审计行
		// 会阻塞用户删除；先清本用例审计行再删用户（测试库专用，审计包测试同模式）。
		if _, err := db.Exec(`DELETE FROM audit_log WHERE user_id IN ($1,$2,$3)`,
			authHandlerAdminID, authHandlerUserID, authHandlerUser2ID); err != nil {
			t.Logf("cleanup: delete audit_log failed: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM refresh_tokens WHERE user_id IN ($1,$2,$3)`,
			authHandlerAdminID, authHandlerUserID, authHandlerUser2ID); err != nil {
			t.Logf("cleanup: delete refresh_tokens failed: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM revoked_tokens WHERE user_id IN ($1,$2,$3)`,
			authHandlerAdminID, authHandlerUserID, authHandlerUser2ID); err != nil {
			t.Logf("cleanup: delete revoked_tokens failed: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM users WHERE id IN ($1,$2,$3)`,
			authHandlerAdminID, authHandlerUserID, authHandlerUser2ID); err != nil {
			t.Logf("cleanup: delete users failed: %v", err)
		}
	})
	return db
}

func authToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(authHandlerTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func authReq(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func cookieValueOf(rec *httptest.ResponseRecorder, name string) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func decodeAuthEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (json.RawMessage, string) {
	t.Helper()
	var envelope struct {
		Data      json.RawMessage `json:"data"`
		RequestID string          `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("body: %s err=%v", rec.Body.String(), err)
	}
	return envelope.Data, envelope.RequestID
}

func decodeAuthError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error body: %s err=%v", rec.Body.String(), err)
	}
	return envelope.Error.Code
}

func uniqueAuthUsername(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func assertAuthAudit(t *testing.T, db *sql.DB, requestID, action string) {
	t.Helper()
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE request_id = $1 AND action = $2`, requestID, action,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit_log rows = %d, want 1 (request_id=%s action=%s)", count, requestID, action)
	}
}

// --- 登录 / 锁定 / 停用 ---

func TestHandlerLoginSuccessAndAudit(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)
	t.Setenv("LOGIN_RATE_LIMIT_IP_MAX", "0")

	// 200：登录成功，断言 token pair + user + cookie
	rec := authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"auth_h_user1","password":"Test1234abcd"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d, body=%s", rec.Code, rec.Body.String())
	}
	data, requestID := decodeAuthEnvelope(t, rec)
	var resp struct {
		AccessToken        string `json:"access_token"`
		RefreshToken       string `json:"refresh_token"`
		ExpiresIn          int    `json:"expires_in"`
		CSRFToken          string `json:"csrf_token"`
		MustChangePassword bool   `json:"must_change_password"`
		User               struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" || resp.ExpiresIn <= 0 || resp.CSRFToken == "" {
		t.Fatalf("login response: %+v", resp)
	}
	if resp.User.Username != "auth_h_user1" || resp.User.Role != RoleMember {
		t.Fatalf("user: %+v", resp.User)
	}
	if cookieValueOf(rec, "csrf_token") == "" {
		t.Fatal("csrf_token cookie missing")
	}
	// 审计行：path 派生 action auth.login
	assertAuthAudit(t, db, requestID, "auth.login")
	// S5：last_login_ip 由 Audit 中间件与审计同事务落库
	var lastLoginIP string
	if err := db.QueryRow(`SELECT COALESCE(last_login_ip, '') FROM users WHERE id = $1`, authHandlerUserID).Scan(&lastLoginIP); err != nil {
		t.Fatal(err)
	}
	if lastLoginIP == "" {
		t.Fatal("last_login_ip must be persisted after login")
	}

	// 401：密码错误
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"auth_h_user1","password":"wrong-pass"}`)
	if rec.Code != http.StatusUnauthorized || decodeAuthError(t, rec) != "invalid_credentials" {
		t.Fatalf("wrong password = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 401：未知用户
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"no_such_user","password":"Test1234abcd"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user = %d", rec.Code)
	}
	// 400：坏 JSON
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "", `{bad`)
	if rec.Code != http.StatusBadRequest || decodeAuthError(t, rec) != "bad_request" {
		t.Fatalf("bad json = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerLoginLockoutAfterFive(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)
	// 锁定目标：新注册用户（must_change_pw=true），避免污染种子用户
	username := uniqueAuthUsername("lock_h")
	svc := NewService(NewRepository(db), []byte(authHandlerTestSecret))
	user, err := svc.Register(username, "Test1234abcd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, user.ID) })

	// 1-4 次失败 → 401；第 5 次 → 429 account_locked；锁定后正确密码也 429
	for i := 1; i <= 4; i++ {
		rec := authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
			`{"username":"`+username+`","password":"bad"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i, rec.Code)
		}
	}
	rec := authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"`+username+`","password":"bad"}`)
	if rec.Code != http.StatusTooManyRequests || decodeAuthError(t, rec) != "account_locked" {
		t.Fatalf("5th attempt = %d body=%s, want 429 account_locked", rec.Code, rec.Body.String())
	}
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"`+username+`","password":"Test1234abcd"}`)
	if rec.Code != http.StatusTooManyRequests || decodeAuthError(t, rec) != "account_locked" {
		t.Fatalf("locked correct login = %d body=%s, want 429", rec.Code, rec.Body.String())
	}
}

func TestHandlerLoginDisabledAccount(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)

	// 停用 b603 后登录 → 403 account_disabled
	if _, err := db.Exec(`UPDATE users SET disabled = TRUE WHERE id = $1`, authHandlerUser2ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`UPDATE users SET disabled = FALSE WHERE id = $1`, authHandlerUser2ID) })
	rec := authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"auth_h_user2","password":"Test1234abcd"}`)
	if rec.Code != http.StatusForbidden || decodeAuthError(t, rec) != "account_disabled" {
		t.Fatalf("disabled login = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerLoginIPRateLimit(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)
	t.Setenv("LOGIN_RATE_LIMIT_IP_MAX", "2")

	// 同一 IP 第 3 次登录 → 429 rate_limit_exceeded（跨用户名聚合）
	rec := authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"auth_h_user1","password":"bad1"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("call1 = %d", rec.Code)
	}
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"auth_h_user2","password":"bad2"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("call2 = %d", rec.Code)
	}
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"auth_h_user1","password":"bad3"}`)
	if rec.Code != http.StatusTooManyRequests || decodeAuthError(t, rec) != "rate_limit_exceeded" {
		t.Fatalf("call3 = %d body=%s, want 429 rate_limit_exceeded", rec.Code, rec.Body.String())
	}
}

// --- 注册 ---

func TestHandlerRegister(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)
	svc := NewService(NewRepository(db), []byte(authHandlerTestSecret))
	newInvitation := func() string {
		t.Helper()
		i, code, err := svc.CreateInvitationCode(authHandlerAdminID, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Exec(`DELETE FROM invitation_codes WHERE id = $1`, i.ID) })
		return code
	}

	// 默认关闭 → 403 registration_disabled
	rec := authReq(t, router, http.MethodPost, "/api/v1/auth/register", "", "",
		`{"username":"reg_1","password":"Test1234abcd"}`)
	if rec.Code != http.StatusForbidden || decodeAuthError(t, rec) != "registration_disabled" {
		t.Fatalf("register disabled = %d body=%s", rec.Code, rec.Body.String())
	}

	// 开启后：201 成功
	t.Setenv("ALLOW_REGISTER", "true")
	// 开启后无邀请码 → 400 invitation_code_required
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/register", "", "",
		`{"username":"reg_no_code","password":"Test1234abcd"}`)
	if rec.Code != http.StatusBadRequest || decodeAuthError(t, rec) != "invitation_code_required" {
		t.Fatalf("register without invitation = %d body=%s", rec.Code, rec.Body.String())
	}
	username := uniqueAuthUsername("reg_h")
	code := newInvitation()
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/register", "", "",
		`{"username":"`+username+`","password":"Test1234abcd","invitation_code":"`+code+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID := decodeAuthEnvelope(t, rec)
	assertAuthAudit(t, db, requestID, "auth.register")
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE username = $1`, username) })

	// 重复用户名 → 409 username_taken
	duplicateCode := newInvitation()
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/register", "", "",
		`{"username":"`+username+`","password":"Test1234abcd","invitation_code":"`+duplicateCode+`"}`)
	if rec.Code != http.StatusConflict || decodeAuthError(t, rec) != "username_taken" {
		t.Fatalf("duplicate = %d body=%s", rec.Code, rec.Body.String())
	}

	// 注册限流：5 次/小时/IP，第 6 次 → 429（换新 router 保证独立滑动窗口计数，
	// 前面的注册/重复用户名尝试已计入旧窗口）
	limitRouter := newAuthTestRouter(t, db)
	var rateUsers []string
	for i := 0; i < 5; i++ {
		u := uniqueAuthUsername("reg_lim")
		rateUsers = append(rateUsers, u)
		code := newInvitation()
		rec = authReq(t, limitRouter, http.MethodPost, "/api/v1/auth/register", "", "",
			`{"username":"`+u+`","password":"Test1234abcd","invitation_code":"`+code+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("rate register %d = %d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	t.Cleanup(func() {
		for _, u := range rateUsers {
			db.Exec(`DELETE FROM users WHERE username = $1`, u)
		}
	})
	rec = authReq(t, limitRouter, http.MethodPost, "/api/v1/auth/register", "", "",
		`{"username":"`+uniqueAuthUsername("reg_lim")+`","password":"Test1234abcd"}`)
	if rec.Code != http.StatusTooManyRequests || decodeAuthError(t, rec) != "rate_limit_exceeded" {
		t.Fatalf("6th register = %d body=%s", rec.Code, rec.Body.String())
	}
}

// --- refresh / logout ---

func TestHandlerRefreshRotationAndReuse(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)
	t.Setenv("LOGIN_RATE_LIMIT_IP_MAX", "0")

	loginRec := authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"auth_h_user1","password":"Test1234abcd"}`)
	oldRefresh := cookieValueOf(loginRec, "refresh_token")
	if oldRefresh == "" {
		t.Fatal("login did not set refresh_token cookie")
	}

	// 400：缺 Idempotency-Key
	rec := authReq(t, router, http.MethodPost, "/api/v1/auth/refresh", "", "", "")
	if rec.Code != http.StatusBadRequest || decodeAuthError(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idem key = %d body=%s", rec.Code, rec.Body.String())
	}

	// 200：带 cookie + Idempotency-Key 轮换成功
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(""))
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: oldRefresh})
	req.Header.Set("Idempotency-Key", "refresh-key-1")
	reqRec := httptest.NewRecorder()
	router.ServeHTTP(reqRec, req)
	if reqRec.Code != http.StatusOK {
		t.Fatalf("refresh = %d body=%s", reqRec.Code, reqRec.Body.String())
	}
	_, requestID := decodeAuthEnvelope(t, reqRec)
	assertAuthAudit(t, db, requestID, "auth.refresh")
	newRefresh := cookieValueOf(reqRec, "refresh_token")
	if newRefresh == "" || newRefresh == oldRefresh {
		t.Fatal("refresh must rotate token")
	}

	// 旧 token 重放 → 401（真复用）
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(""))
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: oldRefresh})
	req.Header.Set("Idempotency-Key", "refresh-key-2")
	reqRec = httptest.NewRecorder()
	router.ServeHTTP(reqRec, req)
	if reqRec.Code != http.StatusUnauthorized || decodeAuthError(t, reqRec) != "invalid_credentials" {
		t.Fatalf("reuse = %d body=%s, want 401 invalid_credentials", reqRec.Code, reqRec.Body.String())
	}

	// 新 token 可再轮换
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(""))
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: newRefresh})
	req.Header.Set("Idempotency-Key", "refresh-key-3")
	reqRec = httptest.NewRecorder()
	router.ServeHTTP(reqRec, req)
	if reqRec.Code != http.StatusOK {
		t.Fatalf("rotate new = %d body=%s", reqRec.Code, reqRec.Body.String())
	}

	// logout：带 refresh cookie 清 cookie + token 撤销 → 之后 refresh 401
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(""))
	logoutReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: cookieValueOf(reqRec, "refresh_token")})
	logoutRec := httptest.NewRecorder()
	router.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout = %d", logoutRec.Code)
	}
	_, logoutReqID := decodeAuthEnvelope(t, logoutRec)
	assertAuthAudit(t, db, logoutReqID, "auth.logout")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(""))
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: cookieValueOf(reqRec, "refresh_token")})
	req.Header.Set("Idempotency-Key", "refresh-key-4")
	reqRec = httptest.NewRecorder()
	router.ServeHTTP(reqRec, req)
	if reqRec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout = %d, want 401", reqRec.Code)
	}
}

// --- me / change-password / profile ---

func TestHandlerMe(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)

	// 401：无 token
	rec := authReq(t, router, http.MethodGet, "/api/v1/auth/me", "", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me without token = %d", rec.Code)
	}

	// 200：返回 profile
	token := authToken(t, authHandlerUserID, "auth_h_user1", RoleMember)
	rec = authReq(t, router, http.MethodGet, "/api/v1/auth/me", token, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d body=%s", rec.Code, rec.Body.String())
	}
	data, _ := decodeAuthEnvelope(t, rec)
	var me struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(data, &me); err != nil {
		t.Fatal(err)
	}
	if me.ID != authHandlerUserID || me.Username != "auth_h_user1" || me.Role != RoleMember || me.Language != "zh" {
		t.Fatalf("me: %+v", me)
	}
}

func TestHandlerChangePassword(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)
	t.Setenv("LOGIN_RATE_LIMIT_IP_MAX", "0")
	token := authToken(t, authHandlerUserID, "auth_h_user1", RoleMember)

	// 400：缺 Idempotency-Key
	rec := authReq(t, router, http.MethodPost, "/api/v1/auth/change-password", token, "",
		`{"old_password":"Test1234abcd","new_password":"NewPass12345"}`)
	if rec.Code != http.StatusBadRequest || decodeAuthError(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idem = %d body=%s", rec.Code, rec.Body.String())
	}
	// 401：旧密码错误
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/change-password", token, "cp-1",
		`{"old_password":"wrong","new_password":"NewPass12345"}`)
	if rec.Code != http.StatusUnauthorized || decodeAuthError(t, rec) != "invalid_credentials" {
		t.Fatalf("wrong old = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：弱新密码
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/change-password", token, "cp-2",
		`{"old_password":"Test1234abcd","new_password":"weak"}`)
	if rec.Code != http.StatusBadRequest || decodeAuthError(t, rec) != "password_too_short" {
		t.Fatalf("weak new = %d body=%s", rec.Code, rec.Body.String())
	}
	// 200：改密成功 + 审计
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/change-password", token, "cp-3",
		`{"old_password":"Test1234abcd","new_password":"NewPass12345"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("change = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID := decodeAuthEnvelope(t, rec)
	assertAuthAudit(t, db, requestID, "auth.change-password")

	// 旧密码失效、新密码可登录
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"auth_h_user1","password":"Test1234abcd"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old pw login = %d, want 401", rec.Code)
	}
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"auth_h_user1","password":"NewPass12345"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("new pw login = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerUpdateProfile(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)
	token := authToken(t, authHandlerUserID, "auth_h_user1", RoleMember)

	// 400：缺 Idempotency-Key
	rec := authReq(t, router, http.MethodPatch, "/api/v1/auth/profile", token, "", `{"language":"en"}`)
	if rec.Code != http.StatusBadRequest || decodeAuthError(t, rec) != "missing_idempotency_key" {
		t.Fatalf("no idem = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：非法语言
	rec = authReq(t, router, http.MethodPatch, "/api/v1/auth/profile", token, "pf-1", `{"language":"fr"}`)
	if rec.Code != http.StatusBadRequest || decodeAuthError(t, rec) != "invalid_language" {
		t.Fatalf("bad language = %d body=%s", rec.Code, rec.Body.String())
	}
	// 200：切英文 + 审计
	rec = authReq(t, router, http.MethodPatch, "/api/v1/auth/profile", token, "pf-2", `{"language":"en"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile = %d body=%s", rec.Code, rec.Body.String())
	}
	_, requestID := decodeAuthEnvelope(t, rec)
	assertAuthAudit(t, db, requestID, "auth.profile")
	var lang string
	if err := db.QueryRow(`SELECT language FROM users WHERE id = $1`, authHandlerUserID).Scan(&lang); err != nil {
		t.Fatal(err)
	}
	if lang != "en" {
		t.Fatalf("language = %q, want en", lang)
	}
}

// --- 管理员用户管理 ---

func TestHandlerAdminUsers(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)
	admin := authToken(t, authHandlerAdminID, "auth_h_admin", RoleAdmin)
	member := authToken(t, authHandlerUserID, "auth_h_user1", RoleMember)

	// 403：member 访问管理员端点
	rec := authReq(t, router, http.MethodGet, "/api/v1/admin/users", member, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member list = %d, want 403", rec.Code)
	}
	rec = authReq(t, router, http.MethodPost, "/api/v1/admin/users", member, "au-1",
		`{"username":"x1","password":"Test1234abcd"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member create = %d, want 403", rec.Code)
	}

	// 201：开号成功，返回 temporary_password，审计 admin.users
	username := uniqueAuthUsername("admin_h")
	rec = authReq(t, router, http.MethodPost, "/api/v1/admin/users", admin, "au-2",
		`{"username":"`+username+`","display_name":"开号用户","role":"maintainer","password":"Test1234abcd"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", rec.Code, rec.Body.String())
	}
	data, requestID := decodeAuthEnvelope(t, rec)
	var created struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
		TemporaryPassword string `json:"temporary_password"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	if created.User.Username != username || created.User.Role != RoleMaintainer || created.TemporaryPassword != "Test1234abcd" {
		t.Fatalf("created: %+v", created)
	}
	assertAuthAudit(t, db, requestID, "admin.users")
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = $1`, created.User.ID) })

	// 400：缺 Idempotency-Key / 非法角色
	rec = authReq(t, router, http.MethodPost, "/api/v1/admin/users", admin, "",
		`{"username":"x2","password":"Test1234abcd"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idem = %d", rec.Code)
	}
	rec = authReq(t, router, http.MethodPost, "/api/v1/admin/users", admin, "au-3",
		`{"username":"x3","role":"boss","password":"Test1234abcd"}`)
	if rec.Code != http.StatusBadRequest || decodeAuthError(t, rec) != "invalid_role" {
		t.Fatalf("bad role = %d body=%s", rec.Code, rec.Body.String())
	}

	// 200：列表
	rec = authReq(t, router, http.MethodGet, "/api/v1/admin/users", admin, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	// 200：改名
	rec = authReq(t, router, http.MethodPatch, "/api/v1/admin/users/"+authHandlerUserID, admin, "au-4",
		`{"display_name":"改名"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：管理员改自己
	rec = authReq(t, router, http.MethodPatch, "/api/v1/admin/users/"+authHandlerAdminID, admin, "au-5",
		`{"display_name":"自改"}`)
	if rec.Code != http.StatusBadRequest || decodeAuthError(t, rec) != "cannot_modify_self" {
		t.Fatalf("self update = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：缺 Idempotency-Key
	rec = authReq(t, router, http.MethodPatch, "/api/v1/admin/users/"+authHandlerUserID, admin, "",
		`{"display_name":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update no idem = %d", rec.Code)
	}

	// 200：重置密码 → 新密码可登录
	rec = authReq(t, router, http.MethodPost, "/api/v1/admin/users/"+authHandlerUser2ID+"/reset-password", admin, "au-6",
		`{"new_password":"ResetPass123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = authReq(t, router, http.MethodPost, "/api/v1/auth/login", "", "",
		`{"username":"auth_h_user2","password":"ResetPass123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login after reset = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：重置密码弱口令
	rec = authReq(t, router, http.MethodPost, "/api/v1/admin/users/"+authHandlerUser2ID+"/reset-password", admin, "au-7",
		`{"new_password":"weak"}`)
	if rec.Code != http.StatusBadRequest || decodeAuthError(t, rec) != "password_too_short" {
		t.Fatalf("weak reset = %d body=%s", rec.Code, rec.Body.String())
	}
	// 400：重置密码缺 Idempotency-Key
	rec = authReq(t, router, http.MethodPost, "/api/v1/admin/users/"+authHandlerUser2ID+"/reset-password", admin, "",
		`{"new_password":"ResetPass123"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reset no idem = %d", rec.Code)
	}
}

func TestHandlerInvitationCodesRequireAdminIdempotencyAndAudit(t *testing.T) {
	db := openAuthHandlerDB(t)
	router := newAuthTestRouter(t, db)
	admin := authToken(t, authHandlerAdminID, "auth_h_admin", RoleAdmin)
	member := authToken(t, authHandlerUserID, "auth_h_user1", RoleMember)
	path := "/api/v1/admin/invitation-codes/"
	// 幂等键必须随机唯一：DB 幂等存储跨测试运行残留会误判 409
	inviteKey := fmt.Sprintf("invite-admin-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		// 先删邀请码（外键指向 users），再让外层 cleanup 删用户
		db.Exec(`DELETE FROM invitation_codes WHERE created_by = $1`, authHandlerAdminID)
	})
	if rec := authReq(t, router, http.MethodPost, path, admin, "", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing idem = %d", rec.Code)
	}
	if rec := authReq(t, router, http.MethodPost, path, member, "invite-member", `{}`); rec.Code != http.StatusForbidden {
		t.Fatalf("member access = %d", rec.Code)
	}
	rec := authReq(t, router, http.MethodPost, path, admin, inviteKey, `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create invitation = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	assertAuthAudit(t, db, envelope.RequestID, "admin.invitation_codes.create")
	if replay := authReq(t, router, http.MethodPost, path, admin, inviteKey, `{}`); replay.Code != http.StatusConflict {
		t.Fatalf("replay = %d", replay.Code)
	}
}
