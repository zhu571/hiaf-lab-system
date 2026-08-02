package system

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// GetVersion 获取当前与远程版本信息（只读，无审计/幂等）。
func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.GetVersion()
	if err != nil {
		common.WriteError(w, r, http.StatusInternalServerError, "version_failed", err.Error(), nil)
		return
	}
	common.WriteSuccess(w, r, info)
}

// TriggerUpdate 触发一次更新（写操作，需审计 + Idempotency-Key，由路由中间件保证）。
func (h *Handler) TriggerUpdate(w http.ResponseWriter, r *http.Request) {
	middleware.SetAuditAction(r.Context(), "system.update.trigger")
	sessionID, err := h.svc.Trigger()
	if err != nil {
		switch {
		case errors.Is(err, ErrUpdateInProgress):
			common.WriteError(w, r, http.StatusConflict, "update_in_progress", err.Error(), nil)
		case errors.Is(err, ErrScriptMissing):
			common.WriteError(w, r, http.StatusInternalServerError, "script_missing", err.Error(), nil)
		default:
			common.WriteError(w, r, http.StatusInternalServerError, "update_trigger_failed", err.Error(), nil)
		}
		return
	}
	current := h.svc.gitRevParse("HEAD")
	common.WriteSuccess(w, r, TriggerResponse{SessionID: sessionID, Current: current})
}

// UpdateStream 以 SSE 流式返回指定 session 的更新日志。
func (h *Handler) UpdateStream(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	ch, done, err := h.svc.Subscribe(sessionID)
	if err != nil {
		if errors.Is(err, ErrTooManySubscribers) {
			common.WriteError(w, r, http.StatusConflict, "too_many_subscribers", err.Error(), nil)
			return
		}
		common.WriteError(w, r, http.StatusNotFound, "session_not_found", err.Error(), nil)
		return
	}
	defer done()

	flusher, ok := w.(http.Flusher)
	if !ok {
		common.WriteError(w, r, http.StatusInternalServerError, "stream_unsupported", "流式输出不受支持", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	idle := 0
	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return // channel closed = session done / unsubscribe
			}
			idle = 0
			payload, _ := json.Marshal(evt)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", evt.Seq, payload)
			flusher.Flush()
		case <-ticker.C:
			idle++
			if idle%150 == 0 { // ~15s
				w.Write([]byte(": keepalive\n\n"))
				flusher.Flush()
			}
		}
	}
}
