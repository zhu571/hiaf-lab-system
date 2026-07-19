package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

type agentContextKey string

const (
	actingUserKey agentContextKey = "acting_user_id"
	agentTaskKey  agentContextKey = "agent_task_id"
)

// AgentContext validates delegated Agent requests and rejects delegation
// headers sent by ordinary users.
func AgentContext(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserClaims(r.Context())
			actingUserID := strings.TrimSpace(r.Header.Get("X-Acting-User-ID"))
			taskID := strings.TrimSpace(r.Header.Get("X-Agent-Task-ID"))
			if claims == nil {
				common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
				return
			}
			if claims.Role != "agent" {
				if actingUserID != "" || taskID != "" {
					common.WriteError(w, r, http.StatusBadRequest, "invalid_agent_context", "普通用户不得携带 Agent 代理请求头", nil)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if actingUserID == "" || taskID == "" {
				common.WriteError(w, r, http.StatusBadRequest, "invalid_agent_context", "Agent 请求缺少代理用户或任务 ID", nil)
				return
			}
			if !agentBusinessPathAllowed(r.Method, r.URL.Path) {
				common.WriteError(w, r, http.StatusForbidden, "agent_action_forbidden", "Agent 不允许执行该业务操作", nil)
				return
			}

			var valid bool
			if err := db.QueryRowContext(r.Context(),
				`SELECT EXISTS (
					SELECT 1 FROM pending_agent_tasks
					WHERE id = $1 AND acting_user_id = $2 AND status = 'processing'
				)`, taskID, actingUserID,
			).Scan(&valid); err != nil {
				common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "Agent 任务校验失败", nil)
				return
			}
			if !valid {
				common.WriteError(w, r, http.StatusForbidden, "invalid_agent_task", "Agent 任务无效或不属于代理用户", nil)
				return
			}

			ctx := context.WithValue(r.Context(), actingUserKey, actingUserID)
			ctx = context.WithValue(ctx, agentTaskKey, taskID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// QueueTaskContext adds task attribution for complete/fail audit events. The
// service remains responsible for the task status and lease checks.
func QueueTaskContext(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			taskID := chi.URLParam(r, "id")
			var actingUserID sql.NullString
			if err := db.QueryRowContext(r.Context(),
				`SELECT acting_user_id FROM pending_agent_tasks WHERE id = $1`, taskID,
			).Scan(&actingUserID); err == nil {
				ctx := context.WithValue(r.Context(), agentTaskKey, taskID)
				if actingUserID.Valid {
					ctx = context.WithValue(ctx, actingUserKey, actingUserID.String)
				}
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func agentBusinessPathAllowed(method, path string) bool {
	if method == http.MethodGet {
		for _, prefix := range []string{
			"/api/v1/daily-reports", "/api/v1/projects", "/api/v1/logs",
			"/api/v1/issues", "/api/v1/experiences", "/api/v1/attachments",
			"/api/v1/experiment-runs", "/api/v1/test-data",
		} {
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
		return false
	}
	if method != http.MethodPost {
		return false
	}
	return path == "/api/v1/experiences/candidates" ||
		path == "/api/v1/attachments" || path == "/api/v1/attachments/" ||
		(strings.HasPrefix(path, "/api/v1/attachments/") && strings.HasSuffix(path, "/links")) ||
		(strings.HasPrefix(path, "/api/v1/projects/") && strings.HasSuffix(path, "/test-data")) ||
		(strings.HasPrefix(path, "/api/v1/projects/") && (strings.HasSuffix(path, "/issues") || strings.HasSuffix(path, "/logs"))) ||
		(strings.HasPrefix(path, "/api/v1/issues/") && strings.HasSuffix(path, "/comments"))
}

func EffectiveUserID(ctx context.Context) string {
	if id, _ := ctx.Value(actingUserKey).(string); id != "" {
		return id
	}
	if claims := GetUserClaims(ctx); claims != nil {
		return claims.UserID
	}
	return ""
}

func ActingUserID(ctx context.Context) string {
	id, _ := ctx.Value(actingUserKey).(string)
	return id
}

func AgentTaskID(ctx context.Context) string {
	id, _ := ctx.Value(agentTaskKey).(string)
	return id
}
