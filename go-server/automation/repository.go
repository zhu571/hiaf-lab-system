package automation

import (
	"context"
	"database/sql"
)

// Repository 只访问本模块表 automation_rules（模块铁律：
// 入队 pending_agent_tasks 由 DB 触发器负责，本模块不跨模块写）。
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

const ruleColumns = `id, name, trigger_event, action, enabled, created_by, created_at, updated_at`

func (r *Repository) List(ctx context.Context) ([]Rule, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+ruleColumns+` FROM automation_rules ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Rule, 0)
	for rows.Next() {
		var rule Rule
		var createdBy sql.NullString
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.TriggerEvent, &rule.Action,
			&rule.Enabled, &createdBy, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			rule.CreatedBy = &createdBy.String
		}
		items = append(items, rule)
	}
	return items, rows.Err()
}

func (r *Repository) Create(ctx context.Context, rule Rule) (Rule, error) {
	var created Rule
	var createdBy sql.NullString
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO automation_rules (name, trigger_event, action, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+ruleColumns,
		rule.Name, rule.TriggerEvent, rule.Action, rule.CreatedBy,
	).Scan(&created.ID, &created.Name, &created.TriggerEvent, &created.Action,
		&created.Enabled, &createdBy, &created.CreatedAt, &created.UpdatedAt)
	if createdBy.Valid {
		created.CreatedBy = &createdBy.String
	}
	return created, err
}

// SetEnabled 切换开关并刷新 updated_at；sql.ErrNoRows 由 service 映射为未找到。
func (r *Repository) SetEnabled(ctx context.Context, id string, enabled bool) (Rule, error) {
	var rule Rule
	var createdBy sql.NullString
	err := r.db.QueryRowContext(ctx,
		`UPDATE automation_rules SET enabled = $2, updated_at = now()
		 WHERE id = $1 RETURNING `+ruleColumns,
		id, enabled,
	).Scan(&rule.ID, &rule.Name, &rule.TriggerEvent, &rule.Action,
		&rule.Enabled, &createdBy, &rule.CreatedAt, &rule.UpdatedAt)
	if createdBy.Valid {
		rule.CreatedBy = &createdBy.String
	}
	return rule, err
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM automation_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
