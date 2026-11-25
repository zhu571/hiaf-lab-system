package scans

import "time"

// Scan 是 scans 表一行（scan-archive v0.4 §3）。
type Scan struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"project_id"`
	Machine      string     `json:"machine"`
	Filename     string     `json:"filename"`
	SourceTag    string     `json:"source_tag"`
	Tags         []string   `json:"tags"`
	ScanAt       *time.Time `json:"scan_at,omitempty"`
	ScanAtSource string     `json:"scan_at_source"`
	Format       string     `json:"format"`
	Columns      []string   `json:"columns"`
	RowCount     int        `json:"row_count"`
	BadRows      int        `json:"bad_rows"`
	ParseWarning string     `json:"parse_warning,omitempty"`
	TXTSHA256    string     `json:"txt_sha256"`
	PNGSHA256    *string    `json:"png_sha256,omitempty"`
	TXTSize      int64      `json:"txt_size"`
	PNGSize      *int64     `json:"png_size,omitempty"`
	TXTPath      string     `json:"txt_path"`
	PNGPath      *string    `json:"png_path,omitempty"`
	ThumbPath    *string    `json:"thumb_path,omitempty"`
	SourcePath   string     `json:"source_path,omitempty"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	AnnotatedAt  *time.Time `json:"annotated_at,omitempty"`
	IngestSource string     `json:"ingest_source"`
	IsVoid       bool       `json:"is_void"`
	VoidedAt     *time.Time `json:"voided_at,omitempty"`
	VoidedBy     *string    `json:"voided_by,omitempty"`
	VoidReason   *string    `json:"void_reason,omitempty"`
	UploadedBy   *string    `json:"uploaded_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// ListParams 是列表过滤条件；Tags 为空表示不过滤。
type ListParams struct {
	ProjectID    string
	Tags         []string
	TagMode      string // any / all
	From         string
	To           string
	Q            string
	Annotated    *bool
	Format       string
	HasPNG       *bool
	IngestSource string
	IncludeVoid  bool
	Page         int
	PerPage      int
}

type ListResult struct {
	Items   []Scan `json:"items"`
	Total   int    `json:"total"`
	Page    int    `json:"page"`
	PerPage int    `json:"per_page"`
}

type UpdateScanRequest struct {
	Title       *string   `json:"title,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

type VoidRequest struct {
	Reason string `json:"reason"`
}

// UploadRequest 是人工上传/watch ingest 的统一入参（文件字节由 handler 读取）。
type UploadRequest struct {
	Filename     string
	TXT          []byte
	PNG          []byte
	Machine      string
	ScanAt       *time.Time
	ScanAtSource string
	SourceTag    string
	Tags         []string
	Title        string
	Description  string
	SourcePath   string
	IngestSource string
}

type UploadResult struct {
	*Scan
	Duplicate bool `json:"duplicate"`
}

type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}
