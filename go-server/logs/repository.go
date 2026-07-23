package logs

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

func (r *Repository) GetOrCreateTodayReport(authorID, reportDate string) (*DailyReport, error) {
	report, err := r.GetReportByDate(authorID, reportDate)
	if err != nil {
		return nil, err
	}
	if report != nil {
		return report, nil
	}

	var out DailyReport
	err = scanDailyReport(r.db.QueryRow(
		`INSERT INTO daily_reports (report_date, author_id)
		 VALUES ($1::date, $2)
		 ON CONFLICT (report_date, author_id)
		 DO NOTHING
		 RETURNING id, report_date, author_id, raw_text, summary, content_status, quality_status, created_at, updated_at`,
		reportDate, authorID,
	), &out)
	if err != nil {
		if err == sql.ErrNoRows {
			return r.GetReportByDate(authorID, reportDate)
		}
		return nil, fmt.Errorf("get or create daily report: %w", err)
	}
	return &out, nil
}

func (r *Repository) GetReportByID(id string) (*DailyReport, error) {
	return r.getReport(`WHERE id = $1`, id)
}

func (r *Repository) GetReportByDate(authorID, reportDate string) (*DailyReport, error) {
	var out DailyReport
	err := scanDailyReport(r.db.QueryRow(
		`SELECT id, report_date, author_id, raw_text, summary, content_status, quality_status, created_at, updated_at
		 FROM daily_reports
		 WHERE author_id = $1 AND report_date = $2::date`,
		authorID, reportDate,
	), &out)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get daily report by date: %w", err)
	}
	return &out, nil
}

func (r *Repository) ListReports(params ReportListParams) ([]DailyReport, int, error) {
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	where, args := buildReportWhere(params)

	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM daily_reports dr `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count daily reports: %w", err)
	}

	args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	rows, err := r.db.Query(
		`SELECT dr.id, dr.report_date, dr.author_id, u.display_name, dr.raw_text, dr.summary,
		        dr.content_status, dr.quality_status, dr.created_at, dr.updated_at
		 FROM daily_reports dr
		 JOIN users u ON u.id = dr.author_id `+where+fmt.Sprintf(` ORDER BY dr.report_date DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list daily reports: %w", err)
	}
	defer rows.Close()

	items := []DailyReport{}
	for rows.Next() {
		var item DailyReport
		var reportDate time.Time
		if err := rows.Scan(
			&item.ID, &reportDate, &item.AuthorID, &item.AuthorName, &item.RawText, &item.Summary,
			&item.ContentStatus, &item.QualityStatus, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan daily report: %w", err)
		}
		item.ReportDate = reportDate.Format(time.DateOnly)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate daily reports: %w", err)
	}
	return items, total, nil
}

func buildReportWhere(params ReportListParams) (string, []any) {
	args := []any{}
	parts := []string{}
	if strings.TrimSpace(params.AuthorID) != "" {
		args = append(args, strings.TrimSpace(params.AuthorID))
		parts = append(parts, fmt.Sprintf("dr.author_id::text = $%d", len(args)))
	}
	if strings.TrimSpace(params.Status) != "" {
		args = append(args, strings.TrimSpace(params.Status))
		parts = append(parts, fmt.Sprintf("dr.content_status = $%d", len(args)))
	}
	if strings.TrimSpace(params.Keyword) != "" {
		args = append(args, "%"+strings.TrimSpace(params.Keyword)+"%")
		parts = append(parts, fmt.Sprintf("(dr.summary ILIKE $%d OR dr.raw_text ILIKE $%d)", len(args), len(args)))
	}
	if strings.TrimSpace(params.Date) != "" {
		args = append(args, strings.TrimSpace(params.Date))
		parts = append(parts, fmt.Sprintf("dr.report_date = $%d::date", len(args)))
	}
	if len(parts) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(parts, " AND "), args
}

func (r *Repository) UpdateReport(id, rawText string) error {
	res, err := r.db.Exec(
		`UPDATE daily_reports
		 SET raw_text = $2, updated_at = now()
		 WHERE id = $1 AND content_status = 'draft'`,
		id, rawText,
	)
	if err != nil {
		return fmt.Errorf("update daily report: %w", err)
	}
	return requireAffected(res)
}

func (r *Repository) SubmitReport(id, qualityStatus string) (*DailyReport, error) {
	var out DailyReport
	err := scanDailyReport(r.db.QueryRow(
		`UPDATE daily_reports
		 SET content_status = 'submitted', quality_status = $2, updated_at = now()
		 WHERE id = $1 AND content_status = 'draft'
		 RETURNING id, report_date, author_id, raw_text, summary, content_status, quality_status, created_at, updated_at`,
		id, qualityStatus,
	), &out)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("submit daily report: %w", err)
	}
	return &out, nil
}

func (r *Repository) ConfirmReport(id string) error {
	res, err := r.db.Exec(
		`UPDATE daily_reports
		 SET content_status = 'confirmed', updated_at = now()
		 WHERE id = $1 AND content_status = 'submitted'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("confirm daily report: %w", err)
	}
	return requireAffected(res)
}

func (r *Repository) LockReport(id string) error {
	res, err := r.db.Exec(
		`UPDATE daily_reports
		 SET content_status = 'locked', updated_at = now()
		 WHERE id = $1 AND content_status = 'confirmed'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("lock daily report: %w", err)
	}
	return requireAffected(res)
}

func (r *Repository) CreateLog(projectID, authorID string, req CreateLogRequest, occurredAt time.Time) (*Log, error) {
	category := repoDefaultString(req.Category, CategoryGeneral)
	source := repoDefaultString(req.Source, SourceManual)

	var out Log
	err := scanLog(r.db.QueryRow(
		`INSERT INTO logs (project_id, author_id, occurred_at, category, content, source)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, project_id, author_id, occurred_at, category, content, source, content_status, created_at, updated_at`,
		projectID, authorID, occurredAt, category, req.Content, source,
	), &out)
	if err != nil {
		return nil, fmt.Errorf("create log: %w", err)
	}
	return &out, nil
}

func (r *Repository) GetByID(id string) (*Log, error) {
	var out Log
	err := scanLog(r.db.QueryRow(
		`SELECT id, project_id, author_id, occurred_at, category, content, source, content_status, created_at, updated_at
		 FROM logs
		 WHERE id = $1`,
		id,
	), &out)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get log: %w", err)
	}
	return &out, nil
}

func (r *Repository) List(projectID string, params LogListParams) ([]Log, int, error) {
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	if params.PerPage > 100 {
		params.PerPage = 100
	}
	if params.Status == "" {
		params.Status = LogStatusConfirmed
	}

	where, args := buildLogWhere(projectID, params)
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM logs `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count logs: %w", err)
	}

	args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	rows, err := r.db.Query(
		`SELECT id, project_id, author_id, occurred_at, category, content, source, content_status, created_at, updated_at
		 FROM logs `+where+fmt.Sprintf(` ORDER BY occurred_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list logs: %w", err)
	}
	defer rows.Close()

	items, err := scanLogs(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *Repository) UpdateLog(id string, req UpdateLogRequest, occurredAt *time.Time) (*Log, error) {
	var out Log
	err := scanLog(r.db.QueryRow(
		`UPDATE logs
		 SET category = COALESCE(NULLIF($2, ''), category),
		     content = COALESCE($3, content),
		     occurred_at = COALESCE($4, occurred_at),
		     content_status = COALESCE($5, content_status),
		     updated_at = now()
		 WHERE id = $1 AND content_status = 'draft'
		 RETURNING id, project_id, author_id, occurred_at, category, content, source, content_status, created_at, updated_at`,
		id, repoStringPtrValue(req.Category), req.Content, occurredAt, req.ContentStatus,
	), &out)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update log: %w", err)
	}
	return &out, nil
}

func (r *Repository) ConfirmLog(id string) error {
	res, err := r.db.Exec(
		`UPDATE logs
		 SET content_status = 'confirmed', updated_at = now()
		 WHERE id = $1 AND content_status = 'draft'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("confirm log: %w", err)
	}
	return requireAffected(res)
}

func (r *Repository) VoidLog(id string) error {
	res, err := r.db.Exec(
		`UPDATE logs
		 SET content_status = 'voided', updated_at = now()
		 WHERE id = $1 AND content_status IN ('draft','confirmed')`,
		id,
	)
	if err != nil {
		return fmt.Errorf("void log: %w", err)
	}
	return requireAffected(res)
}

func (r *Repository) LinkLogToReport(reportID, logID string) error {
	if _, err := r.db.Exec(
		`INSERT INTO daily_report_log_links (daily_report_id, log_id)
		 VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`,
		reportID, logID,
	); err != nil {
		return fmt.Errorf("link log to report: %w", err)
	}
	return nil
}

func (r *Repository) GetLogsByReport(reportID string) ([]Log, error) {
	rows, err := r.db.Query(
		`SELECT l.id, l.project_id, l.author_id, l.occurred_at, l.category, l.content, l.source, l.content_status, l.created_at, l.updated_at
		 FROM logs l
		 JOIN daily_report_log_links link ON link.log_id = l.id
		 WHERE link.daily_report_id = $1
		 ORDER BY l.occurred_at ASC`,
		reportID,
	)
	if err != nil {
		return nil, fmt.Errorf("get logs by report: %w", err)
	}
	defer rows.Close()
	return scanLogs(rows)
}

func (r *Repository) GetReportsByLog(logID string) ([]DailyReport, error) {
	rows, err := r.db.Query(
		`SELECT dr.id, dr.report_date, dr.author_id, dr.raw_text, dr.summary, dr.content_status, dr.quality_status, dr.created_at, dr.updated_at
		 FROM daily_reports dr
		 JOIN daily_report_log_links link ON link.daily_report_id = dr.id
		 WHERE link.log_id = $1
		 ORDER BY dr.report_date DESC`,
		logID,
	)
	if err != nil {
		return nil, fmt.Errorf("get reports by log: %w", err)
	}
	defer rows.Close()
	return scanDailyReports(rows)
}

func (r *Repository) CountByProject(projectID string) (int, error) {
	var count int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM logs WHERE project_id = $1`,
		projectID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count logs by project: %w", err)
	}
	return count, nil
}

func (r *Repository) CountLogsByReport(reportID string) (int, error) {
	var count int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM daily_report_log_links WHERE daily_report_id = $1`, reportID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count logs by report: %w", err)
	}
	return count, nil
}

func (r *Repository) getReport(where string, arg any) (*DailyReport, error) {
	var out DailyReport
	err := scanDailyReport(r.db.QueryRow(
		`SELECT id, report_date, author_id, raw_text, summary, content_status, quality_status, created_at, updated_at
		 FROM daily_reports `+where,
		arg,
	), &out)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get daily report: %w", err)
	}
	return &out, nil
}

func buildLogWhere(projectID string, params LogListParams) (string, []any) {
	args := []any{projectID}
	parts := []string{"project_id = $1"}
	if params.Category != "" {
		args = append(args, params.Category)
		parts = append(parts, fmt.Sprintf("category = $%d", len(args)))
	}
	if params.DateFrom != "" {
		args = append(args, params.DateFrom)
		parts = append(parts, fmt.Sprintf("occurred_at >= $%d::timestamptz", len(args)))
	}
	if params.DateTo != "" {
		args = append(args, params.DateTo)
		parts = append(parts, fmt.Sprintf("occurred_at <= $%d::timestamptz", len(args)))
	}
	if params.Status != "" {
		args = append(args, params.Status)
		parts = append(parts, fmt.Sprintf("content_status = $%d", len(args)))
	}
	return "WHERE " + strings.Join(parts, " AND "), args
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDailyReport(row rowScanner, report *DailyReport) error {
	var reportDate time.Time
	if err := row.Scan(
		&report.ID, &reportDate, &report.AuthorID, &report.RawText, &report.Summary,
		&report.ContentStatus, &report.QualityStatus, &report.CreatedAt, &report.UpdatedAt,
	); err != nil {
		return err
	}
	report.ReportDate = reportDate.Format(time.DateOnly)
	return nil
}

func scanDailyReports(rows *sql.Rows) ([]DailyReport, error) {
	var out []DailyReport
	for rows.Next() {
		var item DailyReport
		if err := scanDailyReport(rows, &item); err != nil {
			return nil, fmt.Errorf("scan daily report: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily reports: %w", err)
	}
	return out, nil
}

func scanLog(row rowScanner, item *Log) error {
	return row.Scan(
		&item.ID, &item.ProjectID, &item.AuthorID, &item.OccurredAt, &item.Category,
		&item.Content, &item.Source, &item.ContentStatus, &item.CreatedAt, &item.UpdatedAt,
	)
}

func scanLogs(rows *sql.Rows) ([]Log, error) {
	var out []Log
	for rows.Next() {
		var item Log
		if err := scanLog(rows, &item); err != nil {
			return nil, fmt.Errorf("scan log: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate logs: %w", err)
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

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func repoDefaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func repoStringPtrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
