package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// Handler exposes auth HTTP endpoints.
type Handler struct {
	svc *Service
}

// NewHandler creates a new auth handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes mounts auth endpoints on a chi router.
func (h *Handler) Routes(audit ...func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()
	r.Post("/register", h.Register)
	r.Post("/login", h.Login)
	r.Post("/refresh", h.Refresh)
	r.Group(func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		for _, m := range audit {
			r.Use(m)
		}
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
		mapAuthError(w, r, err)
		return
	}

	common.WriteSuccess(w, r, resp)
}

// Refresh rotates a refresh token and issues a new token pair.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}

	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}

	resp, err := h.svc.RefreshAccessToken(req.RefreshToken)
	if err != nil {
		mapAuthError(w, r, err)
		return
	}

	common.WriteSuccess(w, r, resp)
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

func toUserInfo(user *User) UserInfo {
	return UserInfo{
		ID:           user.ID,
		Username:     user.Username,
		DisplayName:  user.DisplayName,
		Role:         user.Role,
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

func mapAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUsernameTaken):
		common.WriteError(w, r, http.StatusConflict, "username_taken", err.Error(), nil)
	case errors.Is(err, ErrPasswordTooShort):
		common.WriteError(w, r, http.StatusBadRequest, "password_too_short", err.Error(), nil)
	case errors.Is(err, ErrAccountLocked):
		common.WriteError(w, r, http.StatusTooManyRequests, "account_locked", err.Error(), nil)
	case errors.Is(err, ErrInvalidCredentials):
		common.WriteError(w, r, http.StatusUnauthorized, "invalid_credentials", err.Error(), nil)
	default:
		slog.Error("auth request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
