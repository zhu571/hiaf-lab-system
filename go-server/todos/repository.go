package todos

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Repository 只访问 todos 表（跨表直读一律在 snapshot.go，禁止本文件跨表 SQL）。
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

const todoColumns = `id, title, priority, status, source, created_by, created_for, project_id, issue_id,
	completed_at, completed_by, created_at, updated_at`

// Create 插入一条待办并返回完整记录。
func (r *Repository) Create(t *Todo) (*Todo, error) {
	out := &Todo{}
	err := scanTodo(r.db.QueryRow(
		`INSERT INTO todos (title, priority, status, source, created_by, created_for, project_id, issue_id)
		 VALUES ($1, $2, $3, $4, $5, $6::date, $7, $8)
		 RETURNING `+todoColumns,
		t.Title, t.Priority, t.Status, t.Source, t.CreatedBy, t.CreatedFor,
		nullableString(t.ProjectID), nullableString(t.IssueID),
	), out)
	if err != nil {
		return nil, fmt.Errorf("create todo: %w", err)
	}
	return out, nil
}

// InsertGenerated 插入一条生成待办；命中 issue 在途唯一索引冲突则跳过（返回 false）。
func (r *Repository) InsertGenerated(t *Todo) (bool, error) {
	err := scanTodo(r.db.QueryRow(
		`INSERT INTO todos (title, priority, status, source, created_by, created_for, project_id, issue_id)
		 VALUES ($1, $2, $3, $4, $5, $6::date, $7, $8)
		 ON CONFLICT (issue_id) WHERE status IN ('pending','deferred') AND issue_id IS NOT NULL DO NOTHING
		 RETURNING `+todoColumns,
		t.Title, t.Priority, t.Status, t.Source, t.CreatedBy, t.CreatedFor,
		nullableString(t.ProjectID), nullableString(t.IssueID),
	), t)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert generated todo: %w", err)
	}
	return true, nil
}

func (r *Repository) GetByID(id string) (*Todo, error) {
	out := &Todo{}
	err := scanTodo(r.db.QueryRow(`SELECT `+todoColumns+` FROM todos WHERE id = $1`, id), out)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get todo: %w", err)
	}
	return out, nil
}

// List 按可见性与状态过滤：scope=mine 只看 created_by；scope=shared/all 追加共享可见
// （project_id IN projectIDs）。projectIDs 为空时共享条件恒假（非成员 200 空列表）。
func (r *Repository) List(userID string, projectIDs []string, p ListParams) ([]Todo, error) {
	where, args := buildListWhere(userID, projectIDs, p)
	if p.Limit <= 0 {
		p.Limit = 100
	}
	args = append(args, p.Limit)
	rows, err := r.db.Query(
		`SELECT t.id, t.title, t.priority, t.status, t.source, t.created_by, t.created_for,
		        t.project_id, t.issue_id, t.completed_at, t.completed_by, t.created_at, t.updated_at,
		        COALESCE(u.display_name, u.username, '')
		 FROM todos t
		 LEFT JOIN users u ON u.id = t.created_by
		 `+where+` ORDER BY CASE t.priority WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END, t.created_at
		 LIMIT `+fmt.Sprintf("$%d", len(args)),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list todos: %w", err)
	}
	defer rows.Close()
	out := []Todo{}
	for rows.Next() {
		item, err := scanTodoRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan todo: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func buildListWhere(userID string, projectIDs []string, p ListParams) (string, []any) {
	// 参数按引用顺序追加（scope=shared 无项目时不引用 userID，避免 42P18 类型推断失败）。
	parts := []string{"t.created_for = $1::date"}
	args := []any{p.Date}
	// scope 口径（方案 §2 line 238）：mine = created_by me；
	// shared = project_id IN 我的 active 项目（含我添加的共享项，不含个人项）；
	// all = mine ∪ shared。非成员 shared → 恒假 → 200 空列表。
	switch p.Scope {
	case ScopeMine:
		parts = append(parts, "t.created_by = $2")
		args = append(args, userID)
	case ScopeShared:
		if len(projectIDs) == 0 {
			parts = append(parts, "false")
		} else {
			placeholders := make([]string, len(projectIDs))
			for i, id := range projectIDs {
				args = append(args, id)
				placeholders[i] = fmt.Sprintf("$%d", len(args))
			}
			parts = append(parts, "t.project_id IS NOT NULL AND t.project_id IN ("+strings.Join(placeholders, ",")+")")
		}
	default: // all
		args = append(args, userID)
		if len(projectIDs) == 0 {
			parts = append(parts, "t.created_by = $2")
		} else {
			placeholders := make([]string, len(projectIDs))
			for i, id := range projectIDs {
				args = append(args, id)
				placeholders[i] = fmt.Sprintf("$%d", len(args))
			}
			parts = append(parts, "(t.created_by = $2 OR (t.project_id IS NOT NULL AND t.project_id IN ("+strings.Join(placeholders, ",")+")))")
		}
	}
	switch p.Status {
	case "open":
		parts = append(parts, "t.status IN ('pending','deferred')")
	case StatusDone:
		parts = append(parts, "t.status = 'done'")
	case StatusCancelled:
		parts = append(parts, "t.status = 'cancelled'")
	case "all":
	default:
		parts = append(parts, "t.status <> 'done' AND t.status <> 'cancelled'")
	}
	return "WHERE " + strings.Join(parts, " AND "), args
}

// UpdateDone 状态守卫：仅 pending/deferred 可完成；0 rows → false。
func (r *Repository) UpdateDone(id, completedBy string, now time.Time) (bool, error) {
	res, err := r.db.Exec(
		`UPDATE todos SET status='done', completed_at=$3, completed_by=$2, updated_at=$3
		 WHERE id=$1 AND status IN ('pending','deferred')`,
		id, completedBy, now,
	)
	if err != nil {
		return false, fmt.Errorf("update todo done: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateDefer 状态守卫：仅 pending 可推迟（deferred 须次日归一后操作）；0 rows → false。
func (r *Repository) UpdateDefer(id, tomorrow string, now time.Time) (bool, error) {
	res, err := r.db.Exec(
		`UPDATE todos SET status='deferred', created_for=$2::date, updated_at=$3
		 WHERE id=$1 AND status='pending'`,
		id, tomorrow, now,
	)
	if err != nil {
		return false, fmt.Errorf("update todo defer: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateEdit 乐观锁编辑：仅 owner 且 updated_at 匹配；0 rows → false。
// project_id 三态：字段缺席=不变；空串=取消共享（置 NULL）；非空=重新共享。
func (r *Repository) UpdateEdit(id string, oldUpdatedAt time.Time, req UpdateRequest, now time.Time) (bool, error) {
	title := req.Title
	priority := req.Priority
	var projectArg any
	if req.ProjectID != nil && *req.ProjectID != "" {
		projectArg = *req.ProjectID
	}
	res, err := r.db.Exec(
		`UPDATE todos SET title=COALESCE($3, title), priority=COALESCE($4, priority),
		        project_id=CASE WHEN $5::boolean THEN $6 ELSE project_id END,
		        updated_at=$7
		 WHERE id=$1 AND updated_at=$2`,
		id, oldUpdatedAt, nullableString(title), nullableString(priority),
		req.ProjectID != nil, projectArg, now,
	)
	if err != nil {
		return false, fmt.Errorf("update todo edit: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *Repository) Delete(id string) (bool, error) {
	res, err := r.db.Exec(`DELETE FROM todos WHERE id=$1`, id)
	if err != nil {
		return false, fmt.Errorf("delete todo: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Rollover 顺延 job（幂等，可补跑）：过期 pending/deferred 与今日 deferred 统一归一为
// created_for=today, status=pending。
func (r *Repository) Rollover(today string, now time.Time) (int64, error) {
	res, err := r.db.Exec(
		`UPDATE todos SET created_for=$1::date, status='pending', updated_at=$2
		 WHERE (created_for < $1::date AND status IN ('pending','deferred'))
		    OR (created_for = $1::date AND status = 'deferred')`,
		today, now,
	)
	if err != nil {
		return 0, fmt.Errorf("rollover todos: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// IssueSync 联动：来源 issue 到终态（resolved/closed）→ 在途待办自动 cancelled（不填完成时间）。
// terminalIDs 由注入的 issueStatusResolver 提供（两段式，本文件不直读 issues 表，见 AGENTS.md §5）；
// 空集合直接返回 0。幂等：重复执行结果一致。
func (r *Repository) IssueSync(terminalIDs []string) (int64, error) {
	if len(terminalIDs) == 0 {
		return 0, nil
	}
	res, err := r.db.Exec(
		`UPDATE todos SET status='cancelled', updated_at=now()
		 WHERE status IN ('pending','deferred') AND issue_id IS NOT NULL
		   AND issue_id = ANY($1)`,
		pq.Array(terminalIDs),
	)
	if err != nil {
		return 0, fmt.Errorf("issue sync todos: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Cleanup 清理两类历史行：done/cancelled 按 created_for、in-flight 按 created_at（30 天兜底）。
func (r *Repository) Cleanup(createdForCutoff string, createdAtCutoff time.Time) (doneCancelled, inflight int64, err error) {
	res, err := r.db.Exec(
		`DELETE FROM todos WHERE created_for < $1::date AND status IN ('done','cancelled')`,
		createdForCutoff,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("cleanup done todos: %w", err)
	}
	doneCancelled, _ = res.RowsAffected()
	res, err = r.db.Exec(
		`DELETE FROM todos WHERE created_at < $1 AND status IN ('pending','deferred')`,
		createdAtCutoff,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("cleanup inflight todos: %w", err)
	}
	inflight, _ = res.RowsAffected()
	return doneCancelled, inflight, nil
}

// OpenVisibleForUser 返回用户某日期可见的在途清单（推送口径，方案 §3：自己的待办 +
// 我 active 项目内他人共享的待办都进入我的推送；排除 done/cancelled）。
// projectIDs 为空时共享条件恒假（非成员只推自己的）。
func (r *Repository) OpenVisibleForUser(userID string, projectIDs []string, date string) ([]Todo, error) {
	args := []any{date, userID}
	vis := "t.created_by = $2"
	if len(projectIDs) > 0 {
		placeholders := make([]string, len(projectIDs))
		for i, id := range projectIDs {
			args = append(args, id)
			placeholders[i] = fmt.Sprintf("$%d", len(args))
		}
		vis = "(t.created_by = $2 OR (t.project_id IS NOT NULL AND t.project_id IN (" + strings.Join(placeholders, ",") + ")))"
	}
	rows, err := r.db.Query(
		`SELECT t.id, t.title, t.priority, t.status, t.source, t.created_by, t.created_for,
		        t.project_id, t.issue_id, t.completed_at, t.completed_by, t.created_at, t.updated_at,
		        COALESCE(u.display_name, u.username, '')
		 FROM todos t
		 LEFT JOIN users u ON u.id = t.created_by
		 WHERE t.created_for = $1::date AND t.status <> 'done' AND t.status <> 'cancelled'
		   AND `+vis+`
		 ORDER BY CASE t.priority WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END, t.created_at`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list visible open todos: %w", err)
	}
	defer rows.Close()
	out := []Todo{}
	for rows.Next() {
		item, err := scanTodoRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan open todo: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// InflightIssueIDs 返回用户已有在途待办的 issue_id 集合（生成去重）。
func (r *Repository) InflightIssueIDs(userID string) (map[string]bool, error) {
	rows, err := r.db.Query(
		`SELECT issue_id FROM todos
		 WHERE created_by = $1 AND status IN ('pending','deferred') AND issue_id IS NOT NULL`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list inflight issue ids: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan inflight issue id: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// CleanupHintCandidates 返回将进入清理窗口的行（done/cancelled 按 created_for、in-flight 按 created_at）。
func (r *Repository) CleanupHintCandidates(userID string, cutoffDate string) ([]Todo, error) {
	rows, err := r.db.Query(
		`SELECT t.id, t.title, t.priority, t.status, t.source, t.created_by, t.created_for,
		        t.project_id, t.issue_id, t.completed_at, t.completed_by, t.created_at, t.updated_at, ''
		 FROM todos t
		 WHERE t.created_by = $1 AND (
		   (t.status IN ('done','cancelled') AND t.created_for >= $2::date)
		   OR (t.status IN ('pending','deferred') AND t.created_at::date >= $2::date)
		 )`,
		userID, cutoffDate,
	)
	if err != nil {
		return nil, fmt.Errorf("list cleanup hint candidates: %w", err)
	}
	defer rows.Close()
	out := []Todo{}
	for rows.Next() {
		item, err := scanTodoRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cleanup hint todo: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// scanTodo 扫描单行（date 列经 time.Time 中转，输出 YYYY-MM-DD）。
func scanTodo(row *sql.Row, t *Todo) error {
	var createdFor time.Time
	if err := row.Scan(
		&t.ID, &t.Title, &t.Priority, &t.Status, &t.Source, &t.CreatedBy,
		&createdFor, &t.ProjectID, &t.IssueID, &t.CompletedAt, &t.CompletedBy,
		&t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return err
	}
	t.CreatedFor = createdFor.Format(time.DateOnly)
	return nil
}

// scanTodoRow 扫描带 owner 显示名的行（List/Open/CleanupHintCandidates 共用）。
type todoScanner interface {
	Scan(dest ...any) error
}

func scanTodoRow(row todoScanner) (Todo, error) {
	var item Todo
	var createdFor time.Time
	if err := row.Scan(
		&item.ID, &item.Title, &item.Priority, &item.Status, &item.Source, &item.CreatedBy,
		&createdFor, &item.ProjectID, &item.IssueID, &item.CompletedAt, &item.CompletedBy,
		&item.CreatedAt, &item.UpdatedAt, &item.OwnerDisplayName,
	); err != nil {
		return item, err
	}
	item.CreatedFor = createdFor.Format(time.DateOnly)
	return item, nil
}

func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
