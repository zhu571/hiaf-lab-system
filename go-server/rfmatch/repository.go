package rfmatch

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const recordColumns = `id, project_id, device, frequency_mhz, s11,
input_freq, input_voltage, input_power, input_desc,
output_freq, output_voltage, output_power, output_desc,
transformer_turns, capacitance_text, transformer_material, shunt_inductance, series_capacitor,
status, notes, measured_at, measured_by, is_void, voided_at, voided_by, void_reason, created_at, updated_at`

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(record *RFMatchingRecord) error {
	err := scanRecord(r.db.QueryRow(`INSERT INTO rf_matching_records
		(project_id, device, frequency_mhz, s11, input_freq, input_voltage, input_power, input_desc,
		 output_freq, output_voltage, output_power, output_desc, transformer_turns, capacitance_text,
		 transformer_material, shunt_inductance, series_capacitor, status, notes, measured_at, measured_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		RETURNING `+recordColumns,
		record.ProjectID, record.Device, record.FrequencyMHz, record.S11, record.InputFreq, record.InputVoltage,
		record.InputPower, record.InputDesc, record.OutputFreq, record.OutputVoltage, record.OutputPower,
		record.OutputDesc, record.TransformerTurns, record.CapacitanceText, record.TransformerMaterial,
		record.ShuntInductance, record.SeriesCapacitor, record.Status, record.Notes, record.MeasuredAt, record.MeasuredBy,
	), record)
	if err != nil {
		return fmt.Errorf("create RF matching record: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(id string) (*RFMatchingRecord, error) {
	var record RFMatchingRecord
	err := scanRecord(r.db.QueryRow(`SELECT `+recordColumns+` FROM rf_matching_records WHERE id = $1 AND is_void = false`, id), &record)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get RF matching record: %w", err)
	}
	return &record, nil
}

func (r *Repository) List(params ListParams) ([]RFMatchingRecord, int, error) {
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	parts := []string{"project_id = $1", "is_void = false"}
	args := []any{params.ProjectID}
	add := func(column, value string) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if params.Device != "" {
		add("device", params.Device)
	}
	if params.Status != "" {
		add("status", params.Status)
	}
	where := " WHERE " + strings.Join(parts, " AND ")
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM rf_matching_records`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count RF matching records: %w", err)
	}
	args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	rows, err := r.db.Query(`SELECT `+recordColumns+` FROM rf_matching_records`+where+
		fmt.Sprintf(` ORDER BY measured_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list RF matching records: %w", err)
	}
	defer rows.Close()
	items := []RFMatchingRecord{}
	for rows.Next() {
		var record RFMatchingRecord
		if err := scanRecord(rows, &record); err != nil {
			return nil, 0, fmt.Errorf("scan RF matching record: %w", err)
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate RF matching records: %w", err)
	}
	return items, total, nil
}

func (r *Repository) Update(id string, req UpdateRFMatchingRequest) error {
	sets := []string{}
	args := []any{id}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if req.S11 != nil {
		add("s11", *req.S11)
	}
	if req.InputFreq != nil {
		add("input_freq", *req.InputFreq)
	}
	if req.InputVoltage != nil {
		add("input_voltage", *req.InputVoltage)
	}
	if req.InputPower != nil {
		add("input_power", *req.InputPower)
	}
	if req.InputDesc != nil {
		add("input_desc", *req.InputDesc)
	}
	if req.OutputFreq != nil {
		add("output_freq", *req.OutputFreq)
	}
	if req.OutputVoltage != nil {
		add("output_voltage", *req.OutputVoltage)
	}
	if req.OutputPower != nil {
		add("output_power", *req.OutputPower)
	}
	if req.OutputDesc != nil {
		add("output_desc", *req.OutputDesc)
	}
	if req.TransformerTurns != nil {
		add("transformer_turns", *req.TransformerTurns)
	}
	if req.CapacitanceText != nil {
		add("capacitance_text", *req.CapacitanceText)
	}
	if req.TransformerMaterial != nil {
		add("transformer_material", *req.TransformerMaterial)
	}
	if req.ShuntInductance != nil {
		add("shunt_inductance", *req.ShuntInductance)
	}
	if req.SeriesCapacitor != nil {
		add("series_capacitor", *req.SeriesCapacitor)
	}
	if req.Status != nil {
		add("status", *req.Status)
	}
	if req.Notes != nil {
		add("notes", *req.Notes)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = now()")
	result, err := r.db.Exec(`UPDATE rf_matching_records SET `+strings.Join(sets, ", ")+` WHERE id = $1 AND is_void = false`, args...)
	if err != nil {
		return fmt.Errorf("update RF matching record: %w", err)
	}
	return requireAffected(result)
}

func (r *Repository) MarkVoid(id, voidedBy, reason string) error {
	result, err := r.db.Exec(`UPDATE rf_matching_records
		SET is_void = true, voided_at = now(), voided_by = $2, void_reason = $3, updated_at = now()
		WHERE id = $1 AND is_void = false`, id, voidedBy, reason)
	if err != nil {
		return fmt.Errorf("void RF matching record: %w", err)
	}
	return requireAffected(result)
}

type rowScanner interface{ Scan(...any) error }

func scanRecord(row rowScanner, record *RFMatchingRecord) error {
	var s11, inputFreq, inputVoltage, inputPower, outputFreq, outputVoltage, outputPower sql.NullFloat64
	var status, measuredBy, voidedBy, voidReason sql.NullString
	var voidedAt sql.NullTime
	err := row.Scan(&record.ID, &record.ProjectID, &record.Device, &record.FrequencyMHz, &s11,
		&inputFreq, &inputVoltage, &inputPower, &record.InputDesc,
		&outputFreq, &outputVoltage, &outputPower, &record.OutputDesc,
		&record.TransformerTurns, &record.CapacitanceText, &record.TransformerMaterial,
		&record.ShuntInductance, &record.SeriesCapacitor, &status, &record.Notes, &record.MeasuredAt,
		&measuredBy, &record.IsVoid, &voidedAt, &voidedBy, &voidReason, &record.CreatedAt, &record.UpdatedAt)
	if err != nil {
		return err
	}
	record.S11 = floatPointer(s11)
	record.InputFreq = floatPointer(inputFreq)
	record.InputVoltage = floatPointer(inputVoltage)
	record.InputPower = floatPointer(inputPower)
	record.OutputFreq = floatPointer(outputFreq)
	record.OutputVoltage = floatPointer(outputVoltage)
	record.OutputPower = floatPointer(outputPower)
	record.Status = stringPointer(status)
	record.MeasuredBy = stringPointer(measuredBy)
	record.VoidedBy = stringPointer(voidedBy)
	record.VoidReason = stringPointer(voidReason)
	if voidedAt.Valid {
		record.VoidedAt = &voidedAt.Time
	}
	return nil
}

func floatPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
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
		return ErrRecordNotFound
	}
	return nil
}
