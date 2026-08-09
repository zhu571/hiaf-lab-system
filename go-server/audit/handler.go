package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type Handler struct {
	svc *Service
}

// Record 是 audit_log 一行记录的 API 输出（含 014 代理四列与 029 hash 链两列）。
type Record struct {
	ID             int64          `json:"id"`
	RequestID      string         `json:"request_id"`
	UserID         *string        `json:"user_id,omitempty"`
	Username       string         `json:"username"`
	Method         string         `json:"method"`
	Path           string         `json:"path"`
	Action         string         `json:"action"`
	Status         int            `json:"status_code"`
	ClientIP       string         `json:"client_ip"`
	Detail         map[string]any `json:"detail,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	ActorType      string         `json:"actor_type,omitempty"`
	ActingUserID   *string        `json:"acting_user_id,omitempty"`
	AgentTaskID    *string        `json:"agent_task_id,omitempty"`
	IdempotencyKey *string        `json:"idempotency_key,omitempty"`
	PrevHash       *string        `json:"prev_hash,omitempty"`
	Hash           *string        `json:"hash,omitempty"`
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// requireAuditor 仅 admin/maintainer 可查审计记录。
func requireAuditor(w http.ResponseWriter, r *http.Request) bool {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || (claims.Role != auth.RoleAdmin && claims.Role != "maintainer") {
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", "无权查询审计记录", nil)
		return false
	}
	return true
}

// GetByRequestID GET /api/v1/audit/{request_id}
func (h *Handler) GetByRequestID(w http.ResponseWriter, r *http.Request) {
	if !requireAuditor(w, r) {
		return
	}
	records, err := h.svc.ListByRequestID(r.Context(), chi.URLParam(r, "request_id"))
	if err != nil {
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "查询审计记录失败", nil)
		return
	}
	common.WriteSuccess(w, r, map[string]any{"items": records, "total": len(records)})
}

// VerifyChain GET /api/v1/audit/verify?from_id=&to_id=
// from_id/to_id 缺省为 0（不设界）即全链重算；增量区间用于定期抽查。
func (h *Handler) VerifyChain(w http.ResponseWriter, r *http.Request) {
	if !requireAuditor(w, r) {
		return
	}
	fromID, err := queryInt64(r, "from_id")
	if err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "from_id 必须是整数", nil)
		return
	}
	toID, err := queryInt64(r, "to_id")
	if err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "to_id 必须是整数", nil)
		return
	}
	if fromID < 0 || toID < 0 || (fromID > 0 && toID > 0 && fromID > toID) {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "from_id/to_id 区间无效", nil)
		return
	}
	result, err := h.svc.VerifyChain(r.Context(), fromID, toID)
	if err != nil {
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "校验审计链失败", nil)
		return
	}
	common.WriteSuccess(w, r, result)
}

// ListEvents GET /api/v1/audit/events?action=&user_id=&actor_type=&from=&to=&page=&per_page=
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	if !requireAuditor(w, r) {
		return
	}
	q := r.URL.Query()
	if uid := q.Get("user_id"); uid != "" && len(uid) != 36 {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "user_id 必须是 UUID", nil)
		return
	}
	filter := EventFilter{
		Action:    q.Get("action"),
		UserID:    q.Get("user_id"),
		ActorType: q.Get("actor_type"),
		Page:      queryPositiveInt(q.Get("page"), 1),
		PerPage:   queryPositiveInt(q.Get("per_page"), 20),
	}
	if filter.PerPage > 100 {
		filter.PerPage = 100
	}
	var err error
	if raw := q.Get("from"); raw != "" {
		if filter.From, err = time.Parse(time.RFC3339, raw); err != nil {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "from 必须是 RFC3339 时间", nil)
			return
		}
	}
	if raw := q.Get("to"); raw != "" {
		if filter.To, err = time.Parse(time.RFC3339, raw); err != nil {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "to 必须是 RFC3339 时间", nil)
			return
		}
	}
	result, err := h.svc.ListEvents(r.Context(), filter)
	if err != nil {
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "查询审计事件失败", nil)
		return
	}
	common.WriteSuccess(w, r, result)
}

// queryInt64 解析可选整型查询参数，缺省为 0。
func queryInt64(r *http.Request, key string) (int64, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func queryPositiveInt(raw string, fallback int) int {
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return fallback
	}
	return v
}

type scanner interface {
	Scan(dest ...any) error
}

// scanRecord 扫描 recordColumns 全列（含 014 四列与 029 hash 链两列）。
func scanRecord(row scanner) (Record, error) {
	var rec Record
	var userID, actingUserID, agentTaskID, idempotencyKey, actorType, prevHash, hash sql.NullString
	var detail []byte
	if err := row.Scan(
		&rec.ID, &rec.RequestID, &userID, &rec.Username, &rec.Method, &rec.Path,
		&rec.Action, &rec.Status, &rec.ClientIP, &detail, &rec.CreatedAt,
		&actorType, &actingUserID, &agentTaskID, &idempotencyKey, &prevHash, &hash,
	); err != nil {
		return rec, fmt.Errorf("scan audit record: %w", err)
	}
	if userID.Valid {
		rec.UserID = &userID.String
	}
	if len(detail) > 0 {
		_ = json.Unmarshal(detail, &rec.Detail)
	}
	rec.ActorType = actorType.String
	if actingUserID.Valid {
		rec.ActingUserID = &actingUserID.String
	}
	if agentTaskID.Valid {
		rec.AgentTaskID = &agentTaskID.String
	}
	if idempotencyKey.Valid {
		rec.IdempotencyKey = &idempotencyKey.String
	}
	if prevHash.Valid {
		rec.PrevHash = &prevHash.String
	}
	if hash.Valid {
		rec.Hash = &hash.String
	}
	return rec, nil
}
