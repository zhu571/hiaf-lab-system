package logs

import (
	"encoding/json"
	"errors"
	"io"
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

func (h *Handler) GetOrCreateTodayReport(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}

	report, err := h.svc.GetOrCreateTodayReport(middleware.EffectiveUserID(r.Context()))
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, report)
}

func (h *Handler) UpdateReportRawText(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req CreateDailyReportRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
			return
		}
	}
	report, err := h.svc.UpdateReportRawText(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), req.RawText)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, report)
}

func (h *Handler) GetReportByDate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	reportDate := r.URL.Query().Get("date")
	if reportDate == "" {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "缺少 date 参数", nil)
		return
	}
	report, err := h.svc.GetReportByDate(middleware.EffectiveUserID(r.Context()), reportDate)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, report)
}

func (h *Handler) GetReportByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	report, err := h.svc.GetReportByID(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, report)
}

func (h *Handler) ListReports(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	params := ReportListParams{
		AuthorID: middleware.EffectiveUserID(r.Context()),
		Status:   r.URL.Query().Get("status"),
		Keyword:  r.URL.Query().Get("keyword"),
		Date:     r.URL.Query().Get("date"),
		Page:     queryInt(r, "page", 1),
		PerPage:  queryInt(r, "per_page", 20),
	}
	reports, total, err := h.svc.ListReports(params)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, map[string]any{"items": reports, "total": total, "page": params.Page})
}

func (h *Handler) SubmitReport(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req SubmitReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	result, err := h.svc.SubmitReport(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req.Force)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, result)
}

func (h *Handler) CreateLog(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req CreateLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	item, err := h.svc.CreateLog(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteCreated(w, r, item)
}

func (h *Handler) ListLogs(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	params := LogListParams{
		Page:     queryInt(r, "page", 1),
		PerPage:  queryInt(r, "per_page", 20),
		Category: r.URL.Query().Get("category"),
		DateFrom: r.URL.Query().Get("date_from"),
		DateTo:   r.URL.Query().Get("date_to"),
		Status:   r.URL.Query().Get("status"),
	}
	result, err := h.svc.ListLogs(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, params)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, result)
}

func (h *Handler) GetLog(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	item, err := h.svc.GetLog(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, item)
}

func (h *Handler) UpdateLog(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req UpdateLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	item, err := h.svc.UpdateLog(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	common.WriteSuccess(w, r, item)
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
	case errors.Is(err, ErrReportNotFound):
		common.WriteError(w, r, http.StatusNotFound, "report_not_found", err.Error(), details)
	case errors.Is(err, ErrLogNotFound):
		common.WriteError(w, r, http.StatusNotFound, "log_not_found", err.Error(), details)
	case errors.Is(err, ErrProjectNotFound):
		common.WriteError(w, r, http.StatusNotFound, "project_not_found", err.Error(), details)
	case errors.Is(err, ErrNotReportOwner), errors.Is(err, ErrForbidden), errors.Is(err, ErrLogOwnerMismatch):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), details)
	case errors.Is(err, ErrAlreadySubmitted):
		common.WriteError(w, r, http.StatusBadRequest, "already_submitted", err.Error(), details)
	case errors.Is(err, ErrEmptyRawText):
		common.WriteError(w, r, http.StatusBadRequest, "empty_raw_text", err.Error(), details)
	case errors.Is(err, ErrNoLogEntries):
		common.WriteError(w, r, http.StatusBadRequest, "no_log_entries", err.Error(), details)
	case errors.Is(err, ErrLogProjectMissing):
		common.WriteError(w, r, http.StatusBadRequest, "log_project_missing", err.Error(), details)
	case errors.Is(err, ErrProjectLifecycleBlocked):
		common.WriteError(w, r, http.StatusBadRequest, "project_lifecycle_blocked", err.Error(), details)
	case errors.Is(err, ErrLogVoided):
		common.WriteError(w, r, http.StatusBadRequest, "log_voided", err.Error(), details)
	case errors.Is(err, ErrLogNotDraft):
		common.WriteError(w, r, http.StatusForbidden, "log_not_draft", err.Error(), details)
	case errors.Is(err, ErrPerPageTooLarge):
		common.WriteError(w, r, http.StatusBadRequest, "per_page_too_large", err.Error(), details)
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTimeZone):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), details)
	default:
		slog.Error("logs request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
