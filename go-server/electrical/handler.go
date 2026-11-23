package electrical

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
	mw "github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Latest(w http.ResponseWriter, r *http.Request) {
	common.WriteSuccess(w, r, h.svc.Latest())
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := 0
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 5000 {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "limit 必须是 1-5000 的整数", nil)
			return
		}
		limit = parsed
	}
	if _, err := fluxTime(query.Get("from"), "-15m"); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "from 必须是 Flux 时长或 RFC3339 时间", nil)
		return
	}
	if _, err := fluxTime(query.Get("to"), "now()"); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "to 必须是 Flux 时长或 RFC3339 时间", nil)
		return
	}
	for _, value := range []string{query.Get("source"), query.Get("channel"), query.Get("metric")} {
		if value != "" && safeHistoryTag(value) != nil {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "source/channel/metric 含非法字符", nil)
			return
		}
	}
	points, err := h.svc.History(r.Context(), HistoryParams{
		Source: query.Get("source"), Channel: query.Get("channel"), Metric: query.Get("metric"),
		From: query.Get("from"), To: query.Get("to"), Limit: limit,
	})
	if err != nil {
		common.WriteError(w, r, http.StatusServiceUnavailable, "electrical_storage_error", "电器监控历史数据查询失败", nil)
		return
	}
	common.WriteSuccess(w, r, HistoryResult{Points: points})
}

func (h *Handler) SingleShot(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.SingleShot(r.Context(), mw.EffectiveUserID(r.Context()))
	if err != nil {
		var rateErr *RateLimitError
		switch {
		case errors.Is(err, ErrSingleShotRunning):
			common.WriteError(w, r, http.StatusConflict, "single_shot_in_progress", "已有一次电器采集正在执行", nil)
		case errors.As(err, &rateErr):
			common.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", "单次采集请求过于频繁", map[string]any{
				"retry_after_seconds": int(math.Ceil(rateErr.RetryAfter.Seconds())),
			})
		case errors.Is(err, ErrNoSamples):
			common.WriteError(w, r, http.StatusBadGateway, "acquisition_failed", "本次采集未获得有效数据，请检查采集健康", nil)
		case errors.Is(err, context.DeadlineExceeded):
			common.WriteError(w, r, http.StatusGatewayTimeout, "acquisition_timeout", "本次采集超时", nil)
		default:
			common.WriteError(w, r, http.StatusBadGateway, "acquisition_failed", "电器单次采集失败", nil)
		}
		mw.SetAuditDetail(r.Context(), map[string]any{"success": false, "sample_count": len(result.Samples)})
		return
	}
	mw.SetAuditDetail(r.Context(), map[string]any{
		"success": true, "sample_count": len(result.Samples), "source_errors": result.Errors,
	})
	common.WriteSuccess(w, r, result)
}
