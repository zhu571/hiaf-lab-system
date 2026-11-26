package main

import (
	"database/sql"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/steptemplates"
)

// registerStepTemplateRoutes 注册步骤模板 CRUD 与 AI 生成。
func registerStepTemplateRoutes(r chi.Router, db *sql.DB, templatesHandler *steptemplates.Handler) {
	r.Route("/api/v1/step-templates", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", templatesHandler.List)
		r.Post("/", templatesHandler.Create)
		r.With(middleware.Timeout(90*time.Second)).Post("/generate", templatesHandler.Generate)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", templatesHandler.GetByID)
			r.Patch("/", templatesHandler.Update)
			r.Delete("/", templatesHandler.SoftDelete)
			r.Patch("/items", templatesHandler.ReplaceItems)
		})
	})
}
