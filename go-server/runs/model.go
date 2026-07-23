package runs

import "time"

const (
	StatusPlanned   = "planned"
	StatusActive    = "active"
	StatusPaused    = "paused"
	StatusCompleted = "completed"
	StatusAborted   = "aborted"

	RunTypeCooldown    = "cooldown"
	RunTypeWarmup      = "warmup"
	RunTypeSteadyState = "steady_state"
	RunTypeTest        = "test"

	GasTypeHe = "He"
	GasTypeAr = "Ar"
	GasTypeXe = "Xe"

	DeviceRFCarpet = "rf_carpet"
	DeviceRFQ      = "rfq"
	DeviceQPIG     = "qpig"
)

type ExperimentRun struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"project_id"`
	Name         string     `json:"name"`
	Campaign     *string    `json:"campaign,omitempty"`
	RunType      string     `json:"run_type"`
	Status       string     `json:"status"`
	GasType      string     `json:"gas_type"`
	TargetTemp   *float64   `json:"target_temp,omitempty"`
	MinTemp      *float64   `json:"min_temp,omitempty"`
	PressureMin  *float64   `json:"pressure_min,omitempty"`
	PressureMax  *float64   `json:"pressure_max,omitempty"`
	PressureUnit string     `json:"pressure_unit"`
	HasBeam      bool       `json:"has_beam"`
	Devices      []string   `json:"devices"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	Description  string     `json:"description,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CreatedBy    *string    `json:"created_by,omitempty"`
}

type CreateRunRequest struct {
	Name         string   `json:"name"`
	Campaign     *string  `json:"campaign,omitempty"`
	RunType      *string  `json:"run_type,omitempty"`
	GasType      *string  `json:"gas_type,omitempty"`
	TargetTemp   *float64 `json:"target_temp,omitempty"`
	MinTemp      *float64 `json:"min_temp,omitempty"`
	PressureMin  *float64 `json:"pressure_min,omitempty"`
	PressureMax  *float64 `json:"pressure_max,omitempty"`
	PressureUnit *string  `json:"pressure_unit,omitempty"`
	HasBeam      *bool    `json:"has_beam,omitempty"`
	Devices      []string `json:"devices,omitempty"`
	Description  string   `json:"description,omitempty"`
}

type UpdateRunRequest struct {
	Name         *string  `json:"name,omitempty"`
	Campaign     *string  `json:"campaign,omitempty"`
	RunType      *string  `json:"run_type,omitempty"`
	GasType      *string  `json:"gas_type,omitempty"`
	TargetTemp   *float64 `json:"target_temp,omitempty"`
	MinTemp      *float64 `json:"min_temp,omitempty"`
	PressureMin  *float64 `json:"pressure_min,omitempty"`
	PressureMax  *float64 `json:"pressure_max,omitempty"`
	PressureUnit *string  `json:"pressure_unit,omitempty"`
	HasBeam      *bool    `json:"has_beam,omitempty"`
	Devices      []string `json:"devices,omitempty"`
	Description  *string  `json:"description,omitempty"`
	Transition   *string  `json:"transition,omitempty"`
}

type RunListParams struct {
	ProjectID string
	Campaign  string
	Status    string
	RunType   string
	Page      int
	PerPage   int
}

type RunListResult struct {
	Items   []ExperimentRun `json:"items"`
	Total   int             `json:"total"`
	Page    int             `json:"page"`
	PerPage int             `json:"per_page"`
}
