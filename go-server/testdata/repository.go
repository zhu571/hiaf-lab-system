package testdata

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const testDataColumns = `id, project_id, run_id, data_type, measurement, value, unit,
quality, source, measured_at, notes, created_at, updated_at, recorded_by`

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(td *TestData) error {
	if err := scanTestData(r.db.QueryRow(
		`INSERT INTO test_data
		 (project_id, run_id, data_type, measurement, value, unit, quality, source, measured_at, notes, recorded_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 RETURNING `+testDataColumns,
		td.ProjectID, nullableString(td.RunID), td.DataType, td.Measurement, td.Value, td.Unit,
		td.Quality, td.Source, td.MeasuredAt, nullableText(td.Notes), nullableString(td.RecordedBy),
	), td); err != nil {
		return fmt.Errorf("create test data: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(id string) (*TestData, error) {
	var td TestData
	err := scanTestData(r.db.QueryRow(`SELECT `+testDataColumns+` FROM test_data WHERE id = $1`, id), &td)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get test data: %w", err)
	}
	return &td, nil
}

func (r *Repository) List(params ListParams) ([]TestData, int, error) {
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	parts := []string{"project_id = $1"}
	args := []any{params.ProjectID}
	add := func(column, value string) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if params.RunID != "" {
		add("run_id", params.RunID)
	}
	if params.DataType != "" {
		add("data_type", params.DataType)
	}
	if params.Quality != "" {
		add("quality", params.Quality)
	} else {
		parts = append(parts, "quality <> 'invalid'")
	}
	where := " WHERE " + strings.Join(parts, " AND ")
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM test_data`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count test data: %w", err)
	}
	args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	rows, err := r.db.Query(
		`SELECT `+testDataColumns+` FROM test_data`+where+
			fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list test data: %w", err)
	}
	defer rows.Close()
	items := []TestData{}
	for rows.Next() {
		var td TestData
		if err := scanTestData(rows, &td); err != nil {
			return nil, 0, fmt.Errorf("scan test data: %w", err)
		}
		items = append(items, td)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate test data: %w", err)
	}
	return items, total, nil
}

func (r *Repository) Update(id string, req UpdateTestDataRequest) error {
	sets := []string{}
	args := []any{id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if req.Measurement != nil {
		add("measurement", *req.Measurement)
	}
	if req.Value != nil {
		add("value", *req.Value)
	}
	if req.Unit != nil {
		add("unit", *req.Unit)
	}
	if req.Quality != nil {
		add("quality", *req.Quality)
	}
	if req.MeasuredAt != nil {
		add("measured_at", *req.MeasuredAt)
	}
	if req.Notes != nil {
		add("notes", nullableText(*req.Notes))
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = now()")
	result, err := r.db.Exec(`UPDATE test_data SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
	if err != nil {
		return fmt.Errorf("update test data: %w", err)
	}
	return requireAffected(result)
}

func (r *Repository) MarkInvalid(id, _ string) error {
	result, err := r.db.Exec(
		`UPDATE test_data SET quality = 'invalid', updated_at = now() WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("mark test data invalid: %w", err)
	}
	return requireAffected(result)
}

type rowScanner interface{ Scan(...any) error }

func scanTestData(row rowScanner, td *TestData) error {
	var runID, recordedBy sql.NullString
	var measuredAt sql.NullTime
	var notes sql.NullString
	if err := row.Scan(
		&td.ID, &td.ProjectID, &runID, &td.DataType, &td.Measurement, &td.Value, &td.Unit,
		&td.Quality, &td.Source, &measuredAt, &notes, &td.CreatedAt, &td.UpdatedAt, &recordedBy,
	); err != nil {
		return err
	}
	if runID.Valid {
		td.RunID = &runID.String
	}
	if measuredAt.Valid {
		td.MeasuredAt = &measuredAt.Time
	}
	if notes.Valid {
		td.Notes = notes.String
	}
	if recordedBy.Valid {
		td.RecordedBy = &recordedBy.String
	}
	return nil
}

func nullableString(value *string) sql.NullString {
	if value == nil || strings.TrimSpace(*value) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.TrimSpace(*value), Valid: true}
}

func nullableText(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
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

func requireAffected(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTestDataNotFound
	}
	return nil
}
