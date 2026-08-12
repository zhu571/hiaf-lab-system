package weekly

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

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

// Routes 挂载 /api/v1/weekly（权限/审计/幂等在 main.go 路由层完成）。
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/summary", h.Summary)
	return r
}

// Summary 手动触发周报生成（maintainer+，写接口：Idempotency-Key + 审计）。
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req SummaryRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "invalid_input", "请求体读取失败", nil)
		return
	}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			common.WriteError(w, r, http.StatusBadRequest, "invalid_input", "请求体不是合法 JSON", nil)
			return
		}
	}
	notify := true
	if req.Notify != nil {
		notify = *req.Notify
	}
	result, err := h.svc.Generate(r.Context(), claims.UserID, req.WeekStart, notify)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidWeekStart), errors.Is(err, ErrNoReports):
			common.WriteError(w, r, http.StatusBadRequest, "invalid_input", err.Error(), nil)
		case errors.Is(err, ErrNotConfigured), errors.Is(err, ErrUpstream), errors.Is(err, ErrInvalidLLMOutput):
			common.WriteError(w, r, http.StatusBadGateway, "upstream_error", err.Error(), nil)
		default:
			common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "周报生成失败", nil)
		}
		return
	}
	common.WriteSuccess(w, r, result)
}
