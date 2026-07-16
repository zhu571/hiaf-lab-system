package issues

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req CreateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	issue, err := h.svc.Create(chi.URLParam(r, "id"), claims.UserID, claims.Role, req)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteCreated(w, r, issue)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	params := IssueListParams{
		Status:   r.URL.Query().Get("status"),
		Severity: r.URL.Query().Get("severity"),
		Assignee: r.URL.Query().Get("assignee"),
		Author:   r.URL.Query().Get("author"),
		Search:   r.URL.Query().Get("search"),
		Page:     queryInt(r, "page", 1),
		PerPage:  queryInt(r, "per_page", 20),
		Sort:     r.URL.Query().Get("sort"),
		Order:    r.URL.Query().Get("order"),
	}
	result, err := h.svc.List(chi.URLParam(r, "id"), claims.UserID, claims.Role, params)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, result)
}

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	issue, err := h.svc.GetByID(chi.URLParam(r, "id"), claims.UserID, claims.Role)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, issue)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req UpdateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	issue, err := h.svc.Update(chi.URLParam(r, "id"), claims.UserID, claims.Role, req)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, issue)
}

func (h *Handler) Transition(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req TransitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	issue, err := h.svc.Transition(chi.URLParam(r, "id"), claims.UserID, claims.Role, req)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, issue)
}

func (h *Handler) AddComment(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req AddCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	comment, err := h.svc.AddComment(chi.URLParam(r, "id"), claims.UserID, claims.Role, req)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteCreated(w, r, comment)
}

func queryInt(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") == "" {
		common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
		return false
	}
	return true
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error, details map[string]any) {
	switch {
	case errors.Is(err, ErrIssueNotFound):
		common.WriteError(w, r, http.StatusNotFound, "issue_not_found", err.Error(), details)
	case errors.Is(err, ErrProjectNotFound):
		common.WriteError(w, r, http.StatusNotFound, "project_not_found", err.Error(), details)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), details)
	case errors.Is(err, ErrProjectLifecycleBlocked):
		common.WriteError(w, r, http.StatusBadRequest, "project_lifecycle_blocked", err.Error(), details)
	case errors.Is(err, ErrIssueClosed):
		common.WriteError(w, r, http.StatusBadRequest, "issue_closed", err.Error(), details)
	case errors.Is(err, ErrCommentsDisabled):
		common.WriteError(w, r, http.StatusForbidden, "comments_disabled", err.Error(), details)
	case errors.Is(err, ErrInvalidTransition):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_transition", err.Error(), details)
	case errors.Is(err, ErrReasonRequired):
		common.WriteError(w, r, http.StatusBadRequest, "reason_required", err.Error(), details)
	case errors.Is(err, ErrRelatedLogNotFound):
		common.WriteError(w, r, http.StatusNotFound, "related_log_not_found", err.Error(), details)
	case errors.Is(err, ErrRelatedLogProjectMismatch):
		common.WriteError(w, r, http.StatusBadRequest, "related_log_project_mismatch", err.Error(), details)
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrTransitionForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), details)
	default:
		slog.Error("issues request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
