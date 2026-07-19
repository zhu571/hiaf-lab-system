package instruments

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// Handler holds the instruments service and implements HTTP handlers.
type Handler struct {
	svc     *Service
	workers map[string]*InstrumentWorker
}

// NewHandler creates an instruments Handler.
func NewHandler(svc *Service, workerMaps ...map[string]*InstrumentWorker) *Handler {
	workers := map[string]*InstrumentWorker{}
	if len(workerMaps) > 0 {
		workers = workerMaps[0]
	}
	return &Handler{svc: svc, workers: workers}
}

// InstrumentStatus handles GET /api/v1/instruments/{id}/status.
func (h *Handler) InstrumentStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	worker, ok := h.workers[id]
	if !ok {
		common.WriteError(w, r, http.StatusNotFound, "instrument_not_found", "仪器不存在", nil)
		return
	}
	state := worker.State()
	common.WriteSuccess(w, r, map[string]any{
		"instrument_id": id,
		"state":         state,
		"rate_limited":  state == WorkerStateRateLimited,
	})
}

// ExecuteCommand handles POST /api/v1/instruments/{id}/commands.
func (h *Handler) ExecuteCommand(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	worker, ok := h.workers[id]
	if !ok {
		common.WriteError(w, r, http.StatusNotFound, "instrument_not_found", "仪器不存在", nil)
		return
	}
	var req struct {
		Command string         `json:"command"`
		Params  map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	def, err := GetCommand(id, req.Command)
	if err != nil || def.Risk == "red" || !IsCommandAllowed(id, req.Command, def.Risk) {
		common.WriteError(w, r, http.StatusBadRequest, "command_not_allowed", "命令不在允许的白名单中", nil)
		return
	}
	cmd := &QueueCommand{
		Name:       req.Command,
		Params:     req.Params,
		Risk:       def.Risk,
		ResponseCh: make(chan CommandResult, 1),
	}
	if err := worker.Submit(cmd); err != nil {
		common.WriteError(w, r, http.StatusServiceUnavailable, "instrument_unavailable", err.Error(), nil)
		return
	}
	result := <-cmd.ResponseCh
	if result.Error != nil {
		common.WriteError(w, r, http.StatusBadGateway, "command_failed", result.Error.Error(), nil)
		return
	}
	common.WriteSuccess(w, r, result)
}

// EmergencyStop handles POST /api/v1/instruments/{id}/emergency-stop.
func (h *Handler) EmergencyStop(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	worker, ok := h.workers[chi.URLParam(r, "id")]
	if !ok {
		common.WriteError(w, r, http.StatusNotFound, "instrument_not_found", "仪器不存在", nil)
		return
	}
	if err := worker.EmergencyStop(); err != nil {
		common.WriteError(w, r, http.StatusServiceUnavailable, "instrument_unavailable", err.Error(), nil)
		return
	}
	common.WriteSuccess(w, r, map[string]string{"status": "emergency_stop_queued"})
}

// ListInstruments handles GET /api/v1/instruments.
func (h *Handler) ListInstruments(w http.ResponseWriter, r *http.Request) {
	ids := make([]string, 0, len(h.workers))
	for id := range h.workers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	instruments := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		instruments = append(instruments, map[string]any{
			"id":    id,
			"name":  whitelist[id].Name,
			"state": h.workers[id].State(),
		})
	}
	common.WriteSuccess(w, r, instruments)
}

// GetWhitelist handles GET /api/v1/instruments/whitelist.
func (h *Handler) GetWhitelist(w http.ResponseWriter, r *http.Request) {
	ids := make([]string, 0, len(h.workers))
	for id := range h.workers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	commands := make([]CommandDef, 0)
	for _, id := range ids {
		commands = append(commands, ListCommands(id)...)
	}
	common.WriteSuccess(w, r, commands)
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
