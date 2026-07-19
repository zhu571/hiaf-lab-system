package audit

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type Handler struct {
	db *sql.DB
}

type Record struct {
	ID        int64          `json:"id"`
	RequestID string         `json:"request_id"`
	UserID    *string        `json:"user_id,omitempty"`
	Username  string         `json:"username"`
	Method    string         `json:"method"`
	Path      string         `json:"path"`
	Action    string         `json:"action"`
	Status    int            `json:"status_code"`
	ClientIP  string         `json:"client_ip"`
	Detail    map[string]any `json:"detail,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

func (h *Handler) GetByRequestID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || (claims.Role != auth.RoleAdmin && claims.Role != "maintainer") {
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", "无权查询审计记录", nil)
		return
	}

	rows, err := h.db.Query(
		`SELECT id, request_id, user_id, username, method, path, action, status_code, client_ip, detail, created_at
		 FROM audit_log
		 WHERE request_id = $1
		 ORDER BY created_at ASC`,
		chi.URLParam(r, "request_id"),
	)
	if err != nil {
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "查询审计记录失败", nil)
		return
	}
	defer rows.Close()

	records := []Record{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "解析审计记录失败", nil)
			return
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "读取审计记录失败", nil)
		return
	}
	common.WriteSuccess(w, r, map[string]any{"items": records, "total": len(records)})
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRecord(row scanner) (Record, error) {
	var rec Record
	var userID sql.NullString
	var detail []byte
	if err := row.Scan(
		&rec.ID, &rec.RequestID, &userID, &rec.Username, &rec.Method, &rec.Path,
		&rec.Action, &rec.Status, &rec.ClientIP, &detail, &rec.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rec, nil
		}
		return rec, fmt.Errorf("scan audit record: %w", err)
	}
	if userID.Valid {
		rec.UserID = &userID.String
	}
	if len(detail) > 0 {
		_ = json.Unmarshal(detail, &rec.Detail)
	}
	return rec, nil
}
