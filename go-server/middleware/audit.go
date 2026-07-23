package middleware

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

type auditActionKeyType string

const auditActionKey auditActionKeyType = "audit_action"

// responseWriter wraps http.ResponseWriter to capture the written status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Audit logs writes and reads that explicitly set an audit action.
func Audit(db *sql.DB) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actionOverride := ""
			r = r.WithContext(context.WithValue(r.Context(), auditActionKey, &actionOverride))
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)
			if (r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions) && actionOverride == "" {
				return
			}

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
			if actionOverride != "" {
				action = actionOverride
			}

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

func SetAuditAction(ctx context.Context, action string) {
	if target, _ := ctx.Value(auditActionKey).(*string); target != nil {
		*target = action
	}
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
