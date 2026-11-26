package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/internal/config"
)

// runHTTPServer 启动 HTTP server，并在 ctx 取消后 10s 内优雅排空。
func runHTTPServer(ctx context.Context, handler http.Handler, cfg *config.Config, port string) error {
	srv := &http.Server{Addr: ":" + port, Handler: handler}
	if cfg.Features.RequestTimeouts {
		srv.ReadHeaderTimeout = cfg.HTTP.ReadHeaderTimeout
		srv.IdleTimeout = cfg.HTTP.IdleTimeout
		srv.MaxHeaderBytes = 1 << 20
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("http server shutdown", "error", err)
		}
	}()
	slog.Info("server starting", "port", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
