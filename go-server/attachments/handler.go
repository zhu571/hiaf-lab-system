package attachments

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

const maxUploadSize = 100 << 20

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			common.WriteError(w, r, http.StatusRequestEntityTooLarge, "attachment_too_large", "附件不能超过 100 MiB", nil)
			return
		}
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "multipart 请求解析失败", nil)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "缺少附件文件", nil)
		return
	}
	defer file.Close()
	result, err := h.svc.Upload(file, header, middleware.EffectiveUserID(r.Context()), claims.Role,
		r.FormValue("entity_type"), r.FormValue("entity_id"), r.FormValue("description"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteCreated(w, r, result)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	result, err := h.svc.List(middleware.EffectiveUserID(r.Context()), claims.Role, ListParams{
		EntityType: r.URL.Query().Get("entity_type"), EntityID: r.URL.Query().Get("entity_id"),
		Page: queryInt(r, "page", 1), PerPage: queryInt(r, "per_page", 20),
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, result)
}

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	attachment, err := h.svc.GetByID(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, attachment)
}

func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "attachments.download")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	attachment, file, err := h.svc.Download(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	defer file.Close()
	name := attachment.OriginalName
	if name == "" {
		name = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	if attachment.MimeType != "" {
		w.Header().Set("Content-Type", attachment.MimeType)
	}
	http.ServeContent(w, r, name, attachment.UpdatedAt, file)
}

func (h *Handler) AddLink(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req CreateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	link, err := h.svc.AddLink(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteCreated(w, r, link)
}

func (h *Handler) RemoveLink(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	attachmentID, linkID := chi.URLParam(r, "id"), chi.URLParam(r, "link_id")
	if err := h.svc.RemoveLink(attachmentID, linkID, middleware.EffectiveUserID(r.Context()), claims.Role); err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]string{"attachment_id": attachmentID, "link_id": linkID})
}

func (h *Handler) SoftDelete(w http.ResponseWriter, r *http.Request) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.svc.SoftDelete(id, middleware.EffectiveUserID(r.Context()), claims.Role); err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]string{"id": id})
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrAttachmentNotFound):
		common.WriteError(w, r, http.StatusNotFound, "attachment_not_found", err.Error(), nil)
	case errors.Is(err, ErrFileNotFound):
		common.WriteError(w, r, http.StatusNotFound, "attachment_file_not_found", err.Error(), nil)
	case errors.Is(err, ErrLinkNotFound):
		common.WriteError(w, r, http.StatusNotFound, "attachment_link_not_found", err.Error(), nil)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
	case errors.Is(err, ErrForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), nil)
	case errors.Is(err, ErrLinkExists):
		common.WriteError(w, r, http.StatusConflict, "attachment_link_exists", err.Error(), nil)
	default:
		slog.Error("attachments request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}

func queryInt(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return value
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") != "" {
		return true
	}
	common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
	return false
}
