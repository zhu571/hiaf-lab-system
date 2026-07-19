package sensors

// SensorPoint is a single data point returned from InfluxDB.
type SensorPoint struct {
	Time  string                 `json:"time"`
	Tag   string                 `json:"tag"`
	Value float64                `json:"value"`
	Meta  map[string]string      `json:"meta,omitempty"`
}

// LatestResult wraps a single latest sensor reading.
type LatestResult struct {
	Points []SensorPoint `json:"points"`
}

// HistoryResult wraps a time-series result.
type HistoryResult struct {
	Points []SensorPoint `json:"points"`
}
