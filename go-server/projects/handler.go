package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/notify"
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

	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	project, err := h.svc.Create(req, middleware.EffectiveUserID(r.Context()))
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteCreated(w, r, project)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	projects, err := h.svc.List(middleware.EffectiveUserID(r.Context()), claims.Role, r.URL.Query().Get("status"))
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, projects)
}

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	project, err := h.svc.GetByID(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, project)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var req UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	project, err := h.svc.Update(chi.URLParam(r, "id"), req)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, project)
}

func (h *Handler) TransitionStatus(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}

	var req StatusTransitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	id := chi.URLParam(r, "id")
	oldProject, err := h.svc.GetByID(id)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	project, warnings, err := h.svc.TransitionStatus(id, req, middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		details := map[string]any{}
		if len(warnings) > 0 {
			details["warnings"] = warnings
		}
		if project != nil {
			details["project"] = project
		}
		h.writeError(w, r, err, details)
		return
	}
	go notify.Send("lab-alerts", fmt.Sprintf("项目: %s→%s", oldProject.Status, project.Status), project.Name+" ("+claims.Username+")", notify.WebURL+"/projects/"+project.ID, "default", nil)
	common.WriteSuccess(w, r, map[string]any{"project": project, "warnings": warnings})
}

func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req AddMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	member, err := h.svc.AddMember(chi.URLParam(r, "id"), req, middleware.EffectiveUserID(r.Context()))
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteCreated(w, r, member)
}

func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.svc.ListMembers(chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, members)
}

func (h *Handler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var req UpdateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	member, err := h.svc.UpdateMemberRole(chi.URLParam(r, "id"), chi.URLParam(r, "userID"), req)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, member)
}

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	if err := h.svc.RemoveMember(chi.URLParam(r, "id"), chi.URLParam(r, "userID")); err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, map[string]bool{"success": true})
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
	case errors.Is(err, ErrProjectNotFound):
		common.WriteError(w, r, http.StatusNotFound, "project_not_found", err.Error(), details)
	case errors.Is(err, ErrCodeTaken):
		common.WriteError(w, r, http.StatusConflict, "project_code_taken", err.Error(), details)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), details)
	case errors.Is(err, ErrInvalidTransition):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_transition", err.Error(), details)
	case errors.Is(err, ErrUserNotFound):
		common.WriteError(w, r, http.StatusNotFound, "user_not_found", err.Error(), details)
	case errors.Is(err, ErrTransitionWarning):
		common.WriteError(w, r, http.StatusConflict, "transition_warning", err.Error(), details)
	case errors.Is(err, ErrForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), details)
	case errors.Is(err, ErrLastOwner):
		common.WriteError(w, r, http.StatusBadRequest, "last_owner", err.Error(), details)
	default:
		slog.Error("project request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
