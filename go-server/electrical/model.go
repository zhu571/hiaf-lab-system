package electrical

import "time"

const (
	SourceMESTEK  = "mestek"
	Source6485    = "keithley6485"
	SourceRIGOL   = "rigol_dho814"
	SourceTek     = "tek_mdo3034"
	Source33512B  = "agilent_33512b"
	Source33210A  = "agilent_33210a"
	Measurement   = "electrical_samples"
	MappingDirect = "direct"
	MappingNone   = "unmapped"
	MappingPaired = "request_response"
)

var sourceOrder = []string{SourceMESTEK, Source6485, SourceRIGOL, SourceTek, Source33512B, Source33210A}

// Sample is the unified electrical reading used by memory, InfluxDB and the API.
// Value and TextValue are mutually exclusive. Seq is a process-local sample sequence,
// deliberately stored as a field rather than an InfluxDB tag to avoid high cardinality.
type Sample struct {
	Time      time.Time `json:"time"`
	Source    string    `json:"source"`
	Channel   string    `json:"channel"`
	Metric    string    `json:"metric"`
	Unit      string    `json:"unit,omitempty"`
	Mapping   string    `json:"mapping"`
	Signature string    `json:"signature,omitempty"`
	Value     *float64  `json:"value,omitempty"`
	TextValue string    `json:"text_value,omitempty"`
	Raw       string    `json:"raw,omitempty"`
	Quality   string    `json:"quality"`
	Seq       uint64    `json:"seq"`
}

// SourceHealth exposes acquisition and persistence health independently. A device can
// be acquiring normally while its StorageError reports an InfluxDB backlog/write failure.
type SourceHealth struct {
	Source         string     `json:"source"`
	Mode           string     `json:"mode"`
	Status         string     `json:"status"`
	Connected      bool       `json:"connected"`
	LastSampleAt   *time.Time `json:"last_sample_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	StorageError   string     `json:"storage_error,omitempty"`
	SampleCount    uint64     `json:"sample_count"`
	ErrorCount     uint64     `json:"error_count"`
	WriteErrors    uint64     `json:"write_errors"`
	MaxGapMS       int64      `json:"max_gap_ms"`
	CRCErrors      uint64     `json:"crc_errors"`
	PartialBytes   int        `json:"partial_bytes"`
	PairingErrors  uint64     `json:"pairing_errors"`
	RequestFrames  uint64     `json:"request_frames"`
	ResponseFrames uint64     `json:"response_frames"`
	MappedFrames   uint64     `json:"mapped_frames"`
	Mapping        string     `json:"mapping,omitempty"`
}

type LatestResult struct {
	ContinuousEnabled bool           `json:"continuous_enabled"`
	UpdatedAt         time.Time      `json:"updated_at"`
	WriteBacklog      int            `json:"write_backlog"`
	Sources           []SourceHealth `json:"sources"`
	Samples           []Sample       `json:"samples"`
}

type HistoryResult struct {
	Points []Sample `json:"points"`
}

type SingleShotResult struct {
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt time.Time         `json:"finished_at"`
	Samples    []Sample          `json:"samples"`
	Sources    []SourceHealth    `json:"sources"`
	Errors     map[string]string `json:"errors,omitempty"`
}
