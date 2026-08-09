package middleware

import (
	"expvar"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
)

// 指标实现（P2-2，零第三方依赖）：
//   - counter 用 expvar.NewMap().Add（按 status_class 分桶）；
//   - gauge（inflight / agent 队列深度）用 expvar.Func 包装 atomic 值
//     （expvar 无原生原子 gauge，Func 求值时读取当前值）。
// /metrics 以 Prometheus text exposition 格式输出（手写迷你 encoder）。

var (
	httpRequestsTotal = expvar.NewMap("lab_http_requests_total")
	requestsInflight  int64

	// agentQueueProvider 由 main.go 经 SetAgentQueueProvider 注入（agent 模块
	// Service.QueueDepth），杜绝 middleware 跨模块直读表。
	agentQueueProvider atomic.Value // func() float64
)

// SetAgentQueueProvider 注入 agent 队列深度读取函数（未注入时 /metrics 输出 0）。
func SetAgentQueueProvider(f func() float64) {
	agentQueueProvider.Store(f)
}

// Metrics 是请求计数中间件：包裹所有请求（状态分类计数 + inflight +1/-1）。
// 注册位置约束见 main.go：RequestID 之后、RequestLogger 之前。
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestsInflight, 1)
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			atomic.AddInt64(&requestsInflight, -1)
			httpRequestsTotal.Add(statusClass(sw.status), 1)
		}()
		next.ServeHTTP(sw, r)
	})
}

// statusWriter 捕获首个非 1xx 状态码（无 WriteHeader 调用视为 200）。
// 实现 http.Flusher：system/instruments 的 SSE 流端点依赖 Flush()。
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 && code >= 200 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func statusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}

// MetricsHandler 输出 Prometheus text exposition 格式（/metrics，不经过 AuthRequired，
// 运维探针可达；ServiceToken 白名单只拦 /api/v1/daily-reports/by-date 与 /api/v1/ask/execute）。
func MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintln(w, "# HELP lab_http_requests_total Total HTTP requests by status class.")
	fmt.Fprintln(w, "# TYPE lab_http_requests_total counter")
	httpRequestsTotal.Do(func(kv expvar.KeyValue) {
		fmt.Fprintf(w, "lab_http_requests_total{status_class=%q} %s\n", kv.Key, kv.Value.String())
	})
	fmt.Fprintln(w, "# TYPE lab_http_requests_inflight gauge")
	fmt.Fprintf(w, "lab_http_requests_inflight %d\n", atomic.LoadInt64(&requestsInflight))
	fmt.Fprintln(w, "# TYPE lab_agent_queue_depth gauge")
	fmt.Fprintf(w, "lab_agent_queue_depth %s\n", strconv.FormatFloat(agentQueueDepth(), 'f', -1, 64))
}

func agentQueueDepth() float64 {
	if f, ok := agentQueueProvider.Load().(func() float64); ok && f != nil {
		return f()
	}
	return 0
}
