package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/issues"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerIssueRoutes 注册 issue 列表/详情/状态流转/评论。
func registerIssueRoutes(r chi.Router, db *sql.DB, issuesHandler *issues.Handler) {
	r.Route("/api/v1/issues", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", issuesHandler.ListByLog)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", issuesHandler.GetByID)
			r.Patch("/", issuesHandler.Update)
			r.Post("/transition", issuesHandler.Transition)
			r.Post("/comments", issuesHandler.AddComment)
		})
	})
}
