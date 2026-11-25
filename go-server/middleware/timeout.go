package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// Timeout 给单条路由加 deadline（AR-07）。SSE / 大上传路由不要使用。
// 前提：handler 把 r.Context() 传到 DB / 下游 HTTP；否则超时只返回 504，底层仍会跑完。
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			tw := &timeoutWriter{ResponseWriter: w}
			next.ServeHTTP(tw, r.WithContext(ctx))

			if ctx.Err() == context.DeadlineExceeded && !tw.wroteHeader {
				common.WriteError(tw, r, http.StatusGatewayTimeout, "request_timeout", "请求处理超时，请稍后重试", nil)
			}
		})
	}
}

type timeoutWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *timeoutWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *timeoutWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *timeoutWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
