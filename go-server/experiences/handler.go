package experiences

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
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

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req CreateExperienceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	exp, err := h.svc.Create(middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteCreated(w, r, exp)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	params := ExperienceListParams{
		ProjectID: r.URL.Query().Get("project_id"),
		Status:    r.URL.Query().Get("status"),
		Tags:      parseTagsQuery(r.URL.Query().Get("tags")),
		Keyword:   r.URL.Query().Get("keyword"),
		Page:      queryInt(r, "page", 1),
		PerPage:   queryInt(r, "per_page", 20),
	}
	result, err := h.svc.List(middleware.EffectiveUserID(r.Context()), claims.Role, params)
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
	exp, err := h.svc.GetByID(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, exp)
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
	req, err := decodeUpdateRequest(r)
	if err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	exp, err := h.svc.Update(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, exp)
}

func decodeUpdateRequest(r *http.Request) (UpdateExperienceRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return UpdateExperienceRequest{}, err
	}
	if _, ok := raw["ai_generated"]; ok {
		return UpdateExperienceRequest{}, errors.New("ai_generated is immutable")
	}
	if _, ok := raw["agent_task_id"]; ok {
		return UpdateExperienceRequest{}, errors.New("agent_task_id is immutable")
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return UpdateExperienceRequest{}, err
	}
	var req UpdateExperienceRequest
	err = json.NewDecoder(bytes.NewReader(body)).Decode(&req)
	return req, err
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	exp, err := h.svc.Publish(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, exp)
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	exp, err := h.svc.Archive(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, exp)
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

func parseTagsQuery(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, ",")
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
	case errors.Is(err, ErrExperienceNotFound):
		common.WriteError(w, r, http.StatusNotFound, "experience_not_found", err.Error(), details)
	case errors.Is(err, ErrProjectNotFound):
		common.WriteError(w, r, http.StatusNotFound, "project_not_found", err.Error(), details)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), details)
	case errors.Is(err, ErrNotCandidate):
		common.WriteError(w, r, http.StatusBadRequest, "not_candidate", err.Error(), details)
	case errors.Is(err, ErrNotPublished):
		common.WriteError(w, r, http.StatusBadRequest, "not_published", err.Error(), details)
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrPublishForbidden), errors.Is(err, ErrGlobalExperienceAdminOnly):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), details)
	default:
		slog.Error("experiences request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
