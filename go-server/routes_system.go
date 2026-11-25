package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/system"
)

// registerSystemAdminRoutes 注册系统更新（admin only）：版本查询/触发更新/SSE 日志流。
func registerSystemAdminRoutes(r chi.Router, db *sql.DB, systemHandler *system.Handler) {
	r.Route("/api/v1/admin/system", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.RequireRole(auth.RoleAdmin))

		// 版本查询 — 只读，无审计/幂等
		r.Get("/version", systemHandler.GetVersion)

		// 触发更新 — 写操作，需审计+幂等
		r.Group(func(r chi.Router) {
			r.Use(middleware.Audit(db))
			r.Use(middleware.RequireIdempotencyKey(db))
			r.Post("/update", systemHandler.TriggerUpdate)
		})

		// SSE 日志流 — 流式，无审计/幂等
		r.Get("/update/stream/{sessionId}", systemHandler.UpdateStream)
	})
}
