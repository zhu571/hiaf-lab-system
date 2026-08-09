package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

type auditActionKeyType string

const auditActionKey auditActionKeyType = "audit_action"

type auditDetailKeyType string

const auditDetailKey auditDetailKeyType = "audit_detail"

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
			detail := map[string]any(nil)
			r = r.WithContext(context.WithValue(r.Context(), auditDetailKey, &detail))
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
			} else if IsServiceCall(r.Context()) {
				// service token 调用（白名单端点，见 service_token.go）：无 JWT claims，
				// 落 actor_type='system' 便于审计区分系统内部调用。
				actorType = "system"
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

			if err := insertAuditLog(r.Context(), db, auditRow{
				requestID:      common.GetRequestID(r.Context()),
				userID:         nullString(userID),
				username:       username,
				method:         r.Method,
				path:           r.URL.Path,
				action:         action,
				statusCode:     rw.statusCode,
				clientIP:       clientIP,
				actorType:      actorType,
				actingUserID:   nullString(ActingUserID(r.Context())),
				agentTaskID:    nullString(AgentTaskID(r.Context())),
				idempotencyKey: nullString(r.Header.Get("Idempotency-Key")),
				detail:         detail,
			}); err != nil {
				slog.Error("audit log insert failed", "error", err, "request_id", common.GetRequestID(r.Context()))
			}
		})
	}
}

// auditRow 是 audit_log 一行记录的字段集合，供 HTTP 中间件与系统内部写入共用。
type auditRow struct {
	requestID      string
	userID         sql.NullString
	username       string
	method         string
	path           string
	action         string
	statusCode     int
	clientIP       string
	actorType      string
	actingUserID   sql.NullString
	agentTaskID    sql.NullString
	idempotencyKey sql.NullString
	detail         map[string]any
}

// insertAuditLog 是 audit_log 的共享 INSERT 写入器。
// 029 hash 链并发正确性：BIGSERIAL 的 nextval 在 BEFORE INSERT 触发器执行前已分配，
// 仅触发器内加锁会产生"id 序 ≠ 锁序"分叉；因此 advisory lock 前置到本函数——
// INSERT 同一事务先取 pg_advisory_xact_lock(714001)，id 分配发生在锁内、id 序==链序。
// 触发器内锁保留（同事务可重入），防 psql 直插等非应用层写入。
func insertAuditLog(ctx context.Context, db *sql.DB, row auditRow) error {
	// detail 是 JSONB：lib/pq 不接受 map 直接传参，必须先 Marshal（[]byte 按 JSONB 写入）。
	var detail any
	if row.detail != nil {
		data, err := json.Marshal(row.detail)
		if err != nil {
			return fmt.Errorf("marshal audit detail: %w", err)
		}
		detail = data
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin audit tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(714001)`); err != nil {
		return fmt.Errorf("acquire audit chain lock: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_log
		 (request_id, user_id, username, method, path, action, status_code, client_ip,
		  actor_type, acting_user_id, agent_task_id, idempotency_key, detail)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		row.requestID,
		row.userID,
		row.username,
		row.method,
		row.path,
		row.action,
		row.statusCode,
		row.clientIP,
		row.actorType,
		row.actingUserID,
		row.agentTaskID,
		row.idempotencyKey,
		detail,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// WriteSystemAudit 追加一条 actor_type='system' 的审计行。
// 例外声明：scheduler 无 HTTP 上下文，无法走 Audit 中间件；audit_log 为审计专用
// append-only 表（应用层约定），此写入仅 INSERT 不修改/删除，为最小跨模块写例外。
func WriteSystemAudit(ctx context.Context, db *sql.DB, action string, detail map[string]any) error {
	requestID := common.GetRequestID(ctx)
	if requestID == "" {
		requestID = "sys_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return insertAuditLog(ctx, db, auditRow{
		requestID:  requestID,
		username:   "system",
		method:     "SYSTEM",
		path:       "",
		action:     action,
		statusCode: 200,
		clientIP:   "",
		actorType:  "system",
		detail:     detail,
	})
}

func SetAuditAction(ctx context.Context, action string) {
	if target, _ := ctx.Value(auditActionKey).(*string); target != nil {
		*target = action
	}
}

// SetAuditDetail 让 handler 补充审计明细（如 AI 解析的返回状态与条数），仍由 Audit 中间件统一落库。
func SetAuditDetail(ctx context.Context, detail map[string]any) {
	if target, _ := ctx.Value(auditDetailKey).(*map[string]any); target != nil {
		*target = detail
	}
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
