package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// recordColumns 是 audit_log 全量输出列（含 014 代理四列与 029 hash 链两列），
// 各查询共用同一份列清单，与 scanRecord 的扫描顺序一一对应，防漂移。
const recordColumns = `id, request_id, user_id, username, method, path, action, status_code, client_ip,
                       detail, created_at, actor_type, acting_user_id, agent_task_id, idempotency_key,
                       prev_hash, hash`

// genesisPrevHash 创世块 prev_hash，与 029 触发器/回填的常量语义一致。
const genesisPrevHash = "0000000000000000000000000000000000000000000000000000000000000000"

// Service 提供 audit_log 的只读查询。audit_log 归本模块与 middleware 管辖，
// 其他模块（如 agent trace）必须经本 Service 注入读取，不得直接 SELECT（模块单向依赖）。
type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service { return &Service{db: db} }

// ListByRequestID 返回某 request_id 的全部审计行（/api/v1/audit/{request_id}）。
func (s *Service) ListByRequestID(ctx context.Context, requestID string) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+recordColumns+` FROM audit_log WHERE request_id = $1 ORDER BY created_at ASC`,
		requestID,
	)
	if err != nil {
		return nil, fmt.Errorf("list audit by request id: %w", err)
	}
	defer rows.Close()
	return scanRecords(rows)
}

// ListByAgentTaskID 返回某 Agent 任务相关的全部审计行，按写入顺序升序。
func (s *Service) ListByAgentTaskID(taskID string) ([]Record, error) {
	rows, err := s.db.Query(
		`SELECT `+recordColumns+` FROM audit_log WHERE agent_task_id = $1 ORDER BY id ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("list audit by agent task: %w", err)
	}
	defer rows.Close()
	return scanRecords(rows)
}

// VerifyResult 是 /api/v1/audit/verify 的响应体。
type VerifyResult struct {
	Valid         bool   `json:"valid"`
	Total         int64  `json:"total"`
	Checked       int64  `json:"checked"`
	FirstBrokenID *int64 `json:"first_broken_id"`
	Message       string `json:"message"`
}

// VerifyChain 重算 hash 链并逐条比对，O(n) 单趟。
// fromID/toID 为 0 表示不设界；fromID>0 时取前一行 hash 作锚点（增量抽查信任锚点之前的链）。
// sha256 输入与 029 触发器严格同式：prev_hash|audit_chain_content(...)。
func (s *Service) VerifyChain(ctx context.Context, fromID, toID int64) (VerifyResult, error) {
	var res VerifyResult
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log`).Scan(&res.Total); err != nil {
		return res, fmt.Errorf("count audit_log: %w", err)
	}

	prev := genesisPrevHash
	if fromID > 0 {
		var anchor sql.NullString
		err := s.db.QueryRowContext(ctx,
			`SELECT hash FROM audit_log WHERE id < $1 ORDER BY id DESC LIMIT 1`, fromID,
		).Scan(&anchor)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// from_id 之前无行：等价于从创世块校验。
		case err != nil:
			return res, fmt.Errorf("fetch anchor hash: %w", err)
		case !anchor.Valid:
			res.Message = "锚点行未接入链，无法增量校验"
			return res, nil
		default:
			prev = anchor.String
		}
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, prev_hash, hash,
		        audit_chain_content(id, request_id, user_id, username, method, path, action,
		                            status_code, client_ip, detail, actor_type, acting_user_id,
		                            agent_task_id, idempotency_key, created_at)
		 FROM audit_log
		 WHERE ($1 = 0 OR id >= $1) AND ($2 = 0 OR id <= $2)
		 ORDER BY id ASC`, fromID, toID,
	)
	if err != nil {
		return res, fmt.Errorf("query audit chain: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var prevHash, hash sql.NullString
		var content string
		if err := rows.Scan(&id, &prevHash, &hash, &content); err != nil {
			return res, fmt.Errorf("scan audit chain row: %w", err)
		}
		res.Checked++
		broken := false
		msg := ""
		switch {
		case !prevHash.Valid || !hash.Valid:
			broken = true
			msg = fmt.Sprintf("第 %d 行未接入链（hash 为空）", id)
		case prevHash.String != prev:
			broken = true
			msg = fmt.Sprintf("第 %d 行 prev_hash 与前一行断链", id)
		default:
			sum := sha256.Sum256([]byte(prev + "|" + content))
			if hex.EncodeToString(sum[:]) != hash.String {
				broken = true
				msg = fmt.Sprintf("第 %d 行内容与 hash 不匹配（疑似篡改）", id)
			}
		}
		if broken {
			res.FirstBrokenID = &id
			res.Message = msg
			return res, nil
		}
		prev = hash.String
	}
	if err := rows.Err(); err != nil {
		return res, fmt.Errorf("iterate audit chain: %w", err)
	}
	res.Valid = true
	res.Message = "链校验通过"
	return res, nil
}

// EventFilter 是 /api/v1/audit/events 的过滤条件（零值字段不过滤）。
type EventFilter struct {
	Action    string
	UserID    string
	ActorType string
	From      time.Time
	To        time.Time
	Page      int
	PerPage   int
}

// EventList 是 events 端点的分页结果，最新写入在前。
type EventList struct {
	Items   []Record `json:"items"`
	Total   int      `json:"total"`
	Page    int      `json:"page"`
	PerPage int      `json:"per_page"`
}

// ListEvents 按过滤条件分页查询审计行（/api/v1/audit/events）。
func (s *Service) ListEvents(ctx context.Context, f EventFilter) (*EventList, error) {
	where := []string{"TRUE"}
	args := []any{}
	if f.Action != "" {
		args = append(args, f.Action)
		where = append(where, fmt.Sprintf("action = $%d", len(args)))
	}
	if f.UserID != "" {
		args = append(args, f.UserID)
		where = append(where, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if f.ActorType != "" {
		args = append(args, f.ActorType)
		where = append(where, fmt.Sprintf("actor_type = $%d", len(args)))
	}
	if !f.From.IsZero() {
		args = append(args, f.From)
		where = append(where, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if !f.To.IsZero() {
		args = append(args, f.To)
		where = append(where, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM audit_log WHERE `+clause, args...,
	).Scan(&total); err != nil {
		return nil, fmt.Errorf("count audit events: %w", err)
	}

	args = append(args, f.PerPage, (f.Page-1)*f.PerPage)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+recordColumns+` FROM audit_log WHERE `+clause+
			fmt.Sprintf(` ORDER BY id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()

	items, err := scanRecords(rows)
	if err != nil {
		return nil, err
	}
	return &EventList{Items: items, Total: total, Page: f.Page, PerPage: f.PerPage}, nil
}

func scanRecords(rows *sql.Rows) ([]Record, error) {
	records := []Record{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit records: %w", err)
	}
	return records, nil
}
