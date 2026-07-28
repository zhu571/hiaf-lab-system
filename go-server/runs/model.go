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

// 实验步骤状态与装配步骤一致。
const (
	StepStatusPlanned    = "planned"
	StepStatusInProgress = "in_progress"
	StepStatusPaused     = "paused"
	StepStatusCompleted  = "completed"
	StepStatusSkipped    = "skipped"
	StepStatusCancelled  = "cancelled"

	StepTransitionStart    = "start"
	StepTransitionPause    = "pause"
	StepTransitionResume   = "resume"
	StepTransitionComplete = "complete"
	StepTransitionSkip     = "skip"
	StepTransitionCancel   = "cancel"
)

var StepAllowedTransitions = map[string][]string{
	StepStatusPlanned:    {StepTransitionStart, StepTransitionCancel},
	StepStatusInProgress: {StepTransitionPause, StepTransitionComplete, StepTransitionSkip, StepTransitionCancel},
	StepStatusPaused:     {StepTransitionResume, StepTransitionCancel},
	StepStatusSkipped:    {StepTransitionStart},
}

type RunStep struct {
	ID               string     `json:"id"`
	RunID            string     `json:"run_id"`
	Name             string     `json:"name"`
	Description      string     `json:"description,omitempty"`
	DependsOn        *string    `json:"depends_on,omitempty"`
	Status           string     `json:"status"`
	StepOrder        int        `json:"step_order"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	SourceTemplateID *string    `json:"source_template_id,omitempty"`
	CreatedBy        *string    `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type CreateStepRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	DependsOn   *string `json:"depends_on,omitempty"`
	StepOrder   int     `json:"step_order"`
}

type UpdateStepRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	DependsOn   *string `json:"depends_on,omitempty"`
	Transition  *string `json:"transition,omitempty"`
}

type ApplyTemplateRequest struct {
	TemplateID   *string   `json:"template_id,omitempty"`
	Steps        []StepDef `json:"steps,omitempty"`
	SourcePrompt string    `json:"source_prompt,omitempty"`
}

type StepDef struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	StepOrder      int    `json:"step_order"`
	DependsOnOrder *int   `json:"depends_on_order,omitempty"`
}

type ReorderRequest struct {
	RunID string        `json:"run_id"`
	Steps []ReorderItem `json:"steps"`
}

type ReorderItem struct {
	ID        string `json:"id"`
	StepOrder int    `json:"step_order"`
}

type StepListResult struct {
	Items []RunStep `json:"items"`
	Total int       `json:"total"`
}
