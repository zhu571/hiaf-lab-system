package main

import (
	"database/sql"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerDailyReportRoutes 注册日报列表/today/by-date/详情/提交/AI 解析/翻译。
func registerDailyReportRoutes(r chi.Router, db *sql.DB, logsHandler *logs.Handler) {
	r.Route("/api/v1/daily-reports", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", logsHandler.ListReports)
		r.Post("/today", logsHandler.GetOrCreateTodayReport)
		r.Get("/by-date", logsHandler.GetReportByDate)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", logsHandler.GetReportByID)
			r.Patch("/", logsHandler.UpdateReportRawText)
			r.Post("/submit", logsHandler.SubmitReport)
			r.With(middleware.Timeout(90*time.Second)).Post("/ai-parse", logsHandler.AiParseReport)
			r.Post("/translations", logsHandler.Translation)
			r.Patch("/translations", logsHandler.Translation)
			r.Post("/translations/", logsHandler.Translation)
			r.Patch("/translations/", logsHandler.Translation)
		})
	})
}
