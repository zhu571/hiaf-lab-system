package testdata

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

const testDataColumns = `id, project_id, run_id, data_type, measurement, value, unit,
quality, source, measured_at, notes, created_at, updated_at, recorded_by`

const curveColumns = `id, project_id, run_id, name, curve_type, x_label, y_label, unit, points,
quality, source, notes, is_void, voided_at, voided_by, void_reason, measured_at, created_by, created_at, updated_at`

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

// CreateBatch 在单个事务内逐条 INSERT ... RETURNING，任一失败整体回滚。
// 逐条而非 multi-VALUES：① RETURNING 直接回填 id（审计 created_ids 需要）；
// ② 失败时可精确定位违例行 index（run_id FK 竞态兜底依赖）；③ 与单条 Create 复用同一 INSERT 语句模板。
func (r *Repository) CreateBatch(items []*TestData) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin test data batch: %w", err)
	}
	defer tx.Rollback()
	for i, td := range items {
		if err := scanTestData(tx.QueryRow(
			`INSERT INTO test_data
			 (project_id, run_id, data_type, measurement, value, unit, quality, source, measured_at, notes, recorded_by)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			 RETURNING `+testDataColumns,
			td.ProjectID, nullableString(td.RunID), td.DataType, td.Measurement, td.Value, td.Unit,
			td.Quality, td.Source, td.MeasuredAt, nullableText(td.Notes), nullableString(td.RecordedBy),
		), td); err != nil {
			if isRunFKViolation(err) {
				return &RowError{Index: i, Field: "run_id", Code: "run_not_found", Message: "实验批次不存在"}
			}
			return fmt.Errorf("create test data batch row %d: %w", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit test data batch: %w", err)
	}
	return nil
}

// isRunFKViolation 识别 run_id 外键违例（SQLSTATE 23503）：
// 出现在「校验 → 插入」窗口内 run 被硬删/软删时，与 service 层预校验语义一致。
func isRunFKViolation(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr.Code != "23503" {
		return false
	}
	return strings.Contains(pqErr.Constraint, "run_id") || strings.Contains(pqErr.Detail, "(run_id)")
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

func (r *Repository) CreateCurve(curve *Curve) error {
	points, err := json.Marshal(curve.Points)
	if err != nil {
		return fmt.Errorf("marshal test data curve points: %w", err)
	}
	err = scanCurve(r.db.QueryRow(`INSERT INTO test_data_curves
		(project_id, run_id, name, curve_type, x_label, y_label, unit, points, quality, source, notes, measured_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING `+curveColumns,
		curve.ProjectID, nullableString(curve.RunID), curve.Name, curve.CurveType, curve.XLabel, curve.YLabel,
		curve.Unit, string(points), curve.Quality, curve.Source, curve.Notes, curve.MeasuredAt, nullableString(curve.CreatedBy),
	), curve)
	if err != nil {
		return fmt.Errorf("create test data curve: %w", err)
	}
	return nil
}

func (r *Repository) GetCurve(id string) (*Curve, error) {
	var curve Curve
	err := scanCurve(r.db.QueryRow(`SELECT `+curveColumns+` FROM test_data_curves WHERE id = $1 AND is_void = false`, id), &curve)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get test data curve: %w", err)
	}
	return &curve, nil
}

func (r *Repository) ListCurves(params ListCurvesParams) ([]Curve, int, error) {
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	parts := []string{"project_id = $1", "is_void = false"}
	args := []any{params.ProjectID}
	add := func(column, value string) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if params.RunID != "" {
		add("run_id", params.RunID)
	}
	if params.CurveType != "" {
		add("curve_type", params.CurveType)
	}
	if params.Quality != "" {
		add("quality", params.Quality)
	} else {
		parts = append(parts, "quality <> 'invalid'")
	}
	where := " WHERE " + strings.Join(parts, " AND ")
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM test_data_curves`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count test data curves: %w", err)
	}
	args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	rows, err := r.db.Query(`SELECT `+curveColumns+` FROM test_data_curves`+where+
		fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list test data curves: %w", err)
	}
	defer rows.Close()
	items := []Curve{}
	for rows.Next() {
		var curve Curve
		if err := scanCurve(rows, &curve); err != nil {
			return nil, 0, fmt.Errorf("scan test data curve: %w", err)
		}
		items = append(items, curve)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate test data curves: %w", err)
	}
	return items, total, nil
}

func (r *Repository) UpdateCurve(id string, req UpdateCurveRequest) error {
	sets := []string{}
	args := []any{id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if req.Name != nil {
		add("name", *req.Name)
	}
	if req.XLabel != nil {
		add("x_label", *req.XLabel)
	}
	if req.YLabel != nil {
		add("y_label", *req.YLabel)
	}
	if req.Unit != nil {
		add("unit", *req.Unit)
	}
	if req.Points != nil {
		points, err := json.Marshal(*req.Points)
		if err != nil {
			return fmt.Errorf("marshal test data curve points: %w", err)
		}
		add("points", string(points))
	}
	if req.Quality != nil {
		add("quality", *req.Quality)
	}
	if req.Notes != nil {
		add("notes", *req.Notes)
	}
	if req.MeasuredAt != nil {
		add("measured_at", *req.MeasuredAt)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = now()")
	result, err := r.db.Exec(`UPDATE test_data_curves SET `+strings.Join(sets, ", ")+` WHERE id = $1 AND is_void = false`, args...)
	if err != nil {
		return fmt.Errorf("update test data curve: %w", err)
	}
	return requireCurveAffected(result)
}

func (r *Repository) MarkCurveVoid(id, voidedBy, reason string) error {
	result, err := r.db.Exec(`UPDATE test_data_curves
		SET is_void = true, voided_at = now(), voided_by = $2, void_reason = $3, updated_at = now()
		WHERE id = $1 AND is_void = false`, id, voidedBy, nullableText(reason))
	if err != nil {
		return fmt.Errorf("void test data curve: %w", err)
	}
	return requireCurveAffected(result)
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

func scanCurve(row rowScanner, curve *Curve) error {
	var runID, voidedBy, voidReason, createdBy sql.NullString
	var voidedAt, measuredAt sql.NullTime
	var points []byte
	if err := row.Scan(&curve.ID, &curve.ProjectID, &runID, &curve.Name, &curve.CurveType, &curve.XLabel,
		&curve.YLabel, &curve.Unit, &points, &curve.Quality, &curve.Source, &curve.Notes, &curve.IsVoid,
		&voidedAt, &voidedBy, &voidReason, &measuredAt, &createdBy, &curve.CreatedAt, &curve.UpdatedAt); err != nil {
		return err
	}
	if err := json.Unmarshal(points, &curve.Points); err != nil {
		return fmt.Errorf("decode points: %w", err)
	}
	curve.RunID = nullStringPointer(runID)
	curve.VoidedBy = nullStringPointer(voidedBy)
	curve.VoidReason = nullStringPointer(voidReason)
	curve.CreatedBy = nullStringPointer(createdBy)
	if voidedAt.Valid {
		curve.VoidedAt = &voidedAt.Time
	}
	if measuredAt.Valid {
		curve.MeasuredAt = &measuredAt.Time
	}
	return nil
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
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

func requireCurveAffected(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCurveNotFound
	}
	return nil
}
