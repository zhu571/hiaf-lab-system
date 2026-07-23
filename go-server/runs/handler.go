package runs

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

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "experiment_run.create")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req CreateRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	run, err := h.svc.Create(projectID(r), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteCreated(w, r, run)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	params := RunListParams{
		Campaign: r.URL.Query().Get("campaign"), Status: r.URL.Query().Get("status"),
		RunType: r.URL.Query().Get("run_type"), Page: queryInt(r, "page", 1), PerPage: queryInt(r, "per_page", 20),
	}
	result, err := h.svc.List(projectID(r), middleware.EffectiveUserID(r.Context()), claims.Role, params)
	if err != nil {
		h.writeError(w, r, err)
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
	run, err := h.svc.GetByID(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, run)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "experiment_run.update")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req UpdateRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	run, err := h.svc.Update(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, run)
}

func (h *Handler) SoftDelete(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "experiment_run.delete")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.svc.SoftDelete(id, middleware.EffectiveUserID(r.Context()), claims.Role); err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]string{"id": id})
}

func (h *Handler) AddReportLink(w http.ResponseWriter, r *http.Request) {
	h.changeReportLink(w, r, true)
}

func (h *Handler) RemoveReportLink(w http.ResponseWriter, r *http.Request) {
	h.changeReportLink(w, r, false)
}

func (h *Handler) changeReportLink(w http.ResponseWriter, r *http.Request, add bool) {
	action := "experiment_run.link.delete"
	if add {
		action = "experiment_run.link.create"
	}
	middleware.SetAuditAction(r.Context(), action)
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	runID, reportID := chi.URLParam(r, "id"), chi.URLParam(r, "report_id")
	var (
		links []string
		err   error
	)
	if add {
		links, err = h.svc.AddReportLink(runID, reportID, middleware.EffectiveUserID(r.Context()), claims.Role)
	} else {
		links, err = h.svc.RemoveReportLink(runID, reportID, middleware.EffectiveUserID(r.Context()), claims.Role)
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]any{"run_id": runID, "report_ids": links})
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrRunNotFound):
		common.WriteError(w, r, http.StatusNotFound, "experiment_run_not_found", err.Error(), nil)
	case errors.Is(err, ErrProjectNotFound):
		common.WriteError(w, r, http.StatusNotFound, "project_not_found", err.Error(), nil)
	case errors.Is(err, ErrReportLinkNotFound):
		common.WriteError(w, r, http.StatusNotFound, "report_link_not_found", err.Error(), nil)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
	case errors.Is(err, ErrInvalidTransition):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_transition", err.Error(), nil)
	case errors.Is(err, ErrForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), nil)
	case errors.Is(err, ErrRunConflict):
		common.WriteError(w, r, http.StatusConflict, "status_conflict", err.Error(), nil)
	default:
		slog.Error("experiment runs request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}

func projectID(r *http.Request) string {
	if id := chi.URLParam(r, "project_id"); id != "" {
		return id
	}
	return chi.URLParam(r, "id")
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") != "" {
		return true
	}
	common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
	return false
}
