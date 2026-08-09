package automation

import (
	"encoding/json"
	"errors"
	"net/http"

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

// Routes 返回本模块子路由；角色/审计/幂等中间件由 main.go 统一挂载
// （与 /api/v1/admin/users、/api/v1/admin/system 同模式）。
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/rules", h.ListRules)
	r.Post("/rules", h.CreateRule)
	r.Patch("/rules/{id}", h.UpdateRule)
	r.Delete("/rules/{id}", h.DeleteRule)
	return r
}

// ListRules GET /rules —— 规则列表（含 enabled）。
func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List(r.Context())
	if err != nil {
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "查询自动化规则失败", nil)
		return
	}
	common.WriteSuccess(w, r, map[string]any{"items": items, "total": len(items)})
}

// CreateRule POST /rules —— 新建规则（service 层校验 action JSON schema）。
func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	var req CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体必须是合法 JSON", nil)
		return
	}
	rule, err := h.svc.Create(r.Context(), middleware.EffectiveUserID(r.Context()), req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	middleware.SetAuditDetail(r.Context(), map[string]any{
		"rule_id": rule.ID, "name": rule.Name, "trigger_event": rule.TriggerEvent,
	})
	common.WriteCreated(w, r, rule)
}

// UpdateRule PATCH /rules/{id} —— 一期仅允许切换 enabled。
func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	var req UpdateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体必须是合法 JSON", nil)
		return
	}
	rule, err := h.svc.SetEnabled(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	middleware.SetAuditDetail(r.Context(), map[string]any{"rule_id": rule.ID, "enabled": rule.Enabled})
	common.WriteSuccess(w, r, rule)
}

// DeleteRule DELETE /rules/{id} —— 硬删除，由 Audit 中间件留痕。
func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	middleware.SetAuditDetail(r.Context(), map[string]any{"rule_id": id, "deleted": true})
	common.WriteSuccess(w, r, map[string]string{"id": id})
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrRuleNotFound):
		common.WriteError(w, r, http.StatusNotFound, "rule_not_found", "自动化规则不存在", nil)
	case errors.Is(err, ErrInvalidName):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_rule", "规则名称不能为空且不超过 128 字符", nil)
	case errors.Is(err, ErrInvalidTrigger):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_rule", "trigger_event 不在一期白名单内（仅 daily_report.submitted）", nil)
	case errors.Is(err, ErrInvalidAction):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_rule", "action 必须是对象且 type 在一期白名单内（仅 enqueue_agent_task）", nil)
	case errors.Is(err, ErrNothingToUpdate):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_rule", "一期仅允许切换 enabled", nil)
	default:
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "自动化规则操作失败", nil)
	}
}
