package instruments

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

func (h *Handler) CreateLease(w http.ResponseWriter, r *http.Request) {
	if h.safety == nil {
		common.WriteError(w, r, 500, "internal_error", "租约服务不可用", nil)
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	var req struct {
		Purpose         string `json:"purpose"`
		DurationSeconds int    `json:"duration_seconds"`
		Force           bool   `json:"force"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		common.WriteError(w, r, 400, "bad_request", "请求体解析失败", nil)
		return
	}
	lease, err := h.safety.CreateLease(r.Context(), chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), req.Purpose, time.Duration(req.DurationSeconds)*time.Second, req.Force, claims.Role)
	if err != nil {
		writeInstrumentSafetyError(w, r, err)
		return
	}
	middleware.SetAuditAction(r.Context(), "instrument.lease.created")
	common.WriteCreated(w, r, lease)
}
func (h *Handler) RenewLease(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	_ = claims
	var req struct {
		Reason          string `json:"reason"`
		DurationSeconds int    `json:"duration_seconds"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		common.WriteError(w, r, 400, "bad_request", "请求体解析失败", nil)
		return
	}
	lease, err := h.safety.RenewLease(r.Context(), chi.URLParam(r, "lease_id"), middleware.EffectiveUserID(r.Context()), req.Reason, time.Duration(req.DurationSeconds)*time.Second)
	if err != nil {
		writeInstrumentSafetyError(w, r, err)
		return
	}
	middleware.SetAuditAction(r.Context(), "instrument.lease.renewed")
	common.WriteSuccess(w, r, lease)
}
func (h *Handler) ReleaseLease(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.safety.ReleaseLease(r.Context(), chi.URLParam(r, "lease_id"), middleware.EffectiveUserID(r.Context()), claims.Role); err != nil {
		writeInstrumentSafetyError(w, r, err)
		return
	}
	middleware.SetAuditAction(r.Context(), "instrument.lease.released")
	common.WriteSuccess(w, r, map[string]string{"status": "released"})
}

func (h *Handler) RequestApproval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LeaseID string         `json:"lease_id"`
		Command string         `json:"command"`
		Params  map[string]any `json:"params"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		common.WriteError(w, r, 400, "bad_request", "请求体解析失败", nil)
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	a, err := h.safety.RequestCommandApproval(r.Context(), chi.URLParam(r, "id"), req.LeaseID, claims.UserID, middleware.EffectiveUserID(r.Context()), req.Command, req.Params)
	if err != nil {
		writeInstrumentSafetyError(w, r, err)
		return
	}
	middleware.SetAuditAction(r.Context(), "instrument.approval.requested")
	common.WriteCreated(w, r, a)
}
func (h *Handler) ApproveCommand(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	a, err := h.safety.Approve(r.Context(), chi.URLParam(r, "approval_id"), claims.UserID, claims.Role)
	if err != nil {
		writeInstrumentSafetyError(w, r, err)
		return
	}
	middleware.SetAuditAction(r.Context(), "instrument.approval.approved")
	common.WriteSuccess(w, r, a)
}

func writeAccepted(w http.ResponseWriter, r *http.Request, data any) {
	_ = common.WriteJSON(w, http.StatusAccepted, common.SuccessResponse{Data: data, RequestID: common.GetRequestID(r.Context())})
}

// requireFlowEnabled 是 M6 灰度止血开关的 handler 层兜底：路由注册在
// main.go 已按 INSTRUMENT_FLOW_ENABLED 条件化，这里再挡一层防回归。
func (h *Handler) requireFlowEnabled(w http.ResponseWriter, r *http.Request) bool {
	if !FlowEnabled() {
		common.WriteError(w, r, http.StatusNotFound, "flow_disabled", "仪器流程功能未启用（INSTRUMENT_FLOW_ENABLED 默认关闭）", nil)
		return false
	}
	return true
}

// flowEnvelope 组装审批人在批准前可核对的完整不可变包络（M2）：仪器/flow
// kind/命令集合与参数区间/频率网格/点数与命令数上限/deadline/白名单版本/
// 审批状态与有效期/包络 hash。
func (h *Handler) flowEnvelope(ctx context.Context, f *FlowSession) *FlowEnvelope {
	env := &FlowEnvelope{
		InstrumentID: f.InstrumentID, FlowKind: f.FlowKind, Objective: f.Objective, ObjectType: f.ObjectType,
		ActorID: f.ActorID, ActingUserID: f.ActingUserID, LeaseID: f.LeaseID,
		WhitelistVersion: f.WhitelistVersion, AllowedCommands: []FlowCommandSummary{},
		FrequencyMinHz: f.Limits.FrequencyMinHz, FrequencyMaxHz: f.Limits.FrequencyMaxHz,
		FrequencyGrid: f.FrequencyGrid, MaxPoints: f.Limits.MaxPoints, RetryBudget: f.Limits.RetryBudget,
		MaxCommands: f.Limits.MaxCommands, DeadlineAt: f.DeadlineAt, RestoreFrequency: f.Limits.RestoreFrequency,
	}
	if inst, ok := whitelist[f.InstrumentID]; ok {
		env.InstrumentName = inst.Name
	}
	for _, name := range f.Limits.AllowedCommands {
		if def, err := GetCommand(f.InstrumentID, name); err == nil && def.Risk != "red" {
			env.AllowedCommands = append(env.AllowedCommands, FlowCommandSummary{Name: def.Name, Description: def.Description, Risk: def.Risk, TimeoutMS: def.TimeoutMS, Params: def.Params})
		}
	}
	if f.ApprovalID != nil && h.repo != nil {
		if a, err := h.repo.GetApproval(ctx, *f.ApprovalID); err == nil {
			env.ApprovalID = a.ID
			env.ApprovalStatus = a.Status
			env.ApprovedBy = a.ApprovedBy
			env.ApprovalExpiresAt = &a.ExpiresAt
			env.EnvelopeHash = a.EnvelopeHash
		}
	}
	return env
}

func (h *Handler) CreateFlow(w http.ResponseWriter, r *http.Request) {
	if !h.requireFlowEnabled(w, r) {
		return
	}
	if h.flows == nil {
		common.WriteError(w, r, 500, "internal_error", "流程服务不可用", nil)
		return
	}
	var req CreateFlowRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil {
		common.WriteError(w, r, 400, "bad_request", "流程请求格式无效", nil)
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	f, err := h.flows.Create(r.Context(), chi.URLParam(r, "id"), claims.UserID, middleware.EffectiveUserID(r.Context()), common.GetRequestID(r.Context()), req)
	if err != nil {
		writeInstrumentSafetyError(w, r, err)
		return
	}
	f.Envelope = h.flowEnvelope(r.Context(), f)
	middleware.SetAuditAction(r.Context(), "instrument.flow.created")
	writeAccepted(w, r, f)
}
func (h *Handler) GetFlow(w http.ResponseWriter, r *http.Request) {
	f, err := h.repo.GetFlow(r.Context(), chi.URLParam(r, "flow_id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			common.WriteError(w, r, 404, "flow_not_found", "流程不存在", nil)
		} else {
			common.WriteError(w, r, 500, "internal_error", "查询流程失败", nil)
		}
		return
	}
	if f.InstrumentID != chi.URLParam(r, "id") {
		common.WriteError(w, r, 404, "flow_not_found", "流程不存在", nil)
		return
	}
	f.Envelope = h.flowEnvelope(r.Context(), f)
	common.WriteSuccess(w, r, f)
}
func (h *Handler) ApproveFlow(w http.ResponseWriter, r *http.Request) {
	if !h.requireFlowEnabled(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	current, lookupErr := h.repo.GetFlow(r.Context(), chi.URLParam(r, "flow_id"))
	if lookupErr != nil || current.InstrumentID != chi.URLParam(r, "id") {
		common.WriteError(w, r, http.StatusNotFound, "flow_not_found", "流程不存在", nil)
		return
	}
	f, err := h.flows.Approve(r.Context(), chi.URLParam(r, "flow_id"), claims.UserID, claims.Role)
	if err != nil {
		writeInstrumentSafetyError(w, r, err)
		return
	}
	f.Envelope = h.flowEnvelope(r.Context(), f)
	middleware.SetAuditAction(r.Context(), "instrument.flow.approved")
	go h.flows.Run(contextWithoutCancel(r.Context()), f.ID)
	writeAccepted(w, r, f)
}
func (h *Handler) StopFlow(w http.ResponseWriter, r *http.Request) {
	if !h.requireFlowEnabled(w, r) {
		return
	}
	current, lookupErr := h.repo.GetFlow(r.Context(), chi.URLParam(r, "flow_id"))
	if lookupErr != nil || current.InstrumentID != chi.URLParam(r, "id") {
		common.WriteError(w, r, http.StatusNotFound, "flow_not_found", "流程不存在", nil)
		return
	}
	if err := h.repo.StopFlow(r.Context(), chi.URLParam(r, "flow_id"), middleware.EffectiveUserID(r.Context())); err != nil {
		writeInstrumentSafetyError(w, r, err)
		return
	}
	middleware.SetAuditAction(r.Context(), "instrument.flow.stop_requested")
	writeAccepted(w, r, map[string]string{"status": "stop_requested"})
}

func contextWithoutCancel(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }

func writeInstrumentSafetyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrLeaseBusy):
		common.WriteError(w, r, 409, "instrument_busy", "仪器已有有效租约", nil)
	case errors.Is(err, ErrLeaseInvalid):
		common.WriteError(w, r, 403, "invalid_lease", "租约不存在、已过期或不属于当前用户", nil)
	case errors.Is(err, ErrApprovalInvalid), errors.Is(err, ErrApprovalSeparation):
		common.WriteError(w, r, 403, "invalid_approval", err.Error(), nil)
	case errors.Is(err, sql.ErrNoRows):
		common.WriteError(w, r, 404, "not_found", "记录不存在", nil)
	default:
		common.WriteError(w, r, 400, "validation_failed", err.Error(), nil)
	}
}
