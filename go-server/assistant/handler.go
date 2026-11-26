package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// serviceAPI 是 Handler 依赖的窄接口，便于 HTTP 层注入 fake。
type serviceAPI interface {
	Create(ctx context.Context, userID string, req CreateSessionRequest) (*Session, error)
	List(ctx context.Context, userID, status, query string, limit int) ([]Session, error)
	Get(ctx context.Context, userID, id string) (*Session, error)
	Update(ctx context.Context, userID, id string, req UpdateSessionRequest) (*Session, error)
	Archive(ctx context.Context, userID, id string) error
	Messages(ctx context.Context, userID, id string) ([]Message, error)
	AddMessage(ctx context.Context, userID, id, content string) (*Message, error)
	StartRun(ctx context.Context, userID, sessionID, content string) (*RunResult, error)
	Usage(ctx context.Context, userID string) (Usage, error)
	Export(ctx context.Context, userID, id string) (string, error)
}

type Handler struct {
	svc serviceAPI
}

func NewHandler(svc serviceAPI) *Handler { return &Handler{svc: svc} }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.List(r.Context(), userID, r.URL.Query().Get("status"), r.URL.Query().Get("q"), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]any{"items": items})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "请求体格式错误", nil)
		return
	}
	sess, err := h.svc.Create(r.Context(), userID, req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, sess)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	sess, err := h.svc.Get(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, sess)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "请求体格式错误", nil)
		return
	}
	sess, err := h.svc.Update(r.Context(), userID, chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, sess)
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	if err := h.svc.Archive(r.Context(), userID, chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]bool{"archived": true})
}

func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	items, err := h.svc.Messages(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, map[string]any{"items": items})
}

func (h *Handler) AddMessage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	var req CreateMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "请求体格式错误", nil)
		return
	}
	m, err := h.svc.AddMessage(r.Context(), userID, chi.URLParam(r, "id"), req.Content)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, m)
}

func (h *Handler) StartRun(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	var req CreateRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "请求体格式错误", nil)
		return
	}
	result, err := h.svc.StartRun(r.Context(), userID, chi.URLParam(r, "id"), req.Content)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, result)
}

func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	usage, err := h.svc.Usage(r.Context(), userID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	common.WriteSuccess(w, r, usage)
}

func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserClaims(r.Context()).UserID
	id := chi.URLParam(r, "id")
	md, err := h.svc.Export(r.Context(), userID, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "assistant-"+id+".md"))
	_, _ = w.Write([]byte(md))
}

func (h *Handler) StartRunStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		common.WriteError(w, r, http.StatusInternalServerError, "stream_unsupported", "SSE 不可用", nil)
		return
	}
	userID := middleware.GetUserClaims(r.Context()).UserID
	var req CreateRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "请求体格式错误", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, "event: start\ndata: {\"status\":\"running\"}\n\n")
	flusher.Flush()

	type outcome struct {
		result *RunResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		// 客户端断开不应取消已开始的 run；LLM 客户端自带 120s 超时。
		ctx := context.WithoutCancel(r.Context())
		result, err := h.svc.StartRun(ctx, userID, chi.URLParam(r, "id"), req.Content)
		done <- outcome{result: result, err: err}
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	enc := json.NewEncoder(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case out := <-done:
			if out.err != nil {
				code, message := streamError(out.err)
				fmt.Fprint(w, "event: error\ndata: ")
				_ = enc.Encode(map[string]string{"code": code, "message": message})
				fmt.Fprint(w, "\n")
				flusher.Flush()
				return
			}
			fmt.Fprint(w, "event: result\ndata: ")
			_ = enc.Encode(out.result)
			fmt.Fprint(w, "\n")
			fmt.Fprint(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			return
		}
	}
}

func streamError(err error) (string, string) {
	switch {
	case errors.Is(err, ErrQuota):
		return "quota_exceeded", "今日 AI 助手次数已用完"
	case errors.Is(err, ErrUpstream):
		return "upstream_error", "AI 服务暂时不可用"
	case errors.Is(err, ErrNotFound):
		return "not_found", "会话不存在"
	case errors.Is(err, ErrForbidden):
		return "permission_denied", "无权操作该会话"
	case errors.Is(err, ErrInvalid):
		return "invalid_request", "参数不合法"
	default:
		return "internal_error", "服务器内部错误"
	}
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		common.WriteError(w, r, http.StatusNotFound, "not_found", "会话不存在", nil)
	case errors.Is(err, ErrForbidden):
		common.WriteError(w, r, http.StatusForbidden, "permission_denied", "无权操作该会话", nil)
	case errors.Is(err, ErrProjectUnsupported):
		common.WriteError(w, r, http.StatusBadRequest, "not_supported", "项目共享会话暂未开放", nil)
	case errors.Is(err, ErrInvalid):
		common.WriteError(w, r, http.StatusBadRequest, "invalid_request", "参数不合法", nil)
	case errors.Is(err, ErrUpstream):
		common.WriteError(w, r, http.StatusBadGateway, "upstream_error", "AI 服务暂时不可用", nil)
	case errors.Is(err, ErrQuota):
		common.WriteError(w, r, http.StatusTooManyRequests, "quota_exceeded", "今日 AI 助手次数已用完", nil)
	default:
		common.WriteError(w, r, http.StatusInternalServerError, "internal_error", "服务器内部错误", nil)
	}
}
