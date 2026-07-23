package middleware

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func RequireIdempotencyKey(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
				return
			}

			var existingRequestID string
			err := db.QueryRow(
				`SELECT request_id FROM idempotency_keys WHERE idempotency_key = $1`,
				key,
			).Scan(&existingRequestID)
			if err == nil {
				common.WriteError(w, r, http.StatusConflict, "duplicate_idempotency_key",
					"该 Idempotency-Key 已被使用，请勿重复提交", map[string]any{
						"existing_request_id": existingRequestID,
					})
				return
			}
			if err != sql.ErrNoRows {
				slog.Error("idempotency key lookup failed", "error", err, "key", key)
			}

			requestID := common.GetRequestID(r.Context())
			_, err = db.Exec(
				`INSERT INTO idempotency_keys (idempotency_key, request_id, created_at)
				 VALUES ($1, $2, now())
				 ON CONFLICT (idempotency_key) DO NOTHING`,
				key, requestID,
			)
			if err != nil {
				slog.Error("store idempotency key failed", "error", err, "key", key)
			}

			next.ServeHTTP(w, r)
		})
	}
}
