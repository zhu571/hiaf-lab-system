package steptemplates

import "time"

type StepTemplate struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"`
	Description  string     `json:"description,omitempty"`
	SourcePrompt string     `json:"source_prompt,omitempty"`
	AIGenerated  bool       `json:"ai_generated"`
	CreatedBy    *string    `json:"created_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"-"`
	Items        []StepTemplateItem `json:"items,omitempty"`
}

type StepTemplateItem struct {
	ID             string    `json:"id"`
	TemplateID     string    `json:"template_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	StepOrder      int       `json:"step_order"`
	DependsOnOrder *int      `json:"depends_on_order,omitempty"`
	Meta           []byte    `json:"meta"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	DeletedAt      *time.Time `json:"-"`
}

const (
	MaxItems       = 30
	MinItems       = 1
	MaxNameLen     = 256
	MaxDescLen     = 2000
)

type ItemDef struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	StepOrder      int    `json:"step_order"`
	DependsOnOrder *int   `json:"depends_on_order,omitempty"`
}

type StepCandidate struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	StepOrder      int    `json:"step_order"`
	DependsOnOrder *int   `json:"depends_on_order,omitempty"`
}

type GenerateRequest struct {
	Kind    string         `json:"kind"`
	Prompt  string         `json:"prompt"`
	Context map[string]any `json:"context,omitempty"`
}

type GenerateResponseData struct {
	Status         string          `json:"status"`
	NameSuggestion string          `json:"name_suggestion"`
	Steps          []StepCandidate `json:"steps"`
	Question       *string         `json:"question,omitempty"`
	Reason         *string         `json:"reason,omitempty"`
	Model          string          `json:"model"`
	PromptVersion  string          `json:"prompt_version"`
}

type CreateTemplateRequest struct {
	Name           string    `json:"name"`
	Kind           string    `json:"kind"`
	Description    string    `json:"description,omitempty"`
	SourcePrompt   string    `json:"source_prompt,omitempty"`
	AIGenerated    bool      `json:"ai_generated,omitempty"`
	Items          []ItemDef `json:"items"`
	ApplyToProjectID *string `json:"apply_to_project_id,omitempty"`
}

type UpdateTemplateRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type ReplaceItemsRequest struct {
	Items []ItemDef `json:"items"`
}

type ListResult struct {
	Items   []StepTemplate `json:"items"`
	Total   int            `json:"total"`
	Page    int            `json:"page"`
	PerPage int            `json:"per_page"`
}
