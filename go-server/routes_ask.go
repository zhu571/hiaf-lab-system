package main

import (
	"database/sql"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/ask"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerAskRoutes 注册 AI 问答 chat/history 与内部 execute。
func registerAskRoutes(r chi.Router, db *sql.DB, askHandler *ask.Handler) {
	r.Route("/api/v1/ask", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthRequired)
			r.Use(middleware.Audit(db))
			r.Use(middleware.RequireIdempotencyKey(db))
			r.Use(middleware.Timeout(90 * time.Second))
			r.Post("/chat", askHandler.Chat)
			r.Get("/history", askHandler.History)
			r.Get("/history/{id}", askHandler.HistoryByID)
		})
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthRequired)
			r.Use(middleware.Audit(db))
			r.Post("/execute", askHandler.Execute)
		})
	})
}
