package sensors

import (
	"log/slog"
	"net/http"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// Handler holds the sensors service and implements HTTP handlers.
type Handler struct {
	svc *Service
}

// NewHandler creates a sensors Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Latest handles GET /api/v1/sensors/latest?tags=...
func (h *Handler) Latest(w http.ResponseWriter, r *http.Request) {
	tags := r.URL.Query().Get("tags")
	result, err := h.svc.Latest(tags)
	if err != nil {
		slog.Error("sensors latest failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusServiceUnavailable, "sensor_error", "传感器数据查询失败", nil)
		return
	}
	common.WriteSuccess(w, r, result)
}

// History handles GET /api/v1/sensors/history?tag=&from=&to=&interval=
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("tag")
	if tag == "" {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "tag 参数必填", nil)
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	interval := r.URL.Query().Get("interval")

	result, err := h.svc.History(tag, from, to, interval)
	if err != nil {
		slog.Error("sensors history failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusServiceUnavailable, "sensor_error", "传感器历史数据查询失败", nil)
		return
	}
	common.WriteSuccess(w, r, result)
}
