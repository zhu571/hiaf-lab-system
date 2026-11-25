package scans

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Upload POST /api/v1/projects/{id}/scans（multipart）
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "scan.create")
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		common.WriteError(w, r, http.StatusRequestEntityTooLarge, "scan_too_large", "上传包超过 50 MiB", nil)
		return
	}
	txtFile, txtHeader, err := r.FormFile("txt")
	if err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "缺少 txt 文件", nil)
		return
	}
	defer txtFile.Close()
	txt, err := io.ReadAll(io.LimitReader(txtFile, maxScanTXT+1))
	if err != nil || len(txt) > maxScanTXT {
		common.WriteError(w, r, http.StatusRequestEntityTooLarge, "scan_too_large", "txt 超过 5 MiB", nil)
		return
	}
	var pngData []byte
	if pngFile, _, err := r.FormFile("png"); err == nil {
		defer pngFile.Close()
		pngData, err = io.ReadAll(io.LimitReader(pngFile, maxScanPNG+1))
		if err != nil || len(pngData) > maxScanPNG {
			common.WriteError(w, r, http.StatusRequestEntityTooLarge, "scan_too_large", "png 超过 20 MiB", nil)
			return
		}
	}

	req := UploadRequest{
		Filename:     txtHeader.Filename,
		TXT:          txt,
		PNG:          pngData,
		Machine:      r.FormValue("machine"),
		Tags:         splitCSV(r.FormValue("tags")),
		SourceTag:    r.FormValue("source_tag"),
		Title:        r.FormValue("title"),
		Description:  r.FormValue("description"),
		SourcePath:   r.FormValue("source_path"),
		IngestSource: "manual",
		ScanAtSource: "upload",
	}
	if raw := strings.TrimSpace(r.FormValue("scan_at")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "scan_at 必须是 RFC3339", nil)
			return
		}
		req.ScanAt = &parsed
	}
	result, err := h.svc.Upload(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	status := http.StatusCreated
	if result.Duplicate {
		status = http.StatusOK
	}
	middleware.SetAuditDetail(r.Context(), map[string]any{
		"scan_id": result.ID, "project_id": result.ProjectID, "filename": result.Filename,
		"format": result.Format, "duplicate": result.Duplicate,
	})
	if status == http.StatusOK {
		common.WriteSuccess(w, r, result)
	} else {
		common.WriteCreated(w, r, result)
	}
}

// Ingest POST /api/v1/scans/ingest（仅 SERVICE_TOKEN；watcher/backfill）
func (h *Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "scan.create")
	if !middleware.IsServiceCall(r.Context()) {
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", "仅内部服务可调用该端点", nil)
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		common.WriteError(w, r, http.StatusRequestEntityTooLarge, "scan_too_large", "上传包超过 50 MiB", nil)
		return
	}
	txtFile, txtHeader, err := r.FormFile("txt")
	if err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "缺少 txt 文件", nil)
		return
	}
	defer txtFile.Close()
	txt, err := io.ReadAll(io.LimitReader(txtFile, maxScanTXT+1))
	if err != nil || len(txt) > maxScanTXT {
		common.WriteError(w, r, http.StatusRequestEntityTooLarge, "scan_too_large", "txt 超过 5 MiB", nil)
		return
	}
	var pngData []byte
	if pngFile, _, err := r.FormFile("png"); err == nil {
		defer pngFile.Close()
		pngData, err = io.ReadAll(io.LimitReader(pngFile, maxScanPNG+1))
		if err != nil || len(pngData) > maxScanPNG {
			common.WriteError(w, r, http.StatusRequestEntityTooLarge, "scan_too_large", "png 超过 20 MiB", nil)
			return
		}
	}
	ingestSource := strings.TrimSpace(r.FormValue("ingest_source"))
	if ingestSource == "" {
		ingestSource = "watcher"
	}
	if ingestSource != "watcher" && ingestSource != "backfill" {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "ingest_source 必须为 watcher/backfill", nil)
		return
	}
	req := UploadRequest{
		Filename:     txtHeader.Filename,
		TXT:          txt,
		PNG:          pngData,
		Machine:      r.FormValue("machine"),
		Tags:         splitCSV(r.FormValue("tags")),
		SourceTag:    r.FormValue("source_tag"),
		Title:        r.FormValue("title"),
		Description:  r.FormValue("description"),
		SourcePath:   r.FormValue("source_path"),
		IngestSource: ingestSource,
		ScanAtSource: r.FormValue("scan_at_source"),
	}
	if req.ScanAtSource == "" {
		req.ScanAtSource = "filename"
	}
	if raw := strings.TrimSpace(r.FormValue("scan_at")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			common.WriteError(w, r, http.StatusBadRequest, "bad_request", "scan_at 必须是 RFC3339", nil)
			return
		}
		req.ScanAt = &parsed
	}
	result, err := h.svc.Ingest(r.FormValue("project_id"), req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	middleware.SetAuditDetail(r.Context(), map[string]any{
		"scan_id": result.ID, "project_id": result.ProjectID, "filename": result.Filename,
		"format": result.Format, "duplicate": result.Duplicate, "ingest_source": ingestSource,
	})
	if result.Duplicate {
		common.WriteSuccess(w, r, result)
	} else {
		common.WriteCreated(w, r, result)
	}
}

// File GET /api/v1/scans/{id}/file/{kind}
func (h *Handler) File(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	download := r.URL.Query().Get("download") == "1"
	if download {
		middleware.SetAuditAction(r.Context(), "scan.download")
		if !requireIdempotencyKey(w, r) {
			return
		}
	}
	file, item, contentType, err := h.svc.OpenFile(chi.URLParam(r, "id"), chi.URLParam(r, "kind"),
		middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	defer file.Close()

	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	headers := w.Header()
	headers.Set("Content-Type", contentType)
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("Cache-Control", "private, max-age=3600")
	headers.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": item.Filename}))
	etag := item.TXTSHA256
	if chi.URLParam(r, "kind") == "png" && item.PNGSHA256 != nil {
		etag = *item.PNGSHA256
	}
	if etag != "" {
		headers.Set("ETag", `"`+etag+`"`)
	}
	http.ServeContent(w, r, item.Filename, item.UpdatedAt, file)
}

// Thumb GET /api/v1/scans/{id}/thumb
func (h *Handler) Thumb(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	file, item, contentType, err := h.svc.OpenFile(chi.URLParam(r, "id"), "thumb",
		middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if item.ThumbPath == nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "thumb.png", item.UpdatedAt, file)
}

// List GET /api/v1/projects/{id}/scans
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	q := r.URL.Query()
	params := ListParams{
		Tags:         splitCSV(q.Get("tags")),
		TagMode:      q.Get("tag_mode"),
		From:         q.Get("from"),
		To:           q.Get("to"),
		Q:            q.Get("q"),
		Annotated:    queryBoolPtr(q.Get("annotated")),
		Format:       q.Get("format"),
		HasPNG:       queryBoolPtr(q.Get("has_png")),
		IngestSource: q.Get("ingest_source"),
		IncludeVoid:  q.Get("include_void") == "true",
		Page:         queryInt(q.Get("page"), 1),
		PerPage:      queryInt(q.Get("per_page"), 20),
	}
	result, err := h.svc.List(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, params)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, result)
}

// Tags GET /api/v1/projects/{id}/scans/tags
func (h *Handler) Tags(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	items, err := h.svc.Tags(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]any{"items": items})
}

// GetByID GET /api/v1/scans/{id}
func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	item, err := h.svc.Get(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, item)
}

// Update PATCH /api/v1/scans/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "scan.update")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req UpdateScanRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	item, err := h.svc.Update(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	middleware.SetAuditDetail(r.Context(), map[string]any{
		"scan_id": item.ID, "project_id": item.ProjectID, "title": item.Title,
		"tags": item.Tags, "annotated": item.AnnotatedAt != nil,
	})
	common.WriteSuccess(w, r, item)
}

// Void POST /api/v1/scans/{id}/void
func (h *Handler) Void(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "scan.void")
	if !requireIdempotencyKey(w, r) {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
		return
	}
	var req VoidRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", "请求体解析失败", nil)
		return
	}
	item, err := h.svc.Void(chi.URLParam(r, "id"), middleware.EffectiveUserID(r.Context()), claims.Role, req.Reason)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	middleware.SetAuditDetail(r.Context(), map[string]any{
		"scan_id": item.ID, "project_id": item.ProjectID, "reason": req.Reason,
	})
	common.WriteSuccess(w, r, item)
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") != "" {
		return true
	}
	common.WriteError(w, r, http.StatusBadRequest, "missing_idempotency_key", "缺少 Idempotency-Key header", nil)
	return false
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

func queryBoolPtr(raw string) *bool {
	switch raw {
	case "true":
		value := true
		return &value
	case "false":
		value := false
		return &value
	default:
		return nil
	}
}

func queryInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrScanNotFound):
		common.WriteError(w, r, http.StatusNotFound, "scan_not_found", err.Error(), nil)
	case errors.Is(err, ErrProjectNotFound):
		common.WriteError(w, r, http.StatusNotFound, "project_not_found", err.Error(), nil)
	case errors.Is(err, ErrForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", err.Error(), nil)
	case errors.Is(err, ErrInvalidInput):
		common.WriteError(w, r, http.StatusBadRequest, "bad_request", err.Error(), nil)
	default:
		slog.Error("scan request failed", "error", err, "request_id", common.GetRequestID(r.Context()))
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
