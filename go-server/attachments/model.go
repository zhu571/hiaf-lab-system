package attachments

import "time"

const (
	EntityAssemblyStep     = "assembly_step"
	EntityDailyReport      = "daily_report"
	EntityIssue            = "issue"
	EntityLog              = "log"
	EntityTestData         = "test_data"
	EntityExperimentRun    = "experiment_run"
	EntityRFMatchingRecord = "rf_matching_record"
)

type Attachment struct {
	ID           string    `json:"id"`
	StorageKey   string    `json:"-"`
	OriginalName string    `json:"original_name"`
	Sha256       string    `json:"sha256"`
	Description  string    `json:"description"`
	MimeType     string    `json:"mime_type"`
	FileSize     int64     `json:"file_size"`
	UploadedBy   *string   `json:"uploaded_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AttachmentLink struct {
	ID           string    `json:"id"`
	AttachmentID string    `json:"attachment_id"`
	EntityType   string    `json:"entity_type"`
	EntityID     string    `json:"entity_id"`
	Description  string    `json:"description"`
	CreatedBy    *string   `json:"created_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type UploadResponse struct {
	Attachment Attachment       `json:"attachment"`
	Links      []AttachmentLink `json:"links,omitempty"`
}

type CreateLinkRequest struct {
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	Description string `json:"description,omitempty"`
}

type ListParams struct {
	EntityType string
	EntityID   string
	Page       int
	PerPage    int
}

type ListResult struct {
	Items   []Attachment `json:"items"`
	Total   int          `json:"total"`
	Page    int          `json:"page"`
	PerPage int          `json:"per_page"`
}
