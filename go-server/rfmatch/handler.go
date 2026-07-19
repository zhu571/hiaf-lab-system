package rfmatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "rf_matching.create")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req CreateRFMatchingRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	record, err := h.svc.Create(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteCreated(w, r, record)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	result, err := h.svc.List(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, ListParams{
		Device: r.URL.Query().Get("device"), Status: r.URL.Query().Get("status"),
		Page: queryInt(r, "page", 1), PerPage: queryInt(r, "per_page", 20),
	})
	if err != nil {
		h.writeError(w, r, err)
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
	record, err := h.svc.GetByID(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, record)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "rf_matching.update")
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
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体包含不可修改字段或无法解析", nil)
		return
	}
	record, err := h.svc.Update(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, record)
}

func (h *Handler) MarkVoid(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "rf_matching.delete")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
			return
		}
	}
	id := chi.URLParam(r, "id")
	if err := h.svc.MarkVoid(id, middleware.EffectiveUserID(r.Context()), claims.Role, req.Reason); err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]string{"id": id})
}

func decodeUpdateRequest(r *http.Request) (UpdateRFMatchingRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return UpdateRFMatchingRequest{}, err
	}
	allowed := map[string]bool{"s11": true, "input_freq": true, "input_voltage": true, "input_power": true,
		"input_desc": true, "output_freq": true, "output_voltage": true, "output_power": true, "output_desc": true,
		"transformer_turns": true, "capacitance_text": true, "transformer_material": true,
		"shunt_inductance": true, "series_capacitor": true, "status": true, "notes": true}
	for field := range raw {
		if !allowed[field] {
			return UpdateRFMatchingRequest{}, errors.New("immutable or unknown field")
		}
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return UpdateRFMatchingRequest{}, err
	}
	var req UpdateRFMatchingRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	return req, err
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrRecordNotFound):
		common.WriteError(w, r, http.StatusNotFound, "rf_matching_not_found", err.Error(), nil)
	case errors.Is(err, ErrProjectNotFound):
		common.WriteError(w, r, http.StatusNotFound, "project_not_found", err.Error(), nil)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
	case errors.Is(err, ErrForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), nil)
	default:
		slog.Error("RF matching request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") != "" {
		return true
	}
	common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
	return false
}
