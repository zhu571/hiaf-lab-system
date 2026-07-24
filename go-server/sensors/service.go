package sensors

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

var defaultMeasurements = []string{"pressure", "vacuum", "control", "temperature", "pump"}

// safeFluxValue validates a Flux value against injection.
// Only allow durations (like -1h, 30m, 1d), now(), or positive integers.
func safeFluxValue(s string) error {
	if s == "now()" {
		return nil
	}
	if matched, _ := regexp.MatchString(`^-?\d+(ns|us|ms|s|m|h|d|w|mo|y)$`, s); matched {
		return nil
	}
	if matched, _ := regexp.MatchString(`^\d+$`, s); matched {
		return nil
	}
	return fmt.Errorf("invalid flux value: %s", s)
}

// safeTag validates a tag string for Flux injection safety.
func safeTag(s string) error {
	if strings.Contains(s, `"`) || strings.Contains(s, `\`) || strings.Contains(s, `\n`) || strings.Contains(s, `\r`) {
		return fmt.Errorf("invalid tag: %s", s)
	}
	return nil
}

// Service queries InfluxDB for sensor data.
type Service struct {
	client       *http.Client
	addr         string
	token        string
	org          string
	bucket       string
	measurements map[string]bool
}

// Config carries InfluxDB connection settings.
type Config struct {
	Addr         string
	Token        string
	Org          string
	Bucket       string
	Measurements []string
}

// NewService creates a sensors Service from environment and Docker secrets.
func NewService() (*Service, error) {
	token, err := common.ReadSecret("/run/secrets/influxdb_token", "INFLUXDB_TOKEN")
	if err != nil {
		return nil, fmt.Errorf("read influxdb token: %w", err)
	}
	cfg := Config{
		Addr:         os.Getenv("INFLUXDB_ADDR"),
		Token:        token,
		Org:          os.Getenv("INFLUXDB_ORG"),
		Bucket:       os.Getenv("INFLUXDB_BUCKET"),
		Measurements: envList("INFLUXDB_MEASUREMENTS", defaultMeasurements),
	}
	if cfg.Addr == "" || cfg.Org == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("INFLUXDB_ADDR, INFLUXDB_ORG, and INFLUXDB_BUCKET are required")
	}
	return NewServiceWithConfig(cfg), nil
}

// NewServiceWithConfig creates a Service for tests and explicit callers.
func NewServiceWithConfig(cfg Config) *Service {
	measurements := make(map[string]bool, len(cfg.Measurements))
	for _, m := range cfg.Measurements {
		m = strings.TrimSpace(m)
		if m != "" {
			measurements[m] = true
		}
	}
	return &Service{
		client:       &http.Client{Timeout: 10 * time.Second},
		addr:         normalizeHTTPBase(cfg.Addr),
		token:        cfg.Token,
		org:          cfg.Org,
		bucket:       cfg.Bucket,
		measurements: measurements,
	}
}

func envList(key string, def []string) []string {
	if v := os.Getenv(key); v != "" {
		return strings.Split(v, ",")
	}
	return def
}

func normalizeHTTPBase(addr string) string {
	addr = strings.TrimRight(addr, "/")
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}

// queryInflux runs a Flux query against InfluxDB v2 API and returns the raw CSV body.
func (s *Service) queryInflux(flux string) ([]byte, error) {
	u := fmt.Sprintf("%s/api/v2/query?org=%s", s.addr, url.QueryEscape(s.org))
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
		var clauses []string
		for _, t := range strings.Split(tags, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				if !s.measurements[t] {
					return nil, fmt.Errorf("unknown measurement: %s", t)
				}
				if err := safeTag(t); err != nil {
					return nil, fmt.Errorf("invalid measurement: %w", err)
				}
				clauses = append(clauses, fmt.Sprintf(`r["_measurement"] == "%s"`, t))
			}
		}
		if len(clauses) > 0 {
			tagFilter = fmt.Sprintf("  |> filter(fn: (r) => %s)\n", strings.Join(clauses, " or "))
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
	if !s.measurements[tag] {
		return nil, fmt.Errorf("unknown measurement: %s", tag)
	}
	rangeStart := "-1h"
	if from != "" {
		if err := safeFluxValue(from); err != nil {
			return nil, fmt.Errorf("invalid from parameter: %w", err)
		}
		rangeStart = from
	}
	rangeStop := "now()"
	if to != "" {
		if err := safeFluxValue(to); err != nil {
			return nil, fmt.Errorf("invalid to parameter: %w", err)
		}
		rangeStop = to
	}

	flux := fmt.Sprintf(`from(bucket: "%s")
  |> range(start: %s, stop: %s)
  |> filter(fn: (r) => r["_measurement"] == "%s")`, s.bucket, rangeStart, rangeStop, tag)

	if interval != "" {
		if err := safeFluxValue(interval); err != nil {
			return nil, fmt.Errorf("invalid interval parameter: %w", err)
		}
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
		v, err := strconv.ParseFloat(strings.TrimSpace(cols[valueIdx]), 64)
		if err != nil {
			continue
		}
		tag := ""
		if tagIdx >= 0 {
			tag = strings.TrimSpace(cols[tagIdx])
		}
		p := SensorPoint{
			Time:  strings.TrimSpace(cols[timeIdx]),
			Tag:   tag,
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
