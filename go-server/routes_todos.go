package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/todos"
)

// registerTodoRoutes 注册待办 CRUD、LLM 解析/添加与通知主题。
func registerTodoRoutes(r chi.Router, db *sql.DB, todosHandler *todos.Handler) {
	r.Route("/api/v1/todos", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", todosHandler.List)
		r.Post("/", todosHandler.Create)
		r.Post("/llm-parse", todosHandler.ParseLLM)
		r.Post("/llm-add", todosHandler.LLMAdd)
		r.Get("/notification-topic", todosHandler.NotificationTopic)
		r.Post("/notification-topic/provision", todosHandler.Provision)
		r.Post("/notification-topic/redeem", todosHandler.Redeem)
		r.Route("/{id}", func(r chi.Router) {
			r.Patch("/", todosHandler.Edit)
			r.Patch("/done", todosHandler.Done)
			r.Patch("/defer", todosHandler.Defer)
			r.Delete("/", todosHandler.Delete)
		})
	})
}
