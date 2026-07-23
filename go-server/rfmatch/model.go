package rfmatch

import "time"

const (
	DeviceRFCarpet = "rf_carpet"
	DeviceRFQ      = "rfq"
	DeviceQPIG     = "qpig"

	StatusPass   = "pass"
	StatusAdjust = "adjust"
	StatusFail   = "fail"
)

type RFMatchingRecord struct {
	ID                  string     `json:"id"`
	ProjectID           string     `json:"project_id"`
	Device              string     `json:"device"`
	FrequencyMHz        float64    `json:"frequency_mhz"`
	S11                 *float64   `json:"s11,omitempty"`
	InputFreq           *float64   `json:"input_freq,omitempty"`
	InputVoltage        *float64   `json:"input_voltage,omitempty"`
	InputPower          *float64   `json:"input_power,omitempty"`
	InputDesc           string     `json:"input_desc,omitempty"`
	OutputFreq          *float64   `json:"output_freq,omitempty"`
	OutputVoltage       *float64   `json:"output_voltage,omitempty"`
	OutputPower         *float64   `json:"output_power,omitempty"`
	OutputDesc          string     `json:"output_desc,omitempty"`
	TransformerTurns    string     `json:"transformer_turns,omitempty"`
	CapacitanceText     string     `json:"capacitance_text,omitempty"`
	TransformerMaterial string     `json:"transformer_material,omitempty"`
	ShuntInductance     string     `json:"shunt_inductance,omitempty"`
	SeriesCapacitor     string     `json:"series_capacitor,omitempty"`
	Status              *string    `json:"status,omitempty"`
	Notes               string     `json:"notes,omitempty"`
	MeasuredAt          time.Time  `json:"measured_at"`
	MeasuredBy          *string    `json:"measured_by,omitempty"`
	IsVoid              bool       `json:"is_void"`
	VoidedAt            *time.Time `json:"voided_at,omitempty"`
	VoidedBy            *string    `json:"voided_by,omitempty"`
	VoidReason          *string    `json:"void_reason,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type CreateRFMatchingRequest struct {
	Device              string     `json:"device"`
	FrequencyMHz        float64    `json:"frequency_mhz"`
	S11                 *float64   `json:"s11,omitempty"`
	InputFreq           *float64   `json:"input_freq,omitempty"`
	InputVoltage        *float64   `json:"input_voltage,omitempty"`
	InputPower          *float64   `json:"input_power,omitempty"`
	InputDesc           string     `json:"input_desc,omitempty"`
	OutputFreq          *float64   `json:"output_freq,omitempty"`
	OutputVoltage       *float64   `json:"output_voltage,omitempty"`
	OutputPower         *float64   `json:"output_power,omitempty"`
	OutputDesc          string     `json:"output_desc,omitempty"`
	TransformerTurns    string     `json:"transformer_turns,omitempty"`
	CapacitanceText     string     `json:"capacitance_text,omitempty"`
	TransformerMaterial string     `json:"transformer_material,omitempty"`
	ShuntInductance     string     `json:"shunt_inductance,omitempty"`
	SeriesCapacitor     string     `json:"series_capacitor,omitempty"`
	Status              *string    `json:"status,omitempty"`
	Notes               string     `json:"notes,omitempty"`
	MeasuredAt          *time.Time `json:"measured_at,omitempty"`
}

type UpdateRFMatchingRequest struct {
	S11                 *float64 `json:"s11,omitempty"`
	InputFreq           *float64 `json:"input_freq,omitempty"`
	InputVoltage        *float64 `json:"input_voltage,omitempty"`
	InputPower          *float64 `json:"input_power,omitempty"`
	InputDesc           *string  `json:"input_desc,omitempty"`
	OutputFreq          *float64 `json:"output_freq,omitempty"`
	OutputVoltage       *float64 `json:"output_voltage,omitempty"`
	OutputPower         *float64 `json:"output_power,omitempty"`
	OutputDesc          *string  `json:"output_desc,omitempty"`
	TransformerTurns    *string  `json:"transformer_turns,omitempty"`
	CapacitanceText     *string  `json:"capacitance_text,omitempty"`
	TransformerMaterial *string  `json:"transformer_material,omitempty"`
	ShuntInductance     *string  `json:"shunt_inductance,omitempty"`
	SeriesCapacitor     *string  `json:"series_capacitor,omitempty"`
	Status              *string  `json:"status,omitempty"`
	Notes               *string  `json:"notes,omitempty"`
}

type ListParams struct {
	ProjectID string
	Device    string
	Status    string
	Page      int
	PerPage   int
}

type ListResult struct {
	Items   []RFMatchingRecord `json:"items"`
	Total   int                `json:"total"`
	Page    int                `json:"page"`
	PerPage int                `json:"per_page"`
}
