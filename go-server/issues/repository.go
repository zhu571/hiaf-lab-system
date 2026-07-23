package issues

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(projectID, authorID string, req CreateIssueRequest, occurredAt time.Time, reportDate string) (*Issue, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin create issue: %w", err)
	}
	defer rollback(tx)

	var out Issue
	err = scanIssue(tx.QueryRow(
		`INSERT INTO issues
		 (project_id, title, description, severity, author_id, assignee_id, run_id, report_date, occurred_at, ai_generated, agent_task_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8, $9, $10)
		 RETURNING id, project_id, title, description, status, severity, author_id, assignee_id,
		           ai_generated, agent_task_id, run_id, report_date, occurred_at, resolved_at, created_at, updated_at`,
		projectID, strings.TrimSpace(req.Title), req.Description, defaultSeverity(req.Severity),
		authorID, nullableStringPtr(req.AssigneeID), reportDate, occurredAt, req.AiGenerated, nullableStringPtr(req.AgentTaskID),
	), &out)
	if err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}

	for _, logID := range req.RelatedLogIDs {
		if _, err := tx.Exec(
			`INSERT INTO issue_log_links (issue_id, log_id) VALUES ($1, $2)`,
			out.ID, logID,
		); err != nil {
			return nil, fmt.Errorf("link issue log: %w", err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO issue_project_links (issue_id, project_id, relation)
		 VALUES ($1, $2, 'primary')`,
		out.ID, projectID,
	); err != nil {
		return nil, fmt.Errorf("link issue project: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create issue: %w", err)
	}
	return &out, nil
}

func (r *Repository) GetByID(id string) (*Issue, error) {
	var out Issue
	err := scanIssue(r.db.QueryRow(
		`SELECT id, project_id, title, description, status, severity, author_id, assignee_id,
		        ai_generated, agent_task_id, run_id, report_date, occurred_at, resolved_at, created_at, updated_at
		 FROM issues
		 WHERE id = $1`,
		id,
	), &out)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get issue: %w", err)
	}

	comments, err := r.getComments(id, 1, 50)
	if err != nil {
		return nil, err
	}
	out.Comments = comments
	return &out, nil
}

func (r *Repository) List(projectID string, params IssueListParams) ([]Issue, int, error) {
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	where, args := buildIssueWhere(projectID, params)

	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM issues `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count issues: %w", err)
	}

	args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	rows, err := r.db.Query(
		`SELECT id, project_id, title, description, status, severity, author_id, assignee_id,
		        ai_generated, agent_task_id, run_id, report_date, occurred_at, resolved_at, created_at, updated_at
		 FROM issues `+where+fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", issueOrderBy(params), len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list issues: %w", err)
	}
	defer rows.Close()

	items, err := scanIssues(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *Repository) Update(id string, req UpdateIssueRequest) (*Issue, error) {
	var out Issue
	err := scanIssue(r.db.QueryRow(
		`UPDATE issues
		 SET title = COALESCE(NULLIF($2, ''), title),
		     description = COALESCE($3, description),
		     severity = COALESCE(NULLIF($4, ''), severity),
		     assignee_id = COALESCE($5, assignee_id),
		     updated_at = now()
		 WHERE id = $1
		 RETURNING id, project_id, title, description, status, severity, author_id, assignee_id,
		           ai_generated, agent_task_id, run_id, report_date, occurred_at, resolved_at, created_at, updated_at`,
		id, stringPtrValue(req.Title), req.Description, stringPtrValue(req.Severity), nullableStringPtr(req.AssigneeID),
	), &out)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update issue: %w", err)
	}
	return &out, nil
}

func (r *Repository) TransitionStatus(id, targetStatus, userID, comment string, addComment bool) (*Issue, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transition issue: %w", err)
	}
	defer rollback(tx)

	var out Issue
	// 状态值需传两次：SET 子句会把 $2 推断为 varchar，而 CASE 中的字符串比较
	// 会把同一参数推断为 text，导致 PostgreSQL 报 42P08 类型冲突，故 CASE 改用 $3。
	err = scanIssue(tx.QueryRow(
		`UPDATE issues
		 SET status = $2,
		     resolved_at = CASE WHEN $3 = 'resolved' THEN now()
		                        WHEN $3 = 'open' THEN NULL
		                        ELSE resolved_at END,
		     updated_at = now()
		 WHERE id = $1
		 RETURNING id, project_id, title, description, status, severity, author_id, assignee_id,
		           ai_generated, agent_task_id, run_id, report_date, occurred_at, resolved_at, created_at, updated_at`,
		id, targetStatus, targetStatus,
	), &out)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("transition issue: %w", err)
	}

	if addComment {
		if _, err := tx.Exec(
			`INSERT INTO issue_comments (issue_id, author_id, content) VALUES ($1, $2, $3)`,
			id, userID, comment,
		); err != nil {
			return nil, fmt.Errorf("insert transition comment: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transition issue: %w", err)
	}
	return &out, nil
}

func (r *Repository) AddComment(issueID, authorID, content string) (*Comment, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin add comment: %w", err)
	}
	defer rollback(tx)

	var out Comment
	if err := scanComment(tx.QueryRow(
		`INSERT INTO issue_comments (issue_id, author_id, content)
		 VALUES ($1, $2, $3)
		 RETURNING id, issue_id, author_id, content, created_at`,
		issueID, authorID, content,
	), &out); err != nil {
		return nil, fmt.Errorf("insert issue comment: %w", err)
	}
	if _, err := tx.Exec(`UPDATE issues SET updated_at = now() WHERE id = $1`, issueID); err != nil {
		return nil, fmt.Errorf("touch issue after comment: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit add comment: %w", err)
	}
	return &out, nil
}

func (r *Repository) GetComments(issueID string, page, perPage int) ([]Comment, error) {
	return r.getComments(issueID, page, perPage)
}

func (r *Repository) CountRelatedLogs(projectID string, logIDs []string) (int, error) {
	if len(logIDs) == 0 {
		return 0, nil
	}
	args := []any{projectID}
	placeholders := make([]string, 0, len(logIDs))
	for _, id := range logIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	var count int
	if err := r.db.QueryRow(
		`SELECT COUNT(*) FROM logs
		 WHERE project_id = $1 AND id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count related logs: %w", err)
	}
	return count, nil
}

func (r *Repository) CountLogsByIDs(logIDs []string) (int, error) {
	if len(logIDs) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(logIDs))
	placeholders := make([]string, 0, len(logIDs))
	for _, id := range logIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	var count int
	if err := r.db.QueryRow(
		`SELECT COUNT(*) FROM logs WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count logs by ids: %w", err)
	}
	return count, nil
}

func (r *Repository) getComments(issueID string, page, perPage int) ([]Comment, error) {
	page, perPage = normalizePage(page, perPage)
	if perPage > 100 {
		perPage = 100
	}
	rows, err := r.db.Query(
		`SELECT id, issue_id, author_id, content, created_at
		 FROM (
		     SELECT id, issue_id, author_id, content, created_at
		     FROM issue_comments
		     WHERE issue_id = $1
		     ORDER BY created_at DESC
		     LIMIT $2 OFFSET $3
		 ) recent
		 ORDER BY created_at ASC`,
		issueID, perPage, (page-1)*perPage,
	)
	if err != nil {
		return nil, fmt.Errorf("get issue comments: %w", err)
	}
	defer rows.Close()
	return scanComments(rows)
}

func buildIssueWhere(projectID string, params IssueListParams) (string, []any) {
	args := []any{projectID}
	parts := []string{"project_id = $1"}
	if params.Status != "" {
		args = append(args, params.Status)
		parts = append(parts, fmt.Sprintf("status = $%d", len(args)))
	}
	if params.Severity != "" {
		args = append(args, params.Severity)
		parts = append(parts, fmt.Sprintf("severity = $%d", len(args)))
	}
	if params.Assignee != "" {
		args = append(args, params.Assignee)
		parts = append(parts, fmt.Sprintf("assignee_id = $%d", len(args)))
	}
	if params.Author != "" {
		args = append(args, params.Author)
		parts = append(parts, fmt.Sprintf("author_id = $%d", len(args)))
	}
	if strings.TrimSpace(params.Search) != "" {
		args = append(args, "%"+strings.TrimSpace(params.Search)+"%")
		parts = append(parts, fmt.Sprintf("(title ILIKE $%d OR description ILIKE $%d)", len(args), len(args)))
	}
	return "WHERE " + strings.Join(parts, " AND "), args
}

func issueOrderBy(params IssueListParams) string {
	order := "DESC"
	if strings.EqualFold(params.Order, "asc") {
		order = "ASC"
	}
	switch strings.TrimSpace(params.Sort) {
	case "created":
		return "created_at " + order
	case "updated":
		return "updated_at " + order
	case "severity", "":
		if order == "ASC" {
			return "CASE severity WHEN 'low' THEN 1 WHEN 'medium' THEN 2 WHEN 'high' THEN 3 WHEN 'critical' THEN 4 ELSE 0 END ASC, created_at DESC"
		}
		return "CASE severity WHEN 'critical' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END DESC, created_at DESC"
	default:
		return "CASE severity WHEN 'critical' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END DESC, created_at DESC"
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanIssue(row rowScanner, item *Issue) error {
	var reportDate time.Time
		var assigneeID, agentTaskID, runID sql.NullString
	var resolvedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.ProjectID, &item.Title, &item.Description, &item.Status, &item.Severity,
		&item.AuthorID, &assigneeID, &item.AiGenerated, &agentTaskID, &runID, &reportDate, &item.OccurredAt, &resolvedAt, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return err
	}
	if assigneeID.Valid {
		item.AssigneeID = &assigneeID.String
	}
		if agentTaskID.Valid {
			item.AgentTaskID = &agentTaskID.String
		}
		if runID.Valid {
			item.RunID = &runID.String
		}
	item.ReportDate = reportDate.Format(time.DateOnly)
	if resolvedAt.Valid {
		item.ResolvedAt = &resolvedAt.Time
	}
	return nil
}

func scanIssues(rows *sql.Rows) ([]Issue, error) {
	var out []Issue
	for rows.Next() {
		var item Issue
		if err := scanIssue(rows, &item); err != nil {
			return nil, fmt.Errorf("scan issue: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issues: %w", err)
	}
	return out, nil
}

func scanComment(row rowScanner, item *Comment) error {
	return row.Scan(&item.ID, &item.IssueID, &item.AuthorID, &item.Content, &item.CreatedAt)
}

func scanComments(rows *sql.Rows) ([]Comment, error) {
	var out []Comment
	for rows.Next() {
		var item Comment
		if err := scanComment(rows, &item); err != nil {
			return nil, fmt.Errorf("scan issue comment: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issue comments: %w", err)
	}
	return out, nil
}

func normalizePage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	return page, perPage
}

func defaultSeverity(v string) string {
	if strings.TrimSpace(v) == "" {
		return SeverityMedium
	}
	return strings.TrimSpace(v)
}

func nullableStringPtr(s *string) sql.NullString {
	if s == nil || strings.TrimSpace(*s) == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: strings.TrimSpace(*s), Valid: true}
}

func stringPtrValue(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}
