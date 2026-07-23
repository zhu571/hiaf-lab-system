package instruments

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/notify"
)

// Handler holds the instruments service and implements HTTP handlers.
type Handler struct {
	svc     *Service
	db      *sql.DB
	workers map[string]*InstrumentWorker
	epoch   int64
	nlMu    sync.Mutex
	nlCalls map[string][]time.Time
}

// NewHandler creates an instruments Handler.
func NewHandler(svc *Service, db *sql.DB, workerMaps ...map[string]*InstrumentWorker) *Handler {
	workers := map[string]*InstrumentWorker{}
	for _, m := range workerMaps {
		for k, v := range m {
			workers[k] = v
		}
	}
	return &Handler{svc: svc, db: db, workers: workers, epoch: time.Now().Unix(), nlCalls: map[string][]time.Time{}}
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
	normalized, err := NormalizeParams(id, req.Command, req.Params)
	if err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		return
	}
	cmd := &QueueCommand{
		Name:       req.Command,
		Params:     normalized,
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

// InterpretCommand translates natural language into a validated candidate and never executes it.
func (h *Handler) InterpretCommand(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	if _, ok := h.workers[id]; !ok {
		common.WriteError(w, r, http.StatusNotFound, "instrument_not_found", "仪器不存在", nil)
		return
	}
	var req NLCommandRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || strings.TrimSpace(req.Input) == "" || len(req.Input) > 1000 || len(req.History) > 10 {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "自然语言请求格式无效", nil)
		return
	}
	for _, item := range req.History {
		if (item.Role != "user" && item.Role != "assistant") || len(item.Content) > 1000 {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "对话历史格式无效", nil)
			return
		}
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || !h.allowNL(claims.UserID) {
		common.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", "AI 翻译请求过于频繁", nil)
		return
	}
	middleware.SetAuditAction(r.Context(), "instrument.nl.translated")
	candidate, err := h.svc.Interpret(r.Context(), id, req)
	if err != nil {
		slog.Error("instrument interpretation failed", "error", err, "instrument_id", id, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusBadGateway, "agent_unavailable", "AI 翻译服务不可用", nil)
		return
	}
	common.WriteSuccess(w, r, candidate)
}

// NLExecute translates natural language, executes the command, and returns the result.
func (h *Handler) NLExecute(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	worker, ok := h.workers[id]
	if !ok {
		common.WriteError(w, r, http.StatusNotFound, "instrument_not_found", "仪器不存在", nil)
		return
	}

	var req NLCommandRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || strings.TrimSpace(req.Input) == "" || len(req.Input) > 1000 || len(req.History) > 10 {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "自然语言请求格式无效", nil)
		return
	}
	for _, item := range req.History {
		if (item.Role != "user" && item.Role != "assistant") || len(item.Content) > 1000 {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "对话历史格式无效", nil)
			return
		}
	}

	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}

	role := claims.Role
	if role != "maintainer" && role != "admin" {
		common.WriteError(w, r, http.StatusForbidden, "forbidden", "需要维护者或管理员权限", nil)
		return
	}

	if !h.allowNL(claims.UserID) {
		common.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", "AI 翻译请求过于频繁", nil)
		return
	}

	middleware.SetAuditAction(r.Context(), "instrument.nl.executed")

	candidate, err := h.svc.Interpret(r.Context(), id, req)
	if err != nil {
		slog.Error("instrument interpretation failed", "error", err, "instrument_id", id, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusBadGateway, "agent_unavailable", "AI 翻译服务不可用", nil)
		return
	}

	if candidate.Status != "ok" {
		common.WriteSuccess(w, r, candidate)
		return
	}

	cmd := &QueueCommand{
		Name:       candidate.Command,
		Params:     candidate.Params,
		Risk:       candidate.Risk,
		ResponseCh: make(chan CommandResult, 1),
	}

	if err := worker.Submit(cmd); err != nil {
		slog.Error("instrument command submit failed", "error", err, "instrument_id", id)
		common.WriteError(w, r, http.StatusServiceUnavailable, "instrument_unavailable", err.Error(), nil)
		return
	}

	var result CommandResult
	select {
	case result = <-cmd.ResponseCh:
	case <-time.After(30 * time.Second):
		common.WriteError(w, r, http.StatusGatewayTimeout, "timeout", "命令执行超时 (30s)", nil)
		return
	}

	var points []Point
	var plotType string
	var parsedValue *float64

	def, defErr := GetCommand(id, candidate.Command)
	if defErr == nil && def.Returns != nil {
		if returnsStr, ok := def.Returns.(string); ok && returnsStr == "array" && result.Response != "" {
			points, plotType = parseScanData(result.Response)
		}
	}
	if points == nil && result.Response != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(result.Response), 64); err == nil {
			parsedValue = &v
		}
	}

	requestID := common.GetRequestID(r.Context())
	errCode := ""
	if result.Error != nil {
		errCode = result.Error.Error()
	}
	storeErr := InsertResult(h.db, &InstrumentResult{
		ID:           uuid.New().String(),
		InstrumentID: id,
		CommandName:  candidate.Command,
		SCPI:         candidate.SCPI,
		RawResponse:  result.Response,
		ParsedValue:  parsedValue,
		ParsedPoints: points,
		PlotType:     plotType,
		ErrorCode:    errCode,
		DurationMS:   int(result.Duration.Milliseconds()),
		UserID:       claims.UserID,
		RequestID:    requestID,
	})
	if storeErr != nil {
		slog.Error("store instrument result failed", "error", storeErr)
	}

	common.WriteSuccess(w, r, map[string]any{
		"status":        "ok",
		"command":       candidate.Command,
		"scpi":          candidate.SCPI,
		"explanation":   candidate.Explanation,
		"response":      result.Response,
		"parsed_value":  parsedValue,
		"parsed_points": points,
		"plot_type":     plotType,
		"duration_ms":   int(result.Duration.Milliseconds()),
		"error":         errCode,
	})
}

func (h *Handler) allowNL(userID string) bool {
	now, cutoff := time.Now(), time.Now().Add(-time.Minute)
	h.nlMu.Lock()
	defer h.nlMu.Unlock()
	calls := h.nlCalls[userID][:0]
	for _, call := range h.nlCalls[userID] {
		if call.After(cutoff) {
			calls = append(calls, call)
		}
	}
	if len(calls) >= 10 {
		h.nlCalls[userID] = calls
		return false
	}
	h.nlCalls[userID] = append(calls, now)
	return true
}

// EmergencyStop handles POST /api/v1/instruments/{id}/emergency-stop.
func (h *Handler) EmergencyStop(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	worker, ok := h.workers[id]
	if !ok {
		common.WriteError(w, r, http.StatusNotFound, "instrument_not_found", "仪器不存在", nil)
		return
	}
	if err := worker.EmergencyStop(); err != nil {
		common.WriteError(w, r, http.StatusServiceUnavailable, "instrument_unavailable", err.Error(), nil)
		return
	}
	go notify.InstrumentEmergency(id, middleware.GetUserClaims(r.Context()).Username)
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
	deprecatePiezo(w)
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
	deprecatePiezo(w)
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
	deprecatePiezo(w)
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
	deprecatePiezo(w)
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

// GasCellStatus handles GET /api/v1/instruments/gascell/status.
func (h *Handler) GasCellStatus(w http.ResponseWriter, r *http.Request) {
	common.WriteSuccess(w, r, h.svc.GasCellStatus())
}

// GasCellStream handles the legacy-named SSE endpoint GET /api/v1/ws/gascell.
func (h *Handler) GasCellStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		common.WriteError(w, r, http.StatusInternalServerError, "stream_unsupported", "服务器不支持流式响应", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	last := h.svc.GasCellStatus().Data
	seq := uint64(1)
	if !h.writeSSE(w, seq, "snapshot", last) {
		return
	}
	flusher.Flush()

	// ponytail: per-client polling is enough for the small lab user count; add a shared hub if gateway load is measured.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	idle := 0
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			current := h.svc.GasCellStatus().Data
			changed := make(map[string]PVPoint)
			for name, point := range current {
				if !reflect.DeepEqual(point, last[name]) {
					changed[name] = point
				}
			}
			last = current
			if len(changed) == 0 {
				idle++
				if idle%15 == 0 {
					_, _ = w.Write([]byte(": keepalive\n\n"))
					flusher.Flush()
				}
				continue
			}
			idle = 0
			seq++
			if !h.writeSSE(w, seq, "update", changed) {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *Handler) writeSSE(w http.ResponseWriter, seq uint64, frameType string, data map[string]PVPoint) bool {
	frame := map[string]any{"type": frameType, "seq": seq, "epoch": h.epoch, "data": data}
	payload, err := json.Marshal(frame)
	if err != nil {
		return false
	}
	_, err = w.Write([]byte("id: " + fmt.Sprint(seq) + "\ndata: " + string(payload) + "\n\n"))
	return err == nil
}

func deprecatePiezo(w http.ResponseWriter) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Sunset", "Sat, 31 Oct 2026 00:00:00 GMT")
	w.Header().Set("Link", "</api/v1/instruments/gascell/status>; rel=\"successor-version\"")
}

func (h *Handler) GasCellParams(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var req GasCellParamsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	results, err := h.svc.GasCellParams(userRole(r), req)
	writeGasCellResult(w, r, results, err)
}

func (h *Handler) GasCellStart(w http.ResponseWriter, r *http.Request) {
	h.writeGasCellPV(w, r, "GasCell:Piezo:Running", 1)
}

func (h *Handler) GasCellStop(w http.ResponseWriter, r *http.Request) {
	h.writeGasCellPV(w, r, "GasCell:Piezo:Running", 0)
}

func (h *Handler) GasCellValve(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var req GasCellValueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	result, err := h.svc.GasCellValve(userRole(r), req.Value)
	writeGasCellResult(w, r, result, err)
}

func (h *Handler) GasCellA5Max(w http.ResponseWriter, r *http.Request) {
	var req GasCellValueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	h.writeGasCellPV(w, r, "GasCell:Safety:A5Max", req.Value)
}

func (h *Handler) GasCellA5Clear(w http.ResponseWriter, r *http.Request) {
	h.writeGasCellPV(w, r, "GasCell:Safety:A5Clear", 1)
}

func (h *Handler) writeGasCellPV(w http.ResponseWriter, r *http.Request, name string, value any) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	result, err := h.svc.WriteGasCellPV(userRole(r), name, value)
	writeGasCellResult(w, r, result, err)
}

func userRole(r *http.Request) string {
	if claims := middleware.GetUserClaims(r.Context()); claims != nil {
		return claims.Role
	}
	return ""
}

func writeGasCellResult(w http.ResponseWriter, r *http.Request, result any, err error) {
	if err == nil {
		common.WriteSuccess(w, r, result)
		return
	}
	if errors.Is(err, ErrGasCellPermission) {
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", "无权控制 GasCell", nil)
		return
	}
	if errors.Is(err, ErrGasCellGateway) {
		common.WriteError(w, r, http.StatusBadGateway, "gateway_error", "EPICS 网关写入失败", nil)
		return
	}
	common.WriteError(w, r, http.StatusBadRequest, "validation_failed", err.Error(), nil)
}

// parseScanData parses a CSV-like instrument response into (x,y) points.
// Returns points and plot_type ("line") or nil if not recognized as scan data.
func parseScanData(raw string) ([]Point, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, ""
	}
	parts := strings.Split(trimmed, ",")
	if len(parts) < 4 || len(parts)%2 != 0 {
		return nil, ""
	}
	points := make([]Point, 0, len(parts)/2)
	for i := 0; i+1 < len(parts); i += 2 {
		x, err1 := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
		y, err2 := strconv.ParseFloat(strings.TrimSpace(parts[i+1]), 64)
		if err1 != nil || err2 != nil {
			return nil, ""
		}
		points = append(points, Point{X: x, Y: y})
	}
	return points, "line"
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") == "" {
		common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
		return false
	}
	return true
}
