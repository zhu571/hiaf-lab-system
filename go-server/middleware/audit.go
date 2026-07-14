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

// Audit logs non-read HTTP requests to the audit_log table asynchronously.
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
			var userID, username string
			if claims != nil {
				userID = claims.UserID
				username = claims.Username
			}

			action := strings.TrimPrefix(r.URL.Path, "/api/v1/")
			action = strings.ReplaceAll(action, "/", ".")

			clientIP := r.RemoteAddr
			if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
				clientIP = fwd
			}

			// Async insert so audit logging never blocks the response.
			go func() {
				if _, err := db.Exec(
					`INSERT INTO audit_log (request_id, user_id, username, method, path, action, status_code, client_ip)
					 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
					common.GetRequestID(r.Context()),
					nullString(userID),
					username,
					r.Method,
					r.URL.Path,
					action,
					rw.statusCode,
					clientIP,
				); err != nil {
					slog.Error("audit log insert failed", "error", err, "request_id", common.GetRequestID(r.Context()))
				}
			}()
		})
	}
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
