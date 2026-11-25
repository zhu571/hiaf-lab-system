package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerLogRoutes 注册日志详情/关联日报/更新/翻译。
func registerLogRoutes(r chi.Router, db *sql.DB, logsHandler *logs.Handler) {
	r.Route("/api/v1/logs", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/mine", logsHandler.ListMine)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", logsHandler.GetLog)
			r.Get("/daily-reports", logsHandler.GetReportsByLog)
			r.Patch("/", logsHandler.UpdateLog)
			r.Post("/translations", logsHandler.Translation)
			r.Patch("/translations", logsHandler.Translation)
		})
	})
}
