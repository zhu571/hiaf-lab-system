package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/runs"
)

// registerRunRoutes 注册 experiment-runs 与 run-steps 路由。
func registerRunRoutes(r chi.Router, db *sql.DB, runsHandler *runs.Handler) {
	r.Route("/api/v1/experiment-runs", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", runsHandler.GetByID)
			r.Patch("/", runsHandler.Update)
			r.Delete("/", runsHandler.SoftDelete)
			r.Post("/daily-reports/{report_id}", runsHandler.AddReportLink)
			r.Delete("/daily-reports/{report_id}", runsHandler.RemoveReportLink)
			r.Get("/steps", runsHandler.HandleListSteps)
			r.Post("/steps", runsHandler.HandleCreateStep)
			r.Post("/steps/apply-template", runsHandler.HandleApplyTemplate)
		})
	})
	r.Route("/api/v1/run-steps", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Post("/reorder", runsHandler.HandleReorderSteps)
		r.Route("/{id}", func(r chi.Router) {
			r.Patch("/", runsHandler.HandleUpdateStep)
			r.Delete("/", runsHandler.HandleDeleteStep)
		})
	})
}
