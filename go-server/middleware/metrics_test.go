package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// Metrics 中间件单测（P2-2）：计数/状态分类/inflight + /metrics text 格式。
// expvar 注册为包级全局，测试间相互累计——断言用"差值"或"存在性"而非绝对值。

func metricsServe(status int) *httptest.ResponseRecorder {
	handler := Metrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil))
	return w
}

func readMetricsBody(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	MetricsHandler(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func metricLine(t *testing.T, body, name, labels string) float64 {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, name+"{") || strings.HasPrefix(trimmed, name+" ") {
			if !strings.Contains(trimmed, labels) {
				continue
			}
			fields := strings.Fields(trimmed)
			v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
			if err != nil {
				t.Fatalf("metric %s parse: %v (line=%q)", name, err, trimmed)
			}
			return v
		}
	}
	// 未出现视为 0（expvar map 在无计数时不出 key；before 读数允许为 0）。
	return 0
}

func TestMetricsCountsAndClassifies(t *testing.T) {
	before2xx := metricLine(t, readMetricsBody(t), "lab_http_requests_total", `status_class="2xx"`)
	before4xx := metricLine(t, readMetricsBody(t), "lab_http_requests_total", `status_class="4xx"`)
	before5xx := metricLine(t, readMetricsBody(t), "lab_http_requests_total", `status_class="5xx"`)

	metricsServe(http.StatusOK)
	metricsServe(http.StatusOK)
	metricsServe(http.StatusBadRequest)
	metricsServe(http.StatusInternalServerError)

	after := readMetricsBody(t)
	if got := metricLine(t, after, "lab_http_requests_total", `status_class="2xx"`) - before2xx; got != 2 {
		t.Fatalf("2xx delta = %v, want 2", got)
	}
	if got := metricLine(t, after, "lab_http_requests_total", `status_class="4xx"`) - before4xx; got != 1 {
		t.Fatalf("4xx delta = %v, want 1", got)
	}
	if got := metricLine(t, after, "lab_http_requests_total", `status_class="5xx"`) - before5xx; got != 1 {
		t.Fatalf("5xx delta = %v, want 1", got)
	}
}

func TestMetricsInflightReturnsToZero(t *testing.T) {
	before := metricLine(t, readMetricsBody(t), "lab_http_requests_inflight", "")
	metricsServe(http.StatusOK)
	if got := metricLine(t, readMetricsBody(t), "lab_http_requests_inflight", "") - before; got != 0 {
		t.Fatalf("inflight must return to baseline after request, delta=%v", got)
	}
}

func TestMetricsHandlerFormat(t *testing.T) {
	SetAgentQueueProvider(func() float64 { return 3 })
	body := readMetricsBody(t)
	for _, name := range []string{"lab_http_requests_total", "lab_http_requests_inflight", "lab_agent_queue_depth"} {
		if !strings.Contains(body, name) {
			t.Fatalf("/metrics must expose %s, got:\n%s", name, body)
		}
	}
	if !strings.Contains(body, "text/plain") {
		// Content-Type 头检查在 handler 内
	}
	rec := httptest.NewRecorder()
	MetricsHandler(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	// 未注入 provider 时队列深度输出 0 而非报错。
	SetAgentQueueProvider(nil)
	if got := metricLine(t, readMetricsBody(t), "lab_agent_queue_depth", ""); got != 0 {
		t.Fatalf("queue depth without provider = %v, want 0", got)
	}
}

func TestStatusWriterPreservesFlusher(t *testing.T) {
	handler := Metrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("statusWriter must implement http.Flusher (SSE)")
		}
		flusher.Flush()
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sensors/history", nil))
}

// SlogLogFormatter 单测（P2-1）：捕获 slog 输出，断言请求日志是单行 JSON 且含
// method/path/status/request_id 等字段。
func TestSlogLogEntryWritesJSON(t *testing.T) {
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(orig)

	formatter := SlogLogFormatter{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req = req.WithContext(common.SetRequestID(req.Context(), "req_test_001"))
	entry := formatter.NewLogEntry(req)
	entry.Write(http.StatusOK, 123, nil, 5*time.Millisecond, nil)

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("log line must be valid JSON: %v\n%s", err, buf.String())
	}
	for _, key := range []string{"method", "path", "status", "bytes", "elapsed_ms", "remote_ip", "request_id"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("log entry missing field %q: %v", key, got)
		}
	}
	if got["request_id"] != "req_test_001" {
		t.Fatalf("request_id not propagated: %v", got["request_id"])
	}
}

func TestSlogLogEntryPanic(t *testing.T) {
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(orig)

	entry := SlogLogFormatter{}.NewLogEntry(httptest.NewRequest(http.MethodGet, "/x", nil))
	entry.Panic("boom", []byte("stack-trace"))
	if !bytes.Contains(buf.Bytes(), []byte("boom")) || !bytes.Contains(buf.Bytes(), []byte("stack-trace")) {
		t.Fatalf("panic log must contain value and stack: %s", buf.String())
	}
}
