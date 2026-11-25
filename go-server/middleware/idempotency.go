package middleware

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

// 幂等中间件（AR-03）。
//
// 旧语义（IDEMPOTENCY_V2=false）：同 key 第二次请求直接 409，且不保存响应。
// 新语义（默认）：按 (user_id, method, path, key) 作用域；
//   - 首次请求执行并保存响应；
//   - 同 key 重试原样重放（X-Idempotent-Replay: true）；
//   - 5xx/panic 释放 key，允许客户端重试；
//   - 7 天 TTL，定时清理。
//
// 流式（text/event-stream）与大响应（>256KB）不保存 body：按“不可重放”处理，
// 重复请求返回 409；这是明确的能力边界，文档与 api-contract 需同步。
var idempotencyV2 = false

// SetIdempotencyV2 由 main 按 config.Features.IdempotencyV2 注入。
func SetIdempotencyV2(on bool) { idempotencyV2 = on }

const (
	idempotencyMaxBody   = 256 << 10
	idempotencyLockTTL   = 10 * time.Minute
	idempotencyCleanTick = time.Hour
)

// RequireIdempotencyKey 强制写接口携带 Idempotency-Key。service token 调用跳过
// （业务幂等由服务端保证，与既有行为一致）。
func RequireIdempotencyKey(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			if IsServiceCall(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				writeMissingKey(w, r)
				return
			}
			if !idempotencyV2 {
				legacyRequire(db, next, w, r, key)
				return
			}
			requireV2(db, next, w, r, key)
		})
	}
}

func writeMissingKey(w http.ResponseWriter, r *http.Request) {
	common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
}

func legacyRequire(db *sql.DB, next http.Handler, w http.ResponseWriter, r *http.Request, key string) {
	// 新表结构下 scope_key 为唯一约束；legacy 作用域 = "|||key"（user/method/path 均为空）。
	scope := "|||" + key
	result, err := db.Exec(
		`INSERT INTO idempotency_keys (idempotency_key, request_id, created_at, state, expires_at)
		 VALUES ($1, $2, now(), 'in_progress', now() + interval '7 days')
		 ON CONFLICT (scope_key) DO NOTHING`,
		key, common.GetRequestID(r.Context()),
	)
	if err != nil {
		slog.Error("store idempotency key failed", "error", err, "key", key)
		next.ServeHTTP(w, r)
		return
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 0 {
		next.ServeHTTP(w, r)
		return
	}
	var existing string
	if err := db.QueryRow(`SELECT request_id FROM idempotency_keys WHERE scope_key = $1`, scope).Scan(&existing); err != nil {
		slog.Error("idempotency key lookup failed", "error", err)
		next.ServeHTTP(w, r)
		return
	}
	common.WriteError(w, r, http.StatusConflict, "duplicate_idempotency_key",
		"该 Idempotency-Key 已被使用，请勿重复提交", map[string]any{"existing_request_id": existing})
}

func requireV2(db *sql.DB, next http.Handler, w http.ResponseWriter, r *http.Request, key string) {
	userID := strings.ToLower(idempotencyUserID(r))
	scope := userID + "|" + r.Method + "|" + r.URL.Path + "|" + key

	inserted, err := db.Exec(
		`INSERT INTO idempotency_keys
		     (idempotency_key, request_id, created_at, user_id, method, path, state, locked_at, expires_at)
		 VALUES ($1, $2, now(), NULLIF($3, '')::uuid, $4, $5, 'in_progress', now(), now() + interval '7 days')
		 ON CONFLICT (scope_key) DO NOTHING`,
		key, common.GetRequestID(r.Context()), userID, r.Method, r.URL.Path,
	)
	if err != nil {
		slog.Error("store idempotency key failed", "error", err, "key", key)
		next.ServeHTTP(w, r)
		return
	}
	rows, _ := inserted.RowsAffected()
	if rows == 0 {
		replayOrConflict(db, next, w, r, scope)
		return
	}

	if err := runAndPersist(db, next, w, r, scope); err != nil {
		slog.Error("persist idempotency response failed", "error", err, "scope", scope)
	}
}

// runAndPersist 执行 handler 并落响应；panic/5xx/流式/超大响应会释放 key。
func runAndPersist(db *sql.DB, next http.Handler, w http.ResponseWriter, r *http.Request, scope string) (err error) {
	iw := &idempotencyWriter{ResponseWriter: w}
	completed := false
	defer func() {
		if completed {
			return
		}
		// panic 或 handler 提前退出：释放 key，允许客户端重试。
		if _, delErr := db.Exec(`DELETE FROM idempotency_keys WHERE scope_key = $1 AND state = 'in_progress'`, scope); delErr != nil {
			slog.Error("release idempotency key failed", "error", delErr, "scope", scope)
		}
	}()

	next.ServeHTTP(iw, r)
	completed = true

	status := iw.status
	if status == 0 {
		status = http.StatusOK
	}
	contentType := iw.Header().Get("Content-Type")
	replayable := !iw.overflow && !isStreaming(contentType)
	if status >= 500 || !replayable {
		_, err = db.Exec(`DELETE FROM idempotency_keys WHERE scope_key = $1`, scope)
		return err
	}

	_, err = db.Exec(
		`UPDATE idempotency_keys
		    SET state = 'done', response_status = $2, response_content_type = $3,
		        response_body = $4, locked_at = NULL
		  WHERE scope_key = $1`,
		scope, status, contentType, iw.body.String(),
	)
	return err
}

func replayOrConflict(db *sql.DB, next http.Handler, w http.ResponseWriter, r *http.Request, scope string) {
	var (
		state       string
		status      sql.NullInt64
		contentType sql.NullString
		body        sql.NullString
		lockedAt    sql.NullTime
		existingReq string
	)
	err := db.QueryRow(
		`SELECT state, response_status, response_content_type, response_body, locked_at, request_id
		   FROM idempotency_keys WHERE scope_key = $1`, scope,
	).Scan(&state, &status, &contentType, &body, &lockedAt, &existingReq)
	if err != nil {
		slog.Error("idempotency key lookup failed", "error", err, "scope", scope)
		next.ServeHTTP(w, r)
		return
	}

	if state == "done" {
		if !body.Valid {
			// 迁移前遗留行（只有 key，没有响应快照）：按旧语义返回 409。
			common.WriteError(w, r, http.StatusConflict, "duplicate_idempotency_key",
				"该 Idempotency-Key 已被使用，请勿重复提交", map[string]any{"existing_request_id": existingReq})
			return
		}
		if contentType.Valid && contentType.String != "" {
			w.Header().Set("Content-Type", contentType.String)
		}
		w.Header().Set("X-Idempotent-Replay", "true")
		if status.Valid && status.Int64 != 0 {
			w.WriteHeader(int(status.Int64))
		} else {
			w.WriteHeader(http.StatusOK)
		}
		if body.String != "" {
			_, _ = w.Write([]byte(body.String))
		}
		return
	}

	// in_progress：超过锁租约允许接管；否则 409 + Retry-After。
	if state == "in_progress" && lockedAt.Valid && time.Since(lockedAt.Time) > idempotencyLockTTL {
		res, err := db.Exec(
			`UPDATE idempotency_keys SET locked_at = now()
			  WHERE scope_key = $1 AND state = 'in_progress' AND locked_at < now() - interval '10 minutes'`,
			scope,
		)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 1 {
				_ = runAndPersist(db, next, w, r, scope)
				return
			}
		}
	}
	w.Header().Set("Retry-After", "2")
	common.WriteError(w, r, http.StatusConflict, "idempotency_in_progress",
		"相同 Idempotency-Key 的请求正在处理中，请稍后重试", map[string]any{"existing_request_id": existingReq})
}

func isStreaming(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(ct, "text/event-stream") ||
		strings.HasPrefix(ct, "application/octet-stream")
}

func idempotencyUserID(r *http.Request) string {
	if claims := GetUserClaims(r.Context()); claims != nil {
		return claims.UserID
	}
	return ""
}

// idempotencyWriter 捕获状态/响应体，同时透传底层 writer。
type idempotencyWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
	overflow    bool
}

func (w *idempotencyWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *idempotencyWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if !w.overflow {
		if w.body.Len()+len(b) > idempotencyMaxBody {
			w.overflow = true
			w.body.Reset()
		} else {
			w.body.Write(b)
		}
	}
	return w.ResponseWriter.Write(b)
}

func (w *idempotencyWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// StartIdempotencyCleanup 清理过期 key（main 起 goroutine；ctx 取消即退出）。
func StartIdempotencyCleanup(ctx context.Context, db *sql.DB) {
	clean := func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at < now()`); err != nil && ctx.Err() == nil {
			slog.Warn("idempotency cleanup failed", "error", err)
		}
	}
	clean()
	ticker := time.NewTicker(idempotencyCleanTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			clean()
		}
	}
}
