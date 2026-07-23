package runs

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

const runColumns = `id, project_id, name, campaign, run_type, status, gas_type,
target_temp, min_temp, pressure_min, pressure_max, pressure_unit, has_beam, devices,
started_at, ended_at, description, created_at, updated_at, created_by`

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(run *ExperimentRun) error {
	if err := scanRun(r.db.QueryRow(
		`INSERT INTO experiment_runs
		 (project_id, name, campaign, run_type, status, gas_type, target_temp, min_temp,
		  pressure_min, pressure_max, pressure_unit, has_beam, devices, description, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		 RETURNING `+runColumns,
		run.ProjectID, run.Name, nullableString(run.Campaign), run.RunType, run.Status, run.GasType,
		run.TargetTemp, run.MinTemp, run.PressureMin, run.PressureMax, run.PressureUnit,
		run.HasBeam, pq.Array(run.Devices), run.Description, nullableString(run.CreatedBy),
	), run); err != nil {
		return fmt.Errorf("create experiment run: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(id string) (*ExperimentRun, error) {
	var run ExperimentRun
	err := scanRun(r.db.QueryRow(
		`SELECT `+runColumns+` FROM experiment_runs WHERE id = $1 AND deleted_at IS NULL`, id,
	), &run)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get experiment run: %w", err)
	}
	return &run, nil
}

func (r *Repository) List(params RunListParams) ([]ExperimentRun, int, error) {
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	parts := []string{"project_id = $1", "deleted_at IS NULL"}
	args := []any{params.ProjectID}
	for _, filter := range []struct {
		column string
		value  string
	}{{"campaign", params.Campaign}, {"status", params.Status}, {"run_type", params.RunType}} {
		if filter.value != "" {
			args = append(args, filter.value)
			parts = append(parts, fmt.Sprintf("%s = $%d", filter.column, len(args)))
		}
	}
	where := " WHERE " + strings.Join(parts, " AND ")
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM experiment_runs`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count experiment runs: %w", err)
	}
	args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	rows, err := r.db.Query(
		`SELECT `+runColumns+` FROM experiment_runs`+where+
			fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list experiment runs: %w", err)
	}
	defer rows.Close()
	items := []ExperimentRun{}
	for rows.Next() {
		var run ExperimentRun
		if err := scanRun(rows, &run); err != nil {
			return nil, 0, fmt.Errorf("scan experiment run: %w", err)
		}
		items = append(items, run)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate experiment runs: %w", err)
	}
	return items, total, nil
}

func (r *Repository) Update(id string, req UpdateRunRequest) error {
	sets := []string{}
	args := []any{id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if req.Name != nil {
		add("name", *req.Name)
	}
	if req.Campaign != nil {
		add("campaign", nullableString(req.Campaign))
	}
	if req.RunType != nil {
		add("run_type", *req.RunType)
	}
	if req.GasType != nil {
		add("gas_type", *req.GasType)
	}
	if req.TargetTemp != nil {
		add("target_temp", *req.TargetTemp)
	}
	if req.MinTemp != nil {
		add("min_temp", *req.MinTemp)
	}
	if req.PressureMin != nil {
		add("pressure_min", *req.PressureMin)
	}
	if req.PressureMax != nil {
		add("pressure_max", *req.PressureMax)
	}
	if req.PressureUnit != nil {
		add("pressure_unit", *req.PressureUnit)
	}
	if req.HasBeam != nil {
		add("has_beam", *req.HasBeam)
	}
	if req.Devices != nil {
		add("devices", pq.Array(req.Devices))
	}
	if req.Description != nil {
		add("description", *req.Description)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = now()")
	result, err := r.db.Exec(
		`UPDATE experiment_runs SET `+strings.Join(sets, ", ")+` WHERE id = $1 AND deleted_at IS NULL`, args...,
	)
	if err != nil {
		return fmt.Errorf("update experiment run: %w", err)
	}
	return requireAffected(result, ErrRunNotFound)
}

func (r *Repository) UpdateStatus(id, fromStatus, toStatus string, shouldHaveStartedAt, shouldHaveEndedAt bool) error {
	result, err := r.db.Exec(
		`UPDATE experiment_runs
		 SET status = $3,
		     started_at = CASE WHEN $4 THEN COALESCE(started_at, now()) ELSE started_at END,
		     ended_at = CASE WHEN $5 THEN COALESCE(ended_at, now()) ELSE NULL END,
		     updated_at = now()
		 WHERE id = $1 AND status = $2 AND deleted_at IS NULL`,
		id, fromStatus, toStatus, shouldHaveStartedAt, shouldHaveEndedAt,
	)
	if err != nil {
		return fmt.Errorf("transition experiment run: %w", err)
	}
	return requireAffected(result, ErrRunConflict)
}

func (r *Repository) SoftDelete(id string) error {
	result, err := r.db.Exec(
		`UPDATE experiment_runs SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id,
	)
	if err != nil {
		return fmt.Errorf("delete experiment run: %w", err)
	}
	return requireAffected(result, ErrRunNotFound)
}

func (r *Repository) AddReportLink(runID, reportID string) error {
	_, err := r.db.Exec(
		`INSERT INTO daily_report_run_links (run_id, report_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		runID, reportID,
	)
	if err != nil {
		return fmt.Errorf("add experiment run report link: %w", err)
	}
	return nil
}

func (r *Repository) RemoveReportLink(runID, reportID string) error {
	result, err := r.db.Exec(
		`DELETE FROM daily_report_run_links WHERE run_id = $1 AND report_id = $2`, runID, reportID,
	)
	if err != nil {
		return fmt.Errorf("remove experiment run report link: %w", err)
	}
	return requireAffected(result, ErrReportLinkNotFound)
}

func (r *Repository) GetReportLinks(runID string) ([]string, error) {
	rows, err := r.db.Query(
		`SELECT report_id FROM daily_report_run_links WHERE run_id = $1 ORDER BY report_id`, runID,
	)
	if err != nil {
		return nil, fmt.Errorf("get experiment run report links: %w", err)
	}
	defer rows.Close()
	links := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan experiment run report link: %w", err)
		}
		links = append(links, id)
	}
	return links, rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func scanRun(row rowScanner, run *ExperimentRun) error {
	var campaign, description, createdBy sql.NullString
	var startedAt, endedAt sql.NullTime
	if err := row.Scan(
		&run.ID, &run.ProjectID, &run.Name, &campaign, &run.RunType, &run.Status, &run.GasType,
		&run.TargetTemp, &run.MinTemp, &run.PressureMin, &run.PressureMax, &run.PressureUnit,
		&run.HasBeam, pq.Array(&run.Devices), &startedAt, &endedAt, &description,
		&run.CreatedAt, &run.UpdatedAt, &createdBy,
	); err != nil {
		return err
	}
	if campaign.Valid {
		run.Campaign = &campaign.String
	}
	if startedAt.Valid {
		run.StartedAt = &startedAt.Time
	}
	if endedAt.Valid {
		run.EndedAt = &endedAt.Time
	}
	if description.Valid {
		run.Description = description.String
	}
	if createdBy.Valid {
		run.CreatedBy = &createdBy.String
	}
	if run.Devices == nil {
		run.Devices = []string{}
	}
	return nil
}

func nullableString(value *string) sql.NullString {
	if value == nil || strings.TrimSpace(*value) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.TrimSpace(*value), Valid: true}
}

func normalizePage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}

func requireAffected(result sql.Result, onZero error) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return onZero
	}
	return nil
}
