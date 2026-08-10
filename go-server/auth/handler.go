package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/alert"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/notify"
)

// Handler exposes auth HTTP endpoints.
type Handler struct {
	svc           *Service
	cookieSecure  bool
	regIPMu       sync.Mutex
	regIPCalls    sync.Map // IP string -> []time.Time
	loginIPMu     sync.Mutex
	loginIPCalls  sync.Map // IP string -> []time.Time（跨用户名聚合，S1）
	now           func() time.Time
	alertReporter alert.Reporter
}

// NewHandler creates a new auth handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc, cookieSecure: os.Getenv("COOKIE_SECURE") == "true", now: time.Now}
}

// SetAlertReporter 注入告警上报窄接口（main.go 接 alertSvc；安全事件收敛到告警中心）。
func (h *Handler) SetAlertReporter(r alert.Reporter) {
	h.alertReporter = r
}

// reportAlert 异步上报告警中心（未注入或失败仅记日志，不影响业务路径）。
func (h *Handler) reportAlert(level, source, title, detail string) {
	if h.alertReporter == nil {
		return
	}
	go func() {
		if _, err := h.alertReporter.Report(context.Background(), level, source, title, detail); err != nil {
			slog.Error("alert report failed", "error", err, "source", source, "title", title)
		}
	}()
}

func (h *Handler) SetCookieSecure(secure bool) {
	h.cookieSecure = secure
}

// Routes mounts auth endpoints on a chi router.
func (h *Handler) Routes(audit ...func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()
	for _, m := range audit {
		r.Use(m)
	}
	r.Post("/register", h.Register)
	r.Post("/login", h.Login)
	r.Post("/refresh", h.Refresh)
	r.Post("/logout", h.Logout)
	r.Group(func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Get("/me", h.Me)
		r.Post("/change-password", h.ChangePassword)
		r.Patch("/profile", h.UpdateProfile)
	})
	return r
}

// Register creates a new user account.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	// S2：开放注册开关，默认关闭（管理员开号）。
	if os.Getenv("ALLOW_REGISTER") != "true" {
		common.WriteError(w, r, http.StatusForbidden, "registration_disabled", "注册已关闭，请联系管理员开通账号", nil)
		return
	}
	ip := getClientIP(r)
	if !h.allowRegisterIP(ip) {
		common.WriteError(w, r, http.StatusTooManyRequests, "rate_limit_exceeded", "注册请求过于频繁，请稍后再试", nil)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	middleware.SetAuditUsername(r.Context(), req.Username)

	user, err := h.svc.Register(req.Username, req.Password)
	if err != nil {
		mapAuthError(w, r, err)
		return
	}

	go notify.Send("lab-alerts", "新用户注册", fmt.Sprintf("新用户注册: %s", user.Username), "", "default", []string{"partying_face"})

	common.WriteCreated(w, r, toUserInfo(user))
}

// Login authenticates a user and returns a token pair.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	// S1：IP 级滑动窗口限流（跨用户名聚合），账户锁定 5 次/15min/用户名作第二道（service 内）。
	ip := getClientIP(r)
	if !h.allowLoginIP(ip) {
		h.reportAlert("warning", "security", "登录 IP 限流", "来源 IP "+ip+" 登录尝试过于频繁")
		common.WriteError(w, r, http.StatusTooManyRequests, "rate_limit_exceeded", "登录请求过于频繁，请稍后再试", nil)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	middleware.SetAuditUsername(r.Context(), req.Username)

	resp, newIP, err := h.svc.Login(req.Username, req.Password, ip)
	if err != nil {
		// S5：任意用户名连续失败 5 次触发账户锁定 → 告警（告警中心按 source+title 去重）。
		if errors.Is(err, ErrAccountLocked) {
			h.reportAlert("critical", "security", "登录失败账户锁定", "用户 "+req.Username+" 连续失败 5 次已锁定（来源 IP "+ip+"）")
		}
		if req.Username == "admin" {
			h.reportAlert("warning", "security", "管理员登录失败", "用户 "+req.Username+" 尝试登录失败")
		}
		mapAuthError(w, r, err)
		return
	}
	// S5：新 IP 成功登录告警；last_login 更新由 Audit 中间件与审计行同一事务落库（迁移 036）。
	if newIP {
		h.reportAlert("warning", "security", "新 IP 登录", "用户 "+resp.User.Username+" 从新 IP "+ip+" 登录")
	}
	if ip != "" {
		middleware.SetAuditLastLogin(r.Context(), resp.User.ID, ip)
	}

	h.setTokenCookies(w, resp.AccessToken, resp.RefreshToken)
	resp.CSRFToken = setCSRFCookie(w, h.cookieSecure)
	common.WriteSuccess(w, r, resp)
}

// Refresh rotates a refresh token and issues a new token pair.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	// S1：refresh 与 login 共享同一 IP 级滑动窗口（跨用户名聚合）。
	if !h.allowLoginIP(getClientIP(r)) {
		common.WriteError(w, r, http.StatusTooManyRequests, "rate_limit_exceeded", "登录请求过于频繁，请稍后再试", nil)
		return
	}
	if !requireIdempotencyKey(w, r) {
		return
	}

	req := RefreshRequest{RefreshToken: cookieValue(r, "refresh_token")}
	if req.RefreshToken == "" && r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
			return
		}
	}

	resp, err := h.svc.RefreshAccessToken(req.RefreshToken)
	if err != nil {
		// 降噪（P2-4）：仅真复用（已撤销 token 被重放）才告警；
		// 正常过期/用户禁用/未知 token 一律静默（mapAuthError 返回既有错误码）。
		if reuse, rErr := h.svc.IsRefreshTokenReuse(req.RefreshToken); rErr == nil && reuse {
			h.reportAlert("critical", "security", "Refresh Token 复用", "检测到可能已撤销的 refresh token")
		}
		mapAuthError(w, r, err)
		return
	}

	h.setTokenCookies(w, resp.AccessToken, resp.RefreshToken)
	resp.CSRFToken = setCSRFCookie(w, h.cookieSecure)
	common.WriteSuccess(w, r, resp)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Logout(cookieValue(r, "refresh_token")); err != nil {
		mapAuthError(w, r, err)
		return
	}
	clearCookie(w, "access_token", "/api", h.cookieSecure)
	clearCookie(w, "refresh_token", "/api", h.cookieSecure)
	clearCookie(w, "csrf_token", "/", h.cookieSecure)
	common.WriteSuccess(w, r, map[string]bool{"success": true})
}

// Me returns the current authenticated user's profile.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}

	user, err := h.svc.GetUser(claims.UserID)
	if err != nil {
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "查询用户失败", nil)
		return
	}
	if user == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "用户不存在", nil)
		return
	}

	common.WriteSuccess(w, r, toUserInfo(user))
}

// ChangePassword updates the authenticated user's password.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}

	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}

	if err := h.svc.ChangePassword(claims.UserID, req.OldPassword, req.NewPassword); err != nil {
		mapAuthError(w, r, err)
		return
	}

	common.WriteSuccess(w, r, map[string]bool{"success": true})
}

// UpdateProfile updates the authenticated user's own profile (language preference).
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}

	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}

	info, err := h.svc.UpdateProfile(claims.UserID, req)
	if err != nil {
		mapAuthError(w, r, err)
		return
	}

	common.WriteSuccess(w, r, info)
}

func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.ListUsers()
	if err != nil {
		mapAuthError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, users)
}

func (h *Handler) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var req AdminCreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	password, user, err := h.svc.AdminCreateUser(req)
	if err != nil {
		mapAuthError(w, r, err)
		return
	}
	common.WriteCreated(w, r, map[string]any{"user": user, "temporary_password": password.TemporaryPassword})
}

func (h *Handler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req AdminUpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	user, err := h.svc.AdminUpdateUser(claims.UserID, chi.URLParam(r, "id"), req)
	if err != nil {
		mapAuthError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, user)
}

func (h *Handler) AdminResetPassword(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var req AdminResetPasswordRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	resp, err := h.svc.AdminResetPassword(chi.URLParam(r, "id"), req.NewPassword)
	if err != nil {
		mapAuthError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, resp)
}

// getClientIP 只读来源门规范化的来源 IP（S4，禁止再信任 XFF/RemoteAddr）。
// 来源门未挂载时（单测/降级）回退真实 TCP 对端 host。
func getClientIP(r *http.Request) string {
	if ip := middleware.GetSourceIP(r.Context()); ip != "" {
		return ip
	}
	return hostOnly(r.RemoteAddr)
}

func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// allowRegisterIP 注册限流：5 次/小时/IP（S2 收紧，原 10 次）。
func (h *Handler) allowRegisterIP(ip string) bool {
	return h.slideWindow(&h.regIPMu, &h.regIPCalls, ip, 5, time.Hour)
}

// allowLoginIP 登录 IP 级滑动窗口（S1）：LOGIN_RATE_LIMIT_IP_MAX 次 /
// LOGIN_RATE_LIMIT_IP_WINDOW，跨用户名聚合（键仅 IP）；置 0 关闭。
func (h *Handler) allowLoginIP(ip string) bool {
	max := envInt("LOGIN_RATE_LIMIT_IP_MAX", 20)
	if max <= 0 {
		return true
	}
	return h.slideWindow(&h.loginIPMu, &h.loginIPCalls, ip, max, envDuration("LOGIN_RATE_LIMIT_IP_WINDOW", 15*time.Minute))
}

// slideWindow 滑动窗口计数：窗口外旧调用剔除，达到上限拒绝，否则记录本次。
func (h *Handler) slideWindow(mu *sync.Mutex, calls *sync.Map, ip string, max int, window time.Duration) bool {
	now, cutoff := h.now(), h.now().Add(-window)
	mu.Lock()
	defer mu.Unlock()

	val, _ := calls.Load(ip)
	var list []time.Time
	if val != nil {
		list = val.([]time.Time)[:0]
		for _, c := range val.([]time.Time) {
			if c.After(cutoff) {
				list = append(list, c)
			}
		}
	}

	if len(list) >= max {
		calls.Store(ip, list)
		return false
	}
	calls.Store(ip, append(list, now))
	return true
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func toUserInfo(user *User) UserInfo {
	return UserInfo{
		ID:           user.ID,
		Username:     user.Username,
		DisplayName:  user.DisplayName,
		Role:         user.Role,
		Disabled:     user.Disabled,
		Language:     user.Language,
		CreatedAt:    user.CreatedAt,
		MustChangePW: user.MustChangePW,
	}
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") == "" {
		common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
		return false
	}
	return true
}

func (h *Handler) setTokenCookies(w http.ResponseWriter, accessToken, refreshToken string) {
	setCookie(w, "access_token", accessToken, "/api", int(accessTokenTTL.Seconds()), true, h.cookieSecure)
	setCookie(w, "refresh_token", refreshToken, "/api", int(refreshTokenTTL.Seconds()), true, h.cookieSecure)
}

func setCSRFCookie(w http.ResponseWriter, secure bool) string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)
	// CSRF cookie 使用 Path=/：前端页面不在 /api 路径下，
	// 只有 Path=/ 时 document.cookie 才能读到它用于恢复 X-CSRF-Token header。
	setCookie(w, "csrf_token", token, "/", int(refreshTokenTTL.Seconds()), false, secure)
	return token
}

func setCookie(w http.ResponseWriter, name, value, path string, maxAge int, httpOnly, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		Expires:  time.Now().Add(time.Duration(maxAge) * time.Second),
		HttpOnly: httpOnly,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearCookie(w http.ResponseWriter, name, path string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: name != "csrf_token",
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func mapAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUsernameTaken):
		common.WriteError(w, r, http.StatusConflict, "username_taken", err.Error(), nil)
	case errors.Is(err, ErrPasswordTooShort):
		common.WriteError(w, r, http.StatusBadRequest, "password_too_short", err.Error(), nil)
	case errors.Is(err, ErrAccountLocked):
		common.WriteError(w, r, http.StatusTooManyRequests, "account_locked", err.Error(), nil)
	case errors.Is(err, ErrAccountDisabled):
		common.WriteError(w, r, http.StatusForbidden, "account_disabled", err.Error(), nil)
	case errors.Is(err, ErrInvalidCredentials):
		common.WriteError(w, r, http.StatusUnauthorized, "invalid_credentials", err.Error(), nil)
	case errors.Is(err, ErrInvalidRole):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_role", err.Error(), nil)
	case errors.Is(err, ErrInvalidLanguage):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_language", err.Error(), nil)
	case errors.Is(err, ErrCannotModifySelf):
		common.WriteError(w, r, http.StatusBadRequest, "cannot_modify_self", err.Error(), nil)
	case errors.Is(err, ErrLastActiveAdmin):
		common.WriteError(w, r, http.StatusConflict, "last_active_admin", err.Error(), nil)
	default:
		slog.Error("auth request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
