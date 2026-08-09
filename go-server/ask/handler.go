package ask

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// Chat POST /api/v1/ask/chat —— 用户提问入口（JWT+CSRF+Idempotency-Key+审计）。
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "ask.chat")
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req ChatRequest
	if err := decode(r, &req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	resp, err := h.svc.Chat(r.Context(), middleware.EffectiveUserID(r.Context()), req.Question)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, resp)
}

// Execute POST /api/v1/ask/execute —— 只读执行端点（SERVICE_TOKEN 鉴权，审计
// 中间件按 service call 落 actor_type='system'、action=ask.execute）。
func (h *Handler) Execute(w http.ResponseWriter, r *http.Request) {
	if !middleware.IsServiceCall(r.Context()) {
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", "仅内部服务可调用该端点", nil)
		return
	}
	var req ExecuteRequest
	if err := decode(r, &req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	resp, err := h.svc.Execute(r.Context(), req.SQL)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, resp)
}

// History GET /api/v1/ask/history —— 我的问答历史列表（不含 rows 大字段）。
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	items, total, err := h.svc.List(middleware.EffectiveUserID(r.Context()), page, perPage)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]any{
		"items": items, "total": total, "page": page, "per_page": perPage,
	})
}

// HistoryByID GET /api/v1/ask/history/{id} —— 明细（含 rows 快照，仅本人可读）。
func (h *Handler) HistoryByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if !uuidRe.MatchString(id) {
		h.writeError(w, r, ErrNotFound) // 非法 UUID 直接 404，不送 PG（防 500）
		return
	}
	item, err := h.svc.GetByUser(id, middleware.EffectiveUserID(r.Context()))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, item)
}

func decode(r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		common.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
	case errors.Is(err, ErrSQLRejected):
		common.WriteError(w, r, http.StatusBadRequest, "sql_rejected", err.Error(), nil)
	case errors.Is(err, ErrSQLExec):
		common.WriteError(w, r, http.StatusUnprocessableEntity, "sql_execution_failed", err.Error(), nil)
	case errors.Is(err, ErrRateLimited):
		common.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", err.Error(), nil)
	case errors.Is(err, ErrUpstream):
		slog.Error("ask upstream error", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusBadGateway, "upstream_error", "AI 查询服务暂时不可用，请稍后再试", nil)
	default:
		slog.Error("ask request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
