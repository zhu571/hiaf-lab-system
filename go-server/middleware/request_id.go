package middleware

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// RequestID injects a request ID into the context and response headers.
// It reuses X-Request-Id from the client if present, otherwise generates a UUID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.New().String()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := common.SetRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
