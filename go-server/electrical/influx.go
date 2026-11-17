package electrical

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var fluxDurationPattern = regexp.MustCompile(`^-?\d+(ns|us|ms|s|m|h|d|w)$`)

type InfluxClient struct {
	cfg    InfluxConfig
	client *http.Client
}

func NewInfluxClient(cfg InfluxConfig) *InfluxClient {
	cfg.Addr = strings.TrimRight(cfg.Addr, "/")
	if !strings.HasPrefix(cfg.Addr, "http://") && !strings.HasPrefix(cfg.Addr, "https://") {
		cfg.Addr = "http://" + cfg.Addr
	}
	return &InfluxClient{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}}
}

func (c *InfluxClient) WriteBatch(ctx context.Context, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	var body strings.Builder
	for _, sample := range samples {
		body.WriteString(lineProtocol(sample))
	}
	endpoint := fmt.Sprintf("%s/api/v2/write?org=%s&bucket=%s&precision=ns", c.cfg.Addr, url.QueryEscape(c.cfg.Org), url.QueryEscape(c.cfg.Bucket))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body.String()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+c.cfg.Token)
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("influx write returned %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func lineProtocol(sample Sample) string {
	var line strings.Builder
	line.WriteString(Measurement)
	for _, tag := range [][2]string{
		{"source", sample.Source}, {"channel", sample.Channel}, {"metric", sample.Metric},
		{"unit", sample.Unit}, {"mapping", sample.Mapping}, {"signature", sample.Signature},
	} {
		line.WriteByte(',')
		line.WriteString(escapeLineTag(tag[0]))
		line.WriteByte('=')
		line.WriteString(escapeLineTag(tag[1]))
	}
	line.WriteByte(' ')
	fields := []string{}
	if sample.Value != nil {
		fields = append(fields, "value="+strconv.FormatFloat(*sample.Value, 'g', -1, 64))
	}
	if sample.TextValue != "" {
		fields = append(fields, `text_value="`+escapeLineString(sample.TextValue)+`"`)
	}
	if sample.Raw != "" {
		fields = append(fields, `raw="`+escapeLineString(sample.Raw)+`"`)
	}
	quality := sample.Quality
	if quality == "" {
		quality = "good"
	}
	fields = append(fields, `quality="`+escapeLineString(quality)+`"`)
	fields = append(fields, fmt.Sprintf("seq=%di", sample.Seq))
	line.WriteString(strings.Join(fields, ","))
	line.WriteByte(' ')
	at := sample.Time
	if at.IsZero() {
		at = time.Now()
	}
	line.WriteString(strconv.FormatInt(at.UnixNano(), 10))
	line.WriteByte('\n')
	return line.String()
}

func escapeLineTag(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", " ", "\\ ", ",", "\\,", "=", "\\=", "\n", "\\n", "\r", "\\r")
	return replacer.Replace(value)
}

func escapeLineString(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r")
	return replacer.Replace(value)
}

type HistoryParams struct {
	Source  string
	Channel string
	Metric  string
	From    string
	To      string
	Limit   int
}

func (c *InfluxClient) History(ctx context.Context, params HistoryParams) ([]Sample, error) {
	from, err := fluxTime(params.From, "-15m")
	if err != nil {
		return nil, fmt.Errorf("invalid from: %w", err)
	}
	to, err := fluxTime(params.To, "now()")
	if err != nil {
		return nil, fmt.Errorf("invalid to: %w", err)
	}
	limit := params.Limit
	if limit == 0 {
		limit = 1000
	}
	if limit < 1 || limit > 5000 {
		return nil, fmt.Errorf("limit must be between 1 and 5000")
	}
	filters := []string{`r["_measurement"] == "` + Measurement + `"`}
	for _, filter := range [][3]string{{"source", params.Source, ""}, {"channel", params.Channel, ""}, {"metric", params.Metric, ""}} {
		if filter[1] == "" {
			continue
		}
		if err := safeHistoryTag(filter[1]); err != nil {
			return nil, fmt.Errorf("invalid %s: %w", filter[0], err)
		}
		filters = append(filters, fmt.Sprintf(`r[%q] == %q`, filter[0], filter[1]))
	}
	flux := fmt.Sprintf(`from(bucket: %q)
  |> range(start: %s, stop: %s)
  |> filter(fn: (r) => %s)
  |> pivot(rowKey: ["_time", "source", "channel", "metric", "unit", "mapping", "signature"], columnKey: ["_field"], valueColumn: "_value")
  |> sort(columns: ["_time"], desc: true)
  |> limit(n: %d)
  |> sort(columns: ["_time"])`, c.cfg.Bucket, from, to, strings.Join(filters, " and "), limit)
	endpoint := fmt.Sprintf("%s/api/v2/query?org=%s", c.cfg.Addr, url.QueryEscape(c.cfg.Org))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(flux))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+c.cfg.Token)
	req.Header.Set("Content-Type", "application/vnd.flux")
	req.Header.Set("Accept", "application/csv")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("influx query returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return parseHistoryCSV(body)
}

func fluxTime(value, fallback string) (string, error) {
	if value == "" {
		value = fallback
	}
	if value == "now()" || fluxDurationPattern.MatchString(value) {
		return value, nil
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return strconv.Quote(value), nil
	}
	return "", fmt.Errorf("must be a Flux duration or RFC3339 timestamp")
}

func safeHistoryTag(value string) error {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\"\\\r\n") {
		return fmt.Errorf("unsafe or empty tag value")
	}
	return nil
}

func parseHistoryCSV(body []byte) ([]Sample, error) {
	reader := csv.NewReader(bytes.NewReader(body))
	reader.Comment = '#'
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	points := make([]Sample, 0)
	var headers []string
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse influx CSV: %w", err)
		}
		if len(row) == 0 || (len(row) == 1 && strings.TrimSpace(row[0]) == "") {
			continue
		}
		if containsString(row, "_time") && (containsString(row, "value") || containsString(row, "_value")) {
			headers = row
			continue
		}
		if len(headers) == 0 || len(row) != len(headers) {
			continue
		}
		values := make(map[string]string, len(headers))
		for i, header := range headers {
			values[header] = row[i]
		}
		at, err := time.Parse(time.RFC3339Nano, values["_time"])
		if err != nil {
			continue
		}
		sample := Sample{
			Time: at, Source: values["source"], Channel: values["channel"], Metric: values["metric"],
			Unit: values["unit"], Mapping: values["mapping"], Signature: values["signature"],
			TextValue: values["text_value"], Raw: values["raw"], Quality: values["quality"],
		}
		if sample.Quality == "" {
			sample.Quality = "good"
		}
		if rawValue := values["value"]; rawValue != "" {
			value, err := strconv.ParseFloat(rawValue, 64)
			if err != nil {
				continue
			}
			sample.Value = &value
		}
		if rawSeq := values["seq"]; rawSeq != "" {
			sample.Seq, _ = strconv.ParseUint(rawSeq, 10, 64)
		}
		points = append(points, sample)
	}
	return points, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
