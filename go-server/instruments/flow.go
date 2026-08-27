package instruments

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

const (
	FlowKindImpedanceSweep = "impedance_frequency_sweep"
	flowMaxPoints          = 50
	flowMaxSpanHz          = 100000.0
)

type FlowExecutor struct {
	repo      *Repository
	svc       *Service
	workers   map[string]*InstrumentWorker
	settle    time.Duration
	maxPoints int
	maxSpanHz float64
	deadline  time.Duration
	authorize func(context.Context, string, string, string) error
	audit     func(context.Context, string, map[string]any) error
}

func NewFlowExecutor(repo *Repository, svc *Service, workers map[string]*InstrumentWorker) *FlowExecutor {
	e := &FlowExecutor{repo: repo, svc: svc, workers: workers,
		settle:    time.Duration(envBoundedInt("HIOKI_FLOW_SETTLE_MS", 500, 0, 5000)) * time.Millisecond,
		maxPoints: envBoundedInt("HIOKI_FLOW_MAX_POINTS", flowMaxPoints, 2, flowMaxPoints),
		maxSpanHz: float64(envBoundedInt("HIOKI_FLOW_MAX_SPAN_HZ", int(flowMaxSpanHz), 1, int(flowMaxSpanHz))),
		deadline:  time.Duration(envBoundedInt("HIOKI_FLOW_DEADLINE_SECONDS", 900, 60, 900)) * time.Second}
	e.audit = func(ctx context.Context, action string, detail map[string]any) error {
		return middleware.WriteSystemAudit(ctx, repo.db, action, detail)
	}
	return e
}

func envBoundedInt(name string, fallback, min, max int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < min || value > max {
		return fallback
	}
	return value
}

// FlowEnabled 报告多步流程入口是否开放（设计 §15 灰度止血开关）。
// INSTRUMENT_FLOW_ENABLED 默认关闭：关闭时 flow 写端点不注册、FlowRecovery
// 不启动，py-agent 流程决策端点因无公开执行入口而整体不可达。
func FlowEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("INSTRUMENT_FLOW_ENABLED"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

var sweepNumbers = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(mhz|khz|hz)?`)

func parseSweepObjective(req *CreateFlowRequest) {
	if req.StartHz > 0 && req.StopHz > 0 && req.Points > 0 {
		return
	}
	matches := sweepNumbers.FindAllStringSubmatch(req.Objective, -1)
	values := []float64{}
	for _, m := range matches {
		n, _ := strconv.ParseFloat(m[1], 64)
		switch strings.ToLower(m[2]) {
		case "mhz":
			n *= 1e6
		case "khz":
			n *= 1e3
		}
		values = append(values, n)
	}
	if len(values) >= 2 {
		req.StartHz, req.StopHz = values[0], values[1]
	}
	if len(values) >= 3 {
		req.Points = int(values[2])
	}
	if strings.Contains(strings.ToLower(req.Objective), "linear") || strings.Contains(req.Objective, "线性") {
		req.Spacing = "linear"
	} else if req.Spacing == "" {
		req.Spacing = "log"
	}
}

func frequencyGrid(start, stop float64, points int, spacing string) ([]float64, error) {
	if start >= stop {
		return nil, fmt.Errorf("start_hz must be below stop_hz")
	}
	if points < 2 || points > flowMaxPoints {
		return nil, fmt.Errorf("points must be between 2 and %d", flowMaxPoints)
	}
	if stop-start > flowMaxSpanHz {
		return nil, fmt.Errorf("frequency span exceeds %.0f Hz", flowMaxSpanHz)
	}
	if spacing != "linear" && spacing != "log" {
		return nil, fmt.Errorf("spacing must be linear or log")
	}
	grid := make([]float64, points)
	for i := range grid {
		ratio := float64(i) / float64(points-1)
		if spacing == "log" {
			grid[i] = math.Exp(math.Log(start) + ratio*(math.Log(stop)-math.Log(start)))
		} else {
			grid[i] = start + ratio*(stop-start)
		}
		grid[i] = math.Round(grid[i]*1e6) / 1e6
		if i > 0 && grid[i] <= grid[i-1] {
			return nil, fmt.Errorf("frequency grid is not strictly increasing after quantization")
		}
	}
	grid[0], grid[len(grid)-1] = start, stop
	return grid, nil
}

func (e *FlowExecutor) Create(ctx context.Context, instrument, actor, acting, requestID string, req CreateFlowRequest) (*FlowSession, error) {
	if err := e.svc.InterpreterHealthy(ctx); err != nil {
		return nil, err
	}
	if instrument != "hioki_im3536" {
		return nil, fmt.Errorf("flow kind is only supported for hioki_im3536")
	}
	req.Objective = strings.TrimSpace(req.Objective)
	if req.Objective == "" || len(req.Objective) > 1000 {
		return nil, fmt.Errorf("objective is required")
	}
	if req.FlowKind == "" {
		req.FlowKind = FlowKindImpedanceSweep
	}
	if req.FlowKind != FlowKindImpedanceSweep {
		return nil, fmt.Errorf("unsupported flow kind")
	}
	if req.ObjectType == "" {
		req.ObjectType = "passive_lc_component"
	}
	if req.ObjectType != "passive_lc_component" {
		return nil, fmt.Errorf("unsupported object_type")
	}
	parseSweepObjective(&req)
	if req.Points > e.maxPoints {
		return nil, fmt.Errorf("points exceed configured limit %d", e.maxPoints)
	}
	if req.StopHz-req.StartHz > e.maxSpanHz {
		return nil, fmt.Errorf("frequency span exceeds configured limit %.0f Hz", e.maxSpanHz)
	}
	grid, err := frequencyGrid(req.StartHz, req.StopHz, req.Points, req.Spacing)
	if err != nil {
		return nil, err
	}
	if _, err = NormalizeParams(instrument, "set_frequency", map[string]any{"hz": req.StartHz}); err != nil {
		return nil, err
	}
	if _, err = NormalizeParams(instrument, "set_frequency", map[string]any{"hz": req.StopHz}); err != nil {
		return nil, err
	}
	if _, err = e.repo.ValidLease(ctx, req.LeaseID, instrument, acting); err != nil {
		return nil, ErrLeaseInvalid
	}
	deadline := time.Now().Add(e.deadline)
	limits := FlowLimits{AllowedCommands: []string{"set_frequency", "measure_single"}, FrequencyMinHz: req.StartHz, FrequencyMaxHz: req.StopHz, MaxPoints: req.Points, RetryBudget: req.Points, MaxCommands: 3 * req.Points, DeadlineAt: deadline, RestoreFrequency: true}
	approvalExpiry := deadline
	envelope := map[string]any{"instrument_id": instrument, "acting_user_id": acting, "lease_id": req.LeaseID, "flow_kind": req.FlowKind, "objective": req.Objective, "object_type": req.ObjectType, "whitelist_version": whitelistVersion, "limits": limits, "frequency_grid": grid, "approval_expires_at": approvalExpiry}
	hash, raw, err := canonicalHash(envelope)
	if err != nil {
		return nil, err
	}
	f := &FlowSession{InstrumentID: instrument, FlowKind: req.FlowKind, Objective: req.Objective, ObjectType: req.ObjectType, Status: "awaiting_approval", Limits: limits, FrequencyGrid: grid, LeaseID: req.LeaseID, WhitelistVersion: whitelistVersion, ActorID: actor, ActingUserID: acting, RequestID: requestID, DeadlineAt: deadline}
	a := &Approval{LeaseID: &req.LeaseID, CommandName: "flow:" + req.FlowKind, ParamsHash: hash, RequestedBy: actor, ActingUserID: &acting, Envelope: raw, EnvelopeHash: hash, ExpiresAt: approvalExpiry}
	if err = e.repo.CreateFlow(ctx, f, a); err != nil {
		return nil, err
	}
	return e.repo.GetFlow(ctx, f.ID)
}

func (e *FlowExecutor) Approve(ctx context.Context, id, approver, role string) (*FlowSession, error) {
	if role != "maintainer" && role != "admin" {
		return nil, ErrApprovalSeparation
	}
	f, err := e.repo.ApproveFlow(ctx, id, approver)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrApprovalSeparation
	}
	return f, err
}

func (e *FlowExecutor) Run(ctx context.Context, id string) {
	f, err := e.repo.GetFlow(ctx, id)
	if err != nil {
		return
	}
	worker := e.workers[f.InstrumentID]
	if worker == nil {
		e.finish(ctx, f, "failed", "instrument_unavailable", nil)
		return
	}
	if err = worker.AcquireSession(id, f.DeadlineAt); err != nil {
		e.finish(ctx, f, "failed", "instrument_busy", nil)
		return
	}
	defer worker.ReleaseSession(id)
	defer func() {
		if recover() != nil {
			e.finish(context.Background(), f, "failed", "executor_panic", nil)
		}
	}()
	if err = e.repo.StartFlow(ctx, id); err != nil {
		return
	}
	original, err := e.runRaw(ctx, f, worker, 0, "read_frequency", nil)
	if err != nil {
		e.finish(ctx, f, "failed", "snapshot_failed", nil)
		return
	}
	parsedOriginal, err := strconv.ParseFloat(strings.TrimSpace(original.Response), 64)
	if err != nil {
		e.finish(ctx, f, "failed", "snapshot_parse_failed", nil)
		return
	}
	points := make([]Point, 0, len(f.FrequencyGrid))
	pending := -1
	retries := make(map[int]int)
	var previous any
	defer func() {
		if f.Limits.RestoreFrequency && worker.SessionOwner() == id {
			if _, restoreErr := e.runRaw(context.Background(), f, worker, -1, "set_frequency", map[string]any{"hz": parsedOriginal}); restoreErr != nil {
				if persistErr := e.repo.RestoreFailed(context.Background(), f.ID); persistErr != nil {
					e.persistenceFailure(f, persistErr)
				}
			}
		}
	}()
	for stepNo := 1; stepNo <= f.Limits.MaxCommands+1; stepNo++ {
		fresh, getErr := e.preSendGuard(ctx, f)
		if getErr != nil {
			e.finishGuardFailure(ctx, f, getErr, points)
			return
		}
		f.StepCount, f.PointCount = fresh.StepCount, fresh.PointCount
		decision, decErr := e.svc.NextFlowDecision(ctx, f, points, previous)
		if decErr != nil {
			e.finish(ctx, f, "failed", "decision_failed", nil)
			return
		}
		if decision.Decision == "abort" {
			e.finish(ctx, f, "failed", "model_abort", nil)
			return
		}
		if decision.Decision == "complete" {
			if len(points) != len(f.FrequencyGrid) {
				e.reject(ctx, f, stepNo, decision, "incomplete_flow")
				return
			}
			e.finish(ctx, f, "completed", "", &ParsedResult{Type: "sweep_xy", Points: points, XLabel: "频率 (Hz)", YLabel: "阻抗 |Z| (Ω)"})
			return
		}
		if decision.Decision != "next_command" {
			e.reject(ctx, f, stepNo, decision, "invalid_decision")
			return
		}
		if stepNo > f.Limits.MaxCommands {
			e.reject(ctx, f, stepNo, decision, "max_commands_exceeded")
			return
		}
		expectedCommand := "set_frequency"
		expectedIndex := len(points)
		if pending >= 0 {
			expectedCommand = "measure_single"
			expectedIndex = pending
		}
		if decision.Command != expectedCommand || expectedIndex >= len(f.FrequencyGrid) {
			e.reject(ctx, f, stepNo, decision, "flow_envelope_violation")
			return
		}
		params := decision.Params
		if params == nil {
			params = map[string]any{}
		}
		if decision.Command == "set_frequency" {
			hz, ok := number(params["hz"])
			if !ok || hz != f.FrequencyGrid[expectedIndex] {
				e.reject(ctx, f, stepNo, decision, "frequency_outside_grid")
				return
			}
			pending = expectedIndex
		}
		result, runErr := e.runRaw(ctx, f, worker, stepNo, decision.Command, params)
		step := &FlowStep{SessionID: id, StepNo: stepNo, Decision: decision.Decision, Command: decision.Command, Params: params, Reason: decision.Reason, InputHash: decision.InputHash, OutputHash: decision.OutputHash, Model: decision.Model, PromptVersion: decision.PromptVersion, WhitelistVersion: whitelistVersion, DurationMS: int(result.Duration.Milliseconds())}
		if runErr != nil {
			step.Status = "failed"
			step.ErrorCode = commandErrorCode(runErr)
			if !e.persistStep(ctx, f, worker, step, result.Command != "") {
				return
			}
			if step.ErrorCode == "stop_requested" || step.ErrorCode == "deadline_exceeded" || step.ErrorCode == "flow_not_running" {
				e.finishGuardFailure(ctx, f, runErr, points)
				return
			}
			previous = map[string]any{"command": decision.Command, "error": step.ErrorCode}
			if !canRetryMeasure(decision.Command, pending, retries[pending], runErr, worker.State()) {
				e.finish(ctx, f, "failed", step.ErrorCode, nil)
				return
			}
			retries[pending]++
			continue
		}
		step.Status = "succeeded"
		if decision.Command == "measure_single" {
			def, _ := GetCommand(f.InstrumentID, "measure_single")
			parsed, parseErr := e.svc.ParseResult(def, result.Response)
			if parseErr != nil || parsed == nil || parsed.Value == nil {
				step.Status = "failed"
				step.ErrorCode = "parse_failed"
				if !e.persistStep(ctx, f, worker, step, true) {
					return
				}
				e.finish(ctx, f, "failed", "parse_failed", nil)
				return
			}
			step.Result = parsed
			points = append(points, Point{X: f.FrequencyGrid[pending], Y: *parsed.Value})
			pending = -1
		} else if e.settle > 0 {
			select {
			case <-ctx.Done():
				e.finish(ctx, f, "failed", "cancelled", nil)
				return
			case <-time.After(e.settle):
			}
		}
		if !e.persistStep(ctx, f, worker, step, true) {
			return
		}
		previous = map[string]any{"command": decision.Command, "params": params, "result": step.Result}
	}
	e.finish(ctx, f, "failed", "max_commands_exceeded", nil)
}

func (e *FlowExecutor) runRaw(ctx context.Context, f *FlowSession, w *InstrumentWorker, stepNo int, command string, params map[string]any) (CommandResult, error) {
	def, err := GetCommand(f.InstrumentID, command)
	if err != nil || def.Risk == "red" {
		return CommandResult{}, newCommandError("validation_error", fmt.Errorf("command_not_allowed"))
	}
	normalized, err := NormalizeParams(f.InstrumentID, command, params)
	if err != nil {
		// M5：参数校验失败（中文报错）发生在硬件发送前，直接归类 validation_error，绝不重试。
		return CommandResult{}, newCommandError("validation_error", err)
	}
	if command == "set_frequency" {
		if err = w.WaitFlowYellowSlot(ctx, f.DeadlineAt); err != nil {
			return CommandResult{}, err
		}
	}
	if stepNo > 0 {
		if _, err = e.preSendGuard(ctx, f); err != nil {
			return CommandResult{}, err
		}
	}
	step := stepNo
	flowID := f.ID
	raw, _ := json.Marshal(params)
	norm, _ := json.Marshal(normalized)
	if err = e.repo.InsertCommandLog(ctx, &CommandLogEntry{InstrumentID: f.InstrumentID, CommandName: command, RiskLevel: def.Risk, ParamsRaw: raw, ParamsNormalized: norm, UserID: f.ActorID, ActingUserID: &f.ActingUserID, LeaseID: &f.LeaseID, ApprovalID: f.ApprovalID, WhitelistVersion: whitelistVersion, RequestID: f.RequestID, FlowSessionID: &flowID, StepNo: &step, Phase: "requested"}); err != nil {
		return CommandResult{}, newCommandError("audit_failed", err)
	}
	if err = e.audit(ctx, "instrument.command.requested", map[string]any{"flow_session_id": f.ID, "step_no": stepNo, "command": command, "actor_id": f.ActorID, "acting_user_id": f.ActingUserID, "lease_id": f.LeaseID, "approval_id": f.ApprovalID, "whitelist_version": whitelistVersion}); err != nil {
		return CommandResult{}, newCommandError("audit_failed", err)
	}
	cmd := &QueueCommand{Name: command, Params: normalized, Risk: def.Risk, SessionID: f.ID, ResponseCh: make(chan CommandResult, 1)}
	if err = w.Submit(cmd); err != nil {
		// 未到达硬件的会话/队列拒绝（busy/locked/not acquired 等）不可重试。
		return CommandResult{}, newCommandError("validation_error", err)
	}
	var result CommandResult
	timeout := time.Duration(def.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	select {
	case result = <-cmd.ResponseCh:
	case <-time.After(timeout):
		result = CommandResult{Command: command, Error: newCommandError("timeout", fmt.Errorf("command timeout"))}
	}
	summary := result.Response
	if len(summary) > 512 {
		summary = summary[:512]
	}
	resultHash, _, _ := canonicalHash(result.Response)
	var errCode *string
	if result.Error != nil {
		x := commandErrorCode(result.Error)
		errCode = &x
	}
	duration := int(result.Duration.Milliseconds())
	postErr := e.repo.InsertCommandLog(ctx, &CommandLogEntry{InstrumentID: f.InstrumentID, CommandName: command, RiskLevel: def.Risk, ParamsRaw: raw, ParamsNormalized: norm, UserID: f.ActorID, ActingUserID: &f.ActingUserID, LeaseID: &f.LeaseID, ApprovalID: f.ApprovalID, WhitelistVersion: whitelistVersion, ResultSummary: &summary, ResultHash: &resultHash, ErrorCode: errCode, DurationMS: &duration, RequestID: f.RequestID, FlowSessionID: &flowID, StepNo: &step, Phase: "completed"})
	action := "instrument.command.completed"
	if result.Error != nil {
		action = "instrument.command.failed"
	}
	if postErr == nil {
		postErr = e.audit(ctx, action, map[string]any{"flow_session_id": f.ID, "step_no": stepNo, "command": command, "result_hash": resultHash, "error_code": errCode})
	}
	if postErr != nil {
		w.requireManualCheck(postErr)
		return result, newCommandError("audit_failed", postErr)
	}
	return result, result.Error
}

func (e *FlowExecutor) reject(ctx context.Context, f *FlowSession, n int, d *FlowDecision, code string) {
	if !e.persistStep(ctx, f, nil, &FlowStep{SessionID: f.ID, StepNo: n, Decision: d.Decision, Command: d.Command, Params: d.Params, Status: "rejected", Reason: d.Reason, ErrorCode: code, InputHash: d.InputHash, OutputHash: d.OutputHash, Model: d.Model, PromptVersion: d.PromptVersion, WhitelistVersion: whitelistVersion}, false) {
		return
	}
	e.finish(ctx, f, "failed", code, nil)
}
func (e *FlowExecutor) finish(ctx context.Context, f *FlowSession, status, code string, result *ParsedResult) {
	if err := e.repo.FinishFlow(ctx, f.ID, status, code, result); err != nil {
		e.persistenceFailure(f, err)
		return
	}
	if err := e.audit(ctx, "instrument.flow."+status, map[string]any{"flow_session_id": f.ID, "actor_id": f.ActorID, "acting_user_id": f.ActingUserID, "lease_id": f.LeaseID, "approval_id": f.ApprovalID, "error_code": code}); err != nil {
		if markErr := e.repo.MarkFlowAuditFailure(context.Background(), f.ID); markErr != nil {
			slog.Error("mark instrument flow audit failure failed", "flow_session_id", f.ID, "error", markErr)
		}
		e.persistenceFailure(f, err)
	}
}

func (e *FlowExecutor) preSendGuard(ctx context.Context, f *FlowSession) (*FlowSession, error) {
	fresh, err := e.repo.GetFlow(ctx, f.ID)
	if err != nil {
		return nil, newCommandError("state_read_failed", err)
	}
	if fresh.StopRequested {
		return nil, newCommandError("stop_requested", errors.New("flow stop requested"))
	}
	if fresh.Status != "running" {
		return nil, newCommandError("flow_not_running", fmt.Errorf("flow status is %s", fresh.Status))
	}
	if !time.Now().Before(fresh.DeadlineAt) {
		return nil, newCommandError("deadline_exceeded", context.DeadlineExceeded)
	}
	if fresh.WhitelistVersion != whitelistVersion {
		return nil, newCommandError("whitelist_changed", errors.New("instrument whitelist changed"))
	}
	if _, err = e.repo.ValidLease(ctx, fresh.LeaseID, fresh.InstrumentID, fresh.ActingUserID); err != nil {
		return nil, newCommandError("lease_expired", err)
	}
	if fresh.ApprovalID == nil || e.repo.ValidFlowApproval(ctx, fresh.ID, *fresh.ApprovalID, fresh.LeaseID, fresh.ActingUserID) != nil {
		return nil, newCommandError("approval_expired", ErrApprovalInvalid)
	}
	if e.authorize == nil || e.authorize(ctx, fresh.ActorID, fresh.ActingUserID, fresh.InstrumentID) != nil {
		return nil, newCommandError("permission_revoked", errors.New("instrument flow permission revoked"))
	}
	return fresh, nil
}

func (e *FlowExecutor) finishGuardFailure(ctx context.Context, f *FlowSession, err error, points []Point) {
	code := commandErrorCode(err)
	switch code {
	case "stop_requested":
		e.finish(ctx, f, "stopped", "", &ParsedResult{Type: "sweep_xy", Points: points, XLabel: "频率 (Hz)", YLabel: "阻抗 |Z| (Ω)"})
	case "deadline_exceeded":
		e.finish(ctx, f, "timed_out", code, nil)
	default:
		e.finish(ctx, f, "failed", code, nil)
	}
}

func (e *FlowExecutor) persistStep(ctx context.Context, f *FlowSession, w *InstrumentWorker, step *FlowStep, hardwareTouched bool) bool {
	if err := e.repo.AddStep(ctx, step); err != nil {
		if hardwareTouched && w != nil {
			w.requireManualCheck(err)
		}
		e.finish(ctx, f, "failed", "step_persistence_failed", nil)
		return false
	}
	return true
}

func (e *FlowExecutor) persistenceFailure(f *FlowSession, err error) {
	slog.Error("instrument flow persistence failed", "flow_session_id", f.ID, "error", err)
	if w := e.workers[f.InstrumentID]; w != nil {
		w.requireManualCheck(err)
	}
}

// commandError 给命令失败携带结构化错误类别（M5）。重试判定只认 code，
// 不再对错误消息做子串匹配——中文 NormalizeParams 校验错误此前会落入
// communication_error 而被当作可重试瞬时错误。未知错误一律 validation_error
// （fail-closed：越权/解析/状态类失败绝不重试，仅超时与显式标记的硬件通信
// 错误可重试）。
type commandError struct {
	code string
	err  error
}

func (e *commandError) Error() string { return e.code + ": " + e.err.Error() }
func (e *commandError) Unwrap() error { return e.err }

func newCommandError(code string, err error) error {
	if err == nil {
		err = errors.New(code)
	}
	return &commandError{code: code, err: err}
}

// commandErrorCode 判定结构化错误类别；只有显式标记为 timeout /
// communication_error 的瞬时错误可重试，其余（含未知错误）均不可重试。
func commandErrorCode(err error) string {
	var ce *commandError
	if errors.As(err, &ce) {
		return ce.code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "validation_error"
}

func retryableCommandError(err error) bool {
	code := commandErrorCode(err)
	return code == "timeout" || code == "communication_error"
}

func canRetryMeasure(command string, pending, retries int, err error, state WorkerState) bool {
	return command == "measure_single" && pending >= 0 && retries < 1 && retryableCommandError(err) && state == WorkerStateRunning
}
