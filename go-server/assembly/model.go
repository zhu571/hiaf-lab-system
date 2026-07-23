package assembly

import "time"

const (
	StatusPlanned    = "planned"
	StatusInProgress = "in_progress"
	StatusPaused     = "paused"
	StatusCompleted  = "completed"
	StatusSkipped    = "skipped"
	StatusCancelled  = "cancelled"

	TransitionStart    = "start"
	TransitionPause    = "pause"
	TransitionResume   = "resume"
	TransitionComplete = "complete"
	TransitionSkip     = "skip"
	TransitionCancel   = "cancel"
)

var AllowedTransitions = map[string][]string{
	StatusPlanned:    {TransitionStart, TransitionCancel},
	StatusInProgress: {TransitionPause, TransitionComplete, TransitionSkip, TransitionCancel},
	StatusPaused:     {TransitionResume, TransitionCancel},
	StatusSkipped:    {TransitionStart},
}

type AssemblyStep struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	DependsOn   *string    `json:"depends_on,omitempty"`
	Status      string     `json:"status"`
	AssignedTo  *string    `json:"assigned_to,omitempty"`
	StepOrder   int        `json:"step_order"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedBy   *string    `json:"created_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateStepRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	DependsOn   *string `json:"depends_on,omitempty"`
	AssignedTo  *string `json:"assigned_to,omitempty"`
	StepOrder   int     `json:"step_order"`
}

type UpdateStepRequest struct {
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	AssignedTo     *string `json:"assigned_to,omitempty"`
	Transition     *string `json:"transition,omitempty"`
	OverrideReason *string `json:"override_reason,omitempty"`
}

type ReorderRequest struct {
	ProjectID string        `json:"project_id"`
	Steps     []ReorderItem `json:"steps"`
}

type ReorderItem struct {
	ID        string `json:"id"`
	StepOrder int    `json:"step_order"`
}

type ListParams struct {
	ProjectID string
	Status    string
	Page      int
	PerPage   int
}

type ListResult struct {
	Items   []AssemblyStep `json:"items"`
	Total   int            `json:"total"`
	Page    int            `json:"page"`
	PerPage int            `json:"per_page"`
}
