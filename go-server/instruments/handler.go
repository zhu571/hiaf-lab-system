package instruments

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// Handler holds the instruments service and implements HTTP handlers.
type Handler struct {
	svc *Service
}

// NewHandler creates an instruments Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// PiezoStatus handles GET /api/v1/instruments/piezo/status.
func (h *Handler) PiezoStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.PiezoStatus()
	if err != nil {
		slog.Error("piezo status failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusServiceUnavailable, "gateway_error", "EPICS 网关不可用", nil)
		return
	}
	common.WriteSuccess(w, r, status)
}

// PiezoStart handles POST /api/v1/instruments/piezo/start.
func (h *Handler) PiezoStart(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	if err := h.svc.PiezoStart(); err != nil {
		slog.Error("piezo start failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusServiceUnavailable, "gateway_error", "EPICS 网关不可用", nil)
		return
	}
	common.WriteSuccess(w, r, map[string]string{"status": "started"})
}

// PiezoStop handles POST /api/v1/instruments/piezo/stop.
func (h *Handler) PiezoStop(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	if err := h.svc.PiezoStop(); err != nil {
		slog.Error("piezo stop failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusServiceUnavailable, "gateway_error", "EPICS 网关不可用", nil)
		return
	}
	common.WriteSuccess(w, r, map[string]string{"status": "stopped"})
}

// PiezoSetpoint handles POST /api/v1/instruments/piezo/setpoint.
func (h *Handler) PiezoSetpoint(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var req SetpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	if err := h.svc.PiezoSetpoint(req.Value); err != nil {
		slog.Error("piezo setpoint failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusServiceUnavailable, "gateway_error", "EPICS 网关不可用", nil)
		return
	}
	common.WriteSuccess(w, r, map[string]float64{"setpoint": req.Value})
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") == "" {
		common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
		return false
	}
	return true
}
