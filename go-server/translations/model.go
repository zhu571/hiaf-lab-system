package translations

import "time"

const (
	StatusMissing = "missing"
	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
	StatusStale   = "stale"
)

type Variant struct {
	Status    string    `json:"status"`
	Text      string    `json:"text,omitempty"`
	Origin    string    `json:"origin"`
	Editable  bool      `json:"editable"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type FieldTranslations struct {
	SourceLocale string  `json:"source_locale"`
	SourceHash   string  `json:"source_hash"`
	Zh           Variant `json:"zh"`
	En           Variant `json:"en"`
}

type Sidecar map[string]FieldTranslations

type Request struct {
	Field          string `json:"field"`
	TargetLocale   string `json:"target_locale"`
	Force          bool   `json:"force"`
	TranslatedText string `json:"translated_text"`
}

type Response struct {
	Status         string `json:"status"`
	TranslatedText string `json:"translated_text"`
	Model          string `json:"model"`
	PromptVersion  string `json:"prompt_version"`
}
