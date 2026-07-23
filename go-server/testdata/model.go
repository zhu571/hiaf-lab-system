package testdata

import "time"

const (
	DataTypeCryo       = "cryo"
	DataTypePressure   = "pressure"
	DataTypeVoltage    = "voltage"
	DataTypeRFVoltage  = "rf_voltage"
	DataTypeEfficiency = "efficiency"

	QualityNormal  = "normal"
	QualityOutlier = "outlier"
	QualitySuspect = "suspect"
	QualityInvalid = "invalid"

	SourceManual     = "manual"
	SourceInstrument = "instrument"
	SourceImport     = "import"
	SourceAgent      = "agent"
	SourceBackfill   = "backfill"
)

type TestData struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	RunID       *string    `json:"run_id,omitempty"`
	DataType    string     `json:"data_type"`
	Measurement string     `json:"measurement"`
	Value       float64    `json:"value"`
	Unit        string     `json:"unit"`
	Quality     string     `json:"quality"`
	Source      string     `json:"source"`
	MeasuredAt  *time.Time `json:"measured_at,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	RecordedBy  *string    `json:"recorded_by,omitempty"`
}

type CreateTestDataRequest struct {
	DataType    string     `json:"data_type"`
	RunID       *string    `json:"run_id,omitempty"`
	Measurement string     `json:"measurement"`
	Value       float64    `json:"value"`
	Unit        string     `json:"unit,omitempty"`
	Quality     *string    `json:"quality,omitempty"`
	Source      *string    `json:"source,omitempty"`
	MeasuredAt  *time.Time `json:"measured_at,omitempty"`
	Notes       string     `json:"notes,omitempty"`
}

type UpdateTestDataRequest struct {
	DataType    *string    `json:"data_type,omitempty"`
	Measurement *string    `json:"measurement,omitempty"`
	Value       *float64   `json:"value,omitempty"`
	Unit        *string    `json:"unit,omitempty"`
	Quality     *string    `json:"quality,omitempty"`
	MeasuredAt  *time.Time `json:"measured_at,omitempty"`
	Notes       *string    `json:"notes,omitempty"`
}

type ListParams struct {
	ProjectID string
	RunID     string
	DataType  string
	Quality   string
	Page      int
	PerPage   int
}

type ListResult struct {
	Items   []TestData `json:"items"`
	Total   int        `json:"total"`
	Page    int        `json:"page"`
	PerPage int        `json:"per_page"`
}
