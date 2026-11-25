package main

import (
	"database/sql"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/experiences"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerExperienceRoutes 注册经验库 CRUD、候选提取、发布/归档。
func registerExperienceRoutes(r chi.Router, db *sql.DB, experiencesHandler *experiences.Handler) {
	r.Route("/api/v1/experiences", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", experiencesHandler.List)
		r.Post("/", experiencesHandler.Create)
		r.Post("/candidates", experiencesHandler.Create)
		// AI-2 经验候选提取：maintainer+ 手动触发（对齐 /api/v1/weekly/summary 角色门槛）。
		r.With(middleware.RequireRole(auth.RoleAdmin, auth.RoleMaintainer), middleware.Timeout(360*time.Second)).
			Post("/extract-candidates", experiencesHandler.ExtractCandidates)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", experiencesHandler.GetByID)
			r.Patch("/", experiencesHandler.Update)
			r.Post("/publish", experiencesHandler.Publish)
			r.Post("/archive", experiencesHandler.Archive)
		})
	})
}
