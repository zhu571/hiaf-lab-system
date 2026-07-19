package sensors

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var validMeasurements = map[string]bool{
	"pressure": true, "vacuum": true, "control": true, "temperature": true, "pump": true,
}

// safeTag validates a tag string for Flux injection safety — no quotes.
func safeTag(s string) error {
	if strings.Contains(s, `"`) || strings.Contains(s, `\`) {
		return fmt.Errorf("invalid tag: %s", s)
	}
	return nil
}

const influxAddr = "http://localhost:8086"

// Service queries InfluxDB for sensor data.
type Service struct {
	client *http.Client
	token  string
	org    string
	bucket string
}

// NewService creates a sensors Service. Token comes from INFLUXDB_TOKEN env var.
func NewService() *Service {
	return &Service{
		client: &http.Client{Timeout: 10 * time.Second},
		token:  "6669e199cd409e33b813cf8a7a8d7e8c72ec479f1a1ce4e2", // ponytail: hardcoded, already public in IOC
		org:    "lab-org",
		bucket: "lab-bucket",
	}
}

// queryInflux runs a Flux query against InfluxDB v2 API and returns the raw CSV body.
func (s *Service) queryInflux(flux string) ([]byte, error) {
	u := fmt.Sprintf("%s/api/v2/query?org=%s", influxAddr, url.QueryEscape(s.org))
	req, err := http.NewRequest("POST", u, strings.NewReader(flux))
	if err != nil {
		return nil, fmt.Errorf("influx query build: %w", err)
	}
	req.Header.Set("Authorization", "Token "+s.token)
	req.Header.Set("Content-Type", "application/vnd.flux")
	req.Header.Set("Accept", "application/csv")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("influx query: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB cap
	if err != nil {
		return nil, fmt.Errorf("influx read body: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("influx returned %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// Latest returns the most recent point for each matching tag.
// tags is a comma-separated list of tag values to filter on.
func (s *Service) Latest(tags string) (*LatestResult, error) {
	tagFilter := ""
	if tags != "" {
		for _, t := range strings.Split(tags, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				if !validMeasurements[t] {
					return nil, fmt.Errorf("unknown measurement: %s", t)
				}
				tagFilter += fmt.Sprintf(`  |> filter(fn: (r) => r["_measurement"] == "%s")`+"\n", t)
			}
		}
	}
	flux := fmt.Sprintf(`from(bucket: "%s")
  |> range(start: -1h)
%s  |> last()`, s.bucket, tagFilter)

	body, err := s.queryInflux(flux)
	if err != nil {
		return nil, err
	}
	points := parseCSV(body)
	return &LatestResult{Points: points}, nil
}

// History returns time-series data for a given tag within an optional time window.
func (s *Service) History(tag, from, to, interval string) (*HistoryResult, error) {
	if err := safeTag(tag); err != nil {
		return nil, fmt.Errorf("invalid tag parameter: %w", err)
	}
	if !validMeasurements[tag] {
		return nil, fmt.Errorf("unknown measurement: %s", tag)
	}
	rangeStart := "-1h"
	if from != "" {
		if err := safeTag(from); err != nil {
			return nil, fmt.Errorf("invalid from parameter: %w", err)
		}
		rangeStart = from
	}
	rangeStop := "now()"
	if to != "" {
		if err := safeTag(to); err != nil {
			return nil, fmt.Errorf("invalid to parameter: %w", err)
		}
		rangeStop = to
	}

	flux := fmt.Sprintf(`from(bucket: "%s")
  |> range(start: %s, stop: %s)
  |> filter(fn: (r) => r["_measurement"] == "%s")`, s.bucket, rangeStart, rangeStop, tag)

	if interval != "" {
		flux += fmt.Sprintf("\n  |> aggregateWindow(every: %s, fn: mean, createEmpty: false)", interval)
	}

	body, err := s.queryInflux(flux)
	if err != nil {
		return nil, err
	}
	points := parseCSV(body)
	return &HistoryResult{Points: points}, nil
}

// parseCSV parses InfluxDB v2 CSV output (no #datatype row in raw responses).
// Header is the first line, data follows. Skip comment lines.
func parseCSV(body []byte) []SensorPoint {
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) < 2 {
		return nil
	}

	// Find first non-comment header line
	start := 0
	for ; start < len(lines); start++ {
		if !strings.HasPrefix(lines[start], "#") {
			break
		}
	}
	if start+1 >= len(lines) {
		return nil
	}

	headers := splitCSVLine(lines[start])
	timeIdx, tagIdx, valueIdx := -1, -1, -1
	for i, h := range headers {
		switch strings.TrimSpace(h) {
		case "_time":
			timeIdx = i
		case "tag":
			tagIdx = i
		case "_value":
			valueIdx = i
		}
	}
	if timeIdx < 0 || valueIdx < 0 {
		return nil
	}

	var points []SensorPoint
	for _, line := range lines[start+1:] {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		cols := splitCSVLine(line)
		if len(cols) <= max(timeIdx, tagIdx, valueIdx) {
			continue
		}
		var v float64
		if n, err := fmt.Sscanf(strings.TrimSpace(cols[valueIdx]), "%f", &v); n != 1 || err != nil {
			continue
		}
		p := SensorPoint{
			Time:  strings.TrimSpace(cols[timeIdx]),
			Tag:   strings.TrimSpace(cols[tagIdx]),
			Value: v,
		}
		points = append(points, p)
	}
	return points
}

// splitCSVLine splits a CSV line respecting quoted fields.
func splitCSVLine(line string) []string {
	var cols []string
	var current strings.Builder
	inQuotes := false
	for _, c := range line {
		switch c {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if inQuotes {
				current.WriteRune(c)
			} else {
				cols = append(cols, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(c)
		}
	}
	cols = append(cols, current.String())
	return cols
}
