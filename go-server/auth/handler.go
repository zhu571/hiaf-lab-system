package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/notify"
)

// Handler exposes auth HTTP endpoints.
type Handler struct {
	svc          *Service
	cookieSecure bool
}

// NewHandler creates a new auth handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc, cookieSecure: os.Getenv("COOKIE_SECURE") == "true"}
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
	})
	return r
}

// Register creates a new user account.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}

	user, err := h.svc.Register(req.Username, req.Password)
	if err != nil {
		mapAuthError(w, r, err)
		return
	}

	common.WriteCreated(w, r, toUserInfo(user))
}

// Login authenticates a user and returns a token pair.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}

	resp, err := h.svc.Login(req.Username, req.Password)
	if err != nil {
		if req.Username == "admin" {
			go notify.SecurityAlert("管理员登录失败", "用户 "+req.Username+" 尝试登录失败")
		}
		mapAuthError(w, r, err)
		return
	}

	h.setTokenCookies(w, resp.AccessToken, resp.RefreshToken)
	resp.CSRFToken = setCSRFCookie(w, h.cookieSecure)
	common.WriteSuccess(w, r, resp)
}

// Refresh rotates a refresh token and issues a new token pair.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
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
		go notify.SecurityAlert("Refresh Token 复用", "检测到可能已撤销的 refresh token")
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

func toUserInfo(user *User) UserInfo {
	return UserInfo{
		ID:           user.ID,
		Username:     user.Username,
		DisplayName:  user.DisplayName,
		Role:         user.Role,
		Disabled:     user.Disabled,
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
	case errors.Is(err, ErrCannotModifySelf):
		common.WriteError(w, r, http.StatusBadRequest, "cannot_modify_self", err.Error(), nil)
	case errors.Is(err, ErrLastActiveAdmin):
		common.WriteError(w, r, http.StatusConflict, "last_active_admin", err.Error(), nil)
	default:
		slog.Error("auth request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
