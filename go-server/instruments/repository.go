package instruments

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) CreateLease(ctx context.Context, instrumentID, userID, purpose string, expiresAt time.Time, force bool) (*Lease, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, instrumentID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE instrument_leases SET status='expired', updated_at=now() WHERE instrument_id=$1 AND status='active' AND expires_at<=now()`, instrumentID); err != nil {
		return nil, err
	}
	if force {
		if _, err = tx.ExecContext(ctx, `UPDATE instrument_leases SET status='revoked', revoked_at=now(), revoked_by=$2, updated_at=now() WHERE instrument_id=$1 AND status='active'`, instrumentID, userID); err != nil {
			return nil, err
		}
	}
	lease := &Lease{ID: uuid.NewString(), InstrumentID: instrumentID, UserID: userID, Purpose: purpose, Status: "active", ExpiresAt: expiresAt}
	err = tx.QueryRowContext(ctx, `INSERT INTO instrument_leases (id,instrument_id,user_id,purpose,status,expires_at) VALUES ($1,$2,$3,$4,'active',$5) RETURNING created_at`, lease.ID, instrumentID, userID, purpose, expiresAt).Scan(&lease.CreatedAt)
	if err != nil {
		return nil, err
	}
	return lease, tx.Commit()
}

func (r *Repository) RenewLease(ctx context.Context, id, userID, reason string, expiresAt time.Time) (*Lease, error) {
	lease := &Lease{}
	err := r.db.QueryRowContext(ctx, `UPDATE instrument_leases SET expires_at=$3, purpose=purpose || ' | 续期: ' || $4, updated_at=now() WHERE id=$1 AND user_id=$2 AND status='active' AND expires_at>now() RETURNING id,instrument_id,user_id,purpose,status,expires_at,created_at,revoked_at,revoked_by`, id, userID, expiresAt, reason).Scan(&lease.ID, &lease.InstrumentID, &lease.UserID, &lease.Purpose, &lease.Status, &lease.ExpiresAt, &lease.CreatedAt, &lease.RevokedAt, &lease.RevokedBy)
	return lease, err
}

func (r *Repository) ReleaseLease(ctx context.Context, id, userID string, admin bool) error {
	res, err := r.db.ExecContext(ctx, `UPDATE instrument_leases SET status='released', revoked_at=now(), revoked_by=$2, updated_at=now() WHERE id=$1 AND status='active' AND (user_id=$2 OR $3)`, id, userID, admin)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (r *Repository) ValidLease(ctx context.Context, id, instrumentID, userID string) (*Lease, error) {
	lease := &Lease{}
	err := r.db.QueryRowContext(ctx, `SELECT id,instrument_id,user_id,purpose,status,expires_at,created_at,revoked_at,revoked_by FROM instrument_leases WHERE id=$1 AND instrument_id=$2 AND user_id=$3 AND status='active' AND expires_at>now()`, id, instrumentID, userID).Scan(&lease.ID, &lease.InstrumentID, &lease.UserID, &lease.Purpose, &lease.Status, &lease.ExpiresAt, &lease.CreatedAt, &lease.RevokedAt, &lease.RevokedBy)
	return lease, err
}

func (r *Repository) CreateApproval(ctx context.Context, a *Approval) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	return r.db.QueryRowContext(ctx, `INSERT INTO instrument_approvals (id,lease_id,command_name,params_hash,requested_by,acting_user_id,status,expires_at,envelope,envelope_hash,flow_session_id) VALUES ($1,$2,$3,$4,$5,$6,'pending',$7,$8,$9,$10) RETURNING created_at`, a.ID, a.LeaseID, a.CommandName, a.ParamsHash, a.RequestedBy, a.ActingUserID, a.ExpiresAt, a.Envelope, a.EnvelopeHash, a.FlowSessionID).Scan(&a.CreatedAt)
}

func (r *Repository) Approve(ctx context.Context, id, approverID string) (*Approval, error) {
	a := &Approval{}
	err := r.db.QueryRowContext(ctx, `UPDATE instrument_approvals SET status='approved',approved_by=$2,approved_at=now() WHERE id=$1 AND status='pending' AND expires_at>now() AND requested_by<>$2 AND acting_user_id IS DISTINCT FROM $2 RETURNING id,lease_id,command_name,params_hash,requested_by,approved_by,acting_user_id,flow_session_id,envelope,envelope_hash,status,approved_at,expires_at,created_at`, id, approverID).Scan(&a.ID, &a.LeaseID, &a.CommandName, &a.ParamsHash, &a.RequestedBy, &a.ApprovedBy, &a.ActingUserID, &a.FlowSessionID, &a.Envelope, &a.EnvelopeHash, &a.Status, &a.ApprovedAt, &a.ExpiresAt, &a.CreatedAt)
	return a, err
}

func (r *Repository) ValidApproval(ctx context.Context, id, leaseID, requestedBy, command, paramsHash string) (*Approval, error) {
	a := &Approval{}
	err := r.db.QueryRowContext(ctx, `SELECT id,lease_id,command_name,params_hash,requested_by,approved_by,acting_user_id,flow_session_id,envelope,envelope_hash,status,approved_at,expires_at,created_at FROM instrument_approvals WHERE id=$1 AND lease_id=$2 AND requested_by=$3 AND command_name=$4 AND params_hash=$5 AND status='approved' AND expires_at>now()`, id, leaseID, requestedBy, command, paramsHash).Scan(&a.ID, &a.LeaseID, &a.CommandName, &a.ParamsHash, &a.RequestedBy, &a.ApprovedBy, &a.ActingUserID, &a.FlowSessionID, &a.Envelope, &a.EnvelopeHash, &a.Status, &a.ApprovedAt, &a.ExpiresAt, &a.CreatedAt)
	return a, err
}

// GetApproval 按主键读取审批记录（GET flow 组装包络呈现用，不做有效性判断）。
func (r *Repository) GetApproval(ctx context.Context, id string) (*Approval, error) {
	a := &Approval{}
	err := r.db.QueryRowContext(ctx, `SELECT id,lease_id,command_name,params_hash,requested_by,approved_by,acting_user_id,flow_session_id,envelope,envelope_hash,status,approved_at,expires_at,created_at FROM instrument_approvals WHERE id=$1`, id).Scan(&a.ID, &a.LeaseID, &a.CommandName, &a.ParamsHash, &a.RequestedBy, &a.ApprovedBy, &a.ActingUserID, &a.FlowSessionID, &a.Envelope, &a.EnvelopeHash, &a.Status, &a.ApprovedAt, &a.ExpiresAt, &a.CreatedAt)
	return a, err
}

func (r *Repository) InsertCommandLog(ctx context.Context, e *CommandLogEntry) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO command_log (id,instrument_id,command_name,risk_level,params_raw,params_normalized,user_id,acting_user_id,lease_id,approval_id,whitelist_version,before_snapshot,result_summary,error_code,duration_ms,request_id,flow_session_id,step_no,phase,result_hash) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, e.ID, e.InstrumentID, e.CommandName, e.RiskLevel, e.ParamsRaw, e.ParamsNormalized, e.UserID, e.ActingUserID, e.LeaseID, e.ApprovalID, e.WhitelistVersion, e.BeforeSnapshot, e.ResultSummary, e.ErrorCode, e.DurationMS, e.RequestID, e.FlowSessionID, e.StepNo, e.Phase, e.ResultHash)
	return err
}

func (r *Repository) CreateFlow(ctx context.Context, f *FlowSession, approval *Approval) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	limits, _ := json.Marshal(f.Limits)
	grid, _ := json.Marshal(f.FrequencyGrid)
	if f.ID == "" {
		f.ID = uuid.NewString()
	}
	if approval.ID == "" {
		approval.ID = uuid.NewString()
	}
	f.ApprovalID = &approval.ID
	approval.FlowSessionID = &f.ID
	if _, err = tx.ExecContext(ctx, `INSERT INTO instrument_flow_sessions (id,instrument_id,flow_kind,objective,object_type,status,limits,frequency_grid,lease_id,whitelist_version,actor_id,acting_user_id,request_id,deadline_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, f.ID, f.InstrumentID, f.FlowKind, f.Objective, f.ObjectType, f.Status, limits, grid, f.LeaseID, f.WhitelistVersion, f.ActorID, f.ActingUserID, f.RequestID, f.DeadlineAt); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO instrument_approvals (id,lease_id,command_name,params_hash,requested_by,acting_user_id,status,expires_at,envelope,envelope_hash,flow_session_id) VALUES ($1,$2,$3,$4,$5,$6,'pending',$7,$8,$9,$10)`, approval.ID, approval.LeaseID, approval.CommandName, approval.ParamsHash, approval.RequestedBy, approval.ActingUserID, approval.ExpiresAt, approval.Envelope, approval.EnvelopeHash, f.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE instrument_flow_sessions SET approval_id=$2 WHERE id=$1`, f.ID, approval.ID); err != nil {
		return err
	}
	return tx.Commit()
}

const flowColumns = `id,instrument_id,flow_kind,objective,object_type,status,limits,frequency_grid,lease_id,approval_id,whitelist_version,actor_id,acting_user_id,request_id,step_count,point_count,stop_requested,COALESCE(error_code,''),result,deadline_at,created_at,updated_at`

func scanFlow(row interface{ Scan(...any) error }) (*FlowSession, error) {
	f := &FlowSession{}
	var limits, grid, result []byte
	err := row.Scan(&f.ID, &f.InstrumentID, &f.FlowKind, &f.Objective, &f.ObjectType, &f.Status, &limits, &grid, &f.LeaseID, &f.ApprovalID, &f.WhitelistVersion, &f.ActorID, &f.ActingUserID, &f.RequestID, &f.StepCount, &f.PointCount, &f.StopRequested, &f.ErrorCode, &result, &f.DeadlineAt, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(limits, &f.Limits); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(grid, &f.FrequencyGrid); err != nil {
		return nil, err
	}
	if len(result) > 0 {
		f.Result = &ParsedResult{}
		if err = json.Unmarshal(result, f.Result); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func (r *Repository) GetFlow(ctx context.Context, id string) (*FlowSession, error) {
	f, err := scanFlow(r.db.QueryRowContext(ctx, `SELECT `+flowColumns+` FROM instrument_flow_sessions WHERE id=$1`, id))
	if err != nil {
		return nil, err
	}
	f.Steps, err = r.ListSteps(ctx, id)
	return f, err
}
func (r *Repository) ValidFlowApproval(ctx context.Context, flowID, approvalID, leaseID, actingUserID string) error {
	var ok bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM instrument_approvals WHERE id=$1 AND flow_session_id=$2 AND lease_id=$3 AND acting_user_id=$4 AND status='approved' AND expires_at>now() AND envelope_hash=params_hash)`, approvalID, flowID, leaseID, actingUserID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrApprovalInvalid
	}
	return nil
}
func (r *Repository) ApproveFlow(ctx context.Context, id, approver string) (*FlowSession, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var approvalID string
	err = tx.QueryRowContext(ctx, `UPDATE instrument_approvals a SET status='approved',approved_by=$2,approved_at=now() FROM instrument_flow_sessions f WHERE f.id=$1 AND a.id=f.approval_id AND f.status='awaiting_approval' AND a.status='pending' AND a.expires_at>now() AND a.requested_by<>$2 AND a.acting_user_id IS DISTINCT FROM $2 RETURNING a.id`, id, approver).Scan(&approvalID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE instrument_flow_sessions SET status='queued',updated_at=now() WHERE id=$1 AND status='awaiting_approval'`, id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetFlow(ctx, id)
}
func (r *Repository) StartFlow(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE instrument_flow_sessions SET status='running',started_at=now(),updated_at=now() WHERE id=$1 AND status='queued' AND deadline_at>now()`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("flow is not queued")
	}
	return nil
}
func (r *Repository) StopFlow(ctx context.Context, id, userID string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE instrument_flow_sessions SET stop_requested=true,updated_at=now() WHERE id=$1 AND acting_user_id=$2 AND status IN ('queued','running')`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
func (r *Repository) EmergencyFlow(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE instrument_flow_sessions SET status='emergency_stopped',stop_requested=true,error_code='emergency_stop',finished_at=now(),updated_at=now() WHERE id=$1 AND status IN ('queued','running')`, id)
	return err
}
func (r *Repository) RestoreFailed(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE instrument_flow_sessions SET status='failed',error_code='restore_failed',finished_at=now(),updated_at=now() WHERE id=$1 AND status<>'emergency_stopped'`, id)
	return err
}
func (r *Repository) InterruptActiveFlows(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `UPDATE instrument_flow_sessions SET status='interrupted',error_code='server_restart',finished_at=now(),updated_at=now() WHERE status IN ('queued','running')`)
	return err
}
func (r *Repository) ExpiredActiveFlows(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `UPDATE instrument_flow_sessions SET status='timed_out',error_code='deadline_exceeded',finished_at=now(),updated_at=now() WHERE status IN ('queued','running') AND deadline_at<=now() RETURNING id,instrument_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, instrument string
		if err = rows.Scan(&id, &instrument); err != nil {
			return nil, err
		}
		out[id] = instrument
	}
	return out, rows.Err()
}
func (r *Repository) FinishFlow(ctx context.Context, id, status, code string, result *ParsedResult) error {
	var raw []byte
	if result != nil {
		raw, _ = json.Marshal(result)
	}
	res, err := r.db.ExecContext(ctx, `UPDATE instrument_flow_sessions SET status=$2,error_code=NULLIF($3,''),result=$4,finished_at=now(),updated_at=now() WHERE id=$1 AND status IN ('queued','running')`, id, status, code, raw)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("flow is already terminal")
	}
	return nil
}
func (r *Repository) MarkFlowAuditFailure(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE instrument_flow_sessions SET status='failed',error_code='audit_failed',result=NULL,finished_at=now(),updated_at=now() WHERE id=$1 AND status<>'emergency_stopped'`, id)
	return err
}
func (r *Repository) AddStep(ctx context.Context, s *FlowStep) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	params, _ := json.Marshal(s.Params)
	result, _ := json.Marshal(s.Result)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO instrument_flow_steps (id,session_id,step_no,decision,command_name,params,status,reason,result,error_code,input_hash,output_hash,model,prompt_version,whitelist_version,duration_ms) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,NULLIF($8,''),$9,NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),$15,$16)`, s.ID, s.SessionID, s.StepNo, s.Decision, s.Command, params, s.Status, s.Reason, result, s.ErrorCode, s.InputHash, s.OutputHash, s.Model, s.PromptVersion, s.WhitelistVersion, s.DurationMS); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE instrument_flow_sessions SET step_count=step_count+1,point_count=point_count+CASE WHEN $2 THEN 1 ELSE 0 END,updated_at=now() WHERE id=$1`, s.SessionID, s.Status == "succeeded" && s.Command == "measure_single")
	if err != nil {
		return err
	}
	if n, rowsErr := res.RowsAffected(); rowsErr != nil || n != 1 {
		if rowsErr != nil {
			return rowsErr
		}
		return errors.New("flow session not found while adding step")
	}
	return tx.Commit()
}
func (r *Repository) ListSteps(ctx context.Context, id string) ([]FlowStep, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,session_id,step_no,decision,COALESCE(command_name,''),params,status,COALESCE(reason,''),result,COALESCE(error_code,''),COALESCE(input_hash,''),COALESCE(output_hash,''),COALESCE(model,''),COALESCE(prompt_version,''),whitelist_version,COALESCE(duration_ms,0),created_at FROM instrument_flow_steps WHERE session_id=$1 ORDER BY step_no`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FlowStep{}
	for rows.Next() {
		var s FlowStep
		var p, v []byte
		if err = rows.Scan(&s.ID, &s.SessionID, &s.StepNo, &s.Decision, &s.Command, &p, &s.Status, &s.Reason, &v, &s.ErrorCode, &s.InputHash, &s.OutputHash, &s.Model, &s.PromptVersion, &s.WhitelistVersion, &s.DurationMS, &s.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(p, &s.Params)
		if len(v) > 0 && string(v) != "null" {
			s.Result = &ParsedResult{}
			_ = json.Unmarshal(v, s.Result)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
