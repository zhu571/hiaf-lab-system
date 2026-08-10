package alert

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// Handler 暴露告警中心 HTTP 端点（鉴权由 main.go 路由装配，见方案 §4 矩阵）。
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// Report POST /api/v1/alerts/report —— 仅内部服务可调用（SERVICE_TOKEN 白名单 +
// handler 级 IsServiceCall 强制，杜绝用户/agent 冒充）。审计由 Audit 中间件落
// actor_type='system'（先例：ask/execute）。不挂 Idempotency-Key：聚合窗口 +
// 部分唯一索引天然幂等。
func (h *Handler) Report(w http.ResponseWriter, r *http.Request) {
	if !middleware.IsServiceCall(r.Context()) {
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", "仅内部服务可调用该端点", nil)
		return
	}
	var req ReportRequest
	if err := decode(r, &req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	res, err := h.svc.Report(r.Context(), req.Level, req.Source, req.Title, req.Detail)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, res)
}

// Resolve POST /api/v1/alerts/resolve —— 双通道：
//   - 用户通道：JWT + RequireRoleOrService(admin, maintainer) + CSRF + Idempotency-Key，
//     按 {id} 解除（resolved_by=username）；
//   - 内部通道：SERVICE_TOKEN（CSRF/幂等豁免），按 {source,title} 解除（resolved_by=system）。
//
// 匹配不到 active 行 → 幂等 success（detail 由审计记录）。
func (h *Handler) Resolve(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "alerts.resolve")
	var req ResolveRequest
	if err := decode(r, &req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	resolvedBy := ResolvedBySystem
	matched := false
	if claims := middleware.GetUserClaims(r.Context()); claims != nil {
		resolvedBy = claims.Username
	}

	if req.ID != "" {
		if !uuidRe.MatchString(req.ID) {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "id 非法", nil)
			return
		}
		m, err := h.svc.ResolveByID(r.Context(), req.ID, resolvedBy)
		if err != nil {
			h.writeError(w, r, err)
			return
		}
		matched = m
	} else {
		var err error
		matched, err = h.svc.ResolveBySourceMatched(r.Context(), req.Source, req.Title, resolvedBy)
		if err != nil {
			h.writeError(w, r, err)
			return
		}
	}
	middleware.SetAuditDetail(r.Context(), map[string]any{
		"id": req.ID, "source": req.Source, "title": req.Title,
		"resolved_by": resolvedBy, "matched": matched,
	})
	common.WriteSuccess(w, r, map[string]bool{"resolved": true})
}

// List GET /api/v1/alerts —— JWT 全员可读。?status=active|resolved&limit=50&offset=0。
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)
	if limit < 1 {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "limit 非法", nil)
		return
	}
	if offset < 0 {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "offset 非法", nil)
		return
	}
	items, total, err := h.svc.List(r.Context(), status, limit, offset)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]any{
		"items": items, "total": total, "limit": limit, "offset": offset,
	})
}

// Get GET /api/v1/alerts/{id} —— 单条详情。
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !uuidRe.MatchString(id) {
		h.writeError(w, r, ErrNotFound) // 非法 UUID 直接 404，不送 PG（防 500）
		return
	}
	a, err := h.svc.Get(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, a)
}

// writeError 统一错误映射。
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		common.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
	default:
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}

func decode(r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}
