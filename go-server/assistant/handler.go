package assistant

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.List(r.Context(), userID, r.URL.Query().Get("status"), r.URL.Query().Get("q"), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]any{"items": items})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "请求体格式错误", nil)
		return
	}
	sess, err := h.svc.Create(r.Context(), userID, req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, sess)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	sess, err := h.svc.Get(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, sess)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "请求体格式错误", nil)
		return
	}
	sess, err := h.svc.Update(r.Context(), userID, chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, sess)
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	if err := h.svc.Archive(r.Context(), userID, chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]bool{"archived": true})
}

func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	items, err := h.svc.Messages(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]any{"items": items})
}

func (h *Handler) AddMessage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	var req CreateMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "请求体格式错误", nil)
		return
	}
	m, err := h.svc.AddMessage(r.Context(), userID, chi.URLParam(r, "id"), req.Content)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, m)
}

func (h *Handler) StartRun(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	var req CreateRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "请求体格式错误", nil)
		return
	}
	result, err := h.svc.StartRun(r.Context(), userID, chi.URLParam(r, "id"), req.Content)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, result)
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		common.WriteError(w, r, http.StatusNotFound, "not_found", "会话不存在", nil)
	case errors.Is(err, ErrForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", "无权操作该会话", nil)
	case errors.Is(err, ErrProjectUnsupported):
		common.WriteError(w, r, http.StatusBadRequest, "not_supported", "项目共享会话暂未开放", nil)
	case errors.Is(err, ErrInvalid):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "参数不合法", nil)
	case errors.Is(err, ErrUpstream):
		common.WriteError(w, r, http.StatusBadGateway, "upstream_error", "AI 服务暂时不可用", nil)
	default:
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
