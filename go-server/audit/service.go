package audit

import (
	"database/sql"
	"fmt"
)

// Service 提供 audit_log 的只读查询。audit_log 归本模块与 middleware 管辖，
// 其他模块（如 agent trace）必须经本 Service 注入读取，不得直接 SELECT（模块单向依赖）。
type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service { return &Service{db: db} }

// ListByAgentTaskID 返回某 Agent 任务相关的全部审计行，按写入顺序升序。
func (s *Service) ListByAgentTaskID(taskID string) ([]Record, error) {
	rows, err := s.db.Query(
		`SELECT id, request_id, user_id, username, method, path, action, status_code, client_ip,
		        detail, created_at, actor_type, acting_user_id, agent_task_id, idempotency_key
		 FROM audit_log
		 WHERE agent_task_id = $1
		 ORDER BY id ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("list audit by agent task: %w", err)
	}
	defer rows.Close()

	records := []Record{}
	for rows.Next() {
		rec, err := scanRecordFull(rows)
		if err != nil {
			return nil, fmt.Errorf("scan audit record: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit records: %w", err)
	}
	return records, nil
}
