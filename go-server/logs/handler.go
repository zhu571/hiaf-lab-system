package logs

import (
	"encoding/json"
	"errors"
	"io"
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
	// 显式审计动作：by-date 是 GET 默认不审计，但 service token 拉全量用户日报属敏感
	// 读取，需要 actor_type='system' 审计行（方案 §10 验收）；普通 JWT 调用同样落审计。
	middleware.SetAuditAction(r.Context(), "daily_report.by_date")
	claims := middleware.GetUserClaims(r.Context())
	latest := r.URL.Query().Get("latest") == "true"

	// service token 调用（scheduler 批量拉取）：必须显式传 user_id，落到 daily_reports.author_id。
	if middleware.IsServiceCall(r.Context()) {
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		if userID == "" {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "缺少 user_id 参数", nil)
			return
		}
		report, err := h.svc.GetReportByDateLatest(userID, r.URL.Query().Get("date"), latest)
		if err != nil {
			h.writeError(w, r, err, nil)
			return
		}
		common.WriteSuccess(w, r, report)
		return
	}

	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	// 普通 JWT：user_id 参数被忽略，强制取自己（防越权）。
	report, err := h.svc.GetReportByDateLatest(middleware.EffectiveUserID(r.Context()), r.URL.Query().Get("date"), latest)
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

// AiParseReport 把日报 raw_text 交给 py-agent 整理为结构化日志草稿（不落库）。
// 审计明细由组内 Audit middleware 记录（action + path 含 report_id），不记 raw_text 全文。
func (h *Handler) AiParseReport(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "daily_report.ai_parsed")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	result, err := h.svc.AiParse(r.Context(), chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err, nil)
		return
	}
	middleware.SetAuditDetail(r.Context(), map[string]any{
		"report_id": chi.URLParam(r, "id"),
		"status":    result.Status,
		"log_count": len(result.Logs),
	})
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
	case errors.Is(err, ErrRateLimited):
		common.WriteError(w, r, http.StatusTooManyRequests, "too_many_requests", err.Error(), details)
	case errors.Is(err, ErrAiParseFailed):
		common.WriteError(w, r, http.StatusBadRequest, "ai_parse_failed", err.Error(), details)
	case errors.Is(err, ErrUpstream):
		slog.Error("logs upstream error", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusBadGateway, "upstream_error", "AI 整理服务暂时不可用，请稍后再试", nil)
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidTimeZone):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), details)
	default:
		slog.Error("logs request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
