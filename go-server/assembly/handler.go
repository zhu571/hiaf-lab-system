package assembly

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
	middleware.SetAuditAction(r.Context(), "assembly.create")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req CreateStepRequest
	if err := decode(r, &req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	step, err := h.svc.Create(projectID(r), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteCreated(w, r, step)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	result, err := h.svc.List(projectID(r), middleware.EffectiveUserID(r.Context()), claims.Role, ListParams{
		Status: r.URL.Query().Get("status"), Page: queryInt(r, "page", 1), PerPage: queryInt(r, "per_page", 20),
	})
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
	step, err := h.svc.GetByID(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, step)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "assembly.update")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req UpdateStepRequest
	if err := decode(r, &req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	if req.Transition != nil {
		middleware.SetAuditAction(r.Context(), "assembly.transition")
		if req.OverrideReason != nil && *req.OverrideReason != "" {
			middleware.SetAuditAction(r.Context(), "assembly.transition.override")
		}
	}
	step, err := h.svc.Update(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, step)
}

func (h *Handler) Reorder(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "assembly.reorder")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req ReorderRequest
	if err := decode(r, &req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	if err := h.svc.Reorder(req.ProjectID, middleware.EffectiveUserID(r.Context()), claims.Role, req.Steps); err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, req)
}

func (h *Handler) SoftDelete(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "assembly.delete")
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

func decode(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrStepNotFound):
		common.WriteError(w, r, http.StatusNotFound, "assembly_step_not_found", err.Error(), nil)
	case errors.Is(err, ErrProjectNotFound):
		common.WriteError(w, r, http.StatusNotFound, "project_not_found", err.Error(), nil)
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrDependencyCycle), errors.Is(err, ErrDependencyPending):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
	case errors.Is(err, ErrInvalidTransition):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_transition", err.Error(), nil)
	case errors.Is(err, ErrForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), nil)
	case errors.Is(err, ErrStepConflict):
		common.WriteError(w, r, http.StatusConflict, "status_conflict", err.Error(), nil)
	default:
		slog.Error("assembly request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
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
