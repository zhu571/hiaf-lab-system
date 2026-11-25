package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// Recoverer 兜底 handler panic（AR-07）：
//   - 记录 stack 与 request_id；
//   - lab_panics_total +1；
//   - 响应尚未开始时返回统一 500 envelope；已开始写则只记录（无法再改状态码）。
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tw := &trackingWriter{ResponseWriter: w}
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			panicsTotal.Add(1)
			slog.Error("panic recovered",
				"panic", rec,
				"stack", string(debug.Stack()),
				"request_id", common.GetRequestID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
			)
			if !tw.wroteHeader {
				common.WriteError(tw, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
			}
		}()
		next.ServeHTTP(tw, r)
	})
}

// trackingWriter 只记录是否已写头，避免 panic 后重复 WriteHeader。
type trackingWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *trackingWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *trackingWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *trackingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
