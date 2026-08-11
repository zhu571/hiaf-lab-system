package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// SlogLogFormatter 是 chi 请求日志的 slog JSON 实现（P2-1）。
// 替代 chi middleware.Logger 的纯文本输出，使 docker logs / 日志平台可统一解析；
// 一行请求一条 slog JSON（method/path/status/bytes/elapsed_ms/remote_ip/request_id）。
type SlogLogFormatter struct{}

// NewLogEntry 构造请求日志条目：此时 RequestID 中间件已注入 request_id 到 context
// （顺序约束：RequestLogger 必须在 RequestID 之后注册，见 main.go）；
// source_kind 由外层 SourceGate 写入 ctx（缺失时省略，兼容未挂载来源门的场景）。
func (SlogLogFormatter) NewLogEntry(r *http.Request) middleware.LogEntry {
	return &slogLogEntry{
		method:     r.Method,
		path:       r.URL.Path,
		remoteIP:   r.RemoteAddr,
		requestID:  common.GetRequestID(r.Context()),
		sourceKind: GetSourceKind(r.Context()),
	}
}

type slogLogEntry struct {
	method     string
	path       string
	remoteIP   string
	requestID  string
	sourceKind string
}

// Write 输出请求完成日志（状态码/字节数/耗时），slog.Info 单行 JSON。
func (e *slogLogEntry) Write(status, bytes int, header http.Header, elapsed time.Duration, extra interface{}) {
	attrs := []any{
		"method", e.method,
		"path", e.path,
		"status", status,
		"bytes", bytes,
		"elapsed_ms", elapsed.Milliseconds(),
		"remote_ip", e.remoteIP,
		"request_id", e.requestID,
	}
	if e.sourceKind != "" {
		attrs = append(attrs, "source_kind", e.sourceKind)
	}
	slog.Info("http request", attrs...)
}

// Panic 输出 panic 恢复日志（chi 内部调用），slog.Error + stack。
func (e *slogLogEntry) Panic(v interface{}, stack []byte) {
	slog.Error("http request panicked",
		"method", e.method,
		"path", e.path,
		"panic", v,
		"stack", string(stack),
	)
}
