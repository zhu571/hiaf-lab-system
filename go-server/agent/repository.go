package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ValidateTask(taskID, actingUserID string) (bool, error) {
	var valid bool
	err := r.db.QueryRow(
		`SELECT EXISTS (
			SELECT 1 FROM pending_agent_tasks
			WHERE id = $1 AND acting_user_id = $2 AND status IN ('processing','done')
		)`, taskID, actingUserID,
	).Scan(&valid)
	if err != nil {
		return false, fmt.Errorf("validate agent task: %w", err)
	}
	return valid, nil
}

// CountInQueue 统计队列深度：pending + processing（迁移 013/014 schema：
// status ∈ pending/processing/done/failed/dead）。/metrics 的 lab_agent_queue_depth 用。
func (r *Repository) CountInQueue(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pending_agent_tasks WHERE status IN ('pending','processing')`,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("count agent queue: %w", err)
	}
	return n, nil
}

func (r *Repository) Claim(leaseSeconds int) (*PendingAgentTask, error) {
	// 每次领取生成新所有权 token（028）：Complete/Fail 必须持同一 token，
	// 租约过期被重领后 token 被覆盖，旧 worker 的迟到写全部被拒。
	claimToken, err := newClaimToken()
	if err != nil {
		return nil, fmt.Errorf("generate claim token: %w", err)
	}
	var task PendingAgentTask
	err = scanTask(r.db.QueryRow(
		`UPDATE pending_agent_tasks
		 SET status = 'processing', claimed_at = now(), claim_token = $2,
		     lease_expires_at = now() + $1 * interval '1 second',
		     next_attempt_at = NULL, last_error = NULL, updated_at = now()
		 WHERE id = (
		     SELECT id FROM pending_agent_tasks
		     WHERE acting_user_id IS NOT NULL AND (
		           status = 'pending'
		        OR (status = 'failed' AND COALESCE(next_attempt_at, created_at) <= now())
		        OR (status = 'processing' AND lease_expires_at <= now())
		     )
		     ORDER BY created_at
		     FOR UPDATE SKIP LOCKED
		     LIMIT 1
		 )
		 RETURNING id, report_id, acting_user_id, status, attempts, claim_token, claimed_at,
		           lease_expires_at, next_attempt_at, completed_at, last_error,
		           result, model, prompt_version, agent_confidence,
		           raw_text_snapshot, raw_text_sha256, report_date, created_at, updated_at`,
		leaseSeconds, claimToken,
	), &task)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("claim agent task: %w", err)
	}
	return &task, nil
}

func newClaimToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (r *Repository) Complete(taskID string, req CompleteTaskRequest) (*PendingAgentTask, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin complete agent task: %w", err)
	}
	defer tx.Rollback()

	var task PendingAgentTask
	if err := scanTask(tx.QueryRow(
		`SELECT id, report_id, acting_user_id, status, attempts, claim_token, claimed_at,
		        lease_expires_at, next_attempt_at, completed_at, last_error,
		        result, model, prompt_version, agent_confidence,
		        raw_text_snapshot, raw_text_sha256, report_date, created_at, updated_at
		 FROM pending_agent_tasks WHERE id = $1 FOR UPDATE`, taskID,
	), &task); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("lock agent task: %w", err)
	}
	// 所有权校验：只有持本次领取 token 的 worker 可以完成该任务（028）。
	if req.ClaimToken == "" || task.ClaimToken == nil || *task.ClaimToken != req.ClaimToken {
		return nil, ErrInvalidLease
	}
	if task.Status == TaskDone {
		return &task, nil
	}
	if task.Status != TaskProcessing || task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(time.Now()) {
		return nil, ErrInvalidLease
	}

	for i, candidate := range req.Candidates {
		key := fmt.Sprintf("%s:%s:%d", task.ID, candidate.ActionType, i)
		if _, err := tx.Exec(
			`INSERT INTO agent_candidate_actions
			 (task_id, action_type, project_id, pool_action_key, payload, agent_confidence)
			 VALUES ($1, $2, $3, $4, $5::jsonb, $6)
			 ON CONFLICT (pool_action_key) DO NOTHING`,
			task.ID, candidate.ActionType, nullableString(candidate.ProjectID), key,
			string(candidate.Payload), candidate.AgentConfidence,
		); err != nil {
			return nil, fmt.Errorf("insert candidate action: %w", err)
		}
	}

	// 原始输入快照（030）：worker 回传 raw_text，Go 侧计算 sha256 一并落库；
	// agent 模块不读取 daily_reports，快照是"AI 当时看到的内容"的唯一可信留痕。
	var rawTextSnapshot, rawTextSHA256, reportDate any
	if req.RawTextSnapshot != "" {
		rawTextSnapshot = req.RawTextSnapshot
		sum := sha256.Sum256([]byte(req.RawTextSnapshot))
		rawTextSHA256 = hex.EncodeToString(sum[:])
	}
	if req.ReportDate != "" {
		reportDate = req.ReportDate
	}
	if err := scanTask(tx.QueryRow(
		`UPDATE pending_agent_tasks
		 SET status = 'done', completed_at = now(), lease_expires_at = NULL,
		     result = $2::jsonb, model = $3, prompt_version = $4,
		     agent_confidence = $5, raw_text_snapshot = $6, raw_text_sha256 = $7,
		     report_date = $8::date, updated_at = now()
		 WHERE id = $1
		 RETURNING id, report_id, acting_user_id, status, attempts, claim_token, claimed_at,
		           lease_expires_at, next_attempt_at, completed_at, last_error,
		           result, model, prompt_version, agent_confidence,
		           raw_text_snapshot, raw_text_sha256, report_date, created_at, updated_at`,
		task.ID, string(req.Result), req.Model, req.PromptVersion, req.AgentConfidence,
		rawTextSnapshot, rawTextSHA256, reportDate,
	), &task); err != nil {
		return nil, fmt.Errorf("complete agent task: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit complete agent task: %w", err)
	}
	return &task, nil
}

func (r *Repository) Fail(taskID, lastError string, maxAttempts int, claimToken string) (*PendingAgentTask, error) {
	var task PendingAgentTask
	err := scanTask(r.db.QueryRow(
		`UPDATE pending_agent_tasks
		 SET attempts = attempts + 1,
		     status = CASE WHEN attempts + 1 >= $3 THEN 'dead' ELSE 'failed' END,
		     next_attempt_at = CASE WHEN attempts + 1 >= $3 THEN NULL
		                            ELSE now() + ((attempts + 1) * interval '1 minute') END,
		     lease_expires_at = NULL, last_error = $2, updated_at = now()
		 WHERE id = $1 AND claim_token = $4 AND status = 'processing' AND lease_expires_at > now()
		 RETURNING id, report_id, acting_user_id, status, attempts, claim_token, claimed_at,
		           lease_expires_at, next_attempt_at, completed_at, last_error,
		           result, model, prompt_version, agent_confidence,
		           raw_text_snapshot, raw_text_sha256, report_date, created_at, updated_at`,
		taskID, lastError, maxAttempts, claimToken,
	), &task)
	if err != nil {
		if err == sql.ErrNoRows {
			var exists bool
			if checkErr := r.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pending_agent_tasks WHERE id = $1)`, taskID).Scan(&exists); checkErr != nil {
				return nil, fmt.Errorf("check agent task: %w", checkErr)
			}
			if !exists {
				return nil, ErrTaskNotFound
			}
			return nil, ErrInvalidLease
		}
		return nil, fmt.Errorf("fail agent task: %w", err)
	}
	return &task, nil
}

// Renew 在租约未过期且 claim_token 匹配的前提下延长租约（R8）。
// 条件与 Fail 同式（token + status + 未过期）：过期后被重领的任务，旧 worker
// 的续约请求一律 409 invalid_agent_lease，与 complete/fail 的所有权语义一致。
func (r *Repository) Renew(taskID, claimToken string, leaseSeconds int) (*PendingAgentTask, error) {
	var task PendingAgentTask
	err := scanTask(r.db.QueryRow(
		`UPDATE pending_agent_tasks
		 SET lease_expires_at = now() + $2 * interval '1 second', updated_at = now()
		 WHERE id = $1 AND claim_token = $3 AND status = 'processing' AND lease_expires_at > now()
		 RETURNING id, report_id, acting_user_id, status, attempts, claim_token, claimed_at,
		           lease_expires_at, next_attempt_at, completed_at, last_error,
		           result, model, prompt_version, agent_confidence,
		           raw_text_snapshot, raw_text_sha256, report_date, created_at, updated_at`,
		taskID, leaseSeconds, claimToken,
	), &task)
	if err != nil {
		if err == sql.ErrNoRows {
			var exists bool
			if checkErr := r.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pending_agent_tasks WHERE id = $1)`, taskID).Scan(&exists); checkErr != nil {
				return nil, fmt.Errorf("check agent task: %w", checkErr)
			}
			if !exists {
				return nil, ErrTaskNotFound
			}
			return nil, ErrInvalidLease
		}
		return nil, fmt.Errorf("renew agent task lease: %w", err)
	}
	return &task, nil
}

func (r *Repository) ListCandidates(status string, page, perPage int) ([]AgentCandidateAction, int, error) {
	where := ""
	args := []any{}
	if status != "" {
		where = "WHERE c.status = $1"
		args = append(args, status)
	}
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM agent_candidate_actions c `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count candidate actions: %w", err)
	}
	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.db.Query(
		`SELECT c.id, c.task_id, c.action_type, c.project_id, c.pool_action_key, c.payload, c.status,
		        c.agent_confidence, c.reviewed_by, c.reviewed_at, c.review_reason,
		        c.executed_at, c.execution_error, c.created_at, t.report_id
		 FROM agent_candidate_actions c
		 JOIN pending_agent_tasks t ON t.id = c.task_id `+where+fmt.Sprintf(
			" ORDER BY c.created_at LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list candidate actions: %w", err)
	}
	defer rows.Close()
	items := []AgentCandidateAction{}
	for rows.Next() {
		var item AgentCandidateAction
		if err := scanCandidate(rows, &item, &item.ReportID); err != nil {
			return nil, 0, fmt.Errorf("scan candidate action: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate candidate actions: %w", err)
	}
	return items, total, nil
}

func (r *Repository) ApproveCandidate(id, reviewerID string) (*AgentCandidateAction, string, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, "", fmt.Errorf("begin approve candidate: %w", err)
	}
	defer tx.Rollback()

	item, actingUserID, err := getCandidateForUpdate(tx, id)
	if err != nil {
		return nil, "", err
	}
	if item.Status == CandidateExecuted {
		return item, actingUserID, tx.Commit()
	}
	if item.Status != CandidatePending {
		return nil, "", ErrCandidateNotPending
	}
	if err := scanCandidate(tx.QueryRow(
		`UPDATE agent_candidate_actions
		 SET status = 'approved', reviewed_by = $2, reviewed_at = now()
		 WHERE id = $1
		 RETURNING id, task_id, action_type, project_id, pool_action_key, payload, status,
		           agent_confidence, reviewed_by, reviewed_at, review_reason,
		           executed_at, execution_error, created_at`, id, reviewerID,
	), item); err != nil {
		return nil, "", fmt.Errorf("approve candidate action: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("commit approve candidate: %w", err)
	}
	return item, actingUserID, nil
}

func (r *Repository) RejectCandidate(id, reviewerID, reason string) (*AgentCandidateAction, error) {
	var item AgentCandidateAction
	err := scanCandidate(r.db.QueryRow(
		`UPDATE agent_candidate_actions
		 SET status = 'rejected', reviewed_by = $2, reviewed_at = now(), review_reason = $3
		 WHERE id = $1 AND status = 'pending_review'
		 RETURNING id, task_id, action_type, project_id, pool_action_key, payload, status,
		           agent_confidence, reviewed_by, reviewed_at, review_reason,
		           executed_at, execution_error, created_at`, id, reviewerID, reason,
	), &item)
	if err == nil {
		return &item, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("reject candidate action: %w", err)
	}
	existing, _, getErr := r.GetCandidate(id)
	if getErr != nil {
		return nil, getErr
	}
	if existing == nil {
		return nil, ErrCandidateNotFound
	}
	if existing.Status == CandidateRejected {
		return existing, nil
	}
	return nil, ErrCandidateNotPending
}

func (r *Repository) MarkCandidateExecuted(id string) (*AgentCandidateAction, error) {
	var item AgentCandidateAction
	err := scanCandidate(r.db.QueryRow(
		`UPDATE agent_candidate_actions
		 SET status = 'executed', executed_at = now(), execution_error = NULL
		 WHERE id = $1 AND status = 'approved'
		 RETURNING id, task_id, action_type, project_id, pool_action_key, payload, status,
		           agent_confidence, reviewed_by, reviewed_at, review_reason,
		           executed_at, execution_error, created_at`, id,
	), &item)
	if err != nil {
		return nil, fmt.Errorf("mark candidate executed: %w", err)
	}
	return &item, nil
}

func (r *Repository) MarkCandidateFailed(id, detail string) (*AgentCandidateAction, error) {
	var item AgentCandidateAction
	err := scanCandidate(r.db.QueryRow(
		`UPDATE agent_candidate_actions
		 SET status = 'execution_failed', execution_error = $2
		 WHERE id = $1 AND status = 'approved'
		 RETURNING id, task_id, action_type, project_id, pool_action_key, payload, status,
		           agent_confidence, reviewed_by, reviewed_at, review_reason,
		           executed_at, execution_error, created_at`, id, detail,
	), &item)
	if err != nil {
		return nil, fmt.Errorf("mark candidate failed: %w", err)
	}
	return &item, nil
}

func (r *Repository) GetCandidate(id string) (*AgentCandidateAction, string, error) {
	return getCandidate(r.db, id, "")
}

// GetTask 按 id 读取任务（trace 端点用，含 030 快照字段，不加锁）。
func (r *Repository) GetTask(taskID string) (*PendingAgentTask, error) {
	var task PendingAgentTask
	err := scanTask(r.db.QueryRow(
		`SELECT id, report_id, acting_user_id, status, attempts, claim_token, claimed_at,
		        lease_expires_at, next_attempt_at, completed_at, last_error,
		        result, model, prompt_version, agent_confidence,
		        raw_text_snapshot, raw_text_sha256, report_date, created_at, updated_at
		 FROM pending_agent_tasks WHERE id = $1`, taskID,
	), &task)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("get agent task: %w", err)
	}
	return &task, nil
}

type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func getCandidateForUpdate(tx *sql.Tx, id string) (*AgentCandidateAction, string, error) {
	return getCandidate(tx, id, " FOR UPDATE")
}

func getCandidate(q queryer, id, suffix string) (*AgentCandidateAction, string, error) {
	var item AgentCandidateAction
	var actingUserID string
	err := scanCandidateWithActingUser(q.QueryRow(
		`SELECT c.id, c.task_id, c.action_type, c.project_id, c.pool_action_key, c.payload, c.status,
		        c.agent_confidence, c.reviewed_by, c.reviewed_at, c.review_reason,
		        c.executed_at, c.execution_error, c.created_at, t.acting_user_id
		 FROM agent_candidate_actions c
		 JOIN pending_agent_tasks t ON t.id = c.task_id
		 WHERE c.id = $1`+suffix, id,
	), &item, &actingUserID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", ErrCandidateNotFound
		}
		return nil, "", fmt.Errorf("get candidate action: %w", err)
	}
	return &item, actingUserID, nil
}

type rowScanner interface{ Scan(...any) error }

func scanTask(row rowScanner, task *PendingAgentTask) error {
	var actingUserID, claimToken, lastError, model, promptVersion sql.NullString
	var rawTextSnapshot, rawTextSHA256 sql.NullString
	var reportDate sql.NullTime
	var claimedAt, leaseExpiresAt, nextAttemptAt, completedAt sql.NullTime
	var result []byte
	var confidence sql.NullFloat64
	if err := row.Scan(
		&task.ID, &task.ReportID, &actingUserID, &task.Status, &task.Attempts, &claimToken,
		&claimedAt, &leaseExpiresAt, &nextAttemptAt, &completedAt, &lastError,
		&result, &model, &promptVersion, &confidence,
		&rawTextSnapshot, &rawTextSHA256, &reportDate, &task.CreatedAt, &task.UpdatedAt,
	); err != nil {
		return err
	}
	task.ActingUserID = actingUserID.String
	task.ClaimToken = nullStringPtr(claimToken)
	task.ClaimedAt = nullTimePtr(claimedAt)
	task.LeaseExpiresAt = nullTimePtr(leaseExpiresAt)
	task.NextAttemptAt = nullTimePtr(nextAttemptAt)
	task.CompletedAt = nullTimePtr(completedAt)
	task.LastError = nullStringPtr(lastError)
	if len(result) > 0 {
		task.Result = json.RawMessage(result)
	}
	task.Model = nullStringPtr(model)
	task.PromptVersion = nullStringPtr(promptVersion)
	if confidence.Valid {
		task.AgentConfidence = &confidence.Float64
	}
	task.RawTextSnapshot = nullStringPtr(rawTextSnapshot)
	task.RawTextSHA256 = nullStringPtr(rawTextSHA256)
	if reportDate.Valid {
		formatted := reportDate.Time.Format(time.DateOnly)
		task.ReportDate = &formatted
	}
	return nil
}

func scanCandidate(row rowScanner, item *AgentCandidateAction, extra ...any) error {
	return scanCandidateValues(row, item, extra...)
}

func scanCandidateWithActingUser(row rowScanner, item *AgentCandidateAction, actingUserID *string) error {
	return scanCandidateValues(row, item, actingUserID)
}

func scanCandidateValues(row rowScanner, item *AgentCandidateAction, extra ...any) error {
	var projectID, reviewedBy, reviewReason, executionError sql.NullString
	var confidence sql.NullFloat64
	var reviewedAt, executedAt sql.NullTime
	var payload []byte
	dest := []any{
		&item.ID, &item.TaskID, &item.ActionType, &projectID, &item.PoolActionKey,
		&payload, &item.Status, &confidence, &reviewedBy, &reviewedAt,
		&reviewReason, &executedAt, &executionError, &item.CreatedAt,
	}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return err
	}
	item.ProjectID = nullStringPtr(projectID)
	item.Payload = json.RawMessage(payload)
	if confidence.Valid {
		item.AgentConfidence = &confidence.Float64
	}
	item.ReviewedBy = nullStringPtr(reviewedBy)
	item.ReviewedAt = nullTimePtr(reviewedAt)
	item.ReviewReason = nullStringPtr(reviewReason)
	item.ExecutedAt = nullTimePtr(executedAt)
	item.ExecutionError = nullStringPtr(executionError)
	return nil
}

func nullTimePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func nullableString(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	return *v
}
