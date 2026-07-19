package middleware

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// responseWriter wraps http.ResponseWriter to capture the written status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Audit logs non-read HTTP requests to the audit_log table.
func Audit(db *sql.DB) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)

			claims := GetUserClaims(r.Context())
			var userID, username, actorType string
			if claims != nil {
				userID = claims.UserID
				username = claims.Username
				actorType = "user"
				if claims.Role == "agent" {
					actorType = "agent"
				}
			}

			action := strings.TrimPrefix(r.URL.Path, "/api/v1/")
			action = strings.ReplaceAll(action, "/", ".")

			clientIP := r.RemoteAddr
			if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
				clientIP = fwd
			}

			if _, err := db.Exec(
				`INSERT INTO audit_log
				 (request_id, user_id, username, method, path, action, status_code, client_ip,
				  actor_type, acting_user_id, agent_task_id, idempotency_key)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
				common.GetRequestID(r.Context()),
				nullString(userID),
				username,
				r.Method,
				r.URL.Path,
				action,
				rw.statusCode,
				clientIP,
				actorType,
				nullString(ActingUserID(r.Context())),
				nullString(AgentTaskID(r.Context())),
				nullString(r.Header.Get("Idempotency-Key")),
			); err != nil {
				slog.Error("audit log insert failed", "error", err, "request_id", common.GetRequestID(r.Context()))
			}
		})
	}
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
