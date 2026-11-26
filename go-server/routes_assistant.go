package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/assistant"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerAssistantRoutes 注册多人助手私有会话/消息（项目共享会话后续开放）。
func registerAssistantRoutes(r chi.Router, db *sql.DB, assistantHandler *assistant.Handler) {
	r.Route("/api/v1/assistant", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.Audit(db))
		r.Route("/sessions", func(r chi.Router) {
			r.Get("/", assistantHandler.List)
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireIdempotencyKey(db))
				r.Post("/", assistantHandler.Create)
			})
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", assistantHandler.Get)
				r.Get("/messages", assistantHandler.Messages)
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireIdempotencyKey(db))
					r.Patch("/", assistantHandler.Update)
					r.Delete("/", assistantHandler.Archive)
					r.Post("/messages", assistantHandler.AddMessage)
				})
			})
		})
	})
}
