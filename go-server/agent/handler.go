package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/notify"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Claim(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var req ClaimTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	task, err := h.svc.Claim(req.LeaseSeconds)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, task)
}

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var req CompleteTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	task, err := h.svc.Complete(chi.URLParam(r, "id"), req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if len(req.Candidates) > 0 {
		go notify.Send("lab-system", "Agent 待审核", fmt.Sprintf("%d 条候选需人工确认", len(req.Candidates)), notify.WebURL+"/agent-candidates", "default", nil)
	}
	common.WriteSuccess(w, r, task)
}

func (h *Handler) Fail(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var req FailTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	task, err := h.svc.Fail(chi.URLParam(r, "id"), req.Error)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, task)
}

func (h *Handler) ListCandidates(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ListCandidates(
		r.URL.Query().Get("status"), queryInt(r, "page", 1), queryInt(r, "per_page", 20),
	)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, result)
}

func (h *Handler) ApproveCandidate(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	item, err := h.svc.ApproveCandidate(chi.URLParam(r, "id"), claims.UserID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, item)
}

func (h *Handler) RejectCandidate(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req ReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	item, err := h.svc.RejectCandidate(chi.URLParam(r, "id"), claims.UserID, req.Reason)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, item)
}

func queryInt(r *http.Request, key string, fallback int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return v
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") != "" {
		return true
	}
	common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
	return false
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		common.WriteError(w, r, http.StatusNotFound, "agent_task_not_found", err.Error(), nil)
	case errors.Is(err, ErrInvalidLease):
		common.WriteError(w, r, http.StatusConflict, "invalid_agent_lease", err.Error(), nil)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
	case errors.Is(err, ErrCandidateNotFound):
		common.WriteError(w, r, http.StatusNotFound, "candidate_not_found", err.Error(), nil)
	case errors.Is(err, ErrCandidateNotPending):
		common.WriteError(w, r, http.StatusConflict, "candidate_not_pending", err.Error(), nil)
	default:
		slog.Error("agent request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
